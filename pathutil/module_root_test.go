package pathutil

import (
	"os"
	"path/filepath"
	"testing"
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
	if got, ok := FindGoModuleRoot(sub); !ok || got != root {
		t.Fatalf("expected %q, got %q, ok %v", root, got, ok)
	}
	// A non-existent subdirectory still resolves from the same position.
	if got, ok := FindGoModuleRoot(filepath.Join(sub, "missing")); !ok || got != root {
		t.Fatalf("expected %q, got %q, ok %v", root, got, ok)
	}
	// The module root itself is returned directly.
	if got, ok := FindGoModuleRoot(root); !ok || got != root {
		t.Fatalf("expected %q, got %q, ok %v", root, got, ok)
	}
	// A directory named go.mod is not a module marker; the walk skips it
	// and continues to the enclosing module root.
	if err := os.MkdirAll(filepath.Join(root, "c", "go.mod"), 0755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindGoModuleRoot(filepath.Join(root, "c")); !ok || got != root {
		t.Fatalf("expected %q, got %q, ok %v", root, got, ok)
	}

	// No go.mod anywhere above: not found.
	noMod := t.TempDir()
	if got, ok := FindGoModuleRoot(noMod); ok || got != "" {
		t.Fatalf("expected not found, got %q, ok %v", got, ok)
	}
}
