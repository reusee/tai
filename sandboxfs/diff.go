package sandboxfs

import (
	"bytes"
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
// content. Mode is the permission bits the upper layer recorded, and an
// apply reproduces them beside the bytes: a mode the session chose must not
// be replaced with a default at the host. Original is the host content the
// session started from, filled when the host still matches that snapshot; a
// conflict leaves it empty. See TheoryOfApplyConflict,
// TheoryOfCompletePOSIX, and TheoryOfSandboxFilesystem.
type Change struct {
	Path     string
	Kind     ChangeKind
	Dir      bool
	Symlink  bool
	FIFO     bool
	Mode     os.FileMode
	Original []byte
	Current  []byte
	Conflict string
}

// Changes returns the net changes of the sandbox in path order. A change
// that a conflict blocks still appears, with its reason: the review step
// shows every change and lets the user decide. The upper layer hands over
// every row's metadata in one call before any content is read, because the
// sqlite layer holds a single connection: a row set kept open while a content
// query runs would wait for the connection it holds. A live regular row's
// content comes from the layer's chunks, and a deletion's from nothing; a
// metadata-only row's content comes from the base's own prefix, the same bound
// the capture hashed, so a permission or time change of a file of any size
// costs that bounded read rather than the file's own length. A live path whose
// content and kind still match the snapshot but whose recorded mode does not is
// a change of mode alone, which the hash cannot see. The upper layer's own
// record never enters the list: it is the review's source, not a reviewable
// change. See TheoryOfSandboxFilesystem, TheoryOfApplyConflict,
// TheoryOfBaseCapture and TheoryOfChunkedContent.
func (s *Store) Changes() ([]Change, error) {
	all, err := s.upper.allRows()
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
			if r.size >= 0 {
				content, err = s.readContent(r.path, r.size)
			} else {
				// The row records metadata only: the compare uses the base's
				// own prefix. See TheoryOfBaseCapture.
				content, err = readBasePrefix(s.baseRoot, relPath(r.path))
			}
			if err != nil {
				return nil, err
			}
		}
		c := netChange(r, content)
		if c == nil {
			c = s.modeOnlyChange(r, content)
		}
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

// ApplyAll writes every net change to the host and reports the changes a
// conflict kept back, so a caller that applies as a session proceeds — the
// goal runner's per-loop apply — can report what still awaits the user's
// decision. See TheoryOfApplyConflict, TheoryOfChunkedContent and
// pipeline.TheoryOfLoopChangeApply.
func (s *Store) ApplyAll() (applied, conflicts []Change, err error) {
	return s.applyChanges(nil)
}

func netChange(r fileRow, content []byte) *Change {
	if r.kind == KindDir {
		switch {
		case r.deleted:
			if r.originalExists {
				return &Change{Path: r.path, Kind: ChangeDeleted, Dir: true, Mode: r.mode}
			}
			return nil
		case !r.originalExists:
			return &Change{Path: r.path, Kind: ChangeAdded, Dir: true, Mode: r.mode}
		case r.originalHash != "":
			// A base directory records no content hash, so a recorded hash
			// means the base held a file, a link, or a pipe and the session
			// put a directory in its place: a change of kind, which the host
			// does not hold yet. See TheoryOfSandboxFilesystem.
			return &Change{Path: r.path, Kind: ChangeModified, Dir: true, Mode: r.mode}
		}
		return nil
	}
	if r.kind == KindFIFO {
		switch {
		case r.deleted:
			if r.originalExists {
				return &Change{Path: r.path, Kind: ChangeDeleted, FIFO: true, Mode: r.mode}
			}
			return nil
		case !r.originalExists:
			return &Change{Path: r.path, Kind: ChangeAdded, FIFO: true, Mode: r.mode}
		case r.originalHash != fifoOriginalHash:
			return &Change{Path: r.path, Kind: ChangeModified, FIFO: true, Mode: r.mode}
		}
		return nil
	}
	if r.deleted {
		if !r.originalExists {
			return nil
		}
		return &Change{Path: r.path, Kind: ChangeDeleted, Symlink: r.kind == KindSymlink, Mode: r.mode}
	}
	if r.kind == KindSymlink {
		// A link's recorded original carries the kind tag, so a link retargeted
		// to the same string is no change while a regular file whose bytes
		// spell that target is one. See TheoryOfSandboxFilesystem.
		target := []byte(r.target)
		if !r.originalExists {
			return &Change{Path: r.path, Kind: ChangeAdded, Symlink: true, Mode: r.mode, Current: target}
		}
		if symlinkOriginalPrefix+contentHash(target) == r.originalHash {
			return nil
		}
		return &Change{Path: r.path, Kind: ChangeModified, Symlink: true, Mode: r.mode, Current: target}
	}
	if !r.originalExists {
		return &Change{Path: r.path, Kind: ChangeAdded, Mode: r.mode, Current: content}
	}
	if contentHash(content) == r.originalHash {
		return nil
	}
	return &Change{Path: r.path, Kind: ChangeModified, Mode: r.mode, Current: content}
}

// modeOnlyChange reports a live path whose content and kind still match the
// snapshot but whose recorded mode does not. A content hash cannot see a
// permission bit, so the comparison reads the host's mode — the same place the
// conflict check reads the host — and a session pays nothing for it until it
// asks what changed. A symbolic link carries no mode of its own, so its
// recorded bits never form a change. See TheoryOfApplyConflict and
// TheoryOfSandboxFilesystem.
func (s *Store) modeOnlyChange(r fileRow, content []byte) *Change {
	if r.deleted || !r.originalExists || r.kind == KindSymlink {
		return nil
	}
	info, err := s.baseRoot.Lstat(relPath(r.path))
	if err != nil || kindOf(info) != r.kind || info.Mode().Perm() == r.mode.Perm() {
		return nil
	}
	c := &Change{Path: r.path, Kind: ChangeModified, Mode: r.mode}
	switch r.kind {
	case KindDir:
		c.Dir = true
	case KindFIFO:
		c.FIFO = true
	default:
		c.Current = content
	}
	return c
}

// detectConflict fills the change's conflict reason when applying it would
// discard a host modification the session never saw, and fills its Original
// with the host content the snapshot recorded. A pipe has no content to
// compare, so its check is the recorded kind against the host's current
// kind; a directory records no content hash either, so the same comparison
// covers a host path that turned into a directory. A symbolic link's recorded
// hash carries the same kind tag its snapshot used. A session deletion whose
// host path is already gone is not a conflict: the two sides agree on the end
// state, so the apply is a no-op. A deletion whose host path still matches
// the snapshot carries the deleted bytes as Original, matching a
// modification. A regular file's host read stops at the store's bound, the
// same bound the capture hashed, so a host file above maxFileSize costs the
// bounded read rather than an allocation of the file's own size; the two
// sides compare one prefix. See TheoryOfApplyConflict and
// TheoryOfBaseCapture.
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
		content, err := readBasePrefix(s.baseRoot, relPath(r.path))
		if err != nil {
			c.Conflict = err.Error()
			return
		}
		if contentHash(content) != r.originalHash {
			c.Conflict = "the host file changed after the session snapshot"
			return
		}
		c.Original = content
	case KindSymlink:
		target, err := s.baseRoot.Readlink(relPath(r.path))
		if err != nil {
			c.Conflict = err.Error()
			return
		}
		if symlinkOriginalPrefix+contentHash([]byte(target)) != r.originalHash {
			c.Conflict = "the host symbolic link changed after the session snapshot"
			return
		}
		c.Original = []byte(target)
	case KindDir:
		// A directory records no content hash, so a recorded hash means the
		// host held a file, a link, or a pipe at the snapshot and turned into
		// a directory since. A recorded emptiness is the ordinary case: the
		// base held a directory too. See TheoryOfApplyConflict.
		if r.originalHash != "" {
			c.Conflict = "the host path changed kind after the session snapshot"
			return
		}
	case KindFIFO:
		// A pipe carries no content; the kind check above is the whole
		// comparison.
	}
}

