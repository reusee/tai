package gotools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestWorkspace(t *testing.T) {
	root := t.TempDir()

	// Clear GOWORK so workspace detection relies on walking up from the
	// load directory, independent of the host environment.
	t.Setenv("GOWORK", "")

	// go.work at the workspace root listing both modules.
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte(`go 1.21

use (
	./mod1
	./mod2
)
`), 0644); err != nil {
		t.Fatal(err)
	}

	// mod1 is the module containing the load directory.
	mod1Dir := filepath.Join(root, "mod1")
	if err := os.MkdirAll(mod1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "go.mod"), []byte("module example.com/mod1\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// mod2 keeps its Go package in a subdirectory so the module root has
	// no direct .go files: the root is only added to the markdown scan via
	// the workspace module handling.
	mod2Dir := filepath.Join(root, "mod2")
	if err := os.MkdirAll(filepath.Join(mod2Dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod2Dir, "go.mod"), []byte("module example.com/mod2\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod2Dir, "sub", "mod2.go"), []byte("package sub\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod2Dir, "README.md"), []byte("# mod2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(mod1Dir)
		},
	).Call(func(
		workspace Workspace,
		getFiles GetFiles,
	) {
		if workspace == "" {
			t.Fatal("workspace not detected")
		}
		if filepath.Clean(string(workspace)) != filepath.Clean(root) {
			t.Fatalf("workspace root = %q, want %q", workspace, root)
		}
		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}
		var mod1Go, mod2Go, mod2Readme *File
		for _, f := range files {
			switch f.Path {
			case filepath.Join(mod1Dir, "main.go"):
				mod1Go = f
			case filepath.Join(mod2Dir, "sub", "mod2.go"):
				mod2Go = f
			case filepath.Join(mod2Dir, "README.md"):
				mod2Readme = f
			}
		}
		if mod1Go == nil {
			t.Fatal("mod1 main.go not loaded")
		}
		if mod2Go == nil {
			t.Fatal("mod2.go not loaded: workspace modules should all be loaded")
		}
		if !mod2Go.PackageIsRoot {
			t.Error("mod2.go should be a root package in workspace mode")
		}
		if !mod2Go.ModuleIsRoot {
			t.Error("mod2.go module should be a root module in workspace mode")
		}
		if mod2Readme == nil {
			t.Fatal("mod2 README.md should be discovered from the workspace module root")
		}
		if !mod2Readme.PackageIsRoot {
			t.Error("mod2 README.md should be a root package file")
		}
	})
}

func TestWorkspaceNotActivatedForUnlistedModule(t *testing.T) {
	root := t.TempDir()

	// Clear GOWORK so workspace detection relies on walking up from the
	// load directory, independent of the host environment.
	t.Setenv("GOWORK", "")

	// go.work lists only modA.
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.21\n\nuse ./modA\n"), 0644); err != nil {
		t.Fatal(err)
	}

	modADir := filepath.Join(root, "modA")
	if err := os.MkdirAll(modADir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modADir, "go.mod"), []byte("module example.com/modA\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modADir, "a.go"), []byte("package modA\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// modB is not listed in go.work.
	modBDir := filepath.Join(root, "modB")
	if err := os.MkdirAll(modBDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modBDir, "go.mod"), []byte("module example.com/modB\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modBDir, "b.go"), []byte("package modB\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(modBDir)
		},
	).Call(func(workspace Workspace) {
		if workspace != "" {
			t.Fatalf("workspace should not be activated for an unlisted module, got %q", workspace)
		}
	})
}

func TestWorkspaceNotActivatedWhenLoadDirInWorkspaceRootModule(t *testing.T) {
	// A directory with both go.mod and go.work: the load directory sits
	// inside the workspace root's own module. Workspace mode must not
	// activate so that "./..." remains relative to the load directory.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.21\n\nuse .\nuse ./sub\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/root\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	loadDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(loadDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loadDir, "pkg.go"), []byte("package pkg\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(loadDir)
		},
	).Call(func(workspace Workspace) {
		if workspace != "" {
			t.Fatalf("workspace should not be activated inside the workspace root's own module, got %q", workspace)
		}
	})
}

func TestWorkspaceNotActivatedFromModuleSubdirectory(t *testing.T) {
	// Running from a subdirectory of a workspace module keeps
	// module-scoped loading: workspace mode activates only at the
	// workspace root or a module root. See TheoryOfWorkspace.
	root := t.TempDir()
	t.Setenv("GOWORK", "")

	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.21\n\nuse ./mod1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mod1Dir := filepath.Join(root, "mod1")
	if err := os.MkdirAll(filepath.Join(mod1Dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "go.mod"), []byte("module example.com/mod1\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "sub", "sub.go"), []byte("package sub\n"), 0644); err != nil {
		t.Fatal(err)
	}

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(filepath.Join(mod1Dir, "sub"))
		},
	).Call(func(workspace Workspace) {
		if workspace != "" {
			t.Fatalf("workspace should not be activated from a module subdirectory, got %q", workspace)
		}
	})
}

func TestWorkspaceDisabledByGOWORKOff(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.21\n\nuse ./mod1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mod1Dir := filepath.Join(root, "mod1")
	if err := os.MkdirAll(mod1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "go.mod"), []byte("module example.com/mod1\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod1Dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOWORK", "off")

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)

	scope.Fork(
		func() LoadDir {
			return LoadDir(mod1Dir)
		},
	).Call(func(workspace Workspace) {
		if workspace != "" {
			t.Fatalf("workspace should be disabled by GOWORK=off, got %q", workspace)
		}
	})
}
