package taiconfigs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

func TestFindGoModuleRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if got := findGoModuleRoot(sub); got != root {
		t.Fatalf("expected %q, got %q", root, got)
	}
	// A non-existent subdirectory still resolves from the same position.
	if got := findGoModuleRoot(filepath.Join(sub, "missing")); got != root {
		t.Fatalf("expected %q, got %q", root, got)
	}
	// The module root itself is returned directly.
	if got := findGoModuleRoot(root); got != root {
		t.Fatalf("expected %q, got %q", root, got)
	}

	// No go.mod anywhere above: empty result.
	noMod := t.TempDir()
	if got := findGoModuleRoot(noMod); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

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