// Apply writes the selected net changes to the host and drops their upper
// rows, content chunks included, so a repeated apply is a no-op and the next
// diff is exact. Conflicting changes are skipped; selected nil selects every
// change. Deletions apply first, deepest first, then the added and modified
// paths in path order, so a nested creation lands parent-first and a path the
// session turned from a directory into a file meets an emptied host directory.
// See TheoryOfApplyConflict and TheoryOfChunkedContent.
func (s *Store) Apply(selected func(Change) bool) (applied []Change, err error) {
	applied, _, err = s.applyChanges(selected)
	return applied, err
}

// Apply writes the selected net changes to the host and drops their upper
// rows, content chunks included, so a repeated apply is a no-op and the next
// diff is exact. Conflicting changes are skipped; selected nil selects every
// change. Deletions apply first, deepest first, then the added and modified
// paths in path order, so a nested creation lands parent-first and a path the
// session turned from a directory into a file meets an emptied host directory.
// See TheoryOfApplyConflict and TheoryOfChunkedContent.

// applyChanges is Apply with the conflicting changes reported beside the
// applied ones, so ApplyAll can tell its caller what still awaits the user's
// decision. See TheoryOfApplyConflict.
func (s *Store) applyChanges(selected func(Change) bool) (applied, conflicts []Change, err error) {
	// The apply drops the upper rows that showed each change, so its audit row
	// is the only record the write leaves: the trail reaches the upper layer
	// with the apply, never behind a full batch that may never come.
	// See TheoryOfAuditBatching.
	defer s.flushOps()
	changes, err := s.Changes()
	if err != nil {
		return nil, nil, err
	}
	var pending []Change
	for _, c := range changes {
		if selected != nil && !selected(c) {
			continue
		}
		if c.Conflict != "" {
			conflicts = append(conflicts, c)
			continue
		}
		pending = append(pending, c)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		pi, pj := applyPhase(pending[i]), applyPhase(pending[j])
		if pi != pj {
			return pi < pj
		}
		if pi == 0 {
			return len(pending[i].Path) > len(pending[j].Path)
		}
		return pending[i].Path < pending[j].Path
	})
	for _, c := range pending {
		if err := s.applyOne(c); err != nil {
			return applied, conflicts, err
		}
		if err := s.removeAppliedRow(c.Path); err != nil {
			return applied, conflicts, err
		}
		s.audit("apply", c.Path, string(c.Kind))
		applied = append(applied, c)
	}
	return applied, conflicts, nil
}

