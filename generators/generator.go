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

func resolveSpec(name string, specs []Spec) (Spec, error) {
	specMap := make(map[string]Spec)
	aliasMap := make(map[string]string)
	for _, s := range specs {
		if s.Name != "" {
			specMap[s.Name] = s
		}
		for _, alias := range s.Aliases {
			if alias != "" {
				aliasMap[alias] = s.Name
			}
		}
	}

	// resolve alias
	if target, ok := aliasMap[name]; ok {
		name = target
	}

	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return Spec{}, fmt.Errorf("empty name")
	}

	// collect chain
	var chain []Spec
	for i := 1; i <= len(parts); i++ {
		prefix := strings.Join(parts[:i], "/")
		if s, ok := specMap[prefix]; ok {
			chain = append(chain, s)
		}
	}
	if len(chain) == 0 {
		return Spec{}, fmt.Errorf("spec not found: %s", name)
	}

	// merge
	merged := chain[0]
	for _, ov := range chain[1:] {
		if ov.Type != "" {
			merged.Type = ov.Type
		}
		if ov.BaseURL != "" {
			merged.BaseURL = ov.BaseURL
		}
		if ov.APIKey != "" {
			merged.APIKey = ov.APIKey
		}
		if ov.Model != "" {
			merged.Model = ov.Model
		}
		if ov.ContextTokens != 0 {
			merged.ContextTokens = ov.ContextTokens
		}
		if ov.MaxGenerateTokens != nil {
			merged.MaxGenerateTokens = ov.MaxGenerateTokens
		}
		if ov.Temperature != nil {
			merged.Temperature = ov.Temperature
		}
		if ov.DisableSearch != nil {
			merged.DisableSearch = ov.DisableSearch
		}
		if ov.DisableTools != nil {
			merged.DisableTools = ov.DisableTools
		}
		if ov.ExtraArguments != nil {
			merged.ExtraArguments = ov.ExtraArguments
		}
		if ov.IsOpenRouter != nil {
			merged.IsOpenRouter = ov.IsOpenRouter
		}
		if ov.APIVersion != "" {
			merged.APIVersion = ov.APIVersion
		}
		if ov.IsAzure != nil {
			merged.IsAzure = ov.IsAzure
		}
		if ov.ServiceTier != "" {
			merged.ServiceTier = ov.ServiceTier
		}
		if ov.ReasoningEffort != "" {
			merged.ReasoningEffort = ov.ReasoningEffort
		}
		if ov.NoProxy != nil {
			merged.NoProxy = ov.NoProxy
		}
	}
	merged.Name = name

	return merged, nil
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

