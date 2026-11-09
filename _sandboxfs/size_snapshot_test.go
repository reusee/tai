package sandboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTruncateAtTheSizeBoundRemainsAChange(t *testing.T) {
	base := t.TempDir()
	name := filepath.Join(base, "large")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFileSize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.TruncateFile("/large", maxFileSize, 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeModified {
		t.Fatalf("truncation at the size bound disappeared from the changes: %#v", changes)
	}
	applied, conflicts, err := store.ApplyAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || len(conflicts) != 0 {
		t.Fatalf("apply: applied=%d conflicts=%d", len(applied), len(conflicts))
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != maxFileSize {
		t.Fatalf("host size = %d, want %d", info.Size(), maxFileSize)
	}
}

func TestOversizedSnapshotDetectsAHostLengthChange(t *testing.T) {
	base := t.TempDir()
	name := filepath.Join(base, "large")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFileSize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Remove("/large"); err != nil {
		t.Fatal(err)
	}
	// The bounded prefix stays unchanged while the host extends the file. The
	// recorded full length must turn that outside change into a conflict.
	if err := os.Truncate(name, maxFileSize+2); err != nil {
		t.Fatal(err)
	}
	changes, err := store.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Conflict == "" {
		t.Fatalf("host extension was not reported as a conflict: %#v", changes)
	}
	applied, conflicts, err := store.ApplyAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 || len(conflicts) != 1 {
		t.Fatalf("apply: applied=%d conflicts=%d", len(applied), len(conflicts))
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != maxFileSize+2 {
		t.Fatalf("conflicting host size = %d, want %d", info.Size(), maxFileSize+2)
	}
}
