package changes

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sync"
)

const TheoryOfDiskChangeDetection = `
Disk change detection guards the model's context snapshot against concurrent
disk edits. When context assembly reads a file into the model's snapshot,
the file's SHA-256 is recorded in the session's FileHashes baseline. Every
rootStore disk read and write verifies the file's current content against
the baseline: a mismatch, or the disappearance of a baselined file, is a
DiskChangedError. Writes through the store refresh the baseline, so only
external modifications between context assembly and the apply trigger the
error. When a baseline exists, the hash check replaces the mtime
write-conflict check, which stays in force for stores built without a
baseline. The baseline is a dscope-provided value, so each goal loop starts
with a fresh baseline matching the filesystem state that loop loaded; the
generation loop ends the run on DiskChangedError instead of retrying the
attempt, because only a fresh loop that reloads the filesystem can repair
the snapshot divergence. See pipeline.TheoryOfDiskChangeHandoff and
TheoryOfWriteConflictDetection.
`

// DiskChangedError reports that a file on disk no longer matches the
// content snapshot the model context was built from. Callers detect it
// with errors.As. See TheoryOfDiskChangeDetection.
type DiskChangedError struct {
	Path string
}

func (e *DiskChangedError) Error() string {
	return fmt.Sprintf("disk file changed since context was loaded: %s", e.Path)
}

// FileHashes is the per-session baseline of file content hashes. Context
// assembly records every file it hands to the model; the apply layer
// verifies disk content against the baseline before operating on it. All
// methods are safe for concurrent use. See TheoryOfDiskChangeDetection.
type FileHashes struct {
	mu     sync.Mutex
	hashes map[string][sha256.Size]byte
}

// NewFileHashes creates an empty baseline.
func NewFileHashes() *FileHashes {
	return &FileHashes{
		hashes: make(map[string][sha256.Size]byte),
	}
}

// normalizePath canonicalizes a baseline key to an absolute cleaned path,
// so a relative path recorded during context assembly and the absolute
// path derived by the apply layer resolve to the same entry. See
// TheoryOfDiskChangeDetection.
func normalizePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// Set records or refreshes the baseline hash of path for content. The
// path is canonicalized to an absolute cleaned form, so callers may pass
// either relative or absolute paths and both sides of the detection —
// context assembly and the apply layer — resolve to the same key.
func (f *FileHashes) Set(path string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hashes[normalizePath(path)] = sha256.Sum256(content)
}

// Delete drops the baseline of path, so later reads and writes of the
// path are no longer verified.
func (f *FileHashes) Delete(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.hashes, path)
}

// Transfer moves the baseline of oldPath to newPath. Rename preserves file
// content, so the recorded hash stays valid for the new path.
func (f *FileHashes) Transfer(oldPath, newPath string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if hash, ok := f.hashes[oldPath]; ok {
		delete(f.hashes, oldPath)
		f.hashes[newPath] = hash
	}
}

// Has reports whether a baseline exists for path.
func (f *FileHashes) Has(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.hashes[path]
	return ok
}

// Verify compares the hash of content against the baseline of path. A
// missing baseline verifies; a mismatch returns DiskChangedError.
func (f *FileHashes) Verify(path string, content []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	baseline, ok := f.hashes[path]
	if !ok {
		return nil
	}
	if sha256.Sum256(content) != baseline {
		return &DiskChangedError{Path: path}
	}
	return nil
}
