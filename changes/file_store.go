package changes

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/reusee/tai/pathutil"
)

const TheoryOfWriteConflictDetection = `
Write conflict detection tracks the last modification time of every file
this process has written to disk, so that a later write can verify the file
has not been modified externally since. The tracker is shared by all root
stores in the process via a dscope-provided pointer; the check runs at the
point of a real disk write (rootStore.WriteFile), which is the single
convergence point for direct writes (patch, next) and for
MemoryStore.Flush after a successful round.

A write compares the file's current mtime against the recorded last-write
time. A mismatch means the file was very likely modified externally — by
an editor, another tai process, or a git operation — and writing could
overwrite the external change, so the write is rejected with a write
conflict error. A file with no recorded time has no baseline and is
written unconditionally, recording the post-write mtime. Repeated writes
to the same file compare against the newly recorded time and update it,
so a session that applies several changes to one file stays consistent.
Rename preserves the tracked time because rename preserves the file's
mtime; Remove drops the entry so a later recreation of the path is a
fresh file with no baseline.

mtime is an optimistic-lock heuristic, not a strong guarantee: a file
modified and restored to the exact same mtime evades detection, and
coarse-granularity filesystems may report the same time for distinct
writes. Within those limits the tracker surfaces the common case of a
file touched by another process between two of our writes.
`

