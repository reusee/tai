package gocodes

import (
	"fmt"
	"os"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/vars"
)

type LoadDir string

var _ configs.Configurable = LoadDir("")

func (l LoadDir) TaigoConfigurable() {}

var _ flags.Flag = LoadDir("")

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

func (Module) LoadDir(
	loader configs.Loader,
) LoadDir {
	currentDir, _ := os.Getwd() // ignore errors
	return vars.FirstNonZero(
		configs.First[LoadDir](loader, "go.load_dir"),
		configs.First[LoadDir](loader, "go.dir"),
		LoadDir(currentDir),
	)
}
