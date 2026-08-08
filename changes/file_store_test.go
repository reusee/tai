package changes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

func TestMemoryStoreWriteReadFlush(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// Create initial file on disk
	original := "package x\n\nfunc Old() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	// Write new content to memory
	newContent := "package x\n\nfunc New() {}\n"
	if err := store.WriteFile("test.go", []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Read from memory should return new content
	got, err := store.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != newContent {
		t.Fatalf("expected %q, got %q", newContent, string(got))
	}

	// Disk should still have original content (not flushed yet)
	diskContent, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(diskContent) != original {
		t.Fatalf("disk should have original content before flush, got %q", string(diskContent))
	}

	// Flush to disk
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	// Disk should now have new content
	diskContent, err = root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(diskContent) != newContent {
		t.Fatalf("disk should have new content after flush, got %q", string(diskContent))
	}
}

func TestMemoryStoreResetDiscards(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nfunc Old() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	// Write new content to memory
	newContent := "package x\n\nfunc New() {}\n"
	if err := store.WriteFile("test.go", []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Reset discards in-memory changes
	store.Reset()

	// Read should fall back to disk (original content)
	got, err := store.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("expected original content after reset, got %q", string(got))
	}

	// Disk should still have original content
	diskContent, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(diskContent) != original {
		t.Fatalf("disk should have original content after reset, got %q", string(diskContent))
	}
}

func TestMemoryStoreRemoveAndReadFile(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := root.WriteFile("test.go", []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	// Remove in memory
	if err := store.Remove("test.go"); err != nil {
		t.Fatal(err)
	}

	// Read should return os.ErrNotExist
	_, err = store.ReadFile("test.go")
	if err == nil {
		t.Fatal("expected error for deleted file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.IsNotExist error, got: %v", err)
	}

	// Disk should still have the file (not flushed)
	_, err = root.ReadFile("test.go")
	if err != nil {
		t.Fatalf("disk should still have file before flush: %v", err)
	}

	// Flush removes from disk
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	_, err = root.ReadFile("test.go")
	if err == nil {
		t.Fatal("file should be removed from disk after flush")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.IsNotExist error, got: %v", err)
	}
}

func TestMemoryStoreRenameAndRead(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n"
	if err := root.WriteFile("old.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	// Rename in memory
	if err := store.Rename("old.go", "new.go"); err != nil {
		t.Fatal(err)
	}

	// New path should have content
	got, err := store.ReadFile("new.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("expected %q, got %q", original, string(got))
	}

	// Old path should not exist
	_, err = store.ReadFile("old.go")
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected os.IsNotExist for old path, got: %v", err)
	}

	// Disk should still have old file (not flushed)
	_, err = root.ReadFile("old.go")
	if err != nil {
		t.Fatalf("disk should still have old file before flush: %v", err)
	}

	// Flush
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	// Disk should have new file, not old
	_, err = root.ReadFile("new.go")
	if err != nil {
		t.Fatalf("disk should have new file after flush: %v", err)
	}
	_, err = root.ReadFile("old.go")
	if err == nil {
		t.Fatal("old file should be removed from disk after flush")
	}
}

func TestMemoryStoreApplyChangeBlockStore(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlockStore ApplyChangeBlockStore) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		store := NewMemoryStore(NewRootStore(root))

		// Apply a MODIFY change block to the MemoryStore
		h := ChangeBlock{
			Op:       "MODIFY",
			Target:   "Old",
			FilePath: "test.go",
			Body:     "func New() {}",
		}
		if err := applyChangeBlockStore(store, h); err != nil {
			t.Fatalf("ApplyChangeBlockStore failed: %v", err)
		}

		// Memory should have updated content
		got, err := store.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "Old") {
			t.Fatalf("memory should not contain Old:\n%s", string(got))
		}
		if !strings.Contains(string(got), "func New() {}") {
			t.Fatalf("memory should contain New:\n%s", string(got))
		}

		// Disk should still have original content (not flushed)
		diskContent, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(diskContent), "func Old() {}") {
			t.Fatalf("disk should still have original content before flush:\n%s", string(diskContent))
		}

		// Flush to disk
		if err := store.Flush(); err != nil {
			t.Fatal(err)
		}

		// Disk should now have updated content
		diskContent, err = root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(diskContent), "Old") {
			t.Fatalf("disk should not contain Old after flush:\n%s", string(diskContent))
		}
		if !strings.Contains(string(diskContent), "func New() {}") {
			t.Fatalf("disk should contain New after flush:\n%s", string(diskContent))
		}
	})
}

