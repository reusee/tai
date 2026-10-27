package sandboxfs

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

const TheoryOfDirectoryStream = `
The mount answers a directory listing in windows, never in one request. A
request that enumerated a whole directory would make its duration grow with
the directory's entry count, and a request's duration keeps one worker of
the request pool busy — the kernel holds the requests that follow behind the
walk — so every other process's file I/O — the certificate bundle a TLS
handshake reads among them — would wait behind that one walk and fail
against a deadline it never set. See TheoryOfFuseMount and
TheoryOfFuseConcurrency.

The cursor is the fix. Opening a listing reads the directory's own attribute,
its parent's, the upper layer's rows for the directory, and one handle on the
base directory that keeps the enumeration position: work bounded by the
directory's depth and the session's own writes, never by its entry count. Each
later call hands out one window — one getdents run and one stat per entry of
that window, merged against the rows already read. A reply therefore costs the
window it carries, and a directory of a million entries costs a million small
requests instead of one long one.

The upper layer's rows arrive in one query per listing, keyed by entry name,
so a merging window never queries per entry. A fresh session holds no row and
skips the query (see TheoryOfUpperAbsentCache). The entries of one window keep
the base's own order; only the store's whole-listing API sorts, because a
caller of that API reads the result as a set.

A mutation the mount serves marks an open listing stale; it does not reset the
stream. The entries already delivered stay addressable by the cookies the
kernel holds, and the cursor keeps its position, so a walk that a create or a
delete interrupts still reports the rest of the entries instead of an early
end. A read that rewinds to the first entry reseeds from the store, so a
re-listing sees the session's own change; a continued read keeps the stream it
started with, because positioning a fresh cursor at an arbitrary index would
walk every entry before it in one request — the cost this design removes — and
a walk that shifted its own offsets mid-way would repeat or skip a name.

The store's own emptiness test reads one window and stops, so removing a large
directory is one window too: whether a directory holds an entry is the first
window's question, never the whole listing's.

The window is a fixed entry count rather than the request's own size: the
kernel announces the negotiated one-megabyte window even for a small
directory, and a count keeps one reply's work independent of how long the
names happen to be.

The window's entries are stat'ed one at a time beneath the listing handle's
own descriptor: one fstatat names an entry without resolving its whole path,
so a window of a deep directory costs its entry count and never that count
times the depth. The entry stat stays because a dirent carries the entry's
type and a READDIRPLUS reply carries its full attribute — the mode, size, and
time a name alone does not hold. See TheoryOfBaseFdAccess.
`

// dirBatchEntries bounds one window of a directory listing: the entries one
// request fetches from the base and stats. It is a count, not a byte budget,
// because the announced request window is far larger than any window the mount
// needs in one reply. See TheoryOfDirectoryStream.
const dirBatchEntries = 256

// dirCursor is one directory listing in progress: the directory's own
// attribute, its parent's, the upper layer's rows for it read once, and the
// base enumeration's position. next hands out one window at a time, so one
// listing request costs the window it carries. See TheoryOfDirectoryStream.
type dirCursor struct {
	store *Store
	path  string

	// self is the directory's own attribute and parent its parent's, both read
	// once by openDir. The mount's reply reports them as "." and "..", so the
	// kind check a listing pays is the attribute the reply carries.
	// See TheoryOfReadPath.
	self      Attr
	parent    Attr
	hasParent bool

	// pending holds the entries the last refill merged and the caller has not
	// taken yet.
	pending []DirEntry

	// upper holds the live upper rows under the directory, by entry name, and
	// deleted the names a whiteout hides. Both are read once by openDir, so a
	// merged window costs no query. See TheoryOfDirectoryStream.
	upper   map[string]dirUpperRow
	deleted map[string]bool

	// base is the base directory's handle, nil when the base holds none; it
	// keeps the enumeration position between windows. baseDone reports that
	// the base's entries are exhausted, and upperDone that the rows the base
	// does not hold have left the queue. See TheoryOfDirectoryStream.
	base      *os.File
	baseDone  bool
	upperDone bool
}

// dirUpperRow is one live upper row of a listing. noSize marks a metadata-only
// regular row, whose length the base holds: the merging window reports the
// base entry's own size, and a row the base no longer holds costs one lookup.
// See TheoryOfBaseCapture.
type dirUpperRow struct {
	attr   Attr
	noSize bool
}

// openDir opens one directory listing without enumerating it. It reads the
// directory's own attribute, its parent's, the upper layer's rows for the
// directory, and the base directory's handle — work bounded by the directory's
// depth and the session's own writes, never by its entry count. The caller
// drains the cursor with next and calls Close. See TheoryOfDirectoryStream.
func (s *Store) openDir(p string) (*dirCursor, error) {
	p = Canonical(p)
	self, err := s.Stat(p)
	if err != nil {
		return nil, err
	}
	if self.Kind != KindDir {
		return nil, syscall.ENOTDIR
	}
	c := &dirCursor{store: s, path: p, self: self}
	// The parent's name is a prefix of the directory's own, so naming it
	// allocates nothing, and its attributes cost one stat.
	// See TheoryOfReadPath.
	if parent, err := s.Stat(parentOf(p)); err == nil {
		c.parent = parent
		c.hasParent = true
	}
	if err := c.readUpper(); err != nil {
		c.Close()
		return nil, err
	}
	c.openBase()
	return c, nil
}

