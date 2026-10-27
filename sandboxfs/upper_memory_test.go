package sandboxfs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestMemoryStoreServesWritesWithoutItsRecordFile protects TheoryOfUpperLayer:
// a memory-backed store records the same rows and content the sqlite layer
// would, but leaves no file behind, so a session that needs no durable record
// pays no disk round trip per mutation. The path runs the whole store API —
// write, read, list, diff, apply — against the memory layer.
func TestMemoryStoreServesWritesWithoutItsRecordFile(t *testing.T) {
	base := t.TempDir()
	store, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.dbPath != "" {
		t.Fatalf("memory store names a record file: %q", store.dbPath)
	}
	if err := store.WriteFile("/a.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadFile("/a.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("content = %q, err = %v, want %q", got, err, "hello")
	}
	window, err := store.ReadRange("/a.txt", 0, 64)
	if err != nil || !bytes.Equal(window, []byte("hello")) {
		t.Fatalf("window = %q, err = %v, want %q", window, err, "hello")
	}
	entries, err := store.ReadDir("/")
	if err != nil || len(entries) != 1 || entries[0].Name != "a.txt" || entries[0].Attr.Size != 5 {
		t.Fatalf("entries = %+v, err = %v, want one a.txt of 5 bytes", entries, err)
	}
	changes, err := store.Changes()
	if err != nil || len(changes) != 1 || changes[0].Kind != ChangeAdded {
		t.Fatalf("changes = %+v, err = %v, want one added path", changes, err)
	}
	applied, err := store.Apply(nil)
	if err != nil || len(applied) != 1 {
		t.Fatalf("applied = %+v, err = %v, want one change", applied, err)
	}
	hostContent, err := os.ReadFile(filepath.Join(base, "a.txt"))
	if err != nil || string(hostContent) != "hello" {
		t.Fatalf("host content = %q, err = %v, want %q", hostContent, err, "hello")
	}
	changes, err = store.Changes()
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes after apply = %+v, err = %v, want none", changes, err)
	}
}

// TestMemoryStoreDropsItsRecordBetweenSessions protects TheoryOfUpperLayer: a
// memory layer keeps nothing across processes, so a fresh OpenMemory over the
// same base answers from the base as if the previous session never wrote.
func TestMemoryStoreDropsItsRecordBetweenSessions(t *testing.T) {
	base := t.TempDir()
	first, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.WriteFile("/a.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.ReadFile("/a.txt"); err == nil {
		t.Fatal("the second session read a file the first session only wrote in memory")
	}
}
