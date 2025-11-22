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

func (Module) ConfigsLoader(
	logger logs.Logger,
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

	return configs.NewLoader(paths, schema)
}
