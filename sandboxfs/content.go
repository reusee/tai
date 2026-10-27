package sandboxfs

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

const TheoryOfChunkedContent = `
Content lives in fixed-size chunks, one chunk per contentChunkSize bytes, so a
change costs the bytes it changes. The upper layer's rows carry metadata only —
kind, mode, mtime, deleted, size, link target, and the recorded original — and
its chunks carry the bytes; the splice that writes them is
TheoryOfWriteSplice's subject. A single blob column instead rewrote the whole
file per window, which made the write traffic quadratic in the window count —
the defect the execution trace and the profile both pointed at.

The chunk's size is the balance between two measured costs. A partial write
rewrites the chunk it lands inside, so the traffic a small edit costs is the
chunk's size, not the edit's. A bulk window pays one b-tree insert per chunk
it covers in the sqlite layer, so a window of many chunks pays a row count
that grows as the chunk shrinks. Neither extreme wins: a 64 KiB chunk charged
a 4 KiB edit for 128 KiB of traffic, and a one-page chunk made a 1 MiB bulk
window pay 256 row inserts. The store's concurrency lives below the mount, and
small edits dominate the write traffic, so the chosen size favours the partial
write: a small edit stays near the transaction's own cost, and a bulk window
pays its own bytes plus one row (or one map entry) per chunk of payload.

The row is the identity; the chunks are the content. A chunk exists only for a
live regular row: a whiteout, a directory, a link, and a pipe hold no content.
A live regular row holds one of two shapes, and the size column states which: a
stored length means the chunks are the whole content, while the metadata
sentinel means the row records the metadata alone and the content still lives
in the base (see TheoryOfBaseCapture). A mutation that changes content writes
the metadata and the chunks in one atomic step, so a reader never sees a size
and a chunk set that disagree, and reads clamp to the stored size.

Reading a window is one indexed query over the chunks covering it in the sqlite
layer, and one pass over the stored chunks in the memory layer, assembled with
zeros for absent chunks: neither pays the round-trip count a substr over a blob
cost, and neither rewrites a whole column on the write side. The first change
to a base path materialises the base content into chunks, the cold path taken
once per file; a truncation materialises only the prefix it keeps.
`

// captureHashBufferSize is the one buffer a base capture streams through, so
// the recorded original costs this much memory whatever the file's size.
// See TheoryOfBaseCapture.
const captureHashBufferSize = 64 << 10

// contentChunkSize is the length of one stored chunk. See
// TheoryOfChunkedContent.
const contentChunkSize = 4 << 10

// errNoLiveRegularRow reports that the upper layer holds no live regular row
// for a path, so a content mutation must materialise it first.
var errNoLiveRegularRow = errors.New("sandboxfs: no live regular row")

// dbExecer is the statement surface shared by *sql.DB and *sql.Tx, so a
// content helper of the sqlite layer runs inside its caller's transaction.
type dbExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// readContent returns the whole stored content of a live regular path.
func (s *Store) readContent(p string, size int64) ([]byte, error) {
	return s.upper.content(p, 0, int(size))
}

// readBaseContent reads a base file's content, bounded by limit when limit is
// non-negative: a truncating first change keeps only the prefix it keeps, so
// it never reads bytes it is about to drop. A whole-file capture is bounded by
// the store's own limit instead, because the bytes it reads become a stored
// row: a base file above maxFileSize reports EFBIG before a byte reaches
// memory. A base node that is not a regular file reports EINVAL without being
// opened: a device blocks on its peer's data, and the read would swallow what
// it waits for. See TheoryOfBaseCapture.
func (s *Store) readBaseContent(p string, limit int64) ([]byte, error) {
	rel := relPath(p)
	info, err := s.baseRoot.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: the base node holds no content: %s", syscall.EINVAL, p)
	}
	f, err := s.baseRoot.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if limit >= 0 {
		return io.ReadAll(io.LimitReader(f, min(limit, maxFileSize)))
	}
	buf, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxFileSize {
		return nil, syscall.EFBIG
	}
	return buf, nil
}

