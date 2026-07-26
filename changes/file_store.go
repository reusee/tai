package changes

import (
	"os"
	"path/filepath"

	"github.com/reusee/tai/pathutil"
)

const TheoryOfInMemoryApply = `
In-memory apply buffers change block modifications in a MemoryStore during
streaming, deferring disk writes until the round succeeds. This achieves two
goals simultaneously: (1) early error detection — a change block that fails
to apply (invalid target, malformed code) stops generation immediately, and
(2) filesystem consistency on retry — when a round is retried (no completion
block), the MemoryStore is reset, discarding all changes without touching the
disk. Only after a round succeeds are the in-memory changes flushed to disk
in a single batch, so the disk is never left in a partially modified state
by an interrupted round. Subsequent change blocks targeting the same file
within a round use the in-memory content as the base, not the disk content,
so multi-block edits to the same file are applied correctly within the round.
`

// FileStore abstracts file operations for change block application.
// It enables applying changes to an in-memory store (MemoryStore) during
// streaming, then flushing to disk on success, ensuring filesystem
// consistency on retry. The unexported isFileStore method ensures only
// types defined in the changes package can implement this interface,
// preventing accidental use of *os.Root without the rootStore wrapper
// that handles directory creation. See TheoryOfInMemoryApply.
type FileStore interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, content []byte, perm os.FileMode) error
	Remove(path string) error
	Rename(oldPath, newPath string) error
	isFileStore()
}

// rootStore wraps *os.Root to implement FileStore for direct disk access.
// Directory creation is handled inside WriteFile and Rename so that
// ApplyChangeBlockStore does not need a separate MkdirAll method on the
// interface. See TheoryOfInMemoryApply.
type rootStore struct {
	root *os.Root
}

// NewRootStore creates a FileStore backed by the given *os.Root for
// direct disk access.
func NewRootStore(root *os.Root) FileStore {
	return rootStore{root: root}
}

func (s rootStore) isFileStore() {}

func (s rootStore) ReadFile(path string) ([]byte, error) {
	return s.root.ReadFile(path)
}

func (s rootStore) WriteFile(path string, content []byte, perm os.FileMode) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := pathutil.RootMkdirAll(s.root, dir, 0755); err != nil {
			return err
		}
	}
	return s.root.WriteFile(path, content, perm)
}

func (s rootStore) Remove(path string) error {
	return s.root.Remove(path)
}

func (s rootStore) Rename(oldPath, newPath string) error {
	if dir := filepath.Dir(newPath); dir != "." {
		if err := pathutil.RootMkdirAll(s.root, dir, 0755); err != nil {
			return err
		}
	}
	return s.root.Rename(oldPath, newPath)
}

// memoryFile tracks the in-memory state of a file in MemoryStore.
type memoryFile struct {
	content []byte // file content; nil if deleted
	exists  bool   // false if the file has been deleted
}

// MemoryStore is a FileStore that caches writes in memory and reads from
// memory first, falling back to the underlying store for unmodified files.
// Flush writes all cached modifications to the underlying store in a single
// batch. Reset discards all cached modifications, restoring the store to
// its initial state. See TheoryOfInMemoryApply.
type MemoryStore struct {
	underlying FileStore
	files      map[string]*memoryFile
}

// NewMemoryStore creates a MemoryStore that wraps the given underlying
// FileStore. Writes are cached in memory; reads check memory first, then
// fall back to the underlying store. Flush commits all cached changes to
// the underlying store.
func NewMemoryStore(underlying FileStore) *MemoryStore {
	return &MemoryStore{
		underlying: underlying,
		files:      make(map[string]*memoryFile),
	}
}

func (s *MemoryStore) isFileStore() {}

func (s *MemoryStore) ReadFile(path string) ([]byte, error) {
	if mf, ok := s.files[path]; ok {
		if !mf.exists {
			return nil, os.ErrNotExist
		}
		return mf.content, nil
	}
	return s.underlying.ReadFile(path)
}

func (s *MemoryStore) WriteFile(path string, content []byte, perm os.FileMode) error {
	s.files[path] = &memoryFile{content: content, exists: true}
	return nil
}

func (s *MemoryStore) Remove(path string) error {
	s.files[path] = &memoryFile{exists: false}
	return nil
}

func (s *MemoryStore) Rename(oldPath, newPath string) error {
	var content []byte
	var exists bool
	if mf, ok := s.files[oldPath]; ok {
		content = mf.content
		exists = mf.exists
	} else {
		c, err := s.underlying.ReadFile(oldPath)
		if err != nil {
			return err
		}
		content = c
		exists = true
	}
	if !exists {
		return &os.PathError{Op: "rename", Path: oldPath, Err: os.ErrNotExist}
	}
	s.files[newPath] = &memoryFile{content: content, exists: true}
	s.files[oldPath] = &memoryFile{exists: false}
	return nil
}

// Flush writes all cached file modifications to the underlying store,
// committing the in-memory changes to disk in a single batch.
func (s *MemoryStore) Flush() error {
	for path, mf := range s.files {
		if !mf.exists {
			if err := s.underlying.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else {
			if err := s.underlying.WriteFile(path, mf.content, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

// Reset discards all cached modifications, restoring the store to its
// initial state. Used when a generation round is retried or fails, so
// the disk remains untouched by the discarded attempt.
func (s *MemoryStore) Reset() {
	s.files = make(map[string]*memoryFile)
}
