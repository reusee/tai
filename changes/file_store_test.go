package changes

import (
	"os"
	"strings"
	"testing"

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
