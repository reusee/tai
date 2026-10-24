package sandboxfs

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

// TheoryOfChunkedContent explains where content lives and why.
const TheoryOfChunkedContent = `
Content lives in fixed-size chunks, one row per contentChunkSize window, so a
change costs the bytes it changes. The files table carries metadata only —
kind, mode, mtime, deleted, size, link target, and the recorded original — and
the chunks table carries the bytes. A write rewrites the chunks its payload
covers and the two chunks it begins and ends inside; a write past the stored
end inserts no zeros, because an absent chunk reads as zeros; a truncation
drops the chunks beyond the new length and trims the boundary chunk. A single
blob column instead rewrote the whole file per window, which made the write
traffic quadratic in the window count — the defect the execution trace and the
profile both pointed at.

The write path's statement count does not grow with the window either. One
statement guards the row and records the new size, so the delta's
admissibility costs no separate lookup, and one more statement carries every
covered chunk, so a window of one chunk and a window of a thousand cost the
same statements. Only a chunk the payload partly covers is read first, and only
for the bytes the payload leaves untouched.

The row is the identity; the chunks are the content. A chunk exists only for a
live regular row: a whiteout, a directory, a link, and a pipe drop their
chunks, and a metadata write keeps them. A mutation that changes content
writes the metadata and the chunks in one transaction, so a reader never sees a
size and a chunk set that disagree, and reads clamp to the stored size, so the
size column is the authority on length.

Reading a window is one indexed query over the chunks covering it, assembled
with zeros for absent chunks: the round-trip count a substr over the blob cost,
without the whole-column rewrite on the write side. The first change to a base
path materialises the base content into chunks, the cold path taken once per
file; a truncation materialises only the prefix it keeps.
`

// contentChunkSize is the length of one stored chunk. It bounds the bytes a
// partly covered window rewrites and the bytes one read query returns per row.
const contentChunkSize = 64 << 10

// errNoLiveRegularRow reports that the upper layer holds no live regular row
// for a path, so a content mutation must materialise it first.
var errNoLiveRegularRow = errors.New("sandboxfs: no live regular row")

// dbExecer is the statement surface shared by *sql.DB and *sql.Tx, so a
// content helper runs inside its caller's transaction.
type dbExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// putContent stores one row's metadata and replaces its content in one
// transaction, so a reader never sees a size and a chunk set that disagree.
func (s *Store) putContent(r fileRow, content []byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.replaceChunks(tx, r.path, content); err != nil {
		return err
	}
	r.size = int64(len(content))
	if err := s.putMetaTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

// putMetaTx writes one row's metadata through the prepared upsert and drops the
// path's chunks unless the row is a live regular file, whose content a prior
// putContent or splice stored. Chunks therefore exist only for live regular
// rows. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (s *Store) putMetaTx(tx *sql.Tx, r fileRow) error {
	s.mu.Lock()
	delete(s.absent, r.path)
	s.mu.Unlock()
	var deleted, originalExists int
	if r.deleted {
		deleted = 1
	}
	if r.originalExists {
		originalExists = 1
	}
	if _, err := runStmt(tx, s.stmts.putMeta, queryPutMeta,
		r.path, int(r.kind), int64(r.mode), r.mtime.UnixNano(), deleted, r.size,
		r.target, originalExists, r.originalHash,
	); err != nil {
		return err
	}
	if r.deleted || r.kind != KindRegular {
		if _, err := runStmt(tx, s.stmts.dropChunks, queryDropChunks, r.path); err != nil {
			return err
		}
	}
	return nil
}

// readChunk returns one chunk, or nil when the chunk is absent, from the
// prepared chunk select. See TheoryOfPreparedStatements.
func (s *Store) readChunk(ex dbExecer, p string, idx int64) ([]byte, error) {
	var data []byte
	err := queryRowStmt(ex, s.stmts.selectChunk, querySelectChunk, p, idx).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

// putChunks writes a run of chunks in one statement: the rows are the values of
// one INSERT, so a window costs one statement whatever its chunk count. A
// single-chunk run takes the prepared upsert, so the hot shape pays no parse;
// the multi-chunk form keeps its dynamic text, because the row count varies and
// its statement would have to be rebuilt per shape.
// See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (s *Store) putChunks(ex dbExecer, p string, first int64, chunks [][]byte) error {
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) == 1 {
		_, err := runStmt(ex, s.stmts.putChunk, queryPutChunk, p, first, chunks[0])
		return err
	}
	var b strings.Builder
	b.WriteString(`INSERT INTO chunks (path, idx, data) VALUES `)
	args := make([]any, 0, len(chunks)*3)
	for i, data := range chunks {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?, ?, ?)")
		args = append(args, p, first+int64(i), data)
	}
	b.WriteString(` ON CONFLICT(path, idx) DO UPDATE SET data = excluded.data`)
	_, err := ex.Exec(b.String(), args...)
	return err
}

