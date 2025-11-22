package generators

import (
	"context"
	"fmt"
	"strings"

	"github.com/reusee/tai/vars"
)

type Generator interface {
	Args() GeneratorArgs
	CountTokens(string) (int, error)
	Generate(ctx context.Context, state State) (State, error)
}

type GetGenerator func(name string) (Generator, error)

func (Module) GetGenerator(
	newGemini NewGemini,
	newHuoshan NewHuoshan,
	newBaidu NewBaidu,
	newDeepseek NewDeepseek,
	newOpenRouter NewOpenRouter,
	newTencent NewTencent,
	newOpenAI NewOpenAI,
	newAliyn NewAliyun,
	getSpecs GetGeneratorSpecs,
	newZhipu NewZhipu,
	newVercel NewVercel,
) GetGenerator {
	return func(name string) (Generator, error) {

		// user-defined first
		specs, err := getSpecs()
		if err != nil {
			return nil, err
		}
		for _, spec := range specs {
			if spec.Name != name {
				continue
			}
			switch strings.ToLower(spec.Type) {
			case "open-router", "open_router", "openrouter":
				return newOpenRouter(spec.GeneratorArgs), nil
			case "deepseek":
				return newDeepseek(spec.GeneratorArgs), nil
			case "baidu":
				return newBaidu(spec.GeneratorArgs), nil
			case "tencent":
				return newTencent(spec.GeneratorArgs), nil
			case "openai", "open-ai", "open_ai":
				return newOpenAI(spec.GeneratorArgs, spec.APIKey), nil
			case "huoshan":
				return newHuoshan(spec.GeneratorArgs), nil
			case "gemini":
				return newGemini(spec.GeneratorArgs), nil
			case "aliyun":
				return newAliyn(spec.GeneratorArgs), nil
			case "zhipu":
				return newZhipu(spec.GeneratorArgs), nil
			case "ollama":
				spec.GeneratorArgs.BaseURL = "http://127.0.0.1:11434/v1"
				return newOpenAI(spec.GeneratorArgs, ""), nil
			case "vercel":
				return newVercel(spec.GeneratorArgs), nil
			default:
				return nil, fmt.Errorf("unknown generator type: %q", spec.Type)
			}
		}

		// ollama
		provider, modelName, ok := strings.Cut(name, ":")
		if ok && provider == "ollama" {
			return newOpenAI(GeneratorArgs{
				BaseURL:       "http://127.0.0.1:11434/v1",
				Model:         modelName,
				DisableSearch: true,
			}, ""), nil
		}

		// built-ins
		switch name {

		case "flash", "gemini-flash":
			return newGemini(GeneratorArgs{
				Model:             "models/gemini-flash-latest",
				ContextTokens:     192 * K,
				MaxGenerateTokens: vars.PtrTo(32 * K),
				Temperature:       vars.PtrTo(float32(0.1)),
			}), nil

		case "pro", "gemini-pro":
			return newGemini(GeneratorArgs{
				Model:             "models/gemini-pro-latest",
				ContextTokens:     192 * K,
				MaxGenerateTokens: vars.PtrTo(32 * K),
				Temperature:       vars.PtrTo(float32(0.1)),
			}), nil

		}

		return nil, fmt.Errorf("invalid model: %s", name)
	}
}