func TestMemoryStoreMultipleChangesSameFile(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlockStore ApplyChangeBlockStore) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		store := NewMemoryStore(NewRootStore(root))

		// First change: WRITE new content
		h1 := ChangeBlock{
			Op:       "WRITE",
			FilePath: "test.go",
			Body:     "package x\n\nfunc First() {}\n",
		}
		if err := applyChangeBlockStore(store, h1); err != nil {
			t.Fatalf("first ApplyChangeBlockStore failed: %v", err)
		}

		// Second change: MODIFY First → Second (uses in-memory content as base)
		h2 := ChangeBlock{
			Op:       "MODIFY",
			Target:   "First",
			FilePath: "test.go",
			Body:     "func Second() {}",
		}
		if err := applyChangeBlockStore(store, h2); err != nil {
			t.Fatalf("second ApplyChangeBlockStore failed: %v", err)
		}

		// Memory should have Second, not First or Old
		got, err := store.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "First") {
			t.Fatalf("memory should not contain First:\n%s", string(got))
		}
		if strings.Contains(string(got), "Old") {
			t.Fatalf("memory should not contain Old:\n%s", string(got))
		}
		if !strings.Contains(string(got), "func Second() {}") {
			t.Fatalf("memory should contain Second:\n%s", string(got))
		}

		// Disk should still have original content
		diskContent, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(diskContent), "func Old() {}") {
			t.Fatalf("disk should still have original content:\n%s", string(diskContent))
		}

		// Flush
		if err := store.Flush(); err != nil {
			t.Fatal(err)
		}

		// Disk should have final content
		diskContent, err = root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(diskContent), "func Second() {}") {
			t.Fatalf("disk should contain Second after flush:\n%s", string(diskContent))
		}
	})
}

func TestMemoryStoreCreateNewFile(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlockStore ApplyChangeBlockStore) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		store := NewMemoryStore(NewRootStore(root))

		// Create a new file via WRITE
		h := ChangeBlock{
			Op:       "WRITE",
			FilePath: "new.go",
			Body:     "package x\n\nfunc New() {}\n",
		}
		if err := applyChangeBlockStore(store, h); err != nil {
			t.Fatalf("ApplyChangeBlockStore failed: %v", err)
		}

		// File should not exist on disk yet
		_, err = root.ReadFile("new.go")
		if err == nil {
			t.Fatal("file should not exist on disk before flush")
		}

		// Flush
		if err := store.Flush(); err != nil {
			t.Fatal(err)
		}

		// File should exist on disk after flush
		got, err := root.ReadFile("new.go")
		if err != nil {
			t.Fatalf("file should exist on disk after flush: %v", err)
		}
		if !strings.Contains(string(got), "func New() {}") {
			t.Fatalf("disk should contain New:\n%s", string(got))
		}
	})
}

func TestMemoryStoreFlushCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	store := NewMemoryStore(NewRootStore(root))

	// Write to a nested path
	if err := store.WriteFile("sub/dir/file.go", []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Flush should create directories
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	// File should exist
	_, err = root.ReadFile("sub/dir/file.go")
	if err != nil {
		t.Fatalf("file should exist after flush: %v", err)
	}
}

func TestMemoryStoreDeleteNonExistentFileFlush(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	store := NewMemoryStore(NewRootStore(root))

	// Delete a file that doesn't exist on disk
	if err := store.Remove("nonexistent.go"); err != nil {
		t.Fatal(err)
	}

	// Flush should not error (os.IsNotExist is handled)
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush should not error for non-existent file: %v", err)
	}
}

func TestMemoryStoreRenameNonExistentFile(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	store := NewMemoryStore(NewRootStore(root))

	// Rename a file that doesn't exist
	err = store.Rename("nonexistent.go", "new.go")
	if err == nil {
		t.Fatal("expected error for renaming non-existent file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.IsNotExist error, got: %v", err)
	}
}