func applyPhase(c Change) int {
	// Deletions run first, deepest first: a path the session turned from a
	// directory into a file finds the host directory empty when its own
	// change runs, because the session's whiteouts of the directory's
	// entries have already removed the host entries. The path order below
	// keeps a nested creation parent-first. See TheoryOfApplyConflict.
	if c.Kind == ChangeDeleted {
		return 0
	}
	return 1
}

func (s *Store) applyOne(c Change) error {
	if s.isOwnDBPath(c.Path) {
		return fmt.Errorf("%w: refusing to apply a change to the session database: %s", os.ErrPermission, c.Path)
	}
	rel := relPath(c.Path)
	// The mode the row recorded is authoritative, including 000: a session
	// that took every bit away meant exactly that, and a default here would
	// hand the host a mode the session never chose.
	// See TheoryOfCompletePOSIX.
	perm := c.Mode.Perm()
	switch c.Kind {
	case ChangeAdded, ChangeModified:
		// A create takes the mode for a fresh node, and the kernel masks that
		// argument with the daemon's umask; a truncating write into an
		// existing host file keeps the old bits. Both cases lose the mode the
		// row recorded, so every creation below is followed by the explicit
		// chmod. See TheoryOfSandboxFilesystem.
		switch {
		case c.Dir:
			return s.applyDir(rel, perm)
		case c.FIFO:
			return s.applyFIFO(rel, perm)
		case c.Symlink:
			_ = s.baseRoot.Remove(rel)
			return s.baseRoot.Symlink(string(c.Current), rel)
		}
		// A change whose host node is a regular file holding the session's own
		// bytes is a change of mode alone: writing those bytes again would
		// replace the node, break the links to it, and reset its times for a
		// permission bit. The host kind is checked here, not only the bytes: a
		// base symbolic link whose target string spells the session's content
		// compares equal by bytes and is still a change of kind.
		// See TheoryOfApplyConflict.
		if c.Kind == ChangeModified && c.Original != nil && bytes.Equal(c.Original, c.Current) {
			if info, err := s.baseRoot.Lstat(rel); err == nil && info.Mode().IsRegular() {
				return chmodAt(s.baseRoot, rel, perm)
			}
		}
		if info, err := s.baseRoot.Lstat(rel); err == nil && !info.Mode().IsRegular() {
			_ = s.baseRoot.Remove(rel)
		}
		if err := s.baseRoot.WriteFile(rel, c.Current, perm); err != nil {
			return err
		}
		return chmodAt(s.baseRoot, rel, perm)
	case ChangeDeleted:
		err := s.baseRoot.Remove(rel)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return fmt.Errorf("sandboxfs: unknown change kind %q", c.Kind)
}

// applyFIFO writes a named-pipe change: a host pipe keeps its node, so an open
// end survives a mode-only change, and every other host kind is replaced. The
// recorded mode is applied as recorded, 000 included.
// See TheoryOfCompletePOSIX and TheoryOfApplyConflict.
func (s *Store) applyFIFO(rel string, perm os.FileMode) error {
	info, err := s.baseRoot.Lstat(rel)
	switch {
	case err == nil && kindOf(info) == KindFIFO:
		// The host already holds the pipe: only the bits are left.
	case err == nil:
		if err := s.baseRoot.Remove(rel); err != nil {
			return err
		}
		if err := mkfifoAt(s.baseRoot, rel, perm); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		if err := mkfifoAt(s.baseRoot, rel, perm); err != nil {
			return err
		}
	default:
		return err
	}
	return chmodAt(s.baseRoot, rel, perm)
}

// applyDir writes a directory change: a host path that already holds the
// directory keeps its node, because a mode-only change carries no content and
// only the bits are left to set, and every other host kind is replaced. The
// recorded mode is set explicitly either way, because the kernel masks a
// creation's mode with the daemon's umask, and the mode is applied as recorded,
// 000 included. See TheoryOfApplyConflict and TheoryOfSandboxFilesystem.
func (s *Store) applyDir(rel string, perm os.FileMode) error {
	info, err := s.baseRoot.Lstat(rel)
	switch {
	case err == nil && info.IsDir():
		// The host already holds the directory: only the bits are left.
	case err == nil:
		if err := s.baseRoot.Remove(rel); err != nil {
			return err
		}
		if err := s.baseRoot.Mkdir(rel, perm); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		if err := s.baseRoot.Mkdir(rel, perm); err != nil {
			return err
		}
	default:
		return err
	}
	return chmodAt(s.baseRoot, rel, perm)
}

// removeAppliedRow drops one applied path's upper row and its content, so the
// next diff is exact. See TheoryOfChunkedContent and TheoryOfUpperLayer.
func (s *Store) removeAppliedRow(p string) error {
	return s.upper.drop(p)
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

// chmodAt sets one path's permission bits beneath root without opening the
// target: a named pipe would block an open, and a mode with no read bit
// refuses an open of its own file. The parent directory is opened through
// root, so the fchmodat stays contained, and the final component is a node
// the caller just created, never a symbolic link the call could follow.
// See TheoryOfSandboxFilesystem.
func chmodAt(root *os.Root, rel string, mode os.FileMode) error {
	dir := path.Dir(rel)
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Fchmodat(int(f.Fd()), path.Base(rel), uint32(mode.Perm()), 0)
}
