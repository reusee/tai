package codes

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type Debug bool

func (Module) Debug() Debug {
	return false
}

var _ flags.Flag = Debug(true)

func (d Debug) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return Debug(true), args, nil
}

func (d Debug) Keys() map[string]string {
	return map[string]string{
		"-debug-codes": "Enable debug logging for the codes module",
	}
}

// ExtraSystemPrompt configs.Config implementation.
// Defined here because this file imports cuelang.org/go/cue.
// The type is declared in system_prompt.go.

var _ configs.Config = ExtraSystemPrompt("")

func (e ExtraSystemPrompt) ConfigPaths() []string {
	return []string{"extra_system_prompt"}
}

func (e ExtraSystemPrompt) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return ExtraSystemPrompt(s), nil
}
