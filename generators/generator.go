package generators

import (
	"context"
	"fmt"
	"strings"
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

type GetGenerator func(name string) (Generator, error)

func resolveSpec(name string, roots []Spec) (Spec, error) {
	visited := make(map[string]bool)

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
			name = target
		}

		if visited[name] {
			return Spec{}, fmt.Errorf("redirect cycle detected: %s", name)
		}
		visited[name] = true

		parts := strings.Split(name, "/")
		if len(parts) == 0 || name == "" {
			return Spec{}, fmt.Errorf("empty name")
		}

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

		for _, part := range parts {
			spec, ok := currentMap[part]
			if !ok {
				return Spec{}, fmt.Errorf("spec not found: %s", name)
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
			// Redirect is not merged from parent to child; only the
			// final spec in the path determines whether a redirect applies.
			lastRedirect = spec.Redirect
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
			if strings.HasPrefix(lastRedirect, "/") {
				name = lastRedirect[1:]
			} else {
				name = name + "/" + lastRedirect
			}
			continue
		}

		return merged, nil
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
) GetGenerator {
	return func(name string) (Generator, error) {

		// user-defined first
		specs, err := getSpecs()
		if err != nil {
			return nil, err
		}
		if resolvedSpec, err := resolveSpec(name, specs); err == nil {
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
				return nil, fmt.Errorf("unknown generator type: %q", resolvedSpec.Type)
			}
		}

		// ollama
		provider, modelName, ok := strings.Cut(name, ":")
		if ok && provider == "ollama" {
			return newOpenAI(Spec{
				BaseURL:       "http://127.0.0.1:11434/v1",
				Model:         modelName,
				DisableSearch: new(true),
			}, ""), nil
		}

		// built-ins
		switch name {

		case "flash", "gemini-flash":
			return newGemini(Spec{
				Model:             "models/gemini-flash-latest",
				ContextTokens:     192 * K,
				MaxGenerateTokens: new(32 * K),
				Temperature:       new(float32(0.1)),
			}), nil

		case "gemini", "pro", "gemini-pro":
			return newGemini(Spec{
				Model:             "models/gemini-pro-latest",
				ContextTokens:     192 * K,
				MaxGenerateTokens: new(32 * K),
				Temperature:       new(float32(0.1)),
			}), nil

		}

		return nil, fmt.Errorf("invalid model: %s", name)
	}
}