func TestMemoryStoreDiffs(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "package x\n\nfunc Old() {}\n"
	if err := root.WriteFile("a.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("b.go", []byte("# b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	if err := store.WriteFile("a.go", []byte("package x\n\nfunc New() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("c.go", []byte("new file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("b.go"); err != nil {
		t.Fatal(err)
	}

	diffs := store.Diffs()
	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(diffs))
	}
	// Paths are sorted: a.go, b.go, c.go
	if diffs[0].Path != "a.go" || diffs[1].Path != "b.go" || diffs[2].Path != "c.go" {
		t.Fatalf("unexpected diff order: %v, %v, %v", diffs[0].Path, diffs[1].Path, diffs[2].Path)
	}
	if string(diffs[0].Original) != original {
		t.Fatalf("a.go original mismatch: %q", string(diffs[0].Original))
	}
	if !strings.Contains(string(diffs[0].Current), "func New() {}") {
		t.Fatalf("a.go current mismatch: %q", string(diffs[0].Current))
	}
	if !diffs[1].OriginalExists || diffs[1].CurrentExists {
		t.Fatal("b.go should be deleted in current state")
	}
	if diffs[2].OriginalExists || !diffs[2].CurrentExists {
		t.Fatal("c.go should be a new file")
	}
}

func TestMemoryStoreDiffsPersistAcrossReset(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "old content"
	if err := root.WriteFile("a.txt", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	if err := store.WriteFile("a.txt", []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	store.Reset()
	if err := store.WriteFile("a.txt", []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	diffs := store.Diffs()
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if string(diffs[0].Original) != original {
		t.Fatalf("session original should be %q, got %q", original, string(diffs[0].Original))
	}
	if string(diffs[0].Current) != "v2" {
		t.Fatalf("current should be %q, got %q", "v2", string(diffs[0].Current))
	}
}

func TestMemoryStoreDiffsIncludeFlushedRounds(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "old content"
	if err := root.WriteFile("a.txt", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	// Round 1: modify a.txt and flush to disk, as OnRoundSuccess does.
	if err := store.WriteFile("a.txt", []byte("round 1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}

	// Round 2 starts: OnRoundStart resets the store, clearing s.files
	// but retaining s.originals.
	store.Reset()

	// Round 2: modify b.txt.
	if err := store.WriteFile("b.txt", []byte("round 2"), 0644); err != nil {
		t.Fatal(err)
	}

	// Diffs must include both round 1's a.txt change (read from the
	// underlying store, since a.txt is no longer in s.files) and round
	// 2's b.txt change. Without the fix, Diffs only iterates s.files and
	// misses a.txt entirely.
	diffs := store.Diffs()
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d: %+v", len(diffs), diffs)
	}
	// Paths are sorted: a.txt, b.txt
	if diffs[0].Path != "a.txt" || diffs[1].Path != "b.txt" {
		t.Fatalf("unexpected diff order: %v, %v", diffs[0].Path, diffs[1].Path)
	}
	if string(diffs[0].Original) != original {
		t.Fatalf("a.txt original mismatch: %q", string(diffs[0].Original))
	}
	if string(diffs[0].Current) != "round 1" {
		t.Fatalf("a.txt current should be the flushed round-1 content, got %q", string(diffs[0].Current))
	}
	if diffs[1].OriginalExists || !diffs[1].CurrentExists {
		t.Fatal("b.txt should be a new file")
	}
}

func TestMemoryStoreDiffsSkipNoOp(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	original := "old content"
	if err := root.WriteFile("a.txt", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore(NewRootStore(root))

	// A change applied in a failed round and rolled back by Reset must
	// not appear in Diffs: the current state (read from the underlying
	// store) matches the original, so the diff is a no-op.
	if err := store.WriteFile("a.txt", []byte("failed change"), 0644); err != nil {
		t.Fatal(err)
	}
	store.Reset()

	diffs := store.Diffs()
	if len(diffs) != 0 {
		t.Fatalf("expected 0 diffs for rolled-back changes, got %d: %+v", len(diffs), diffs)
	}
}

func TestRootStoreWriteFileCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	store := NewRootStore(root)

	// Write to a nested path — rootStore.WriteFile should create the directory
	if err := store.WriteFile("sub/dir/file.go", []byte("package x\n"), 0644); err != nil {
		t.Fatalf("WriteFile should create directories: %v", err)
	}

	// File should exist
	got, err := root.ReadFile("sub/dir/file.go")
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if string(got) != "package x\n" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}

func TestRootStoreRenameCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := root.WriteFile("old.go", []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewRootStore(root)

	// Rename to a nested path — rootStore.Rename should create the directory
	if err := store.Rename("old.go", "sub/dir/new.go"); err != nil {
		t.Fatalf("Rename should create directories: %v", err)
	}

	// New file should exist
	got, err := root.ReadFile("sub/dir/new.go")
	if err != nil {
		t.Fatalf("new file should exist: %v", err)
	}
	if string(got) != "package x\n" {
		t.Fatalf("unexpected content: %q", string(got))
	}

	// Old file should not exist
	_, err = root.ReadFile("old.go")
	if err == nil {
		t.Fatal("old file should not exist after rename")
	}
}

func TestApplyChangeBlockWrapperDelegatesToStore(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlock ApplyChangeBlock) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		// ApplyChangeBlock (the wrapper) should delegate to ApplyChangeBlockStore
		h := ChangeBlock{
			Op:       "MODIFY",
			Target:   "Old",
			FilePath: "test.go",
			Body:     "func New() {}",
		}
		if err := applyChangeBlock(root, h); err != nil {
			t.Fatalf("ApplyChangeBlock failed: %v", err)
		}

		// Disk should have updated content (wrapper writes directly to disk)
		got, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "Old") {
			t.Fatalf("disk should not contain Old:\n%s", string(got))
		}
		if !strings.Contains(string(got), "func New() {}") {
			t.Fatalf("disk should contain New:\n%s", string(got))
		}
	})
}

func TestApplyChangeBlocksWrapperDelegatesToStore(t *testing.T) {
	newTestScope(t).Call(func(applyChangeBlocks ApplyChangeBlocks) {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		original := "package x\n\nfunc Old() {}\n"
		if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		state := generators.NewPrompts("", nil)
		parserState := blocks.NewParserState(state)
		text := "<<DELIM1 <change op=\"MODIFY\" target=\"Old\" file-path=\"test.go\">\nfunc New() {}\nDELIM1\n"
		newState, err := parserState.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		parserState = newState.(*blocks.ParserState)

		changeBlocks := []blocks.Block{
			{
				Kind:       "change",
				Boundary:   "DELIM1",
				Attributes: map[string]string{"op": "MODIFY", "target": "Old", "file-path": "test.go"},
				Body:       "func New() {}",
			},
		}

		// ApplyChangeBlocks (the wrapper) should delegate to ApplyChangeBlocksStore
		err = applyChangeBlocks(changeBlocks, root)
		if err != nil {
			t.Fatalf("ApplyChangeBlocks failed: %v", err)
		}

		// Disk should have updated content
		got, err := root.ReadFile("test.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "Old") {
			t.Fatalf("disk should not contain Old:\n%s", string(got))
		}
		if !strings.Contains(string(got), "func New() {}") {
			t.Fatalf("disk should contain New:\n%s", string(got))
		}
	})
}

func TestRootStoreWriteConflictDetection(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	writeTimes := NewFileWriteTimes()
	store := NewRootStoreWithWriteTimes(root, writeTimes)

	// First write: no baseline, succeeds and records the post-write mtime.
	if err := store.WriteFile("a.txt", []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "a.txt")
	recorded, ok := writeTimes.Get(key)
	if !ok {
		t.Fatal("expected a recorded write time after the first write")
	}
	info, err := root.Stat("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(recorded) {
		t.Fatalf("recorded time %v does not match file mtime %v", recorded, info.ModTime())
	}

	// Simulate an external modification with a clearly different mtime.
	future := info.ModTime().Add(10 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "a.txt"), future, future); err != nil {
		t.Fatal(err)
	}

	// A write after external modification must be rejected.
	err = store.WriteFile("a.txt", []byte("v2"), 0644)
	if err == nil {
		t.Fatal("expected a write conflict error for an externally modified file")
	}
	if !strings.Contains(err.Error(), "write conflict") {
		t.Fatalf("expected write conflict error, got: %v", err)
	}

	// Files without a recorded time are written unconditionally.
	if err := store.WriteFile("b.txt", []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRootStoreWriteConflictDetectionRepeatedWrites(t *testing.T) {
	// Multiple applies to the same file must update the recorded last
	// write time so the next write still matches. See
	// TheoryOfWriteConflictDetection.
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	writeTimes := NewFileWriteTimes()
	store := NewRootStoreWithWriteTimes(root, writeTimes)

	path := "a.txt"
	key := filepath.Join(dir, path)
	if err := store.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	first, ok := writeTimes.Get(key)
	if !ok {
		t.Fatal("expected a recorded time after the first write")
	}
	// Sleep across a full second so the second write's mtime differs even
	// on filesystems with coarse (1s) mtime granularity.
	time.Sleep(1100 * time.Millisecond)
	if err := store.WriteFile(path, []byte("v2"), 0644); err != nil {
		t.Fatalf("repeated write must succeed: %v", err)
	}
	second, ok := writeTimes.Get(key)
	if !ok {
		t.Fatal("expected a recorded time after the second write")
	}
	if second.Equal(first) {
		t.Fatalf("expected the recorded write time to be updated after the second write, got %v then %v", first, second)
	}
	info, err := root.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(second) {
		t.Fatalf("recorded time %v does not match file mtime %v", second, info.ModTime())
	}
}

func TestRootStoreWriteConflictTrackingRenameRemove(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	writeTimes := NewFileWriteTimes()
	store := NewRootStoreWithWriteTimes(root, writeTimes)

	if err := store.WriteFile("old.txt", []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := writeTimes.Get(filepath.Join(dir, "old.txt")); !ok {
		t.Fatal("expected a recorded time for old.txt")
	}

	// Rename transfers the tracked time to the new path.
	if err := store.Rename("old.txt", "sub/new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := writeTimes.Get(filepath.Join(dir, "old.txt")); ok {
		t.Fatal("recorded time must be dropped for the renamed-away path")
	}
	if _, ok := writeTimes.Get(filepath.Join(dir, "sub", "new.txt")); !ok {
		t.Fatal("recorded time must be transferred to the new path")
	}

	// Remove drops the tracked time.
	if err := store.Remove("sub/new.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := writeTimes.Get(filepath.Join(dir, "sub", "new.txt")); ok {
		t.Fatal("recorded time must be dropped after removal")
	}

	// After removal, a fresh write to the same path has no baseline and
	// succeeds.
	if err := store.WriteFile("sub/new.txt", []byte("v2"), 0644); err != nil {
		t.Fatalf("write after removal must succeed: %v", err)
	}
}

// TestFormatFileDiffsContextLimited verifies that FormatFileDiffs renders
// modified files with context-limited hunks while keeping the file header.
// See TheoryOfReviewDiffContext.
func TestFormatFileDiffsContextLimited(t *testing.T) {
	oldLines, newLines := makeBaseLines(100)
	newLines[49] = "line 50 changed"

	output := FormatFileDiffs([]FileDiff{
		{
			Path:           "test.txt",
			Original:       []byte(strings.Join(oldLines, "\n") + "\n"),
			OriginalExists: true,
			Current:        []byte(strings.Join(newLines, "\n") + "\n"),
			CurrentExists:  true,
		},
	})

	if !strings.Contains(output, "=== test.txt ===\n") {
		t.Fatalf("expected the file header:\n%s", output)
	}
	if !strings.Contains(output, "@@ -20,61 +20,61 @@\n") {
		t.Fatalf("expected the limited-context hunk header:\n%s", output)
	}
	if strings.Contains(output, "  line 10\n") {
		t.Fatalf("line 10 is beyond the context window and must not appear:\n%s", output)
	}
}

// TestFormatUnifiedDiffNoChanges verifies that unchanged content produces
// no hunks and an empty diff string. See TheoryOfReviewDiffContext.
func TestFormatUnifiedDiffNoChanges(t *testing.T) {
	content := []byte("line 1\nline 2\nline 3\n")
	if diff := formatUnifiedDiff("test.txt", content, content); diff != "" {
		t.Fatalf("expected empty diff for unchanged content, got:\n%s", diff)
	}
}

// TestFormatUnifiedDiffSeparateHunks verifies that changes more than
// 2*diffContextLines apart render as separate hunks with their own context
// windows. See TheoryOfReviewDiffContext.
func TestFormatUnifiedDiffSeparateHunks(t *testing.T) {
	oldLines, newLines := makeBaseLines(200)
	newLines[9] = "line 10 changed"
	newLines[149] = "line 150 changed"

	diff := formatUnifiedDiff(
		"test.txt",
		[]byte(strings.Join(oldLines, "\n")+"\n"),
		[]byte(strings.Join(newLines, "\n")+"\n"),
	)

	if !strings.Contains(diff, "@@ -1,40 +1,40 @@\n") {
		t.Fatalf("expected first hunk header @@ -1,40 +1,40 @@:\n%s", diff)
	}
	if !strings.Contains(diff, "@@ -120,61 +120,61 @@\n") {
		t.Fatalf("expected second hunk header @@ -120,61 +120,61 @@:\n%s", diff)
	}
	hunkCount := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@") {
			hunkCount++
		}
	}
	if hunkCount != 2 {
		t.Fatalf("expected 2 separate hunks, got %d:\n%s", hunkCount, diff)
	}
}

// TestFormatUnifiedDiffMergesOverlappingHunks verifies that changes whose
// context windows overlap render as a single merged hunk, like git does.
// See TheoryOfReviewDiffContext.
func TestFormatUnifiedDiffMergesOverlappingHunks(t *testing.T) {
	oldLines, newLines := makeBaseLines(100)
	newLines[49] = "line 50 changed"
	newLines[54] = "line 55 changed"

	diff := formatUnifiedDiff(
		"test.txt",
		[]byte(strings.Join(oldLines, "\n")+"\n"),
		[]byte(strings.Join(newLines, "\n")+"\n"),
	)

	hunkCount := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "@@") {
			hunkCount++
		}
	}
	if hunkCount != 1 {
		t.Fatalf("expected a single merged hunk, got %d:\n%s", hunkCount, diff)
	}
}