// replaceChunks replaces every chunk of p with content: one statement drops the
// old run and one writes the new run, so a whole-file write costs its own bytes
// and a fixed statement count, not one statement per chunk. An empty content
// leaves no chunk at all, so an empty file stores nothing. Both statements run
// the prepared forms through the caller's transaction.
// See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (s *Store) replaceChunks(ex dbExecer, p string, content []byte) error {
	if _, err := runStmt(ex, s.stmts.dropChunks, queryDropChunks, p); err != nil {
		return err
	}
	if len(content) == 0 {
		return nil
	}
	chunks := make([][]byte, 0, (len(content)+contentChunkSize-1)/contentChunkSize)
	for off := 0; off < len(content); off += contentChunkSize {
		chunks = append(chunks, content[off:min(off+contentChunkSize, len(content))])
	}
	return s.putChunks(ex, p, 0, chunks)
}

// readChunks reads the window [off, off+size) of p's stored content in one
// indexed query over the chunks covering it, padding absent chunks with zeros.
// The caller clamps the window to the stored size. The query runs its prepared
// form, so a windowed read pays no SQL parse. See TheoryOfPreparedStatements.
func (s *Store) readChunks(ex dbExecer, p string, off int64, size int) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	end := off + int64(size)
	out := make([]byte, size)
	rows, err := queryStmt(ex, s.stmts.selectChunks, querySelectChunks,
		p, off/contentChunkSize, (end-1)/contentChunkSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var idx int64
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			return nil, err
		}
		base := idx * contentChunkSize
		from := max(off, base)
		to := min(end, base+int64(len(data)))
		if to <= from {
			continue
		}
		copy(out[from-off:to-off], data[from-base:to-base])
	}
	return out, rows.Err()
}

// readContent returns the whole stored content of a live regular path.
func (s *Store) readContent(p string, size int64) ([]byte, error) {
	return s.readChunks(s.db, p, 0, int(size))
}

