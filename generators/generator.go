package generators

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/reusee/tai/logs"
)

const (
	TheoryOfModelResolution = `
Model resolution resolves one name to one generator spec. The name is tried
in order: a configured generator path (aliases expanded, redirects followed),
the ollama shorthand ollama:<model>, and the built-in shortcuts (flash,
gemini). A generator path may be absolute: a leading "/" is stripped at the
entry, so "/foo/bar" resolves from the root exactly like "foo/bar" — the
same syntax an absolute Redirect accepts. A name that matches none of them
is a configuration fault, and its
failure report must carry everything needed to repair it without re-running
the process:

- the reason: the immediate cause — an empty name, a path component no spec
  defines, a redirect cycle, or an unknown generator type;
- the process: how the name was resolved — every alias expanded, every
  redirect followed, the path prefix that matched, and the names available
  at the failing level;
- the configured generator names with their aliases, so the user sees the
  names the configuration actually offers;
- a hint naming the ways to set a usable name.

The report is a ModelResolutionError rendered as multiple lines; the first
line keeps the invalid-model framing so existing diagnostics and log searches
keep working. Name lists are sorted and capped: the report stays readable and
byte-identical for equal configurations.
`

	// knownGeneratorTypes lists the generator type values the resolver
	// accepts, mirroring the type switch in Module.GetGenerator. An unknown
	// type is reported with this list so the configuration can be fixed
	// without guessing.
	knownGeneratorTypes = "openai, openrouter, deepseek, baidu, tencent, huoshan, gemini, aliyun, zhipu, ollama, vercel, nvidia, azure, bedrock, opencode-go"

	// maxReportedNames caps a name list in a resolution failure report so a
	// large configuration cannot flood the message.
	maxReportedNames = 12

	// modelHint tells the user how to set a usable model name: the flag and
	// the config paths the name is read from, and the accepted name forms.
	modelHint = `set the model with the -model flag or the "model" (or "model_name") config path; an accepted name is a configured generator, the built-in flash or gemini, or the ollama shorthand ollama:<model>`
)

type Generator interface {
	Spec() Spec
	CountTokens(string) (int, error)
	Generate(ctx context.Context, state State, options *GenerateOptions) (State, error)
}

type GenerateOptions struct {
	MaxGenerateTokens *int
	ResponseSchema    *Var
	NonStreaming      bool
}

// ModelResolutionError reports that a model name resolved to no generator.
// It carries the detailed reason, the resolution process that led to the
// failure, and an actionable hint, so a misconfigured name is diagnosable
// from the error text alone. See TheoryOfModelResolution.
type ModelResolutionError struct {
	Model   string
	Reason  string
	Process []string
	Hint    string
}

func (e *ModelResolutionError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invalid model: %q", e.Model)
	if e.Reason != "" {
		fmt.Fprintf(&b, "\nreason: %s", e.Reason)
	}
	if len(e.Process) > 0 {
		b.WriteString("\nprocess:")
		for _, line := range e.Process {
			b.WriteString("\n  - ")
			b.WriteString(line)
		}
	}
	if e.Hint != "" {
		fmt.Fprintf(&b, "\nhint: %s", e.Hint)
	}
	return b.String()
}

type GetGenerator func(name string) (Generator, error)

// formatNameList renders candidate names for a failure report: sorted,
// deduplicated, and capped so the report stays readable. An empty list
// reads "none". See TheoryOfModelResolution.
func formatNameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) > maxReportedNames {
		more := len(names) - maxReportedNames
		return strings.Join(names[:maxReportedNames], ", ") + fmt.Sprintf(", ... and %d more", more)
	}
	return strings.Join(names, ", ")
}

