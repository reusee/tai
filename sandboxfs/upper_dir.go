package sandboxfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const TheoryOfDirUpper = `
The directory layer is the third upper layer, and the thinnest: one host
directory holds the whole session record as ordinary files. Each recorded
path has one metadata file holding its row as JSON — kind, mode, mtime,
deletion, size, link target, and the recorded original — and, when the path
carries content, one content file holding the bytes. The audit trail is one
append-only log beside them.

The host filesystem does what the SQL layer buys with statements and the
memory layer with maps. A content file is the content: a read window is one
pread, a write window one pwrite, a truncation one ftruncate, and the file's
length the stored length. The layer carries no chunks, no schema, no
transactions, and no negative-lookup cache: a path absent from the record is
one failed file open, which the host's dentry cache answers for every request
after the first. What remains is the metadata encoding, the name mapping, and
the order of the composite operations. See TheoryOfUpperLayer.

A recorded name is the digest of the canonical path with a fixed suffix, not
the path itself. A path's bytes may exceed the 255 a directory entry allows,
and a flat hashed name keeps every entry inside that bound whatever the
session records; the metadata's own path field carries the path back. The
price is a listing: no index sits beside the files, so rowsUnder and allRows
open every metadata file. The record holds the paths the session changed, not
the project's tree, so one listing pays the session's write count and a walk
multiplies the two. A flat directory of files instead of an index is the
layer's trade; an index is the optimization to add when a session's write
count makes the scan visible.

Ordering gives an operation one visible state at a time. A metadata or content
write lands in a fresh file renamed over the old one, so a reader holds either
the old bytes or the new. The content file is committed before the metadata
that sizes it, and a drop removes the metadata first, so a row never outlives
its content or claims bytes that are gone. The layer's mutex is the atomicity
inside the process; across processes a live record is a per-file snapshot,
not one snapshot of the directory, and the review command reads it the way it
reads the SQL record.

Durability is the host's. A write returns once the bytes reach the operating
system, the point the SQL layer's synchronous=NORMAL commit stops at: the
record survives the process, is lost only to a machine crash, and pays no
fsync per mutation.

The record directory is opened once as a root handle, before any mount pivots
the process root, and every later operation resolves through that handle. An
absolute path would name the sandbox view after a pivot, so a record reached
through it would answer its own request — the recursion the mount's private
base view removes for the base, and the open handle removes for the record.
The path still names the record's place for the store's own-record check, so
the directory and everything beneath it stay outside the session's reach.
See TheoryOfFuseMount and TheoryOfSandboxFilesystem.
`

const (
	// dirMetaSuffix names one recorded path's metadata file.
	dirMetaSuffix = ".meta"
	// dirDataSuffix names one content-bearing path's content file.
	dirDataSuffix = ".data"
	// dirAuditName names the append-only audit log.
	dirAuditName = "audit.log"
)

// dirRow is one metadata file's content: the row a sandbox path records, with
// the path itself because the file's name is its digest. See TheoryOfDirUpper.
type dirRow struct {
	Path           string `json:"path"`
	Kind           int    `json:"kind"`
	Mode           uint32 `json:"mode"`
	MTime          int64  `json:"mtime"`
	Deleted        bool   `json:"deleted"`
	Size           int64  `json:"size"`
	Target         string `json:"target"`
	OriginalExists bool   `json:"original_exists"`
	OriginalHash   string `json:"original_hash"`
}

// dirUpper keeps the upper layer in one host directory: one metadata file per
// recorded path, one content file per content-bearing path, and one audit log.
// The host filesystem does the storage, the windowing, and the truncation; the
// layer maps names and orders composite operations. See TheoryOfDirUpper.
type dirUpper struct {
	mu   sync.Mutex
	dir  string
	root *os.Root
	seq  atomic.Uint64
}

var _ upperStore = (*dirUpper)(nil)
var _ selfBackedUpper = (*dirUpper)(nil)

// openDirUpper creates the record directory when absent and opens it once as a
// root handle, so every later operation resolves through the handle instead of
// the record's absolute path. See TheoryOfDirUpper.
func openDirUpper(dir string) (*dirUpper, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sandboxfs: create the record directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("sandboxfs: open the record directory: %w", err)
	}
	return &dirUpper{dir: dir, root: root}, nil
}

func (u *dirUpper) backingPath() string {
	return u.dir
}

func (u *dirUpper) close() error {
	if u.root == nil {
		return nil
	}
	err := u.root.Close()
	u.root = nil
	return err
}

// metaName names one path's metadata file, and dataName its content file: the
// digest of the canonical path keeps the name a fixed length whatever the path
// spells, and the metadata's own path field carries the path back.
// See TheoryOfDirUpper.
func metaName(p string) string {
	return contentHash([]byte(p)) + dirMetaSuffix
}

func dataName(p string) string {
	return contentHash([]byte(p)) + dirDataSuffix
}