// hashBaseFile returns the recorded-original digest of one base file without
// holding its content: the file streams through one fixed buffer, and the
// digest covers at most the store's bound. Memory stays the buffer's, and a
// path above maxFileSize costs the prefix the store could hold, never a
// sequential read of the whole file, so the request path stays bounded in
// time as well as in memory. The digest is the one contentHash produces
// for the same bytes, and the apply reads the host through the same bound,
// so both sides of the comparison cover one prefix. A base node that is not a
// regular file reports EINVAL before it is opened, so a character device
// never holds a worker on its peer's data. See TheoryOfBaseCapture.
func hashBaseFile(root *os.Root, rel string) (string, error) {
	info, err := root.Lstat(rel)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: the base node holds no content: %s", syscall.EINVAL, rel)
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyBuffer(h, io.LimitReader(f, maxFileSize),
		make([]byte, captureHashBufferSize)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readBasePrefix returns at most maxFileSize bytes of one base file: the
// apply step reads the host this way, so both sides of the hash comparison
// cover the prefix the capture recorded. A host file above the bound costs the
// same bounded read the capture paid. A file the host no longer holds reports
// no content rather than an error, the same answer readBaseContent gives: the
// conflict check names the vanished host path, and a review that stopped at
// the read would lose every other change beside it. A base node that is not a
// regular file reports EINVAL before it is opened, so the review never blocks
// on a device either. See TheoryOfBaseCapture and TheoryOfApplyConflict.
func readBasePrefix(root *os.Root, rel string) ([]byte, error) {
	info, err := root.Lstat(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: the base node holds no content: %s", syscall.EINVAL, rel)
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxFileSize))
}

// materializeContent gives one path a live regular row holding its content.
// The row keeps the time it recorded, so a change that only touches the mode or
// keeps a prefix never presents the content as freshly written: a caller that
// means to stamp the content sets the time after this returns. The mode is
// applied as given, 000 included: a caller that means to keep an existing mode
// reads it first, so no layer substitutes a default. A live regular row another
// writer created while this call ran is that writer's content, and the returned
// row is the one the caller's delta lands on. See TheoryOfCompletePOSIX,
// TheoryOfWriteSplice and TheoryOfUpperLayer.
func (s *Store) materializeContent(p string, copySize int64, perm os.FileMode) (fileRow, error) {
	if err := s.checkParent(p); err != nil {
		return fileRow{}, err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return fileRow{}, err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return fileRow{}, err
		}
	}
	// A whiteout keeps the kind of the row it replaced, and that kind no
	// longer constrains the path: after the deletion the name is free for a
	// regular file. Only a live row of another kind refuses the write.
	// See TheoryOfCompletePOSIX.
	switch r.kind {
	case KindDir:
		if !r.deleted {
			return fileRow{}, syscall.EISDIR
		}
	case KindFIFO, KindSymlink:
		if !r.deleted {
			return fileRow{}, syscall.EINVAL
		}
	}
	var content []byte
	if !r.deleted {
		content, err = s.readBaseContent(p, copySize)
		if err != nil {
			return fileRow{}, err
		}
	}
	r.path = p
	r.kind = KindRegular
	r.mode = perm
	r.deleted = false
	r.target = ""
	created, err := s.upper.createContent(r, content)
	if err != nil {
		return fileRow{}, err
	}
	if created {
		r.size = int64(len(content))
		return r, nil
	}
	// Another writer created the path's live regular row after the lookup
	// above. That row is the content the caller's delta lands on; replacing it
	// would drop the bytes the writer stored. See TheoryOfWriteSplice.
	cur, found, err := s.row(p)
	if err != nil {
		return fileRow{}, err
	}
	if !found || cur.deleted {
		return fileRow{}, ErrNotExist
	}
	switch cur.kind {
	case KindRegular:
		return cur, nil
	case KindDir:
		return fileRow{}, syscall.EISDIR
	default:
		return fileRow{}, syscall.EINVAL
	}
}

// TruncateFile shapes one regular file to size. The chunks beyond the length
// drop and the chunk the length ends inside keeps its prefix, so the work grows
// with the length, never with the file. A metadata-only row materialises the
// prefix the truncation keeps, so the row becomes content-authoritative without
// ever reading the bytes the truncation drops. A truncation is a mutation, so
// it writes an audit row like every other one. See TheoryOfChunkedContent,
// TheoryOfBaseCapture and TheoryOfSandboxFilesystem.
func (s *Store) TruncateFile(p string, size int64, perm os.FileMode) error {
	p = Canonical(p)
	if p == "/" {
		return syscall.EISDIR
	}
	if err := s.checkWrite(p); err != nil {
		return err
	}
	// A negative length is a malformed request, not a size the store cannot
	// hold: EINVAL, while the bound keeps EFBIG. See TheoryOfCompletePOSIX.
	if size < 0 {
		return syscall.EINVAL
	}
	if size > maxFileSize {
		return syscall.EFBIG
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
		// truncate(2) names an existing file. A path the upper and the base
		// both lack reports ENOENT instead of creating one; the first change
		// to a base file still materialises it, with only the prefix the
		// truncation keeps. See TheoryOfCompletePOSIX.
		orig, err := s.captureOriginal(p)
		if err != nil {
			return err
		}
		if !orig.originalExists {
			return ErrNotExist
		}
		r, err = s.materializeContent(p, size, perm)
		if err != nil {
			return err
		}
	} else if r.kind == KindRegular && !r.deleted && r.size < 0 {
		// The row records metadata only: the content is still in the base, and
		// the truncation materialises only the prefix it keeps.
		// See TheoryOfBaseCapture.
		r, err = s.materializeContent(p, size, perm)
		if err != nil {
			return err
		}
	}
	if r.deleted {
		return ErrNotExist
	}
	switch r.kind {
	case KindDir:
		return syscall.EISDIR
	case KindFIFO, KindSymlink:
		return syscall.EINVAL
	}
	if err := s.upper.truncate(p, size, time.Now()); err != nil {
		return err
	}
	s.audit("truncate", p, fmt.Sprintf("%d bytes", size))
	return nil
}