// readUpper reads the upper layer's rows under the cursor's directory once, so
// a merged window costs no query. A fresh session holds no row and skips the
// query. A row's attribute follows the rules the store's stat applies, so a
// window and a stat never disagree on one path.
// See TheoryOfDirectoryStream and TheoryOfUpperLayer.
func (c *dirCursor) readUpper() error {
	rows, err := c.store.upper.rowsUnder(c.path)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	prefix := c.path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	c.upper = make(map[string]dirUpperRow, len(rows))
	c.deleted = make(map[string]bool)
	for _, r := range rows {
		name := strings.TrimPrefix(r.path, prefix)
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		if r.deleted {
			c.deleted[name] = true
			continue
		}
		row := dirUpperRow{attr: attrOfRow(r)}
		// The size column names content, and only a regular file carries it: a
		// link carries its target's length, and a directory or a pipe carries
		// nothing. A metadata-only row keeps the base's own length, which the
		// merging window supplies. See TheoryOfBaseCapture.
		if row.attr.Kind == KindRegular && r.size < 0 {
			row.attr.Size = 0
			row.noSize = true
		}
		c.upper[name] = row
	}
	return nil
}

// openBase opens the base directory once, so later windows resume the
// enumeration instead of reopening it. A directory the base does not hold
// contributes no entry: the upper layer answers it alone. The base's own kind
// is read before the open, because opening a named pipe or a device blocks on
// its peer's data and would hold its worker until the peer writes.
// See TheoryOfDirectoryStream and TheoryOfBaseCapture.
func (c *dirCursor) openBase() {
	c.baseDone = true
	rel := canonicalRel(c.path)
	info, err := c.store.baseRoot.Lstat(rel)
	if err != nil || !info.IsDir() {
		return
	}
	f, err := c.store.baseRoot.Open(rel)
	if err != nil {
		return
	}
	c.base = f
	c.baseDone = false
}

// next hands out the merged listing's next window, at most max entries. An
// empty result reports that the listing is exhausted; the caller owns the
// returned slice and calls next again for the rest. A refill can merge more
// entries than the caller asked for, and the surplus stays pending rather than
// overflowing the caller's window. See TheoryOfDirectoryStream.
func (c *dirCursor) next(max int) ([]DirEntry, error) {
	if max <= 0 {
		return nil, nil
	}
	for len(c.pending) < max {
		more, err := c.refill()
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
	}
	if len(c.pending) == 0 {
		return nil, nil
	}
	out := c.pending
	if len(out) > max {
		out, c.pending = out[:max], out[max:]
	} else {
		c.pending = nil
	}
	return out, nil
}

// refill merges one base window with the upper rows read at open. A base name
// the upper layer hides is dropped, a live row replaces the base's attribute,
// and a row the base does not hold leaves after the base's entries. The report
// is false when both sides are exhausted. See TheoryOfDirectoryStream.
func (c *dirCursor) refill() (bool, error) {
	if !c.baseDone {
		des, err := c.base.ReadDir(dirBatchEntries)
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		if errors.Is(err, io.EOF) {
			c.baseDone = true
		}
		if len(des) > 0 {
			for _, de := range des {
				c.mergeBase(de.Name())
			}
			return true, nil
		}
	}
	if c.upperDone {
		return false, nil
	}
	// The base's entries are exhausted: every remaining live row is a name the
	// base does not hold. See TheoryOfDirectoryStream.
	c.upperDone = true
	for name, row := range c.upper {
		if row.noSize {
			row.attr.Size = c.baseSize(name)
		}
		c.pending = append(c.pending, DirEntry{Name: name, Attr: row.attr})
	}
	clear(c.upper)
	clear(c.deleted)
	return len(c.pending) > 0, nil
}

// mergeBase appends one base entry's merged result to the pending window: a
// whiteout hides it, a live upper row replaces its attribute, and otherwise the
// base's own attribute stands. An entry whose attribute cannot be read is
// skipped, the answer an entry that vanishes mid-walk gets.
// See TheoryOfDirectoryStream.
func (c *dirCursor) mergeBase(name string) {
	if c.deleted[name] {
		delete(c.deleted, name)
		return
	}
	base, ok := c.baseEntry(name)
	if !ok {
		return
	}
	row, live := c.upper[name]
	if !live {
		c.pending = append(c.pending, DirEntry{Name: name, Attr: base})
		return
	}
	delete(c.upper, name)
	if row.noSize {
		row.attr.Size = base.Size
	}
	c.pending = append(c.pending, DirEntry{Name: name, Attr: row.attr})
}

// baseEntry reports one base entry's attribute. The window stats the entry
// beneath the listing handle's own descriptor, so one fstatat names it without
// resolving the entry's whole path: a window of a deep directory costs its
// entry count and never that count times the depth. A store without the fast
// path takes the portable walk unchanged, so a listing reports the same kind
// and mode on both. See TheoryOfBaseFdAccess and TheoryOfDirectoryStream.
func (c *dirCursor) baseEntry(name string) (Attr, bool) {
	if a, ok, err := baseDirEntryStat(c.base, name); ok {
		return a, err == nil
	}
	rel := relPath(childPath(c.path, name))
	if a, ok, err := baseStatAt(c.store.baseFd, rel); ok {
		return a, err == nil
	}
	info, err := c.store.baseRoot.Lstat(rel)
	if err != nil {
		return Attr{}, false
	}
	return attrOf(info), true
}

// baseSize reports the length a metadata-only row takes from the base. A path
// the base no longer holds reports zero, the length the store's own stat
// reports for it. See TheoryOfBaseCapture.
func (c *dirCursor) baseSize(name string) int64 {
	if a, ok := c.baseEntry(name); ok {
		return a.Size
	}
	return 0
}

// Close releases the base directory's handle. It is safe on a cursor the base
// never opened. See TheoryOfDirectoryStream.
func (c *dirCursor) Close() {
	if c.base != nil {
		_ = c.base.Close()
		c.base = nil
	}
}
