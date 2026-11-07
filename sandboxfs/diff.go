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
		kindChanged := false
		if r.kind == KindRegular && !r.deleted {
			if r.size >= 0 {
				content, err = s.readContent(r.path, r.size)
			} else {
				// The row records metadata only: the compare uses the base's
				// own prefix. A host path that vanished or turned into a node
				// of another kind has no prefix, and its empty content would
				// compare equal to an empty base file's recorded hash, so the
				// fact travels in kindChanged and detectConflict reports the
				// conflict. See TheoryOfBaseCapture and TheoryOfApplyConflict.
				info, lerr := s.baseRoot.Lstat(relPath(r.path))
				switch {
				case lerr == nil && info.Mode().IsRegular():
					content, err = readBasePrefix(s.baseRoot, relPath(r.path))
				case lerr == nil:
					kindChanged = true
				case errors.Is(lerr, os.ErrNotExist):
					kindChanged = true
				default:
					err = lerr
				}
			}
			if err != nil {
				return nil, err
			}
		}
		var c *Change
		if kindChanged {
			// The recorded snapshot no longer describes the host path: the
			// change is a change of kind, or a vanished host path, which
			// detectConflict reports as a conflict. See TheoryOfApplyConflict.
			c = &Change{Path: r.path, Kind: ChangeModified, Mode: r.mode}
		} else {
			c = netChange(r, content)
			if c == nil {
				c = s.modeOnlyChange(r, content)
			}
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
// decision. Every change is attempted even when an earlier one fails, and the
// failures are collected: one path the host refuses must not keep the other
// changes of the same loop out of the host. A directory's recorded mode lands
// in a final phase, after the entries the session put inside it, so the bits
// never refuse the tree the session built. See TheoryOfApplyConflict,
// TheoryOfApplyFaultIsolation and TheoryOfApplyWriteBits.
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
			// A conflict is an apply that did not happen, so its row names the
			// path, the change's kind, and the host change that blocked it.
			// See TheoryOfAuditOutcome and TheoryOfApplyConflict.
			s.auditFailure("apply", c.Path, string(c.Kind), errors.New(c.Conflict))
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
	// Every change is attempted even when an earlier one fails, and the
	// failures are collected beside the applied changes: the loop's work is the
	// record's whole content, so one path the host refuses must not keep the
	// others out. A failed path keeps its recorded snapshot, so the next apply
	// retries it against the same base. A failure records its own audit row,
	// because the refused path keeps no upper row and the trail is the only
	// record the attempt leaves. See TheoryOfApplyFaultIsolation and
	// TheoryOfAuditOutcome.
	var failures []error
	run := &applyRun{}
	var written []Change
	for _, c := range pending {
		if err := s.applyOne(c, run); err != nil {
			s.auditFailure("apply", c.Path, string(c.Kind), err)
			failures = append(failures, fmt.Errorf("%s %s: %w", c.Kind, c.Path, err))
			continue
		}
		written = append(written, c)
	}
	// The creating phase left every directory open enough for its children to
	// land. The final phase restores the recorded modes deepest-first, and a
	// refused mode keeps its change's row, so the next apply retries it against
	// the same state. See TheoryOfApplyWriteBits.
	failed := make(map[string]bool, len(run.pending))
	for _, f := range run.finish(s) {
		failures = append(failures, f.err)
		failed[f.path] = true
		s.auditFailure("apply", f.path, "", f.err)
	}
	for _, c := range written {
		if failed[c.Path] {
			continue
		}
		if err := s.removeAppliedRow(c.Path); err != nil {
			s.auditFailure("apply", c.Path, string(c.Kind), err)
			failures = append(failures, fmt.Errorf("drop the row of %s after its apply: %w", c.Path, err))
			continue
		}
		s.audit("apply", c.Path, string(c.Kind))
		applied = append(applied, c)
	}
	if len(failures) > 0 {
		return applied, conflicts, errors.Join(failures...)
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

func (s *Store) applyOne(c Change, run *applyRun) error {
	if s.isOwnDBPath(c.Path) {
		return fmt.Errorf("%w: refusing to apply a change to the session database: %s", os.ErrPermission, c.Path)
	}
	rel := relPath(c.Path)
	// The apply has to reach the path to create, rewrite, or remove it, and the
	// host may hold a directory above it at the bits an earlier loop closed it
	// to. See TheoryOfApplyWriteBits.
	if err := s.openAncestors(c.Path, rel, run); err != nil {
		return err
	}
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
		// chmod. A directory defers that chmod to the run's final phase, so its
		// recorded bits never refuse the entries the session put inside it.
		// See TheoryOfSandboxFilesystem and TheoryOfApplyWriteBits.
		switch {
		case c.Dir:
			return s.applyDir(c.Path, rel, perm, run)
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
		restore, err := s.prepareWriteTarget(rel)
		if err != nil {
			return err
		}
		completed := false
		defer func() {
			if !completed && restore != nil {
				_ = restore()
			}
		}()
		if err := s.baseRoot.WriteFile(rel, c.Current, perm); err != nil {
			return err
		}
		if err := chmodAt(s.baseRoot, rel, perm); err != nil {
			return err
		}
		completed = true
		return nil
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

// applyDir writes a directory change. The creating phase leaves the owner's
// bits open and records the mode for the final phase: the paths of one apply
// sort parent-first, so a directory's change runs before the entries the
// session put inside it, and setting the recorded bits here would refuse every
// child of a tree the session populated and then closed. A host path that
// already holds the directory keeps its node, because a mode-only change
// carries no content; every other host kind is replaced. The kernel masks a
// creation's mode with the daemon's umask, so the final phase sets the recorded
// bits explicitly, 000 included. See TheoryOfApplyWriteBits and
// TheoryOfApplyConflict.
func (s *Store) applyDir(changePath, rel string, perm os.FileMode, run *applyRun) error {
	open := perm | 0o700
	info, err := s.baseRoot.Lstat(rel)
	switch {
	case err == nil && info.IsDir():
		// The host already holds the directory: its node stays, and the bits
		// are widened for the duration of the apply so the entries the session
		// put inside can land. See TheoryOfApplyWriteBits.
		if err := chmodAt(s.baseRoot, rel, info.Mode().Perm()|0o700); err != nil {
			return err
		}
	case err == nil:
		if err := s.baseRoot.Remove(rel); err != nil {
			return err
		}
		if err := s.baseRoot.Mkdir(rel, open); err != nil {
			return err
		}
	case errors.Is(err, os.ErrNotExist):
		if err := s.baseRoot.Mkdir(rel, open); err != nil {
			return err
		}
	default:
		return err
	}
	run.add(changePath, rel, perm)
	return nil
}

// openAncestors leaves every directory above one change open enough for the
// apply to reach it, whether the change creates, rewrites, or removes the path,
// and defers each observed mode for the final phase. A directory the session
// closed in an earlier loop keeps its closed bits on the host with no row left
// to say they were the session's own, so the opening cannot depend on the
// directory carrying a change in this apply: the mutation below would answer
// EACCES for an entry the session already completed. A directory this apply
// created or widened keeps its own record, so the recorded mode of a change
// always wins over the widened bits. A directory the host does not hold, or
// holds as another kind, needs no opening: the mutation below reports the
// path's own error. See TheoryOfApplyWriteBits.
func (s *Store) openAncestors(changePath, rel string, run *applyRun) error {
	var chain []string
	for p := path.Dir(rel); p != "." && p != "/"; p = path.Dir(p) {
		chain = append(chain, p)
	}
	// The chain comes out innermost first, and the widening runs outermost
	// first so each level stays reachable while the level below it is read.
	for i := len(chain) - 1; i >= 0; i-- {
		p := chain[i]
		if run.isWidened(p) {
			continue
		}
		info, err := s.baseRoot.Lstat(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}
		perm := info.Mode().Perm()
		if open := perm | 0o700; open != perm {
			if err := chmodAt(s.baseRoot, p, open); err != nil {
				return err
			}
			run.add(changePath, p, perm)
			continue
		}
		run.markWidened(p)
	}
	return nil
}

// prepareWriteTarget makes one host path accept a content write. A node of
// another kind is removed, so the write lands on a fresh node rather than on a
// directory the kernel refuses to open. A regular file whose own bits refuse
// the write gains the owner's write bit for the duration of the write: the
// recorded mode is the state the host holds afterwards and never a gate on the
// write. The returned cleanup restores the original bits after a failed write
// or final chmod; a successful apply replaces them with the recorded mode. An
// absent path and a node that already admits the write need no cleanup.
// See TheoryOfApplyWriteBits and TheoryOfApplyConflict.
func (s *Store) prepareWriteTarget(rel string) (restore func() error, err error) {
	info, err := s.baseRoot.Lstat(rel)
	if err != nil {
		return nil, nil
	}
	if !info.Mode().IsRegular() {
		_ = s.baseRoot.Remove(rel)
		return nil, nil
	}
	original := info.Mode().Perm()
	if original&0o200 == 0 {
		if err := chmodAt(s.baseRoot, rel, original|0o200); err != nil {
			return nil, err
		}
		return func() error {
			return chmodAt(s.baseRoot, rel, original)
		}, nil
	}
	return nil, nil
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

// TheoryOfApplyFaultIsolation is the apply step's fault-isolation rule: one
// path the host refuses must not keep the other changes of the same loop out
// of the host. TheoryOfApplyConflict owns the conflict case; this constant
// owns the failure case, which the conflict check cannot see because the host
// refuses the write itself. See TheoryOfLoopChangeApply in pipeline for what
// the runner does with the outcome.
const TheoryOfApplyFaultIsolation = `
A failed apply is isolated to its path. One path the host refuses — a parent
directory that admits no write, a node the host holds with another kind, a
directory the host refilled after the session deleted it — must not keep the
other changes of the same loop out of the host: an early return would strand
every later change behind one fault, while the loop's work is the record's
whole content. The failures are collected and reported beside the applied
changes, so the caller names every path the host refused. A failed path keeps
its recorded snapshot — its upper row and its content — so a later apply
retries it against the same base instead of losing it.

The applied changes are not rolled back when another path fails. The store is
the session's filesystem state, and the host receives what the store can
serve; one path's fault says nothing about the correctness of the others, and
reverting them would discard work the session completed. Review remains the
place where the user resolves what an apply kept back.
`

const TheoryOfApplyWriteBits = `
The recorded mode is the state the host holds after the write, never a gate on
the write itself. A tool rewrites an existing file by truncating it and writing
the new bytes, and the truncation records the base file's own bits: a read-only
release artifact, or an output an earlier loop left without the owner's write
bit. The store already holds the session's bytes at that point, so an apply that
wrote through the host's own bits would answer EACCES for a change the session
completed, and the row would wait behind a fault the user cannot see. The
owner's write bit is added for the content write and the recorded bits are set
afterwards, so the path keeps its node and every link to it while it still ends
as the mode the session recorded, 000 included.

A directory follows the same rule one level up. The paths of one apply sort
parent-first, so a directory's change runs before the entries the session put
inside it; setting the recorded bits at that moment would refuse every child of
a tree the session populated and then closed, the shape a vendored or released
directory takes. The creating phase therefore leaves the owner's bits open and
records the mode for the final phase, which sets every directory's recorded
bits deepest-first once the whole tree has landed. A refused final mode keeps
its change's row, so the next apply retries it against the same host state.

The opening reaches every ancestor of every change, never only the directories
a change names. A directory the session closed in one loop is applied there and
its row is dropped; the host keeps the closed bits, and the next loop that
writes or removes an entry inside it carries no row to say those bits were the
session's own. An apply that widened only the directories it changed would meet
EACCES on the open of such an entry, report a fault the host never had, and hold
the loop's work back for a retry that meets the same bits. The apply therefore
opens each ancestor it walks through and defers the mode it found, so the final
phase puts the host back exactly where it stood. A directory this apply already
created or widened keeps its own record, so the recorded mode of a change always
wins over the widened bits.

The final phase restores a mode only where a directory still stands. A widen is
what lets the same apply remove the entry it walked to, so a deferred path may
be gone by the time the phase runs, or replaced with the node the session put in
its place. Reporting a fault there would fail a loop the host had accepted and
keep a row for work already done.
`
