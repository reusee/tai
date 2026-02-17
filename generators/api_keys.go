package generators

import (
	"os"

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
)

func (v VercelAPIKey) TaigoConfigurable() {
}

func (z ZhipuAPIKey) TaigoConfigurable() {
}

func (a AliyunAPIKey) TaigoConfigurable() {
}

func (t TencentAPIKey) TaigoConfigurable() {
}

func (o OpenRouterAPIKey) TaigoConfigurable() {
}

func (d DeepseekAPIKey) TaigoConfigurable() {
}

func (b BaiduAPIKey) TaigoConfigurable() {
}

func (h HuoshanAPIKey) TaigoConfigurable() {
}

func (g GoogleAPIKey) TaigoConfigurable() {
}

func (NvidiaAPIKey) TaigoConfigurable() {
}

var (
	_ configs.Configurable = GoogleAPIKey("")
	_ configs.Configurable = HuoshanAPIKey("")
	_ configs.Configurable = BaiduAPIKey("")
	_ configs.Configurable = DeepseekAPIKey("")
	_ configs.Configurable = OpenRouterAPIKey("")
	_ configs.Configurable = TencentAPIKey("")
	_ configs.Configurable = AliyunAPIKey("")
	_ configs.Configurable = ZhipuAPIKey("")
	_ configs.Configurable = VercelAPIKey("")
	_ configs.Configurable = NvidiaAPIKey("")
)

func (Module) GoogleAPIKey(
	loader configs.Loader,
) GoogleAPIKey {
	return vars.FirstNonZero(
		configs.First[GoogleAPIKey](loader, "google_api_key"),
		GoogleAPIKey(os.Getenv("GOOGLE_API_KEY")),
	)
}

func (Module) HuoshanAPIKey(
	loader configs.Loader,
) HuoshanAPIKey {
	return vars.FirstNonZero(
		configs.First[HuoshanAPIKey](loader, "huoshan_api_key"),
		HuoshanAPIKey(os.Getenv("HUOSHAN_API_KEY")),
	)
}

func (Module) BaiduAPIKey(
	loader configs.Loader,
) BaiduAPIKey {
	return vars.FirstNonZero(
		configs.First[BaiduAPIKey](loader, "baidu_api_key"),
		BaiduAPIKey(os.Getenv("BAIDU_API_KEY")),
	)
}

func (Module) DeepseekAPIKey(
	loader configs.Loader,
) DeepseekAPIKey {
	return vars.FirstNonZero(
		configs.First[DeepseekAPIKey](loader, "deepseek_api_key"),
		DeepseekAPIKey(os.Getenv("DEEPSEEK_API_KEY")),
	)
}

func (Module) OpenRouterAPIKey(
	loader configs.Loader,
) OpenRouterAPIKey {
	return vars.FirstNonZero(
		configs.First[OpenRouterAPIKey](loader, "open_router_api_key"),
		configs.First[OpenRouterAPIKey](loader, "openrouter_api_key"),
		OpenRouterAPIKey(os.Getenv("OPEN_ROUTER_API_KEY")),
		OpenRouterAPIKey(os.Getenv("OPENROUTER_API_KEY")),
	)
}

func (Module) TencentAPIKey(
	loader configs.Loader,
) TencentAPIKey {
	return vars.FirstNonZero(
		configs.First[TencentAPIKey](loader, "tencent_api_key"),
		TencentAPIKey(os.Getenv("TENCENT_API_KEY")),
	)
}

func (Module) AliyunAPIKey(
	loader configs.Loader,
) AliyunAPIKey {
	return vars.FirstNonZero(
		configs.First[AliyunAPIKey](loader, "aliyun_api_key"),
		AliyunAPIKey(os.Getenv("ALIYUN_API_KEY")),
	)
}

func (Module) ZhipuAPIKey(
	loader configs.Loader,
) ZhipuAPIKey {
	return vars.FirstNonZero(
		configs.First[ZhipuAPIKey](loader, "zhipu_api_key"),
		ZhipuAPIKey(os.Getenv("ZHIPU_API_KEY")),
	)
}

func (Module) VercelKey(
	loader configs.Loader,
) VercelAPIKey {
	return vars.FirstNonZero(
		configs.First[VercelAPIKey](loader, "vercel_api_key"),
		VercelAPIKey(os.Getenv("VERCEL_API_KEY")),
	)
}

func (Module) NvidiaAPIKey(
	loader configs.Loader,
) NvidiaAPIKey {
	return vars.FirstNonZero(
		configs.First[NvidiaAPIKey](loader, "nvidia_api_key"),
		NvidiaAPIKey(os.Getenv("NVIDIA_API_KEY")),
	)
}
