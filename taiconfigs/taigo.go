package taiconfigs

import (
	"os"
	"path/filepath"
	"reflect"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/taigo"
)

func TaigoFork(scope dscope.Scope) (ret dscope.Scope, err error) {
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

	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return ret, err
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
			return ret, err
		}
		scope, err = configs.TaigoFork(scope, vm.Scope)
		if err != nil {
			return ret, err
		}
	}

	return scope, nil
}
