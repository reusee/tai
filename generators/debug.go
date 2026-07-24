package generators

import (
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