// readMeta returns one path's row, or reports that the record holds none.
// A metadata file that does not decode is corruption, not an absent row: a
// mutation that treated it as absent would capture the base state again and
// overwrite what the file recorded. See TheoryOfDirUpper.
func (u *dirUpper) readMeta(p string) (dirRow, bool, error) {
	data, err := u.root.ReadFile(metaName(p))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dirRow{}, false, nil
		}
		return dirRow{}, false, err
	}
	var r dirRow
	if err := json.Unmarshal(data, &r); err != nil {
		return dirRow{}, false, fmt.Errorf("sandboxfs: decode the record of %s: %w", p, err)
	}
	return r, true, nil
}

// readMetaName returns one metadata file's row by its file name. A file that
// does not decode as a row is no record, and a listing skips it rather than
// failing the review on a stray file in the directory. See TheoryOfDirUpper.
func (u *dirUpper) readMetaName(name string) (dirRow, bool, error) {
	data, err := u.root.ReadFile(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dirRow{}, false, nil
		}
		return dirRow{}, false, err
	}
	var r dirRow
	if err := json.Unmarshal(data, &r); err != nil {
		return dirRow{}, false, nil
	}
	return r, true, nil
}

// writeMeta commits one row by renaming a fresh file over the path's metadata
// file, so a reader holds either the old row or the new one.
// See TheoryOfDirUpper.
func (u *dirUpper) writeMeta(p string, r dirRow) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, name, err := u.createTemp()
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = u.root.Rename(name, metaName(p))
	}
	if werr != nil {
		_ = u.root.Remove(name)
		return werr
	}
	return nil
}

// writeData commits one path's content the same way: a fresh file renamed over
// the content file, so a concurrent reader never sees a partly written window.
// See TheoryOfDirUpper.
func (u *dirUpper) writeData(p string, content []byte) error {
	tmp, name, err := u.createTemp()
	if err != nil {
		return err
	}
	_, werr := tmp.Write(content)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = u.root.Rename(name, dataName(p))
	}
	if werr != nil {
		_ = u.root.Remove(name)
		return werr
	}
	return nil
}

