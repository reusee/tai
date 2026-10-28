package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActiveStoreRecordsAndClears(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "sandbox.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	defer SetActiveStore(nil)
	SetActiveStore(store)
	if ActiveStore() != store {
		t.Fatal("the recorded store is not the active store")
	}
	SetActiveStore(nil)
	if ActiveStore() != nil {
		t.Fatal("a cleared record still names a store")
	}
}

func TestSandboxUnavailableRecordsAndClears(t *testing.T) {
	defer sandboxfs.SetSandboxUnavailable(false)
	if sandboxfs.SandboxUnavailable() {
		t.Fatal("the default must be available")
	}
	sandboxfs.SetSandboxUnavailable(true)
	if !sandboxfs.SandboxUnavailable() {
		t.Fatal("want unavailable after the set")
	}
	sandboxfs.SetSandboxUnavailable(false)
	if sandboxfs.SandboxUnavailable() {
		t.Fatal("want available after the clear")
	}
}

func TestApplyAllWritesTheHostAndReportsConflicts(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(dir, "sandbox.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, name := range []string{"kept", "changed"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.WriteFile("/kept", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/changed", []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An external write after the session read the file is a conflict: the
	// apply must keep the host content and report the change.
	if err := os.WriteFile(filepath.Join(base, "changed"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	applied, conflicts, err := store.ApplyAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].Path != "/kept" {
		t.Fatalf("applied %v, want /kept", applied)
	}
	if len(conflicts) != 1 || conflicts[0].Path != "/changed" {
		t.Fatalf("conflicts %v, want /changed", conflicts)
	}
	content, err := os.ReadFile(filepath.Join(base, "kept"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("host content %q, want new", content)
	}
	content, err = os.ReadFile(filepath.Join(base, "changed"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "outside" {
		t.Fatalf("conflicting host content %q, want outside", content)
	}
}
