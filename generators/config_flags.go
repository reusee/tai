package generators

import (
	"fmt"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

// DebugGemini configs.Config implementation.
// See flags.TheoryOfConfigFlagParity.

var _ configs.Config = DebugGemini(false)

func (d DebugGemini) ConfigPaths() []string {
	return []string{"debug_gemini"}
}

func (d DebugGemini) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return DebugGemini(b), nil
}

// DebugOpenAI configs.Config implementation.

var _ configs.Config = DebugOpenAI(false)

func (d DebugOpenAI) ConfigPaths() []string {
	return []string{"debug_openai"}
}

func (d DebugOpenAI) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return DebugOpenAI(b), nil
}

// TapOpenAI configs.Config implementation.

var _ configs.Config = TapOpenAI(false)

func (d TapOpenAI) ConfigPaths() []string {
	return []string{"tap_openai"}
}

func (d TapOpenAI) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return TapOpenAI(b), nil
}

// AzureEndpoint flags.Flag implementation. The configs.Config
// implementation is in debug.go. See flags.TheoryOfConfigFlagParity.

var _ flags.Flag = AzureEndpoint("")

func (e AzureEndpoint) Keys() map[string]string {
	return map[string]string{
		"-azure-endpoint": "Set the Azure OpenAI endpoint URL",
	}
}

func (e AzureEndpoint) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	return AzureEndpoint(args[0]), args[1:], nil
}

// AzureAPIVersion flags.Flag implementation.

var _ flags.Flag = AzureAPIVersion("")

func (a AzureAPIVersion) Keys() map[string]string {
	return map[string]string{
		"-azure-api-version": "Set the Azure OpenAI API version",
	}
}

func (a AzureAPIVersion) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	return AzureAPIVersion(args[0]), args[1:], nil
}

// OpenRouterEndpoint flags.Flag implementation.

var _ flags.Flag = OpenRouterEndpoint("")

func (e OpenRouterEndpoint) Keys() map[string]string {
	return map[string]string{
		"-openrouter-endpoint": "Set the OpenRouter API endpoint URL",
	}
}

func (e OpenRouterEndpoint) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	return OpenRouterEndpoint(args[0]), args[1:], nil
}
