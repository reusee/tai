package sandboxfs

import (
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"syscall"

	"golang.org/x/sys/unix"
)

// ChangeKind classifies one net change of the sandbox relative to the host
// state the session started from.
type ChangeKind string

const (
	// ChangeAdded marks a path the session created.
	ChangeAdded ChangeKind = "added"
	// ChangeModified marks a path whose content the session changed.
	ChangeModified ChangeKind = "modified"
	// ChangeDeleted marks a path the session deleted.
	ChangeDeleted ChangeKind = "deleted"
)

// Change is one net filesystem change of the sandbox. Dir, Symlink, and
// FIFO distinguish the added or modified kinds that carry no regular
// content. Original is the host content the session started from, filled
// when the host still matches that snapshot; a conflict leaves it empty.
// See TheoryOfApplyConflict and TheoryOfCompletePOSIX.
type Change struct {
	Path     string
	Kind     ChangeKind
	Dir      bool
	Symlink  bool
	FIFO     bool
	Original []byte
	Current  []byte
	Conflict string
}

// Changes returns the net changes of the sandbox in path order. A change
// that a conflict blocks still appears, with its reason: the review step
// shows every change and lets the user decide. Every upper row's metadata
// leaves one query before any content is read, because the store holds a
// single connection: a row set kept open while a content query runs would wait
// for the connection it holds. A live regular row's content comes from the
// chunk table, and a deletion's from nothing. The store's own database and its
// SQLite siblings never enter the list: they are the review's record, not a
// reviewable change. See TheoryOfSandboxFilesystem, TheoryOfApplyConflict, and
// TheoryOfChunkedContent.
func (s *Store) Changes() ([]Change, error) {
	rows, err := s.db.Query(
		`SELECT path, kind, mode, mtime, deleted, size, target, original_exists, original_hash
		 FROM files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	var all []fileRow
	for rows.Next() {
		r, err := scanFileRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, r := range all {
		if s.isOwnDBPath(r.path) {
			continue
		}
		var content []byte
		if r.kind == KindRegular && !r.deleted {
			content, err = s.readContent(r.path, r.size)
			if err != nil {
				return nil, err
			}
		}
		c := netChange(r, content)
		if c == nil {
			continue
		}
		s.detectConflict(c, r)
		changes = append(changes, *c)
	}
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})
	return changes, nil
}

// netChange derives one path's net change from its recorded original and its
// current upper state, with the current content supplied by the caller because
// it lives in the chunk table. Directories and pipes carry no content, so only
// their existence and kind count; a rewritten-to-identical file and a
// create-then-delete pair yield no change at all.
// See TheoryOfSandboxFilesystem and TheoryOfCompletePOSIX.
func netChange(r fileRow, content []byte) *Change {
	if r.kind == KindDir {
		switch {
		case r.deleted:
			if r.originalExists {
				return &Change{Path: r.path, Kind: ChangeDeleted, Dir: true}
			}
			return nil
		case !r.originalExists:
			return &Change{Path: r.path, Kind: ChangeAdded, Dir: true}
		}
		return nil
	}
	if r.kind == KindFIFO {
		switch {
		case r.deleted:
			if r.originalExists {
				return &Change{Path: r.path, Kind: ChangeDeleted, FIFO: true}
			}
			return nil
		case !r.originalExists:
			return &Change{Path: r.path, Kind: ChangeAdded, FIFO: true}
		case r.originalHash != fifoOriginalHash:
			return &Change{Path: r.path, Kind: ChangeModified, FIFO: true}
		}
		return nil
	}
	if r.deleted {
		if !r.originalExists {
			return nil
		}
		return &Change{Path: r.path, Kind: ChangeDeleted, Symlink: r.kind == KindSymlink}
	}
	current := content
	if r.kind == KindSymlink {
		current = []byte(r.target)
	}
	if !r.originalExists {
		return &Change{Path: r.path, Kind: ChangeAdded, Symlink: r.kind == KindSymlink, Current: current}
	}
	if contentHash(current) == r.originalHash {
		return nil
	}
	return &Change{Path: r.path, Kind: ChangeModified, Symlink: r.kind == KindSymlink, Current: current}
}

// detectConflict fills the change's conflict reason when applying it would
// discard a host modification the session never saw. A pipe has no content
// to compare, so its check is the recorded kind against the host's current
// kind. A session deletion whose host path is already gone is not a conflict:
// the two sides agree on the end state, so the apply is a no-op.
// See TheoryOfApplyConflict and TheoryOfCompletePOSIX.
func (s *Store) detectConflict(c *Change, r fileRow) {
	if !r.originalExists {
		if _, err := s.baseRoot.Lstat(relPath(r.path)); err == nil {
			c.Conflict = "the host now holds a path here that the session did not see"
		}
		return
	}
	info, err := s.baseRoot.Lstat(relPath(r.path))
	if err != nil {
		if c.Kind == ChangeDeleted && errors.Is(err, os.ErrNotExist) {
			// Both sides deleted the path; applying is a no-op.
			return
		}
		c.Conflict = "the host path the change was computed against no longer exists"
		return
	}
	if (r.originalHash == fifoOriginalHash) != (kindOf(info) == KindFIFO) {
		c.Conflict = "the host path changed kind after the session snapshot"
		return
	}
	switch kindOf(info) {
	case KindRegular:
		content, err := s.baseRoot.ReadFile(relPath(r.path))
		if err != nil {
			c.Conflict = err.Error()
			return
		}
		if contentHash(content) != r.originalHash {
			c.Conflict = "the host file changed after the session snapshot"
			return
		}
		if c.Kind != ChangeDeleted {
			c.Original = content
		}
	case KindSymlink:
		target, err := s.baseRoot.Readlink(relPath(r.path))
		if err != nil {
			c.Conflict = err.Error()
			return
		}
		if contentHash([]byte(target)) != r.originalHash {
			c.Conflict = "the host symbolic link changed after the session snapshot"
			return
		}
		if c.Kind != ChangeDeleted {
			c.Original = []byte(target)
		}
	case KindFIFO:
		// A pipe carries no content; the kind check above is the whole
		// comparison.
	}
}

// Apply writes the selected net changes to the host and drops their upper
// rows, content chunks included, so a repeated apply is a no-op and the next
// diff is exact. Conflicting changes are skipped and reported alongside the
// applied ones; selected nil selects every change. Created directories apply
// first, files next, and deletions deepest first, so a nested creation or
// deletion lands in an order the host accepts. See TheoryOfApplyConflict and
// TheoryOfChunkedContent.
func (s *Store) Apply(selected func(Change) bool) (applied []Change, err error) {
	changes, err := s.Changes()
	if err != nil {
		return nil, err
	}
	var pending []Change
	for _, c := range changes {
		if selected != nil && !selected(c) {
			continue
		}
		if c.Conflict != "" {
			continue
		}
		pending = append(pending, c)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		pi, pj := applyPhase(pending[i]), applyPhase(pending[j])
		if pi != pj {
			return pi < pj
		}
		switch pi {
		case 0:
			return len(pending[i].Path) < len(pending[j].Path)
		case 2:
			return len(pending[i].Path) > len(pending[j].Path)
		}
		return pending[i].Path < pending[j].Path
	})
	for _, c := range pending {
		if err := s.applyOne(c); err != nil {
			return applied, err
		}
		if err := s.removeAppliedRow(c.Path); err != nil {
			return applied, err
		}
		s.audit("apply", c.Path, string(c.Kind))
		applied = append(applied, c)
	}
	return applied, nil
}

func applyPhase(c Change) int {
	if c.Kind == ChangeAdded && c.Dir {
		return 0
	}
	if c.Kind != ChangeDeleted {
		return 1
	}
	return 2
}

// applyOne writes one selected change to the host. A path whose host entry
// is not the kind the change replaces is removed first, so a regular write
// never lands on a pipe or a link. The store's own database is a hard
// refusal even if a stale row somehow reached this call: writing it would
// corrupt the record the review depends on. A delete of a host path that is
// already gone is a no-op, matching detectConflict's agreement case.
// See TheoryOfCompletePOSIX and TheoryOfSandboxFilesystem.
func (s *Store) applyOne(c Change) error {
	if s.isOwnDBPath(c.Path) {
		return fmt.Errorf("%w: refusing to apply a change to the session database: %s", os.ErrPermission, c.Path)
	}
	rel := relPath(c.Path)
	switch c.Kind {
	case ChangeAdded, ChangeModified:
		if c.Dir {
			return s.baseRoot.Mkdir(rel, 0o755)
		}
		if c.FIFO {
			_ = s.baseRoot.Remove(rel)
			return mkfifoAt(s.baseRoot, rel, 0o644)
		}
		if c.Symlink {
			_ = s.baseRoot.Remove(rel)
			return s.baseRoot.Symlink(string(c.Current), rel)
		}
		if info, err := s.baseRoot.Lstat(rel); err == nil && !info.Mode().IsRegular() {
			_ = s.baseRoot.Remove(rel)
		}
		return s.baseRoot.WriteFile(rel, c.Current, 0o644)
	case ChangeDeleted:
		err := s.baseRoot.Remove(rel)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return fmt.Errorf("sandboxfs: unknown change kind %q", c.Kind)
}

// removeAppliedRow drops one applied path's upper row and its content chunks in
// one transaction, so the next diff is exact. See TheoryOfChunkedContent.
func (s *Store) removeAppliedRow(p string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM files WHERE path = ?`, p); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE path = ?`, p); err != nil {
		return err
	}
	return tx.Commit()
}

// mkfifoAt creates a named pipe beneath root. os.Root has no mkfifo, so the
// node is created through the parent directory's descriptor with MKNODAT,
// keeping the write inside the root.
func mkfifoAt(root *os.Root, rel string, mode os.FileMode) error {
	dir := path.Dir(rel)
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Mknodat(int(f.Fd()), path.Base(rel),
		uint32(syscall.S_IFIFO)|uint32(mode.Perm()), 0)
}
