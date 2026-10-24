package sandboxfs

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

const TheoryOfChunkedContent = `
Content lives in fixed-size chunks, one row per contentChunkSize bytes, so a
change costs the bytes it changes. The files table carries metadata only —
kind, mode, mtime, deleted, size, link target, and the recorded original — and
the chunks table carries the bytes. A write rewrites the chunks its payload
covers and the two chunks it begins and ends inside; a write past the stored
end inserts no zeros, because an absent chunk reads as zeros; a truncation
drops the chunks beyond the new length and trims the boundary chunk. A single
blob column instead rewrote the whole file per window, which made the write
traffic quadratic in the window count — the defect the execution trace and the
profile both pointed at.

The chunk's size is the balance between two measured costs. A partial write
rewrites the chunk it lands inside, so the traffic a small edit costs is the
chunk's size, not the edit's. A bulk window pays one b-tree insert per chunk
it covers, so a window of many chunks pays a row count that grows as the
chunk shrinks. Neither extreme wins: a 64 KiB chunk charged a 4 KiB edit for
128 KiB of traffic, and a one-page chunk made a 1 MiB bulk window pay 256 row
inserts. The store's concurrency lives below the mount, and small edits dominate
the write traffic, so the chosen size favours the partial write: a small edit
stays near the transaction's own cost, and a bulk window pays its own bytes
plus one row per chunk of payload.

The write path's parse count does not grow with the window either. One
statement guards the row and records the new size, so the delta's
admissibility costs no separate lookup, and one prepared statement carries
each covered chunk, executed once per chunk without a fresh parse. A
multi-row INSERT whose VALUES the window builds instead pays a parse of a
statement whose length grows with the window; the benchmark put the two
shapes within noise of each other, because the prepared form's own
per-execute cost stands close to the parse it saves, so the loop shape is
kept for its simpler code.

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
// Every chunk is one row, so the value trades two measured costs: a partial
// write rewrites the chunk it lands inside, and a bulk write pays one b-tree
// insert per chunk. The store's concurrency lives below the mount, and small
// edits dominate the write traffic, so the value favours the partial write.
// See TheoryOfChunkedContent.
const contentChunkSize = 4 << 10

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
	s.absent.Delete(r.path)
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

// putChunks writes a run of chunks by executing the prepared single-chunk
// upsert once per chunk. The prepared form reaches the insert directly; a
// multi-row INSERT whose VALUES the window builds instead pays a parse of a
// statement whose length grows with the window. The two shapes measured
// within noise of each other — a prepared form's per-execute cost stands
// close to the parse it saves — so the loop is kept for its simpler code and
// one execution path. A single-chunk window, the hot shape of a partial
// write, pays exactly one execute. See TheoryOfChunkedContent and
// TheoryOfPreparedStatements.
func (s *Store) putChunks(ex dbExecer, p string, first int64, chunks [][]byte) error {
	for i, data := range chunks {
		if _, err := runStmt(ex, s.stmts.putChunk, queryPutChunk, p, first+int64(i), data); err != nil {
			return err
		}
	}
	return nil
}

// replaceChunks replaces every chunk of p with content: one statement drops the
// old run, and the new run's chunks leave through the prepared upsert, executed
// once per chunk, so a whole-file write costs its own bytes and no SQL parse
// per chunk. An empty content leaves no chunk at all, so an empty file stores
// nothing. The drop and each upsert run their prepared forms through the
// caller's transaction. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
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
// admissibility costs no separate lookup, and the prepared chunk upsert
// executes once per covered chunk, so a window pays one execute per chunk with
// no SQL parse. A path without a live regular row refuses the splice with
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
// zeros, and every covered chunk leaves through the prepared upsert, executed
// once per chunk. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
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
	// The caller's mode is a default for a path that carries none: the
	// recorded base mode, or the mode an existing row already holds, is the
	// honest source when the caller leaves the choice to the store, so a
	// content or time change never rewrites the permission bits.
	if perm == 0 {
		perm = r.mode
		if perm == 0 {
			perm = 0o644
		}
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
