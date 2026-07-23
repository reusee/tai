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

func (d DebugGemini) Keys() []string {
	return []string{"-debug-gemini"}
}

type DebugOpenAI bool

func (Module) DebugOpenAI() DebugOpenAI {
	return false
}

var _ flags.Flag = DebugOpenAI(false)

func (d DebugOpenAI) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return DebugOpenAI(true), args, nil
}

func (d DebugOpenAI) Keys() []string {
	return []string{"-debug-openai"}
}

type TapOpenAI bool

func (Module) TapOpenAI() TapOpenAI {
	return false
}

var _ flags.Flag = TapOpenAI(false)

func (d TapOpenAI) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return TapOpenAI(true), args, nil
}

func (d TapOpenAI) Keys() []string {
	return []string{"-tap-openai"}
}
