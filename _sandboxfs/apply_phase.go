package sandboxfs

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// applyRun holds the deferred steps of one apply. A directory's recorded mode
// is its state after the apply, never a gate on what the session put inside:
// the creating phase leaves the owner's bits open, and the final phase
// restores every recorded mode deepest-first. See TheoryOfApplyWriteBits.
type applyRun struct {
	pending []deferredChmod
	// widened records the directories the creating phase already left open, so
	// a later change under the same tree pays no second lookup and, above all,
	// no second mode record: a deferred recorded mode belongs to the final
	// phase and must never be replaced by the bits the creating phase needed.
	// See TheoryOfApplyWriteBits.
	widened map[string]bool
}

// markWidened records that the creating phase already left one directory open.
// See TheoryOfApplyWriteBits.
func (r *applyRun) markWidened(rel string) {
	if r.widened == nil {
		r.widened = make(map[string]bool)
	}
	r.widened[rel] = true
}

// deferredChmod is one directory's final mode, held until every change that
// names a path beneath it has landed. path is the change that owned the
// directory, so a refused mode keeps that change's row for the next attempt.
// See TheoryOfApplyWriteBits.
type deferredChmod struct {
	path string
	rel  string
	perm os.FileMode
}

// chmodFailure is one deferred mode the final phase could not set.
// See TheoryOfApplyWriteBits.
type chmodFailure struct {
	path string
	err  error
}

// add records one directory's final mode.
func (r *applyRun) add(changePath, rel string, perm os.FileMode) {
	r.pending = append(r.pending, deferredChmod{path: changePath, rel: rel, perm: perm})
	r.markWidened(rel)
}

// isWidened reports whether the creating phase already left one directory
// open, either by creating it, by widening it, or by deferring its recorded
// mode. See TheoryOfApplyWriteBits.
func (r *applyRun) isWidened(rel string) bool {
	return r.widened[rel]
}

// finish restores every directory's recorded mode, deepest first, so a parent's
// final bits never refuse a child's chmod. A failure names the change that
// owned the directory. A path the same apply removed, or replaced with another
// kind, holds no mode to restore: the widen that deferred one was the step that
// let the change below reach the host, so reporting a fault there would fail a
// loop the host had accepted and keep a row for work already done.
// See TheoryOfApplyWriteBits.
func (r *applyRun) finish(s *Store) []chmodFailure {
	sort.SliceStable(r.pending, func(i, j int) bool {
		return strings.Count(r.pending[i].rel, "/") > strings.Count(r.pending[j].rel, "/")
	})
	var out []chmodFailure
	for _, d := range r.pending {
		if info, err := s.baseRoot.Lstat(d.rel); err != nil || !info.IsDir() {
			continue
		}
		if err := chmodAt(s.baseRoot, d.rel, d.perm); err != nil {
			out = append(out, chmodFailure{
				path: d.path,
				err:  fmt.Errorf("set the mode of %s: %w", d.path, err),
			})
		}
	}
	r.pending = nil
	return out
}
