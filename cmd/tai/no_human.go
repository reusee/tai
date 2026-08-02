package main

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

// NoHuman controls whether interactive human input is disabled. When true,
// chat prompts and REPL sessions are skipped, enabling unattended/autonomous
// operation. Used by the goal subcommand to ensure fully autonomous
// multi-loop execution without any human interaction.
type NoHuman bool

func (Module) NoHuman() NoHuman {
	return false
}

var _ flags.Flag = NoHuman(true)

func (n NoHuman) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := NoHuman(true)
	return &ret, args, nil
}

func (n NoHuman) Keys() map[string]string {
	return map[string]string{
		"-no-human": "Disable interactive chat and REPL for unattended operation",
	}
}

var _ configs.Config = NoHuman(false)

func (n NoHuman) ConfigPaths() []string {
	return []string{"no_human"}
}

func (n NoHuman) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	ret := NoHuman(b)
	return &ret, nil
}
