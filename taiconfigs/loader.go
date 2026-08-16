package taiconfigs

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
)

//go:embed schema.cue
var schema string

// ConfigsLoader builds the tai-specific configuration loader, including the
// embedded schema and the config globals. It is a package-level function
// intended to be forked into a scope that already provides the default
// configs.Loader (from configs.Module), overriding it with the tai loader.
// See configs.Loader.
func ConfigsLoader(
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
	if moduleRoot := findGoModuleRoot(workingDir); moduleRoot != "" && moduleRoot != workingDir {
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

// findGoModuleRoot walks up the directory tree from dir looking for a
// go.mod file and returns the absolute path of the directory containing
// it. It returns "" when the filesystem root is reached without finding
// one.
func findGoModuleRoot(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		dir = filepath.Clean(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
