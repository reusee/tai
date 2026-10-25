package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/tai/sandboxfs"
)

func TestSandboxLoopChangeApplyUsesTheActiveStore(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := sandboxfs.Open(filepath.Join(dir, "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	defer sandboxfs.SetActiveStore(nil)
	sandboxfs.SetActiveStore(store)
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/f", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	applied, conflicts, err := sandboxLoopChangeApply()(1)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || len(conflicts) != 0 {
		t.Fatalf("applied %d change(s), conflicts %v, want 1 and none", applied, conflicts)
	}
	content, err := os.ReadFile(filepath.Join(base, "f"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("host content %q, want new", content)
	}
}

func TestSandboxLoopChangeApplyInertWithoutASandbox(t *testing.T) {
	sandboxfs.SetActiveStore(nil)
	applied, conflicts, err := sandboxLoopChangeApply()(1)
	if err != nil || applied != 0 || len(conflicts) != 0 {
		t.Fatalf("applied %d change(s), conflicts %v, err %v; want an inert hook",
			applied, conflicts, err)
	}
}
