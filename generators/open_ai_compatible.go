package generators

import (
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
)

type OpenRouterEndpoint string

func (Module) AzureEndpoint(
	loader configs.Loader,
) AzureEndpoint {
	return configs.First[AzureEndpoint](loader, "azure_endpoint")
}

type AzureAPIVersion string

var _ configs.Configurable = AzureAPIVersion("")

func (a AzureAPIVersion) TaigoConfigurable() {
}

func (Module) AzureAPIVersion(
	loader configs.Loader,
) AzureAPIVersion {
	if version := configs.First[AzureAPIVersion](loader, "azure_api_version"); version != "" {
		return version
	}
	return "2024-05-01-preview"
}

func (o OpenRouterEndpoint) TaigoConfigurable() {
	panic("unimplemented")
}

type AzureEndpoint string

func (a AzureEndpoint) TaigoConfigurable() {
}

func (Module) OpenRouterEndpoint(
	loader configs.Loader,
) OpenRouterEndpoint {
	if endpoint := configs.First[OpenRouterEndpoint](loader, "openrouter_endpoint"); endpoint != "" {
		return endpoint
	}
	return "https://openrouter.ai/api/v1"
}

type NewOpenRouter func(sepc Spec) *OpenAI

type NewAzure func(spec Spec) *OpenAI

func (Module) NewAzure(
	newOpenAI NewOpenAI,
	apiKey AzureAPIKey,
	endpoint AzureEndpoint,
	apiVersion AzureAPIVersion,
) NewAzure {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = string(endpoint)
		}
		if spec.APIVersion == "" {
			spec.APIVersion = string(apiVersion)
		}
		spec.IsAzure = new(true)
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

func (Module) NewOpenRouter(
	newOpenAI NewOpenAI,
	apiKey OpenRouterAPIKey,
	endpoint OpenRouterEndpoint,
) NewOpenRouter {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = string(endpoint)
		}
		spec.IsOpenRouter = new(true)
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewDeepseek func(spec Spec) *OpenAI

func (Module) NewDeepseek(
	apiKey DeepseekAPIKey,
	newOpenAI NewOpenAI,
) NewDeepseek {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://api.deepseek.com/"
		}
		ret := newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
		ret.TokenCounterOverride = DeepseekTokenCounterFn
		return ret
	}
}

type NewBaidu func(spec Spec) *OpenAI

func (Module) NewBaidu(
	apiKey BaiduAPIKey,
	newOpenAI NewOpenAI,
) NewBaidu {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://qianfan.baidubce.com/v2/"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewTencent func(spec Spec) *OpenAI

func (Module) NewTencent(
	apiKey TencentAPIKey,
	newOpenAI NewOpenAI,
) NewTencent {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://api.hunyuan.cloud.tencent.com/v1"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewHuoshan func(spec Spec) *OpenAI

func (Module) NewHuoshan(
	apiKey HuoshanAPIKey,
	newOpenAI NewOpenAI,
) NewHuoshan {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewAliyun func(spec Spec) *OpenAI

func (Module) NewAliyun(
	apiKey AliyunAPIKey,
	newOpenAI NewOpenAI,
) NewAliyun {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewZhipu func(spec Spec) *OpenAI

func (Module) NewZhipu(
	apiKey ZhipuAPIKey,
	newOpenAI NewOpenAI,
) NewZhipu {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://open.bigmodel.cn/api/paas/v4/"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewVercel func(spec Spec) *OpenAI

func (Module) NewVercel(
	apiKey VercelAPIKey,
	newOpenAI NewOpenAI,
) NewVercel {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://ai-gateway.vercel.sh/v1/"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewNvidia func(spec Spec) *OpenAI

type NewBedrock func(spec Spec) *OpenAI

func (Module) NewBedrock(
	apiKey BedrockAPIKey,
	newOpenAI NewOpenAI,
) NewBedrock {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://bedrock-mantle.ap-northeast-1.api.aws/v1"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

func (Module) NewNvidia(
	apiKey NvidiaAPIKey,
	newOpenAI NewOpenAI,
) NewNvidia {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://integrate.api.nvidia.com/v1"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

type NewOpenCodeGo func(spec Spec) *OpenAI

func (Module) NewOpenCodeGo(
	apiKey OpenCodeGoAPIKey,
	newOpenAI NewOpenAI,
) NewOpenCodeGo {
	return func(spec Spec) *OpenAI {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://opencode.ai/zen/go/v1"
		}
		return newOpenAI(
			spec,
			vars.FirstNonZero(
				spec.APIKey,
				string(apiKey),
			),
		)
	}
}