// createTemp opens a fresh file in the record directory. The sequence keeps a
// name unique against a temporary file a killed process left behind, and the
// exclusive create turns a collision into the next name instead of into a
// silent overwrite. See TheoryOfDirUpper.
func (u *dirUpper) createTemp() (*os.File, string, error) {
	for range 8 {
		name := fmt.Sprintf("tmp-%d-%d", os.Getpid(), u.seq.Add(1))
		f, err := u.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("sandboxfs: no free temporary name in the record directory")
}

func (u *dirUpper) removeData(p string) error {
	if err := u.root.Remove(dataName(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// metaNames lists the record's metadata file names, filtering the content
// files, the audit log, and a temporary file a killed process left behind.
// See TheoryOfDirUpper.
func (u *dirUpper) metaNames() ([]string, error) {
	f, err := u.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	out := names[:0]
	for _, name := range names {
		if strings.HasSuffix(name, dirMetaSuffix) && len(name) > len(dirMetaSuffix) {
			out = append(out, name)
		}
	}
	return out, nil
}

func fileRowOf(r dirRow) fileRow {
	return fileRow{
		path:           r.Path,
		kind:           Kind(r.Kind),
		mode:           os.FileMode(r.Mode),
		mtime:          time.Unix(0, r.MTime),
		deleted:        r.Deleted,
		size:           r.Size,
		target:         r.Target,
		originalExists: r.OriginalExists,
		originalHash:   r.OriginalHash,
	}
}

func dirRowOf(r fileRow) dirRow {
	return dirRow{
		Path:           r.path,
		Kind:           int(r.kind),
		Mode:           uint32(r.mode.Perm()),
		MTime:          r.mtime.UnixNano(),
		Deleted:        r.deleted,
		Size:           r.size,
		Target:         r.target,
		OriginalExists: r.originalExists,
		OriginalHash:   r.originalHash,
	}
}

func (u *dirUpper) row(p string) (fileRow, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok, err := u.readMeta(p)
	if err != nil || !ok {
		return fileRow{}, false, err
	}
	return fileRowOf(r), true, nil
}

func (u *dirUpper) window(p string, off int64, size int) ([]byte, Kind, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok, err := u.readMeta(p)
	if err != nil {
		return nil, 0, false, err
	}
	if !ok {
		return nil, 0, false, nil
	}
	if r.Deleted {
		return nil, 0, true, ErrNotExist
	}
	if Kind(r.Kind) != KindRegular {
		return nil, Kind(r.Kind), true, nil
	}
	if r.Size < 0 {
		// The row records metadata alone: the base holds the content.
		// See TheoryOfBaseCapture.
		return nil, 0, false, nil
	}
	end := min(off+int64(size), r.Size)
	if off >= end {
		return []byte{}, KindRegular, true, nil
	}
	buf := make([]byte, end-off)
	if err := u.readDataAt(p, buf, off); err != nil {
		return nil, 0, true, err
	}
	return buf, KindRegular, true, nil
}

func (u *dirUpper) content(p string, off int64, size int) ([]byte, error) {
	if size <= 0 {
		return []byte{}, nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	buf := make([]byte, size)
	if err := u.readDataAt(p, buf, off); err != nil {
		return nil, err
	}
	return buf, nil
}

// readDataAt fills buf with the content file's bytes at off. A file shorter
// than the window leaves the rest of buf zeroed, the answer a sparse file
// gives, and a path without a content file reads as all zeros: a grown size
// needs no bytes written. See TheoryOfDirUpper.
func (u *dirUpper) readDataAt(p string, buf []byte, off int64) error {
	f, err := u.root.Open(dataName(p))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (u *dirUpper) rowsUnder(dir string) ([]fileRow, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	names, err := u.metaNames()
	if err != nil {
		return nil, err
	}
	prefix := dir
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var out []fileRow
	for _, name := range names {
		r, ok, err := u.readMetaName(name)
		if err != nil {
			return nil, err
		}
		if !ok || metaName(r.Path) != name || !strings.HasPrefix(r.Path, prefix) {
			continue
		}
		if strings.Contains(r.Path[len(prefix):], "/") {
			continue
		}
		out = append(out, fileRowOf(r))
	}
	return out, nil
}

func (u *dirUpper) allRows() ([]fileRow, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	names, err := u.metaNames()
	if err != nil {
		return nil, err
	}
	out := make([]fileRow, 0, len(names))
	for _, name := range names {
		r, ok, err := u.readMetaName(name)
		if err != nil {
			return nil, err
		}
		if !ok || metaName(r.Path) != name {
			continue
		}
		out = append(out, fileRowOf(r))
	}
	slices.SortFunc(out, func(a, b fileRow) int {
		return strings.Compare(a.path, b.path)
	})
	return out, nil
}

func (u *dirUpper) putRow(r fileRow) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.writeMeta(r.path, dirRowOf(r)); err != nil {
		return err
	}
	if r.deleted || r.kind != KindRegular {
		return u.removeData(r.path)
	}
	return nil
}

func (u *dirUpper) replaceContent(r fileRow, content []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	r.size = int64(len(content))
	// The data file is committed before the metadata that sizes it, so a
	// reader never sees a size whose bytes are not there.
	// See TheoryOfDirUpper.
	if err := u.writeData(r.path, content); err != nil {
		return err
	}
	return u.writeMeta(r.path, dirRowOf(r))
}

func (u *dirUpper) createContent(r fileRow, content []byte) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	cur, ok, err := u.readMeta(r.path)
	if err != nil {
		return false, err
	}
	if ok && !cur.Deleted && Kind(cur.Kind) == KindRegular && cur.Size >= 0 {
		// Another writer's live regular row carries the content, and replacing
		// it would drop what that writer stored. See TheoryOfWriteSplice.
		return false, nil
	}
	r.size = int64(len(content))
	if err := u.writeData(r.path, content); err != nil {
		return false, err
	}
	if err := u.writeMeta(r.path, dirRowOf(r)); err != nil {
		return false, err
	}
	return true, nil
}

func (u *dirUpper) splice(p string, off int64, data []byte, mtime time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok, err := u.readMeta(p)
	if err != nil {
		return err
	}
	if !ok || r.Deleted || Kind(r.Kind) != KindRegular || r.Size < 0 {
		return errNoLiveRegularRow
	}
	f, err := u.root.OpenFile(dataName(p), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteAt(data, off)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if end := off + int64(len(data)); end > r.Size {
		r.Size = end
	}
	r.MTime = mtime.UnixNano()
	return u.writeMeta(p, r)
}

func (u *dirUpper) truncate(p string, size int64, mtime time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok, err := u.readMeta(p)
	if err != nil {
		return err
	}
	if !ok || r.Deleted || Kind(r.Kind) != KindRegular || r.Size < 0 || size < 0 {
		return errNoLiveRegularRow
	}
	if size < r.Size {
		// Only a shrink touches the file: a grown size reads as zeros past the
		// file's end, so recording it costs no bytes. See TheoryOfDirUpper.
		f, err := u.root.OpenFile(dataName(p), os.O_RDWR, 0)
		switch {
		case err == nil:
			terr := f.Truncate(size)
			cerr := f.Close()
			if terr == nil {
				terr = cerr
			}
			if terr != nil {
				return terr
			}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}
	r.Size = size
	r.MTime = mtime.UnixNano()
	return u.writeMeta(p, r)
}

func (u *dirUpper) drop(p string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	// The metadata leaves first, so a row never outlives its content.
	// See TheoryOfDirUpper.
	if err := u.root.Remove(metaName(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return u.removeData(p)
}

func (u *dirUpper) writeAudit(ops []Op) error {
	if len(ops) == 0 {
		return nil
	}
	var buf []byte
	for _, o := range ops {
		data, err := json.Marshal(o)
		if err != nil {
			return err
		}
		buf = append(buf, data...)
		buf = append(buf, '\n')
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	f, err := u.root.OpenFile(dirAuditName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(buf)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

func (u *dirUpper) auditRows() ([]Op, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	data, err := u.root.ReadFile(dirAuditName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Op
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var o Op
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			// A line a killed process left half-written is dropped, the same
			// best-effort stand the batch takes. See TheoryOfAuditBatching.
			continue
		}
		out = append(out, o)
	}
	return out, nil
}