// spliceChunks writes one payload into the stored content in one transaction.
// One statement both guards the row and extends its size, so the delta's
// admissibility costs no separate lookup, and one more statement carries every
// covered chunk, so a window costs the same statements whatever its chunk
// count. Both statements run their prepared forms, so a window pays no SQL
// parse. A path without a live regular row refuses the splice with
// errNoLiveRegularRow, so the caller materialises the row and retries.
// See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (s *Store) spliceChunks(p string, off int64, data []byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The guard and the size update are one statement: a matched row is a live
	// regular row, and the new end is the window's end when the window extends
	// the stored content. See TheoryOfChunkedContent.
	res, err := runStmt(tx, s.stmts.guardWrite, queryGuardWrite,
		off+int64(len(data)), time.Now().UnixNano(), p, int(KindRegular),
	)
	if err != nil {
		return err
	}
	matched, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if matched == 0 {
		return errNoLiveRegularRow
	}
	if len(data) > 0 {
		if err := s.spliceChunkRange(tx, p, off, data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// spliceChunkRange writes the payload into the chunks it covers. A chunk the
// payload covers whole stores the payload's own bytes — no read of the stored
// chunk, no second copy of the payload — and a chunk the payload only partly
// covers keeps its bytes outside the payload, read once for that purpose. A
// write past the stored end inserts no zeros, because an absent chunk reads as
// zeros, and every covered chunk leaves in one statement.
// See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (s *Store) spliceChunkRange(ex dbExecer, p string, off int64, data []byte) error {
	end := off + int64(len(data))
	first := off / contentChunkSize
	last := (end - 1) / contentChunkSize
	chunks := make([][]byte, 0, last-first+1)
	for idx := first; idx <= last; idx++ {
		base := idx * contentChunkSize
		from := max(off, base)
		to := min(end, base+contentChunkSize)
		lo, hi := from-base, to-base
		payload := data[from-off : to-off]
		if lo == 0 && hi == contentChunkSize {
			// The payload covers the chunk whole, so the payload's bytes are
			// the stored bytes: no read, no copy. A toolchain's windows are
			// multiples of the chunk, so this is the hot shape.
			chunks = append(chunks, payload)
			continue
		}
		existing, err := s.readChunk(ex, p, idx)
		if err != nil {
			return err
		}
		buf := make([]byte, max(int64(len(existing)), hi))
		copy(buf, existing)
		copy(buf[lo:hi], payload)
		chunks = append(chunks, buf)
	}
	return s.putChunks(ex, p, first, chunks)
}

// truncateChunks shapes the stored chunks to a new length: the chunks beyond
// the length drop, and the chunk the length ends inside keeps its prefix.
// Growing writes nothing, because an absent chunk reads as zeros. Both
// statements run their prepared forms. See TheoryOfChunkedContent and
// TheoryOfPreparedStatements.
func (s *Store) truncateChunks(ex dbExecer, p string, size int64) error {
	keep := (size + contentChunkSize - 1) / contentChunkSize
	if _, err := runStmt(ex, s.stmts.dropFrom, queryDropFrom, p, keep); err != nil {
		return err
	}
	if rem := size % contentChunkSize; rem != 0 {
		if _, err := runStmt(ex, s.stmts.trimChunk, queryTrimChunk, rem, p, keep-1); err != nil {
			return err
		}
	}
	return nil
}

// readBaseContent reads a base file's content, bounded by limit when limit is
// non-negative: a truncating first change keeps only the prefix it keeps, so
// it never reads bytes it is about to drop.
func (s *Store) readBaseContent(p string, limit int64) ([]byte, error) {
	f, err := s.baseRoot.Open(relPath(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	if limit < 0 {
		return io.ReadAll(f)
	}
	return io.ReadAll(io.LimitReader(f, min(limit, maxFileSize)))
}

// materializeContent gives p a live regular row whose content starts from the
// base file, so the caller's first change to a base path keeps the bytes it
// does not change. copySize bounds the base content read: a negative value
// takes the whole file, a non-negative one takes that prefix. It is the cold
// path of every content change, taken once per file; a whiteout starts from
// nothing, because resurrecting the deleted bytes would undo the deletion.
func (s *Store) materializeContent(p string, copySize int64, perm os.FileMode) (fileRow, error) {
	if err := s.checkParent(p); err != nil {
		return fileRow{}, err
	}
	r, ok, err := s.metaRow(p)
	if err != nil {
		return fileRow{}, err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return fileRow{}, err
		}
	}
	switch r.kind {
	case KindDir:
		return fileRow{}, syscall.EISDIR
	case KindFIFO, KindSymlink:
		return fileRow{}, syscall.EINVAL
	}
	var content []byte
	if !r.deleted {
		content, err = s.readBaseContent(p, copySize)
		if err != nil {
			return fileRow{}, err
		}
	}
	if perm == 0 {
		perm = 0o644
	}
	r.path = p
	r.kind = KindRegular
	r.mode = perm
	r.deleted = false
	r.target = ""
	r.mtime = time.Now()
	if err := s.putContent(r, content); err != nil {
		return fileRow{}, err
	}
	r.size = int64(len(content))
	return r, nil
}

// TruncateFile sets the stored content length of one regular file: a shrink
// drops the chunks beyond the length and keeps the boundary chunk's prefix,
// and a grow raises the length alone, because an absent chunk reads as zeros.
// Neither path copies content through the caller, so a truncate of a large
// file costs a size update and at most one chunk trim, both prepared.
// See TheoryOfPreparedStatements.
func (s *Store) TruncateFile(p string, size int64, perm os.FileMode) error {
	p = Canonical(p)
	if p == "/" {
		return syscall.EISDIR
	}
	if size < 0 || size > maxFileSize {
		return syscall.EFBIG
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.truncateChunks(tx, p, size); err != nil {
		return err
	}
	if _, err := runStmt(tx, s.stmts.setSize, querySetSize,
		size, time.Now().UnixNano(), p,
	); err != nil {
		return err
	}
	return tx.Commit()
}
