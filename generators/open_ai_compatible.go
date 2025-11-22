package generators

import (
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
)

type NewOpenRouter func(args GeneratorArgs) *OpenAI

func (Module) NewOpenRouter(
	newOpenAI NewOpenAI,
	apiKey OpenRouterAPIKey,
	loader configs.Loader,
) NewOpenRouter {
	return func(args GeneratorArgs) *OpenAI {
		if endpoint := configs.First[string](loader, "openrouter_endpoint"); endpoint != "" {
			args.BaseURL = endpoint
		} else {
			args.BaseURL = "https://openrouter.ai/api/v1"
		}
		args.IsOpenRouter = true
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewDeepseek func(args GeneratorArgs) *OpenAI

func (Module) NewDeepseek(
	apiKey DeepseekAPIKey,
	newOpenAI NewOpenAI,
) NewDeepseek {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://api.deepseek.com/"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewBaidu func(args GeneratorArgs) *OpenAI

func (Module) NewBaidu(
	apiKey BaiduAPIKey,
	newOpenAI NewOpenAI,
) NewBaidu {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://qianfan.baidubce.com/v2/"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewTencent func(args GeneratorArgs) *OpenAI

func (Module) NewTencent(
	apiKey TencentAPIKey,
	newOpenAI NewOpenAI,
) NewTencent {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://api.hunyuan.cloud.tencent.com/v1"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewHuoshan func(args GeneratorArgs) *OpenAI

func (Module) NewHuoshan(
	apiKey HuoshanAPIKey,
	newOpenAI NewOpenAI,
) NewHuoshan {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewAliyun func(args GeneratorArgs) *OpenAI

func (Module) NewAliyun(
	apiKey AliyunAPIKey,
	newOpenAI NewOpenAI,
) NewAliyun {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewZhipu func(args GeneratorArgs) *OpenAI

func (Module) NewZhipu(
	apiKey ZhipuAPIKey,
	newOpenAI NewOpenAI,
) NewZhipu {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://open.bigmodel.cn/api/paas/v4/"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewVercel func(args GeneratorArgs) *OpenAI

func (Module) NewVercel(
	apiKey VercelAPIKey,
	newOpenAI NewOpenAI,
) NewVercel {
	return func(args GeneratorArgs) *OpenAI {
		args.BaseURL = "https://ai-gateway.vercel.sh/v1/"
		return newOpenAI(
			args,
			vars.FirstNonZero(
				args.APIKey,
				string(apiKey),
			),
		)
	}
}