const TheoryOfInMemoryApply = `
In-memory apply buffers change block modifications in a MemoryStore during
streaming, deferring disk writes until the round succeeds. This achieves two
goals simultaneously: early error detection — a change block that fails to
apply (invalid target, malformed code) stops generation immediately — and
filesystem consistency on retry — when a round is retried (no completion
block), the MemoryStore is reset, discarding all changes without touching
the disk. Only after a round succeeds are the in-memory changes flushed to
disk in a single batch, so the disk is never left in a partially modified
state by an interrupted round. Subsequent change blocks targeting the same
file within a round use the in-memory content as the base, not the disk
content, so multi-block edits to the same file are applied correctly within
the round.
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

// rootStore is a FileStore backed by an *os.Root. writeTimes enables the
// mtime-based write-conflict check; hashes enables content-baseline disk
// change detection. Both are optional. See
// TheoryOfWriteConflictDetection and TheoryOfDiskChangeDetection.
type rootStore struct {
	root       *os.Root
	writeTimes *FileWriteTimes
	hashes     *FileHashes
}

// NewRootStore creates a FileStore backed by the given *os.Root for
// direct disk access.
func NewRootStore(root *os.Root) FileStore {
	return rootStore{root: root}
}

// NewRootStoreWithWriteTimes creates a FileStore backed by the given
// *os.Root for direct disk access, with write conflict detection enabled:
// every WriteFile verifies that the file has not been modified externally
// since the last write recorded in writeTimes. See
// TheoryOfWriteConflictDetection.
func NewRootStoreWithWriteTimes(root *os.Root, writeTimes *FileWriteTimes) FileStore {
	return rootStore{root: root, writeTimes: writeTimes}
}

// NewRootStoreWithSnapshot creates a FileStore backed by the given *os.Root
// with disk change detection: every read and write verifies the file's
// content against the session baseline in hashes, and a mismatch is a
// DiskChangedError. writeTimes may be nil. When hashes is nil no baseline
// verification happens and the mtime-based write-conflict check applies.
// See TheoryOfDiskChangeDetection.
func NewRootStoreWithSnapshot(root *os.Root, writeTimes *FileWriteTimes, hashes *FileHashes) FileStore {
	return rootStore{root: root, writeTimes: writeTimes, hashes: hashes}
}

// FileWriteTimes tracks the last modification time observed for each file
// that this process has written to disk. All root stores in a dscope scope
// share one tracker, so consecutive writes to the same file — whether
// direct (patch, next) or through MemoryStore.Flush — compare against a
// consistent baseline. The provider is re-evaluated on dscope.Reset, so
// independent sessions (e.g., goal loops) start with a fresh tracker: each
// session loads the current filesystem state, and conflict detection
// covers writes within one session. All methods are safe for concurrent
// use. See TheoryOfWriteConflictDetection.
type FileWriteTimes struct {
	mu    sync.Mutex
	times map[string]time.Time
}

// NewFileWriteTimes creates an empty write-time tracker.
func NewFileWriteTimes() *FileWriteTimes {
	return &FileWriteTimes{
		times: make(map[string]time.Time),
	}
}

// Get returns the last recorded write time for path.
func (f *FileWriteTimes) Get(path string) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.times[path]
	return t, ok
}

// Set records the write time for path, replacing any previous value.
func (f *FileWriteTimes) Set(path string, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.times[path] = t
}

// Delete drops the record for path.
func (f *FileWriteTimes) Delete(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.times, path)
}

func (s rootStore) isFileStore() {}

func (s rootStore) ReadFile(path string) ([]byte, error) {
	content, err := s.root.ReadFile(path)
	if s.hashes == nil {
		return content, err
	}
	key := s.trackedPath(path)
	if err != nil {
		// A baselined file that has disappeared was deleted externally
		// after the context snapshot was taken.
		if os.IsNotExist(err) && s.hashes.Has(key) {
			return nil, &DiskChangedError{Path: key}
		}
		return content, err
	}
	if err := s.hashes.Verify(key, content); err != nil {
		return nil, err
	}
	return content, nil
}

func (s rootStore) WriteFile(path string, content []byte, perm os.FileMode) error {
	key := s.trackedPath(path)
	if err := s.checkDiskChangedBeforeWrite(key, path); err != nil {
		return err
	}
	// When a content baseline exists, the hash check governs: content
	// comparison has no coarse-mtime false positives, so a matching hash
	// makes the mtime check redundant noise. Stores without a baseline
	// keep the mtime check. See TheoryOfDiskChangeDetection.
	if s.hashes == nil {
		if err := s.checkWriteConflict(key, path); err != nil {
			return err
		}
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := pathutil.RootMkdirAll(s.root, dir, 0755); err != nil {
			return err
		}
	}
	if err := s.root.WriteFile(path, content, perm); err != nil {
		return err
	}
	s.recordWriteTime(key, path)
	// Our own write refreshes the baseline, so only external changes
	// between the snapshot and the next read or write are detected. See
	// TheoryOfDiskChangeDetection.
	if s.hashes != nil {
		s.hashes.Set(key, content)
	}
	return nil
}

// checkWriteConflict verifies that the file at statPath has not been
// modified externally since the last time this process wrote it. key is
// the canonical (absolute) path used for tracker lookups; statPath is the
// root-relative path used to stat the actual file within the root. A file
// with no recorded write time has no baseline and the write proceeds. A
// recorded time that differs from the file's current mtime means the file
// was likely touched by another process since our last write; writing
// could overwrite the external change, so the write is rejected. See
// TheoryOfWriteConflictDetection.
func (s rootStore) checkWriteConflict(key, statPath string) error {
	if s.writeTimes == nil {
		return nil
	}
	last, ok := s.writeTimes.Get(key)
	if !ok {
		return nil
	}
	info, err := s.root.Stat(statPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("write conflict: %s was deleted by another process since last write at %v", statPath, last)
		}
		return err
	}
	if !info.ModTime().Equal(last) {
		return fmt.Errorf("write conflict: %s was modified by another process since last write at %v (current mtime %v)", statPath, last, info.ModTime())
	}
	return nil
}

// recordWriteTime records the post-write mtime of statPath as the baseline
// for future write conflict checks. Recording the actual filesystem mtime
// (not time.Now) keeps the comparison precise: the recorded value is
// exactly what the next check will read. See
// TheoryOfWriteConflictDetection.
func (s rootStore) recordWriteTime(key, statPath string) {
	if s.writeTimes == nil {
		return
	}
	if info, err := s.root.Stat(statPath); err == nil {
		s.writeTimes.Set(key, info.ModTime())
	}
}

// checkDiskChangedBeforeWrite verifies the file's current disk content
// against the session baseline before overwriting it. A baselined file
// that was modified or deleted externally must not be overwritten: the
// change block was computed against the stale snapshot. A missing baseline
// means the file never entered the model context, so the write proceeds
// unchecked. See TheoryOfDiskChangeDetection.
func (s rootStore) checkDiskChangedBeforeWrite(key, statPath string) error {
	if s.hashes == nil || !s.hashes.Has(key) {
		return nil
	}
	current, err := s.root.ReadFile(statPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &DiskChangedError{Path: key}
		}
		return err
	}
	return s.hashes.Verify(key, current)
}

// trackedPath returns the canonical key used for write-time tracking: an
// absolute, cleaned path. Two roots that happen to use the same relative
// path (e.g., "test.go" in different directories) refer to different
// files, so the tracker must key by absolute path to avoid false
// conflicts between them. See TheoryOfWriteConflictDetection.
func (s rootStore) trackedPath(path string) string {
	full := path
	if !filepath.IsAbs(full) {
		rootPath := s.root.Name()
		if !filepath.IsAbs(rootPath) {
			if abs, err := filepath.Abs(rootPath); err == nil {
				rootPath = abs
			}
		}
		full = filepath.Join(rootPath, path)
	}
	return filepath.Clean(full)
}

func (s rootStore) Remove(path string) error {
	if err := s.root.Remove(path); err != nil {
		return err
	}
	// A removed path can no longer conflict; a later recreation is a
	// fresh file with no baseline. See TheoryOfWriteConflictDetection
	// and TheoryOfDiskChangeDetection.
	key := s.trackedPath(path)
	if s.writeTimes != nil {
		s.writeTimes.Delete(key)
	}
	if s.hashes != nil {
		s.hashes.Delete(key)
	}
	return nil
}

func (s rootStore) Rename(oldPath, newPath string) error {
	if dir := filepath.Dir(newPath); dir != "." {
		if err := pathutil.RootMkdirAll(s.root, dir, 0755); err != nil {
			return err
		}
	}
	if err := s.root.Rename(oldPath, newPath); err != nil {
		return err
	}
	// Rename preserves the file's mtime and content, so the tracked write
	// time and the content baseline move with the file and the old
	// path's records are dropped. See TheoryOfWriteConflictDetection and
	// TheoryOfDiskChangeDetection.
	if s.writeTimes != nil {
		oldKey := s.trackedPath(oldPath)
		if t, ok := s.writeTimes.Get(oldKey); ok {
			s.writeTimes.Delete(oldKey)
			s.writeTimes.Set(s.trackedPath(newPath), t)
		}
	}
	if s.hashes != nil {
		s.hashes.Transfer(s.trackedPath(oldPath), s.trackedPath(newPath))
	}
	return nil
}

// memoryFile tracks the in-memory state of a file in MemoryStore.
type memoryFile struct {
	content []byte // file content; nil if deleted
	exists  bool   // false if the file has been deleted
}

// FileDiff represents a file's original (pre-session) and current state.
// It is produced by MemoryStore.Diffs for review loops. See
// TheoryOfReviewLoop in pipeline/generate.go.
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

const TheoryOfReviewDiffContext = `
Review diffs are rendered as unified diffs with a bounded context window
instead of whole-file hunks. Whole-file hunks flood the review model's
context with unchanged lines when a large file has small localized changes,
wasting the review budget and diluting attention. Each changed region
therefore carries a bounded window of unchanged context lines around it,
matching the familiar git diff -U30 output, and hunks whose windows overlap
are merged like git does. The window size is set by diffContextLines in this
file. New and deleted files are exempt: every line is new or removed, so the
full content is shown, exactly as git diff does for such files.
`

// diffContextLines is the number of unchanged context lines shown around
// each changed region in a rendered unified diff, matching `git diff -U30`.
// See TheoryOfReviewDiffContext.
const diffContextLines = 30

// maxDiffMatrixCells caps the LCS matrix size for line diffs. Beyond this,
// a full old/new listing is used instead of an exact minimal diff.
const maxDiffMatrixCells = 4_000_000

// formatUnifiedDiff produces a unified diff between original and current
// file content. Each changed region is rendered as a hunk with at most
// diffContextLines of unchanged context around it; overlapping hunks are
// merged into a single hunk, matching git diff -U30. Returns an empty
// string when the content is unchanged. See TheoryOfReviewDiffContext.
func formatUnifiedDiff(path string, original, current []byte) string {
	oldLines := splitLines(original)
	newLines := splitLines(current)
	ops := computeDiffOps(oldLines, newLines)
	hunks := buildDiffHunks(oldLines, newLines, ops)
	if len(hunks) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)
	for _, hunk := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", hunk.oldStart, hunk.oldCount, hunk.newStart, hunk.newCount)
		for _, line := range hunk.lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
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

// buildDiffHunks groups an edit script into hunks with at most
// diffContextLines of unchanged context around each changed region,
// merging hunks whose context windows overlap. This mirrors git diff -U30
// output: each hunk carries the context needed to understand the change
// while keeping large unchanged regions out of the diff. See
// TheoryOfReviewDiffContext.
func buildDiffHunks(oldLines, newLines []string, ops []diffOp) []diffHunk {
	// Find maximal runs of changed (non-equal) ops.
	var runs [][2]int // [start, end) indices into ops
	runStart := -1
	for i, op := range ops {
		if op.kind != diffOpEqual {
			if runStart == -1 {
				runStart = i
			}
		} else if runStart != -1 {
			runs = append(runs, [2]int{runStart, i})
			runStart = -1
		}
	}
	if runStart != -1 {
		runs = append(runs, [2]int{runStart, len(ops)})
	}

	// Extend each run with context and merge overlapping ranges.
	type hunkRange struct{ start, end int }
	var ranges []hunkRange
	for _, run := range runs {
		start := max(0, run[0]-diffContextLines)
		end := min(len(ops), run[1]+diffContextLines)
		if len(ranges) > 0 && start <= ranges[len(ranges)-1].end {
			ranges[len(ranges)-1].end = max(ranges[len(ranges)-1].end, end)
		} else {
			ranges = append(ranges, hunkRange{start, end})
		}
	}

	// Materialize hunks with line numbers and rendered lines.
	hunks := make([]diffHunk, 0, len(ranges))
	for _, r := range ranges {
		hunk := diffHunk{
			lines: make([]string, 0, r.end-r.start),
		}
		first := ops[r.start]
		hunk.oldStart = first.oldIdx + 1
		hunk.newStart = first.newIdx + 1
		for _, op := range ops[r.start:r.end] {
			switch op.kind {
			case diffOpEqual:
				hunk.lines = append(hunk.lines, "  "+oldLines[op.oldIdx])
				hunk.oldCount++
				hunk.newCount++
			case diffOpDelete:
				hunk.lines = append(hunk.lines, "- "+oldLines[op.oldIdx])
				hunk.oldCount++
			case diffOpInsert:
				hunk.lines = append(hunk.lines, "+ "+newLines[op.newIdx])
				hunk.newCount++
			}
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}

// computeDiffOps computes the line-level edit script between oldLines and
// newLines using an LCS table. When the matrix exceeds maxDiffMatrixCells,
// the fallback lists all old lines as deletions followed by all new lines
// as additions. See maxDiffMatrixCells.
func computeDiffOps(oldLines, newLines []string) []diffOp {
	n, m := len(oldLines), len(newLines)
	if n*m > maxDiffMatrixCells {
		ops := make([]diffOp, 0, n+m)
		for i := range n {
			ops = append(ops, diffOp{kind: diffOpDelete, oldIdx: i})
		}
		for j := range m {
			ops = append(ops, diffOp{kind: diffOpInsert, newIdx: j})
		}
		return ops
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			ops = append(ops, diffOp{kind: diffOpEqual, oldIdx: i, newIdx: j})
			i++
			j++
		} else if dp[i][j+1] >= dp[i+1][j] {
			ops = append(ops, diffOp{kind: diffOpInsert, oldIdx: i, newIdx: j})
			j++
		} else {
			ops = append(ops, diffOp{kind: diffOpDelete, oldIdx: i, newIdx: j})
			i++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{kind: diffOpDelete, oldIdx: i, newIdx: m})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{kind: diffOpInsert, oldIdx: n, newIdx: j})
	}
	return ops
}

// diffHunk is a contiguous changed region with surrounding context,
// rendered as a unified diff hunk:
// @@ -oldStart,oldCount +newStart,newCount @@
// followed by the hunk's lines. See TheoryOfReviewDiffContext.
type diffHunk struct {
	oldStart int
	newStart int
	oldCount int
	newCount int
	lines    []string
}

// diffOp is a single line-level diff operation in the edit script between
// old and new content. For equal ops both indices point at the matched
// lines. For delete ops oldIdx is the removed old line and newIdx marks the
// position in the new content where the removal happens. For insert ops
// newIdx is the added new line and oldIdx marks the position in the old
// content where the insertion happens.
type diffOp struct {
	kind   diffOpKind
	oldIdx int
	newIdx int
}

const (
	diffOpEqual  diffOpKind = ' '
	diffOpDelete diffOpKind = '-'
	diffOpInsert diffOpKind = '+'
)

// diffOpKind identifies the kind of a single line-level diff operation.
type diffOpKind byte

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
	if err := s.captureOriginal(path); err != nil {
		return err
	}
	s.files[path] = &memoryFile{content: content, exists: true}
	return nil
}

func (s *MemoryStore) Remove(path string) error {
	if err := s.captureOriginal(path); err != nil {
		return err
	}
	s.files[path] = &memoryFile{exists: false}
	return nil
}

func (s *MemoryStore) Rename(oldPath, newPath string) error {
	if err := s.captureOriginal(oldPath); err != nil {
		return err
	}
	if err := s.captureOriginal(newPath); err != nil {
		return err
	}
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
// recorded original so Diffs always shows the full session delta. A
// DiskChangedError from the underlying store is propagated: the baseline
// must not be recorded from content that no longer matches the snapshot.
func (s *MemoryStore) captureOriginal(path string) error {
	if _, ok := s.originals[path]; ok {
		return nil
	}
	content, err := s.underlying.ReadFile(path)
	if err != nil {
		if _, ok := err.(*DiskChangedError); ok {
			return err
		}
		// A read error (including file not found) means the original
		// state is "does not exist"; the diff will show a new file or
		// the absence of an old one.
		s.originals[path] = &memoryFile{exists: false}
		return nil
	}
	s.originals[path] = &memoryFile{content: slices.Clone(content), exists: true}
	return nil
}

// Diffs returns the accumulated session changes as FileDiff entries,
// comparing the pre-session original state against the current state.
// Paths are sorted for deterministic ordering. A path modified in an
// earlier round and flushed to disk is no longer in s.files (OnRoundStart
// clears it), but its original is retained in s.originals; the current
// state is read from the underlying store so the diff reflects the full
// session delta. No-op diffs (current state matches the original, e.g., a
// change applied in a failed round and rolled back by Reset) are skipped.
// See TheoryOfReviewLoop in codes/generate.go.
func (s *MemoryStore) Diffs() []FileDiff {
	// Collect paths from both the current in-memory files and the session
	// originals. A path modified in an earlier round and flushed to disk
	// is no longer in s.files (OnRoundStart clears it), but its original
	// is retained in s.originals; the current state is read from the
	// underlying store so the diff reflects the full session delta.
	pathSet := make(map[string]bool)
	for path := range s.files {
		pathSet[path] = true
	}
	for path := range s.originals {
		pathSet[path] = true
	}
	paths := slices.Collect(maps.Keys(pathSet))
	slices.Sort(paths)
	var diffs []FileDiff
	for _, path := range paths {
		mf, ok := s.files[path]
		if !ok {
			// Path was modified in an earlier round and flushed to disk.
			// Read the current state from the underlying store.
			content, err := s.underlying.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					mf = &memoryFile{exists: false}
				} else {
					continue
				}
			} else {
				mf = &memoryFile{content: content, exists: true}
			}
		}
		orig := s.originals[path]
		if orig == nil {
			orig = &memoryFile{}
		}
		// Skip no-op diffs: a path whose current state matches its
		// original (e.g., a change applied in a failed round and rolled
		// back by Reset) is not a session change.
		if bytes.Equal(orig.content, mf.content) && orig.exists == mf.exists {
			continue
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
