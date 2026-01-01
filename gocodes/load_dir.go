package gocodes

import (
	"os"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/vars"
)

type LoadDir string

var _ configs.Configurable = LoadDir("")

func (l LoadDir) ConfigExpr() string {
	return "GoLoadDir"
}

var loadDirFlag = cmds.Var[string]("-load-dir")

func (Module) LoadDir(
	loader configs.Loader,
) LoadDir {
	currentDir, _ := os.Getwd() // ignore errors
	return vars.FirstNonZero(
		LoadDir(*loadDirFlag),
		configs.First[LoadDir](loader, "go.load_dir"),
		configs.First[LoadDir](loader, "go.dir"),
		LoadDir(currentDir),
	)
}