func resolveSpec(name string, roots []Spec) (Spec, error) {
	requested := name
	// A leading "/" marks an absolute path resolved from the spec root,
	// the same syntax an absolute Redirect accepts, so "/foo/bar"
	// resolves like "foo/bar".
	name = strings.TrimPrefix(name, "/")
	visited := make(map[string]bool)
	var process []string

	for {
		// build alias map
		aliasMap := make(map[string]string)
		var collectAliases func(spec Spec, prefix string)
		collectAliases = func(spec Spec, prefix string) {
			fullPath := spec.Name
			if prefix != "" {
				fullPath = prefix + "/" + spec.Name
			}
			for _, alias := range spec.Aliases {
				if alias != "" {
					aliasMap[alias] = fullPath
				}
			}
			for _, child := range spec.Variants {
				collectAliases(child, fullPath)
			}
		}
		for _, root := range roots {
			collectAliases(root, "")
		}

		// resolve alias
		if target, ok := aliasMap[name]; ok {
			process = append(process, fmt.Sprintf("alias: %q -> %q", name, target))
			name = target
		}

		if visited[name] {
			return Spec{}, &ModelResolutionError{
				Model:   requested,
				Reason:  fmt.Sprintf("redirect cycle detected: %q was already resolved", name),
				Process: process,
			}
		}
		visited[name] = true

		if name == "" {
			return Spec{}, &ModelResolutionError{
				Model:   requested,
				Reason:  "empty model name",
				Process: process,
			}
		}

		parts := strings.Split(name, "/")

		// build root map
		rootMap := make(map[string]Spec)
		for _, root := range roots {
			if root.Name != "" {
				rootMap[root.Name] = root
			}
			for _, alias := range root.Aliases {
				if alias != "" {
					rootMap[alias] = root
				}
			}
		}

		// traverse and merge
		merged := Spec{}
		currentMap := rootMap
		var lastRedirect string
		var lastRandomRedirects []string
		var resolvedPrefix string

		for _, part := range parts {
			spec, ok := currentMap[part]
			if !ok {
				where := "the root level"
				if resolvedPrefix != "" {
					where = fmt.Sprintf("the level of %q", resolvedPrefix)
				}
				failure := &ModelResolutionError{
					Model:   requested,
					Reason:  fmt.Sprintf("spec not found: path component %q of %q is not defined at %s", part, name, where),
					Process: process,
				}
				if resolvedPrefix != "" {
					failure.Process = append(failure.Process,
						"available at that level: "+formatNameList(slices.Sorted(maps.Keys(currentMap))))
				}
				return Spec{}, failure
			}
			if resolvedPrefix == "" {
				resolvedPrefix = part
			} else {
				resolvedPrefix += "/" + part
			}
			// merge fields
			if spec.Type != "" {
				merged.Type = spec.Type
			}
			if spec.BaseURL != "" {
				merged.BaseURL = spec.BaseURL
			}
			if spec.APIKey != "" {
				merged.APIKey = spec.APIKey
			}
			if spec.Model != "" {
				merged.Model = spec.Model
			}
			if spec.Family != "" {
				merged.Family = spec.Family
			}
			if spec.ContextTokens != 0 {
				merged.ContextTokens = spec.ContextTokens
			}
			if spec.MaxGenerateTokens != nil {
				merged.MaxGenerateTokens = spec.MaxGenerateTokens
			}
			if spec.MaxThinkingTokens != nil {
				merged.MaxThinkingTokens = spec.MaxThinkingTokens
			}
			if spec.Temperature != nil {
				merged.Temperature = spec.Temperature
			}
			if spec.DisableSearch != nil {
				merged.DisableSearch = spec.DisableSearch
			}
			if spec.DisableTools != nil {
				merged.DisableTools = spec.DisableTools
			}
			if spec.ExtraArguments != nil {
				merged.ExtraArguments = spec.ExtraArguments
			}
			if spec.IsOpenRouter != nil {
				merged.IsOpenRouter = spec.IsOpenRouter
			}
			if spec.APIVersion != "" {
				merged.APIVersion = spec.APIVersion
			}
			if spec.IsAzure != nil {
				merged.IsAzure = spec.IsAzure
			}
			if spec.ServiceTier != "" {
				merged.ServiceTier = spec.ServiceTier
			}
			if spec.ReasoningEffort != "" {
				merged.ReasoningEffort = spec.ReasoningEffort
			}
			if spec.NoProxy != nil {
				merged.NoProxy = spec.NoProxy
			}
			if spec.PreservedThinking != nil {
				merged.PreservedThinking = spec.PreservedThinking
			}
			if spec.ZeroDataRetention != nil {
				merged.ZeroDataRetention = spec.ZeroDataRetention
			}
			if spec.Provider != nil {
				merged.Provider = merged.Provider.merge(spec.Provider)
			}
			// Redirect and RandomRedirect are not merged from parent to
			// child; only the final spec in the path determines whether a
			// redirect applies.
			lastRedirect = spec.Redirect
			lastRandomRedirects = spec.RandomRedirect
			// descend into variants
			nextMap := make(map[string]Spec, len(spec.Variants))
			for _, v := range spec.Variants {
				if v.Name != "" {
					nextMap[v.Name] = v
				}
				for _, alias := range v.Aliases {
					if alias != "" {
						nextMap[alias] = v
					}
				}
			}
			currentMap = nextMap
		}

		merged.Name = name

		// Handle redirect: if the last spec in the path has a Redirect
		// field, re-resolve with the redirected path. A relative redirect
		// (e.g., "child") is appended to the current path as additional
		// components. An absolute redirect starting with "/" (e.g.,
		// "/foo/bar") resolves from the root, replacing the current path.
		if lastRedirect != "" {
			target := lastRedirect
			if strings.HasPrefix(target, "/") {
				target = target[1:]
			} else {
				target = name + "/" + target
			}
			process = append(process, fmt.Sprintf("redirect: %q -> %q", name, target))
			name = target
			continue
		}

		// Handle random redirect: if the last spec in the path has a
		// RandomRedirect field (and Redirect is not set), randomly pick
		// one entry and apply it as a redirect. This enables load
		// balancing across multiple endpoints. Entries follow the same
		// resolution rules as Redirect: relative paths append to the
		// current path, absolute paths starting with "/" replace it.
		if len(lastRandomRedirects) > 0 {
			picked := lastRandomRedirects[rand.IntN(len(lastRandomRedirects))]
			target := picked
			if strings.HasPrefix(target, "/") {
				target = target[1:]
			} else {
				target = name + "/" + target
			}
			process = append(process, fmt.Sprintf("random redirect: %q -> %q", name, target))
			name = target
			continue
		}

		return merged, nil
	}
}

