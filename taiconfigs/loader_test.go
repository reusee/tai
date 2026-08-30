package taiconfigs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
)

func TestConfigsLoaderIncludesGoModuleRoot(t *testing.T) {
	// The loader must discover tai.cue / .tai.cue files at the root of
	// the Go module that contains the working directory. The working
	// directory config still takes precedence (checked first); the
	// module root config precedes the user config dir.
	root := t.TempDir()
	// Isolate from the developer's personal config.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".tai.cue"), []byte("model_name: \"module-config\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	workDir := filepath.Join(root, "pkg", "sub")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Call(func(loader configs.Loader) {
		var name flags.ModelName
		if err := loader.AssignFirst("model_name", &name); err != nil {
			t.Fatal(err)
		}
		if name != "module-config" {
			t.Fatalf("expected %q, got %q", "module-config", name)
		}
	})
}