// TestFormatUnifiedDiffChangeAtFileStart verifies that a change at the
// very start of a file produces a hunk whose context window extends only
// forward, starting at line 1. See TheoryOfReviewDiffContext.
func TestFormatUnifiedDiffChangeAtFileStart(t *testing.T) {
	oldLines, newLines := makeBaseLines(100)
	newLines[0] = "line 1 changed"

	diff := formatUnifiedDiff(
		"test.txt",
		[]byte(strings.Join(oldLines, "\n")+"\n"),
		[]byte(strings.Join(newLines, "\n")+"\n"),
	)

	if !strings.Contains(diff, "@@ -1,31 +1,31 @@\n") {
		t.Fatalf("expected hunk header @@ -1,31 +1,31 @@:\n%s", diff)
	}
	if !strings.Contains(diff, "  line 31\n") {
		t.Fatalf("expected context line 31 (within the window):\n%s", diff)
	}
	if strings.Contains(diff, "  line 32\n") {
		t.Fatalf("line 32 is beyond the context window and must not appear:\n%s", diff)
	}
}

// TestFormatUnifiedDiffContextLimited verifies that a single localized
// change renders as one hunk carrying at most diffContextLines of context
// on each side (git diff -U30 style), with lines beyond the window
// excluded. See TheoryOfReviewDiffContext.
func TestFormatUnifiedDiffContextLimited(t *testing.T) {
	oldLines, newLines := makeBaseLines(100)
	newLines[49] = "line 50 changed"

	diff := formatUnifiedDiff(
		"test.txt",
		[]byte(strings.Join(oldLines, "\n")+"\n"),
		[]byte(strings.Join(newLines, "\n")+"\n"),
	)

	if !strings.Contains(diff, "@@ -20,61 +20,61 @@\n") {
		t.Fatalf("expected hunk header @@ -20,61 +20,61 @@:\n%s", diff)
	}
	if !strings.Contains(diff, "  line 20\n") {
		t.Fatalf("expected context line 20 (within the window):\n%s", diff)
	}
	if !strings.Contains(diff, "  line 80\n") {
		t.Fatalf("expected context line 80 (within the window):\n%s", diff)
	}
	if !strings.Contains(diff, "- line 50\n") || !strings.Contains(diff, "+ line 50 changed\n") {
		t.Fatalf("expected the changed line:\n%s", diff)
	}
	if strings.Contains(diff, "  line 10\n") {
		t.Fatalf("line 10 is beyond the context window and must not appear:\n%s", diff)
	}
	if strings.Contains(diff, "  line 90\n") {
		t.Fatalf("line 90 is beyond the context window and must not appear:\n%s", diff)
	}
}

// makeBaseLines returns two identical line slices with the given number of
// 1-based numbered lines ("line 1" through "line N"). Tests mutate the
// returned newLines to simulate changes.
func makeBaseLines(n int) (oldLines, newLines []string) {
	oldLines = make([]string, 0, n)
	newLines = make([]string, 0, n)
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf("line %d", i)
		oldLines = append(oldLines, line)
		newLines = append(newLines, line)
	}
	return
}
