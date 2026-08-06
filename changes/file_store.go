package changes

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// FileDiff represents a file's original (pre-session) and current state.
// It is produced by MemoryStore.Diffs for review loops. See
// TheoryOfReviewLoop in codes/generate.go.
type FileDiff struct {
	Path           string
	Original       []byte
	OriginalExists bool
	Current        []byte
	CurrentExists  bool
}

// FormatFileDiffs renders session diffs as a readable review context:
// new/deleted files are shown in full, modified files as unified diffs.
func FormatFileDiffs(diffs []FileDiff) string {
	var b strings.Builder
	for _, diff := range diffs {
		b.WriteString("\n")
		switch {
		case !diff.OriginalExists:
			fmt.Fprintf(&b, "=== %s (new file) ===\n", diff.Path)
			b.Write(diff.Current)
		case !diff.CurrentExists:
			fmt.Fprintf(&b, "=== %s (deleted) ===\n", diff.Path)
			b.Write(diff.Original)
		default:
			fmt.Fprintf(&b, "=== %s ===\n", diff.Path)
			b.WriteString(formatUnifiedDiff(diff.Path, diff.Original, diff.Current))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// maxDiffMatrixCells caps the LCS matrix size for line diffs. Beyond this,
// a full old/new listing is used instead of an exact minimal diff.
const maxDiffMatrixCells = 4_000_000

// formatUnifiedDiff produces a unified diff between original and current
// file content. Line numbers are coarse (whole-file hunks) but sufficient
// for review purposes.
func formatUnifiedDiff(path string, original, current []byte) string {
	oldLines := splitLines(original)
	newLines := splitLines(current)
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, line := range diffLines(oldLines, newLines) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitLines splits file content into lines, ignoring a trailing newline.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(content), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diffLines computes a line-level diff between two line slices using an LCS
// table. When the matrix exceeds maxDiffMatrixCells, the fallback is to list
// all old lines as deletions and all new lines as additions.
func diffLines(a, b []string) []string {
	n, m := len(a), len(b)
	if n*m > maxDiffMatrixCells {
		res := make([]string, 0, n+m)
		for _, line := range a {
			res = append(res, "- "+line)
		}
		for _, line := range b {
			res = append(res, "+ "+line)
		}
		return res
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var res []string
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			res = append(res, "  "+a[i])
			i++
			j++
		} else if dp[i][j+1] >= dp[i+1][j] {
			res = append(res, "+ "+b[j])
			j++
		} else {
			res = append(res, "- "+a[i])
			i++
		}
	}
	for ; i < n; i++ {
		res = append(res, "- "+a[i])
	}
	for ; j < m; j++ {
		res = append(res, "+ "+b[j])
	}
	return res
}

// MemoryStore is a FileStore that caches writes in memory and reads from
// memory first, falling back to the underlying store for unmodified files.
// Flush writes all cached modifications to the underlying store in a single
// batch. Reset discards per-round cached modifications, restoring the store
// to its initial state. Session originals are retained across Reset so that
// Diffs always compares against the state before the first modification of
// the session. See TheoryOfInMemoryApply.
type MemoryStore struct {
	underlying FileStore
	files      map[string]*memoryFile
	originals  map[string]*memoryFile
}

// NewMemoryStore creates a MemoryStore that wraps the given underlying
// FileStore. Writes are cached in memory; reads check memory first, then
// fall back to the underlying store. Flush commits all cached changes to
// the underlying store. The session originals map is initialized empty so
// the first modification of each path records the pre-session content.
func NewMemoryStore(underlying FileStore) *MemoryStore {
	return &MemoryStore{
		underlying: underlying,
		files:      make(map[string]*memoryFile),
		originals:  make(map[string]*memoryFile),
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
	s.captureOriginal(path)
	s.files[path] = &memoryFile{content: content, exists: true}
	return nil
}

func (s *MemoryStore) Remove(path string) error {
	s.captureOriginal(path)
	s.files[path] = &memoryFile{exists: false}
	return nil
}

func (s *MemoryStore) Rename(oldPath, newPath string) error {
	s.captureOriginal(oldPath)
	s.captureOriginal(newPath)
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

// Reset discards all per-round cached modifications, restoring the store
// to its initial state. Session originals are intentionally retained: a
// session spans multiple rounds (each OnRoundStart calls Reset), and Diffs
// must compare against the state before the first modification of the whole
// session, not the state before the current round. See TheoryOfInMemoryApply.
func (s *MemoryStore) Reset() {
	s.files = make(map[string]*memoryFile)
}

// captureOriginal records the pre-session content of a path the first time
// the path is modified in this session. Subsequent modifications reuse the
// recorded original so Diffs always shows the full session delta.
func (s *MemoryStore) captureOriginal(path string) {
	if _, ok := s.originals[path]; ok {
		return
	}
	content, err := s.underlying.ReadFile(path)
	if err != nil {
		// A read error (including file not found) means the original
		// state is "does not exist"; the diff will show a new file or
		// the absence of an old one.
		s.originals[path] = &memoryFile{exists: false}
		return
	}
	s.originals[path] = &memoryFile{content: slices.Clone(content), exists: true}
}

// Diffs returns the accumulated session changes as FileDiff entries,
// comparing the pre-session original state against the current in-memory
// state. Paths are sorted for deterministic ordering. See
// TheoryOfReviewLoop in codes/generate.go.
func (s *MemoryStore) Diffs() []FileDiff {
	paths := slices.Collect(maps.Keys(s.files))
	slices.Sort(paths)
	var diffs []FileDiff
	for _, path := range paths {
		mf := s.files[path]
		orig := s.originals[path]
		if orig == nil {
			orig = &memoryFile{}
		}
		diffs = append(diffs, FileDiff{
			Path:           path,
			Original:       orig.content,
			OriginalExists: orig.exists,
			Current:        mf.content,
			CurrentExists:  mf.exists,
		})
	}
	return diffs
}
