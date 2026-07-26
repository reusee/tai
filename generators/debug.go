package generators

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type DebugGemini bool

func (Module) DebugGemini() DebugGemini {
	return false
}

var _ flags.Flag = DebugGemini(false)

func (d DebugGemini) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return DebugGemini(true), args, nil
}

func (d DebugGemini) Keys() map[string]string {
	return map[string]string{
		"-debug-gemini": "Enable debug logging for the Gemini generator",
	}
}

type DebugOpenAI bool

func (Module) DebugOpenAI() DebugOpenAI {
	return false
}

var _ flags.Flag = DebugOpenAI(false)

func (d DebugOpenAI) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return DebugOpenAI(true), args, nil
}

func (d DebugOpenAI) Keys() map[string]string {
	return map[string]string{
		"-debug-openai": "Enable debug logging for the OpenAI generator",
	}
}

type TapOpenAI bool

func (Module) TapOpenAI() TapOpenAI {
	return false
}

var _ flags.Flag = TapOpenAI(false)

func (d TapOpenAI) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return TapOpenAI(true), args, nil
}

func (d TapOpenAI) Keys() map[string]string {
	return map[string]string{
		"-tap-openai": "Enable Starlark REPL tap for the OpenAI generator",
	}
}

// Config implementations for types defined in other generator files.
// These are placed here because this file imports cuelang.org/go/cue.

var _ configs.Config = AzureEndpoint("")

func (e AzureEndpoint) ConfigPaths() []string {
	return []string{"azure_endpoint"}
}

func (e AzureEndpoint) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return AzureEndpoint(s), nil
}

var _ configs.Config = AzureAPIVersion("")

func (a AzureAPIVersion) ConfigPaths() []string {
	return []string{"azure_api_version"}
}

func (a AzureAPIVersion) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return AzureAPIVersion(s), nil
}

var _ configs.Config = OpenRouterEndpoint("")

func (e OpenRouterEndpoint) ConfigPaths() []string {
	return []string{"openrouter_endpoint"}
}

func (e OpenRouterEndpoint) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return OpenRouterEndpoint(s), nil
}
