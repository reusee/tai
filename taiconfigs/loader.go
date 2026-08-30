package taiconfigs

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/pathutil"
)

//go:embed schema.cue
var schema string

// ConfigsLoader builds the tai-specific configuration loader, including the
// embedded schema and the config globals. It is a Module method, so forking
// new(Module) provides the tai loader, overriding the default configs.Loader
// (from configs.Module). See configs.Loader.
func (Module) ConfigsLoader(
	logger logs.Logger,
	configGlobals ConfigGlobals,
) configs.Loader {
	var paths []string
	defer func() {
		if len(paths) > 0 {
			logger.Info("config file",
				"paths", paths,
			)
		}
	}()

	filenames := []string{
		"tai.cue",
		".tai.cue",
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

	// go module root directory
	// When the working directory is inside a Go module, config files at
	// the module root apply to the whole module and are shared by every
	// invocation within it. The module root is checked after the working
	// directory so a local config still takes precedence, and before the
	// user config dir so a project-level config overrides personal
	// defaults. When the working directory is the module root itself,
	// the two paths coincide and the check is skipped to avoid a
	// duplicate entry.
	if moduleRoot, ok := pathutil.FindGoModuleRoot(workingDir); ok && moduleRoot != workingDir {
		for _, filename := range filenames {
			path := filepath.Join(moduleRoot, filename)
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
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

	// system wide dir
	for _, filename := range filenames {
		path := filepath.Join("/etc", filename)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}

	return configs.NewLoader(paths, configs.LoaderConfig{
		Schema:  schema,
		Globals: configGlobals,
	})
}
