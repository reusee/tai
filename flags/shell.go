package flags

import (
	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
)

// Shell configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Shell(false)

type Shell bool

func (Module) Shell() (ret Shell) {
	return
}

var _ Flag = Shell(false)

func (s Shell) Keys() map[string]string {
	return map[string]string{
		"-shell":    "Enable shell block execution",
		"-no-shell": "Disable shell block execution",
	}
}

func (s Shell) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	// The matched key determines the boolean value; "shell" sets true,
	// "no-shell" sets false. No arguments are consumed.
	newValue = Shell(key == "-shell")
	remainArgs = args
	return
}

func (s Shell) ConfigPaths() []string {
	return []string{"shell"}
}

func (s Shell) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Shell(b), nil
}