// invalidModelError builds the failure report for a name that resolved to no
// generator: the reason and the resolution steps of the failed attempt, the
// configured generator names, and the hint. See TheoryOfModelResolution.
func invalidModelError(name, reason string, steps []string, hint string, specs []Spec) *ModelResolutionError {
	var names []string
	for _, spec := range specs {
		if spec.Name == "" {
			continue
		}
		entry := spec.Name
		var aliases []string
		for _, alias := range spec.Aliases {
			if alias != "" {
				aliases = append(aliases, alias)
			}
		}
		if len(aliases) > 0 {
			entry += " (aliases: " + strings.Join(aliases, ", ") + ")"
		}
		names = append(names, entry)
	}
	process := slices.Clone(steps)
	process = append(process, "configured generators: "+formatNameList(names))
	return &ModelResolutionError{
		Model:   name,
		Reason:  reason,
		Process: process,
		Hint:    hint,
	}
}

func (Module) GetGenerator(
	newGemini NewGemini,
	newHuoshan NewHuoshan,
	newBaidu NewBaidu,
	newDeepseek NewDeepseek,
	newOpenRouter NewOpenRouter,
	newTencent NewTencent,
	newOpenAI NewOpenAI,
	newAliyun NewAliyun,
	getSpecs GetGeneratorSpecs,
	newZhipu NewZhipu,
	newVercel NewVercel,
	newNvidia NewNvidia,
	newAzure NewAzure,
	newBedrock NewBedrock,
	newOpenCodeGo NewOpenCodeGo,
	confidential ConfidentialMode,
	logger logs.Logger,
) GetGenerator {
	return func(name string) (Generator, error) {

		logger.InfoContext(context.Background(), "resolving generator",
			"name", name,
			"confidential", bool(confidential),
		)

		// user-defined first
		specs, err := getSpecs()
		if err != nil {
			logger.WarnContext(context.Background(), "loading generator specs failed",
				"name", name,
				"error", err,
			)
			return nil, err
		}
		logger.InfoContext(context.Background(), "generator specs available",
			"name", name,
			"count", len(specs),
		)
		var resolveErr error
		if resolvedSpec, err := resolveSpec(name, specs); err == nil {
			logger.InfoContext(context.Background(), "generator spec resolved",
				"name", name,
				"generator", resolvedSpec.Name,
				"type", resolvedSpec.Type,
				"model", resolvedSpec.Model,
				"base_url", resolvedSpec.BaseURL,
				"context_tokens", resolvedSpec.ContextTokens,
				"family", resolvedSpec.Family,
			)
			if err := confidential.check(resolvedSpec, name); err != nil {
				logger.WarnContext(context.Background(), "confidential mode rejected generator",
					"name", name,
					"generator", resolvedSpec.Name,
					"error", err,
				)
				return nil, err
			}
			logger.InfoContext(context.Background(), "creating generator",
				"name", name,
				"generator", resolvedSpec.Name,
				"type", strings.ToLower(resolvedSpec.Type),
			)
			switch strings.ToLower(resolvedSpec.Type) {
			case "open-router", "open_router", "openrouter":
				return newOpenRouter(resolvedSpec), nil
			case "deepseek":
				return newDeepseek(resolvedSpec), nil
			case "baidu":
				return newBaidu(resolvedSpec), nil
			case "tencent":
				return newTencent(resolvedSpec), nil
			case "openai", "open-ai", "open_ai":
				return newOpenAI(resolvedSpec, resolvedSpec.APIKey), nil
			case "huoshan":
				return newHuoshan(resolvedSpec), nil
			case "gemini":
				return newGemini(resolvedSpec), nil
			case "aliyun":
				return newAliyun(resolvedSpec), nil
			case "zhipu":
				return newZhipu(resolvedSpec), nil
			case "ollama":
				if resolvedSpec.BaseURL == "" {
					resolvedSpec.BaseURL = "http://127.0.0.1:11434/v1"
				}
				return newOpenAI(resolvedSpec, ""), nil
			case "vercel":
				return newVercel(resolvedSpec), nil
			case "nvidia":
				return newNvidia(resolvedSpec), nil
			case "azure":
				return newAzure(resolvedSpec), nil
			case "bedrock":
				return newBedrock(resolvedSpec), nil
			case "opencode-go", "opencode_go", "opencodego":
				return newOpenCodeGo(resolvedSpec), nil
			default:
				reason := fmt.Sprintf("the resolved generator %q has unknown type %q", resolvedSpec.Name, resolvedSpec.Type)
				if resolvedSpec.Type == "" {
					reason = fmt.Sprintf("the resolved generator %q sets no type", resolvedSpec.Name)
				}
				logger.WarnContext(context.Background(), "unknown generator type",
					"name", name,
					"generator", resolvedSpec.Name,
					"type", resolvedSpec.Type,
				)
				return nil, invalidModelError(name, reason, nil,
					"set the generator's type in the generators config to one of: "+knownGeneratorTypes, specs)
			}
		} else {
			resolveErr = err
			logger.InfoContext(context.Background(), "no configured generator matched",
				"name", name,
				"error", err,
			)
		}

		// ollama
		provider, modelName, ok := strings.Cut(name, ":")
		if ok && provider == "ollama" {
			logger.InfoContext(context.Background(), "using ollama shorthand",
				"name", name,
				"model", modelName,
			)
			spec := Spec{
				BaseURL:       "http://127.0.0.1:11434/v1",
				Model:         modelName,
				DisableSearch: new(true),
			}
			if err := confidential.check(spec, name); err != nil {
				logger.WarnContext(context.Background(), "confidential mode rejected generator",
					"name", name,
					"error", err,
				)
				return nil, err
			}
			return newOpenAI(spec, ""), nil
		}

		// built-ins
		switch name {

		case "flash", "gemini-flash":
			spec := Spec{
				Model:             "models/gemini-flash-latest",
				ContextTokens:     192 * K,
				MaxGenerateTokens: new(32 * K),
				Temperature:       new(float32(0.1)),
			}
			if err := confidential.check(spec, name); err != nil {
				logger.WarnContext(context.Background(), "confidential mode rejected generator",
					"name", name,
					"error", err,
				)
				return nil, err
			}
			logger.InfoContext(context.Background(), "using built-in generator",
				"name", name,
				"model", spec.Model,
			)
			return newGemini(spec), nil

		case "gemini", "pro", "gemini-pro":
			spec := Spec{
				Model:             "models/gemini-pro-latest",
				ContextTokens:     192 * K,
				MaxGenerateTokens: new(32 * K),
				Temperature:       new(float32(0.1)),
			}
			if err := confidential.check(spec, name); err != nil {
				logger.WarnContext(context.Background(), "confidential mode rejected generator",
					"name", name,
					"error", err,
				)
				return nil, err
			}
			logger.InfoContext(context.Background(), "using built-in generator",
				"name", name,
				"model", spec.Model,
			)
			return newGemini(spec), nil

		}

		reason := "the name resolved to no generator"
		var steps []string
		if detail, ok := resolveErr.(*ModelResolutionError); ok {
			reason = detail.Reason
			steps = detail.Process
		} else if resolveErr != nil {
			reason = "resolving the configured generators failed: " + resolveErr.Error()
		}
		logger.WarnContext(context.Background(), "generator resolution failed",
			"name", name,
			"reason", reason,
		)
		return nil, invalidModelError(name, reason, steps, modelHint, specs)
	}
}
