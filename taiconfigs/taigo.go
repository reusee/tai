package taiconfigs

import (
	"os"
	"path/filepath"
	"reflect"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/taigo"
	"github.com/reusee/tai/taivm"
)

func TaigoFork(scope dscope.Scope) (dscope.Scope, error) {
	var paths []string

	filenames := []string{
		"tai.go",
		".tai.go",
	}

	// system wide dir
	for _, filename := range filenames {
		path := filepath.Join("/etc", filename)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}

	// user config dir
	configDir, err := os.UserConfigDir()
	if err == nil {
		for _, filename := range filenames {
			path := filepath.Join(configDir, filename)
			_, err := os.Stat(path)
			if err == nil {
				paths = append(paths, path)
			}
		}
	}

	// working directory
	workingDir, err := os.Getwd()
	if err == nil {
		for _, filename := range filenames {
			path := filepath.Join(workingDir, filename)
			_, err := os.Stat(path)
			if err == nil {
				paths = append(paths, path)
			}
		}
	}

	configurableType := reflect.TypeFor[configs.Configurable]()

	var lastEnv *taivm.Env
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return scope, err
		}
		env := &taigo.Env{
			Source:     content,
			SourceName: path,
			Globals:    make(map[string]any),
		}
		for t := range scope.AllTypes() {
			if t.Implements(configurableType) {
				env.Globals[t.Name()] = t
			}
		}
		vm, err := env.RunVM()
		if err != nil {
			return scope, err
		}
		// Hierarchical Chaining: the next environment's parent is the current one.
		vm.Scope.Parent = lastEnv
		lastEnv = vm.Scope
		scope, err = configs.TaigoFork(scope, vm.Scope)
		if err != nil {
			return scope, err
		}
	}

	scope = scope.Fork(ConfigGoEnv(lastEnv))

	return scope, nil
}

type ConfigGoEnv *taivm.Env

func (Module) ConfigGoEnv() ConfigGoEnv {
	return nil
}

const Theory = `
Hierarchical Configuration:
Configuration files (tai.go) are loaded and executed in a specific order:
1. /etc/tai.go (System-wide)
2. ~/.config/tai.go (User-specific)
3. ./tai.go (Local project)

Each file is executed in its own VM, but their global environments are chained.
The environment of a 'later' file (e.g., Local) has the environment of the
'earlier' file (e.g., User) as its parent. This creates a hierarchy where
definitions in more local files shadow those in more global ones.

This chained environment is preserved as ConfigGoEnv and used for expanding
Go expressions embedded in prompts or arguments (e.g., using \go(expr) syntax).
`
