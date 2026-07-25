package gocodes

import (
	"fmt"
	"os"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

type LoadDir string

var _ flags.Flag = LoadDir("")

var _ configs.Config = LoadDir("")

func (l LoadDir) ConfigPaths() []string {
	return []string{"go.load_dir", "go.dir"}
}

func (l LoadDir) HandleConfig(path string, values []*cue.Value) (any, error) {
	s, err := values[0].String()
	if err != nil {
		return nil, err
	}
	return LoadDir(s), nil
}

func (l LoadDir) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expected load dir, got empty")
	}
	return LoadDir(args[0]), args[1:], nil
}

func (l LoadDir) Keys() map[string]string {
	return map[string]string{
		"-load-dir": "Set the root directory for loading Go packages",
	}
}

func (Module) LoadDir() LoadDir {
	currentDir, _ := os.Getwd() // ignore errors
	return LoadDir(currentDir)
}
