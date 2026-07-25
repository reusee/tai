package generators

import (
	"os"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
)

type (
	GoogleAPIKey     string
	HuoshanAPIKey    string
	BaiduAPIKey      string
	DeepseekAPIKey   string
	OpenRouterAPIKey string
	TencentAPIKey    string
	AliyunAPIKey     string
	ZhipuAPIKey      string
	VercelAPIKey     string
	NvidiaAPIKey     string
	AzureAPIKey      string
	BedrockAPIKey    string
)

var _ configs.Config = GoogleAPIKey("")

func (g GoogleAPIKey) ConfigPaths() []string {
	return []string{"google_api_key"}
}

func (g GoogleAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return GoogleAPIKey(s), nil
}

func (Module) GoogleAPIKey() GoogleAPIKey {
	return GoogleAPIKey(os.Getenv("GOOGLE_API_KEY"))
}

var _ configs.Config = HuoshanAPIKey("")

func (h HuoshanAPIKey) ConfigPaths() []string {
	return []string{"huoshan_api_key"}
}

func (h HuoshanAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return HuoshanAPIKey(s), nil
}

func (Module) HuoshanAPIKey() HuoshanAPIKey {
	return HuoshanAPIKey(os.Getenv("HUOSHAN_API_KEY"))
}

var _ configs.Config = BaiduAPIKey("")

func (b BaiduAPIKey) ConfigPaths() []string {
	return []string{"baidu_api_key"}
}

func (b BaiduAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return BaiduAPIKey(s), nil
}

func (Module) BaiduAPIKey() BaiduAPIKey {
	return BaiduAPIKey(os.Getenv("BAIDU_API_KEY"))
}

var _ configs.Config = DeepseekAPIKey("")

func (d DeepseekAPIKey) ConfigPaths() []string {
	return []string{"deepseek_api_key"}
}

func (d DeepseekAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return DeepseekAPIKey(s), nil
}

func (Module) DeepseekAPIKey() DeepseekAPIKey {
	return DeepseekAPIKey(os.Getenv("DEEPSEEK_API_KEY"))
}

var _ configs.Config = OpenRouterAPIKey("")

func (o OpenRouterAPIKey) ConfigPaths() []string {
	return []string{"open_router_api_key", "openrouter_api_key"}
}

func (o OpenRouterAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return OpenRouterAPIKey(s), nil
}

func (Module) OpenRouterAPIKey() OpenRouterAPIKey {
	return OpenRouterAPIKey(vars.FirstNonZero(
		OpenRouterAPIKey(os.Getenv("OPEN_ROUTER_API_KEY")),
		OpenRouterAPIKey(os.Getenv("OPENROUTER_API_KEY")),
	))
}

var _ configs.Config = TencentAPIKey("")

func (t TencentAPIKey) ConfigPaths() []string {
	return []string{"tencent_api_key"}
}

func (t TencentAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return TencentAPIKey(s), nil
}

func (Module) TencentAPIKey() TencentAPIKey {
	return TencentAPIKey(os.Getenv("TENCENT_API_KEY"))
}

var _ configs.Config = AliyunAPIKey("")

func (a AliyunAPIKey) ConfigPaths() []string {
	return []string{"aliyun_api_key"}
}

func (a AliyunAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return AliyunAPIKey(s), nil
}

func (Module) AliyunAPIKey() AliyunAPIKey {
	return AliyunAPIKey(os.Getenv("ALIYUN_API_KEY"))
}

var _ configs.Config = ZhipuAPIKey("")

func (z ZhipuAPIKey) ConfigPaths() []string {
	return []string{"zhipu_api_key"}
}

func (z ZhipuAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return ZhipuAPIKey(s), nil
}

func (Module) ZhipuAPIKey() ZhipuAPIKey {
	return ZhipuAPIKey(os.Getenv("ZHIPU_API_KEY"))
}

var _ configs.Config = VercelAPIKey("")

func (v VercelAPIKey) ConfigPaths() []string {
	return []string{"vercel_api_key"}
}

func (v VercelAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return VercelAPIKey(s), nil
}

func (Module) VercelKey() VercelAPIKey {
	return VercelAPIKey(os.Getenv("VERCEL_API_KEY"))
}

var _ configs.Config = NvidiaAPIKey("")

func (n NvidiaAPIKey) ConfigPaths() []string {
	return []string{"nvidia_api_key"}
}

func (n NvidiaAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return NvidiaAPIKey(s), nil
}

func (Module) NvidiaAPIKey() NvidiaAPIKey {
	return NvidiaAPIKey(os.Getenv("NVIDIA_API_KEY"))
}

var _ configs.Config = AzureAPIKey("")

func (a AzureAPIKey) ConfigPaths() []string {
	return []string{"azure_api_key"}
}

func (a AzureAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return AzureAPIKey(s), nil
}

func (Module) AzureAPIKey() AzureAPIKey {
	return AzureAPIKey(os.Getenv("AZURE_API_KEY"))
}

var _ configs.Config = BedrockAPIKey("")

func (b BedrockAPIKey) ConfigPaths() []string {
	return []string{"aws_bedrock_api_key"}
}

func (b BedrockAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return BedrockAPIKey(s), nil
}

func (Module) BedrockAPIKey() BedrockAPIKey {
	return BedrockAPIKey(os.Getenv("AWS_BEDROCK_API_KEY"))
}

type OpenCodeGoAPIKey string

var _ configs.Config = OpenCodeGoAPIKey("")

func (o OpenCodeGoAPIKey) ConfigPaths() []string {
	return []string{"opencode_go_api_key"}
}

func (o OpenCodeGoAPIKey) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return OpenCodeGoAPIKey(s), nil
}

func (Module) OpenCodeGoAPIKey() OpenCodeGoAPIKey {
	return OpenCodeGoAPIKey(os.Getenv("OPENCODE_GO_API_KEY"))
}
