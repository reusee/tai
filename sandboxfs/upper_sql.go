package sandboxfs

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	// The sqlite driver is linked here because the default constructor opens
	// it. A DuckDB driver is the application's own import: this package names
	// the driver and links none, so an environment that never selects the
	// DuckDB dialect pays no cgo and no native library.
	// See TheoryOfSqlDialects.
	_ "modernc.org/sqlite"
)

const TheoryOfSqlUpper = `
The SQL layer is one durable upper layer: one database file holds the rows, the
content chunks, and the audit trail, so a session's filesystem state is one
portable file the review command browses and applies later. It is opt-in — the
tai tool's default record is the directory layer — and a caller selects it by
naming the record. Its dialect decides the engine that holds the file — sqlite
by default, DuckDB on request — and supplies the driver, the schema, and the
two statements whose syntax differs (see TheoryOfSqlDialects). Its connection,
its prepared statement set, and its negative-lookup cache live here, beside the
row and content operations they serve (see TheoryOfUpperLayer). The layers'
shared cost model is TheoryOfChunkedContent's.

The connection is opened once, for the process lifetime. The database file may
sit inside the filesystem the mount serves, and a connection opened later would
resolve its own path through that filesystem — a deadlock with the server
answering the open. The schema commit also creates the record's sibling files
(SQLite's -wal and -shm, DuckDB's .wal) while they are still reachable. See
TheoryOfFuseMount.

Every write statement the request path runs is prepared at open, so no request
pays a SQL parse (see TheoryOfPreparedStatements), and a fresh session answers
every lookup from the base without asking this layer at all (see
TheoryOfUpperAbsentCache). A dialect's driver must serve prepared statements:
one that refuses them fails the open, which is the honest report, because the
layer would otherwise pay a parse per statement silently.
`

const TheoryOfUpperAbsentCache = `
The read path is the hot path, and most of its paths never enter the upper
layer: a dependency the session only reads is answered by the base. An
uncached negative lookup pays a database round trip for every one of them, and
that round trip dominates a read request which otherwise costs one base read.

A fresh session starts with an empty table, so the per-path record is not the
cheapest statement of that fact: one flag says the whole layer holds no row,
and a lookup that reads it answers from the base without touching the database.
The flag is set once, when the layer opens and finds the files table empty, and
the first metadata write clears it, because the metadata upsert is the only
statement that ever inserts a row. A directory listing reads the same flag and
skips its select, which saves a query per directory of a walk that never wrote
one. A resumed session finds rows and takes the per-path cache, so the flag
changes nothing for a store that already holds work.

The cache therefore records the paths a lookup found absent, and clears a
path's entry whenever the layer writes the path. The metadata upsert is the
only writer of the table — a row appears or changes only through it — so a
cleared entry is refreshed on the next lookup, and a cached absence states
exactly what the table said when it was recorded. The apply step only removes
rows, which can make an absence more true, never less.

A lookup and the write that invalidates it run on different goroutines, so the
record must lose to a write that intervened. The lookup stamps itself with the
write epoch before it queries, and the write moves the epoch and clears the
entry under one lock; a record whose stamp no longer holds writes nothing. The
stamp alone would leave a window between the check and the store, so the two
writers of the cache share a mutex — taken once per path, never on the lookup
itself. Without that order a lookup that missed a row just before a concurrent
write committed would hide that row for the rest of the session, because
nothing else clears an entry.

The cache is bounded by the paths a session touches, and the audit trail
already spends one record per such path, so it adds no memory the session did
not already spend. It is a sync.Map, so a lookup runs without a lock: the read
path consults it on every request, and a shared mutex would serialize every
reader against the first-read audit. The first-read record uses the same form
— one lock-free Load per read, and a LoadOrStore only for the first — because
the audit keeps one record per path, not one per read.

The memory layer keeps no absence cache: its row lookup is one map access
under its own mutex, so a miss already costs nothing a cache could remove.
See TheoryOfUpperLayer.
`

// queryAllRows runs once per diff and queryAuditRows once per trail read, so
// both keep their SQL text instead of a prepared form: a statement prepared
// for a single call pays the parse it would have paid anyway. Likewise
// queryDropRow runs once per applied change. See TheoryOfPreparedStatements.
const (
	queryAllRows = `SELECT path, kind, mode, mtime, deleted, size, target, original_exists, original_hash
		 FROM files ORDER BY path`
	queryAuditRows = `SELECT time, op, path, detail FROM ops ORDER BY id`
	queryDropRow   = `DELETE FROM files WHERE path = ?`
)

// selfBackedUpper reports the file an upper layer records itself in, so the
// store never mutates its own record. A memory layer does not implement it:
// nothing it writes is a file. See TheoryOfSandboxFilesystem.
type selfBackedUpper interface {
	backingPath() string
}

// sqlUpper is the durable upper layer: one SQL database file records the
// session's rows, content chunks, and audit trail, and the layer's dialect
// decides the engine that holds it. See TheoryOfUpperLayer and
// TheoryOfSqlUpper.
type sqlUpper struct {
	db      *sql.DB
	dbPath  string
	dialect sqlDialect

	// absent caches the paths the layer is known not to hold. A write of the
	// path clears its entry. See TheoryOfUpperAbsentCache.
	absent sync.Map // p string -> bool

	// absentMu serialises the cache's two writers: the record a lookup makes
	// and the invalidation a write makes. A lookup still reads the map without
	// a lock; the lock is taken once per path, never once per request.
	// See TheoryOfUpperAbsentCache.
	absentMu sync.Mutex

	// epoch counts the metadata writes the layer received. A lookup stamps
	// itself with the value before it queries and records an absence only
	// while the stamp still holds, so an absence found before a concurrent
	// write is never recorded after it. See TheoryOfUpperAbsentCache.
	epoch atomic.Uint64

	// empty reports that the layer holds no row at all, so every lookup is
	// answered by the base without a database round trip.
	// See TheoryOfUpperAbsentCache.
	empty atomic.Bool

	// stmts is the statement set the request path executes. It is a value, so
	// a layer built without preparation holds nil statements and every helper
	// falls back to its SQL text. See TheoryOfPreparedStatements.
	stmts preparedStatements
}

var _ upperStore = (*sqlUpper)(nil)

func (u *sqlUpper) backingPath() string {
	return u.dbPath
}

// openSQLUpper opens the durable upper layer at dbPath with the given dialect:
// the dialect names the driver, builds the DSN, creates the schema, and
// supplies the two statements whose syntax the engines spell differently. See
// TheoryOfSqlUpper and TheoryOfSqlDialects.
func openSQLUpper(dialect sqlDialect, dbPath string) (*sqlUpper, error) {
	db, err := sql.Open(dialect.driver, dialect.dsn(dbPath))
	if err != nil {
		return nil, fmt.Errorf("sandboxfs: open %s record: %w", dialect.name, err)
	}
	// One connection for the process lifetime. See TheoryOfSqlUpper.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sandboxfs: open %s record: %w", dialect.name, err)
	}
	// The schema runs one statement at a time, so a driver that refuses a
	// multi-statement Exec is still served, and a dialect's sequence is
	// created before the table whose insert names it. See TheoryOfSqlDialects.
	for _, stmt := range dialect.schema {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("sandboxfs: %s schema: %w", dialect.name, err)
		}
	}
	u := &sqlUpper{db: db, dbPath: dbPath, dialect: dialect}
	// The statement set is prepared before the layer serves a request, so no
	// request pays a parse or waits for a preparation.
	// See TheoryOfPreparedStatements.
	if err := u.prepareStatements(); err != nil {
		db.Close()
		return nil, err
	}
	// The whole-layer shortcut is decided once, before the layer serves a
	// request: a database that already holds rows takes the per-path cache
	// instead. See TheoryOfUpperAbsentCache.
	if err := u.detectEmpty(); err != nil {
		db.Close()
		return nil, err
	}
	return u, nil
}

// openSQLiteUpper opens the durable upper layer with the sqlite dialect, the
// default record. See TheoryOfSqlDialects.
func openSQLiteUpper(dbPath string) (*sqlUpper, error) {
	return openSQLUpper(sqliteDialect, dbPath)
}

// openDuckDBUpper opens the durable upper layer with the DuckDB dialect. The
// binary must have imported a DuckDB driver; without one the open reports the
// unknown driver and names the dialect. See TheoryOfSqlDialects.
func openDuckDBUpper(dbPath string) (*sqlUpper, error) {
	return openSQLUpper(duckDBDialect, dbPath)
}

// upperAbsent reports whether the layer is known not to hold p. A missing
// entry says nothing: the caller must ask the database. An empty layer answers
// for every path at once, so a fresh session reaches the base without a query.
// See TheoryOfUpperAbsentCache.
func (u *sqlUpper) upperAbsent(p string) bool {
	if u.empty.Load() {
		return true
	}
	_, ok := u.absent.Load(p)
	return ok
}

// detectEmpty records whether the layer holds no row, so every lookup of a
// fresh session costs no database round trip. It runs once, from the open
// path, before the layer serves a request. A database that already holds rows
// takes the per-path cache instead, so a resumed session reads exactly as it
// did before. See TheoryOfUpperAbsentCache.
func (u *sqlUpper) detectEmpty() error {
	var one int
	err := u.db.QueryRow(`SELECT 1 FROM files LIMIT 1`).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		u.empty.Store(true)
		return nil
	case err != nil:
		return err
	}
	return nil
}

// rememberAbsent records that the layer does not hold p, so the next lookup of
// the path costs no database round trip. A write of the path clears the entry,
// and a write that committed after epoch was read keeps the entry from being
// recorded at all: that write's row is what the lookup must serve.
// See TheoryOfUpperAbsentCache.
func (u *sqlUpper) rememberAbsent(p string, epoch uint64) {
	u.absentMu.Lock()
	defer u.absentMu.Unlock()
	if u.epoch.Load() != epoch {
		return
	}
	u.absent.Store(p, true)
}

// markWritten invalidates what the absence cache says about one written path.
// The row exists from this write on, so a lookup that found the path absent
// before it must not record that absence after it. The epoch moves and the
// entry clears under one lock, so neither writer of the cache can interleave
// with the other. The call runs before the transaction commits, so no later
// lookup can pass the stamp check in the interval between the commit and the
// invalidation. See TheoryOfUpperAbsentCache.
func (u *sqlUpper) markWritten(p string) {
	u.absentMu.Lock()
	defer u.absentMu.Unlock()
	u.epoch.Add(1)
	u.absent.Delete(p)
	u.empty.Store(false)
}

// row reads one path's full row from the prepared row statement, so a
// mutation's lookup pays no SQL parse. The scan reads metadata only: content
// lives in the chunk table, and a write already holds the bytes it is about to
// store, so a read that assembled content would copy a file the write is about
// to discard. See TheoryOfChunkedContent, TheoryOfPreparedStatements and
// TheoryOfUpperAbsentCache.
func (u *sqlUpper) row(p string) (fileRow, bool, error) {
	if u.upperAbsent(p) {
		return fileRow{}, false, nil
	}
	// The stamp is read before the query: a write that commits between the
	// query and the record below moves the epoch, and the absence is then not
	// recorded. See TheoryOfUpperAbsentCache.
	epoch := u.epoch.Load()
	r, err := scanFileRow(queryRowStmt(u.db, u.stmts.selectRow, querySelectRow, p))
	if errors.Is(err, sql.ErrNoRows) {
		u.rememberAbsent(p, epoch)
		return fileRow{}, false, nil
	}
	if err != nil {
		return fileRow{}, false, err
	}
	return r, true, nil
}

// window reads one window of a path the layer holds, clamped to the stored
// size. A metadata-only row — the size sentinel saying the content still lives
// in the base — reports found false, so the caller serves the window from the
// base. See TheoryOfBaseCapture, TheoryOfPreparedStatements and
// TheoryOfUpperAbsentCache.
func (u *sqlUpper) window(p string, off int64, size int) ([]byte, Kind, bool, error) {
	if u.upperAbsent(p) {
		return nil, 0, false, nil
	}
	// The stamp is read before the query, so a write that commits in between
	// keeps its row from being hidden by the absence recorded below.
	// See TheoryOfUpperAbsentCache.
	epoch := u.epoch.Load()
	var kind, deleted int
	var stored int64
	err := queryRowStmt(u.db, u.stmts.selectUpper, querySelectUpper, p).Scan(&kind, &deleted, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		u.rememberAbsent(p, epoch)
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if deleted != 0 {
		return nil, 0, true, ErrNotExist
	}
	if Kind(kind) != KindRegular {
		return nil, Kind(kind), true, nil
	}
	if stored < 0 {
		// The row records metadata only: its content still lives in the base.
		// See TheoryOfBaseCapture.
		return nil, 0, false, nil
	}
	end := min(off+int64(size), stored)
	if off >= end {
		return []byte{}, KindRegular, true, nil
	}
	data, err := u.readChunks(u.db, p, off, int(end-off))
	if err != nil {
		return nil, 0, true, err
	}
	return data, KindRegular, true, nil
}

// content returns one live regular row's stored content window. The caller
// holds the row, so the layer needs no second metadata lookup.
// See TheoryOfChunkedContent.
func (u *sqlUpper) content(p string, off int64, size int) ([]byte, error) {
	return u.readChunks(u.db, p, off, size)
}

// rowsUnder reads the layer's rows under one directory in a single query, so a
// merging window costs no query. A fresh session holds no row and skips the
// query. The scan reads the fields a listing entry reports; the recorded
// originals stay out because only the diff reads them.
// See TheoryOfDirectoryStream and TheoryOfUpperAbsentCache.
func (u *sqlUpper) rowsUnder(dir string) ([]fileRow, error) {
	if u.empty.Load() {
		return nil, nil
	}
	prefix := dir
	if len(prefix) == 0 || prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	hi := prefix[:len(prefix)-1] + "0"
	rows, err := queryStmt(u.db, u.stmts.selectDir, querySelectDir, prefix, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fileRow
	for rows.Next() {
		var r fileRow
		var kind, deleted int
		var mode, mtime int64
		if err := rows.Scan(&r.path, &kind, &mode, &mtime, &deleted, &r.size, &r.target); err != nil {
			return nil, err
		}
		r.kind = Kind(kind)
		r.mode = os.FileMode(mode)
		r.mtime = time.Unix(0, mtime)
		r.deleted = deleted != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// allRows returns every row in path order, from one query. The diff reads the
// whole set before it reads any content, because the layer holds a single
// connection: a row set kept open while a content query runs would wait for
// the connection it holds. See TheoryOfChunkedContent.
func (u *sqlUpper) allRows() ([]fileRow, error) {
	rows, err := u.db.Query(queryAllRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fileRow
	for rows.Next() {
		r, err := scanFileRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// putRow stores one row's metadata. Content lives in the chunk table: a caller
// that changes content writes it through replaceContent or splice, while a row
// that stops being a live regular file drops its chunks here.
// See TheoryOfChunkedContent.
func (u *sqlUpper) putRow(r fileRow) error {
	tx, err := u.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := u.putMetaTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

func (u *sqlUpper) putMetaTx(tx *sql.Tx, r fileRow) error {
	// A row exists from this write on, so no lookup may keep the path cached as
	// absent: the write epoch moves and the entry clears under one lock, and a
	// lookup whose stamp predates this write records nothing. The call runs
	// before the transaction commits, so a lookup that passes the stamp check
	// still sees either the pre-write state or the committed row, never a
	// stale absence on top of it. See TheoryOfUpperAbsentCache.
	u.markWritten(r.path)
	var deleted, originalExists int
	if r.deleted {
		deleted = 1
	}
	if r.originalExists {
		originalExists = 1
	}
	if _, err := runStmt(tx, u.stmts.putMeta, queryPutMeta,
		r.path, int(r.kind), int64(r.mode), r.mtime.UnixNano(), deleted, r.size,
		r.target, originalExists, r.originalHash,
	); err != nil {
		return err
	}
	if r.deleted || r.kind != KindRegular {
		if _, err := runStmt(tx, u.stmts.dropChunks, queryDropChunks, r.path); err != nil {
			return err
		}
	}
	return nil
}

// replaceContent stores one row's metadata and replaces its content in one
// transaction, so a reader never sees a size and a chunk set that disagree.
func (u *sqlUpper) replaceContent(r fileRow, content []byte) error {
	tx, err := u.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := u.replaceChunks(tx, r.path, content); err != nil {
		return err
	}
	r.size = int64(len(content))
	if err := u.putMetaTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

// createContent stores one row's metadata and its content, but only while the
// path holds no live regular row that already carries content: such a row is
// another writer's committed bytes, and replacing it would drop what that
// writer stored. A metadata-only row is not that row — its size sentinel says
// it records metadata alone, and materialising it is exactly this call's job.
// created reports whether this call wrote the row. The guard and the write
// share one transaction, so the row that decided the outcome is the row the
// write replaces. See TheoryOfWriteSplice and TheoryOfBaseCapture.
func (u *sqlUpper) createContent(r fileRow, content []byte) (created bool, err error) {
	tx, err := u.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var kind, deleted int
	var size int64
	switch err := queryRowStmt(tx, u.stmts.selectUpper, querySelectUpper, r.path).
		Scan(&kind, &deleted, &size); {
	case err == nil:
		// A committed metadata-only row carries the size sentinel: no content
		// write produced it, so it is the state this call exists to replace.
		// See TheoryOfBaseCapture.
		if deleted == 0 && Kind(kind) == KindRegular && size >= 0 {
			return false, nil
		}
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	r.size = int64(len(content))
	if err := u.replaceChunks(tx, r.path, content); err != nil {
		return false, err
	}
	if err := u.putMetaTx(tx, r); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// splice writes one payload into the stored content in one transaction. One
// statement both guards the row and extends its size, so the delta's
// admissibility costs no separate lookup, and the prepared chunk upsert
// executes once per covered chunk, so a window pays one execute per chunk with
// no SQL parse. A path without a live regular row refuses the splice with
// errNoLiveRegularRow, so the caller materialises the row and retries.
// See TheoryOfChunkedContent, TheoryOfWriteSplice and
// TheoryOfPreparedStatements.
func (u *sqlUpper) splice(p string, off int64, data []byte, mtime time.Time) error {
	tx, err := u.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The guard and the size update are one statement: a matched row is a live
	// regular row, and the new end is the window's end when the window extends
	// the stored content. The scalar maximum is the dialect's spelling.
	// See TheoryOfChunkedContent and TheoryOfSqlDialects.
	res, err := runStmt(tx, u.stmts.guardWrite, u.dialect.guardWrite,
		off+int64(len(data)), mtime.UnixNano(), p, int(KindRegular),
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
		if err := u.spliceRange(tx, p, off, data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// spliceRange writes the payload into the chunks it covers. A chunk the payload
// covers whole stores the payload's own bytes — no read of the stored chunk, no
// second copy of the payload — and a chunk the payload only partly covers keeps
// its bytes outside the payload, read once for that purpose. A write past the
// stored end inserts no zeros, because an absent chunk reads as zeros, and
// every covered chunk leaves through the prepared upsert, executed once per
// chunk. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (u *sqlUpper) spliceRange(ex dbExecer, p string, off int64, data []byte) error {
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
		existing, err := u.readChunk(ex, p, idx)
		if err != nil {
			return err
		}
		buf := make([]byte, max(int64(len(existing)), hi))
		copy(buf, existing)
		copy(buf[lo:hi], payload)
		chunks = append(chunks, buf)
	}
	return u.putChunks(ex, p, first, chunks)
}

// readChunk returns one chunk, or nil when the chunk is absent, from the
// prepared chunk select. See TheoryOfPreparedStatements.
func (u *sqlUpper) readChunk(ex dbExecer, p string, idx int64) ([]byte, error) {
	var data []byte
	err := queryRowStmt(ex, u.stmts.selectChunk, querySelectChunk, p, idx).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

// putChunks writes a run of chunks by executing the prepared single-chunk
// upsert once per chunk, so a single-chunk window — the hot shape of a partial
// write — pays exactly one execute. See TheoryOfPreparedStatements.
func (u *sqlUpper) putChunks(ex dbExecer, p string, first int64, chunks [][]byte) error {
	for i, chunk := range chunks {
		if _, err := runStmt(ex, u.stmts.putChunk, queryPutChunk, p, first+int64(i), chunk); err != nil {
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
// caller's transaction. See TheoryOfChunkedContent and
// TheoryOfPreparedStatements.
func (u *sqlUpper) replaceChunks(ex dbExecer, p string, content []byte) error {
	if _, err := runStmt(ex, u.stmts.dropChunks, queryDropChunks, p); err != nil {
		return err
	}
	if len(content) == 0 {
		return nil
	}
	chunks := make([][]byte, 0, (len(content)+contentChunkSize-1)/contentChunkSize)
	for off := 0; off < len(content); off += contentChunkSize {
		chunks = append(chunks, content[off:min(off+contentChunkSize, len(content))])
	}
	return u.putChunks(ex, p, 0, chunks)
}

// readChunks reads the window [off, off+size) of p's stored content in one
// indexed query over the chunks covering it, padding absent chunks with zeros.
// The caller clamps the window to the stored size. The query runs its prepared
// form, so a windowed read pays no SQL parse. See TheoryOfPreparedStatements.
func (u *sqlUpper) readChunks(ex dbExecer, p string, off int64, size int) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	end := off + int64(size)
	out := make([]byte, size)
	rows, err := queryStmt(ex, u.stmts.selectChunks, querySelectChunks,
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

// truncate shapes the stored chunks to a new length and records the length and
// mtime, in one transaction. See TheoryOfChunkedContent.
func (u *sqlUpper) truncate(p string, size int64, mtime time.Time) error {
	tx, err := u.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := u.truncateChunks(tx, p, size); err != nil {
		return err
	}
	if _, err := runStmt(tx, u.stmts.setSize, querySetSize,
		size, mtime.UnixNano(), p,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// truncateChunks shapes the stored chunks to a new length: the chunks beyond
// the length drop, and the chunk the length ends inside keeps its prefix.
// Growing writes nothing, because an absent chunk reads as zeros. Both
// statements run their prepared forms. See TheoryOfChunkedContent and
// TheoryOfPreparedStatements.
func (u *sqlUpper) truncateChunks(ex dbExecer, p string, size int64) error {
	keep := (size + contentChunkSize - 1) / contentChunkSize
	if _, err := runStmt(ex, u.stmts.dropFrom, queryDropFrom, p, keep); err != nil {
		return err
	}
	if rem := size % contentChunkSize; rem != 0 {
		if _, err := runStmt(ex, u.stmts.trimChunk, queryTrimChunk, rem, p, keep-1); err != nil {
			return err
		}
	}
	return nil
}

// drop removes one path's row and its content chunks in one transaction, so the
// next diff is exact. The row delete runs as text because an apply is not a hot
// path. See TheoryOfChunkedContent and TheoryOfPreparedStatements.
func (u *sqlUpper) drop(p string) error {
	tx, err := u.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(queryDropRow, p); err != nil {
		return err
	}
	if _, err := runStmt(tx, u.stmts.dropChunks, queryDropChunks, p); err != nil {
		return err
	}
	return tx.Commit()
}

// writeAudit writes one batch of audit rows in a single transaction, keeping
// the buffer's order. The batch takes one transaction view of the insert and
// executes it per row: database/sql caches a transaction's view of a prepared
// statement from the first Tx.Stmt call on, so a row that asked for its own
// view paid a lookup and a lock per row. The insert's text is the dialect's,
// because a dialect without an auto-increment column names its id.
// See TheoryOfAuditBatching, TheoryOfPreparedStatements and
// TheoryOfSqlDialects.
func (u *sqlUpper) writeAudit(ops []Op) error {
	if len(ops) == 0 {
		return nil
	}
	tx, err := u.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exec func(args ...any) (sql.Result, error)
	if u.stmts.putOp == nil {
		exec = func(args ...any) (sql.Result, error) {
			return tx.Exec(u.dialect.insertOp, args...)
		}
	} else {
		exec = tx.Stmt(u.stmts.putOp).Exec
	}
	for _, o := range ops {
		if _, err := exec(o.Time.Format(time.RFC3339Nano), o.Op, o.Path, o.Detail); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// auditRows returns the audit trail in insertion order.
// See TheoryOfAuditBatching.
func (u *sqlUpper) auditRows() ([]Op, error) {
	rows, err := u.db.Query(queryAuditRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var at, op, p, detail string
		if err := rows.Scan(&at, &op, &p, &detail); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, at)
		out = append(out, Op{Time: t, Op: op, Path: p, Detail: detail})
	}
	return out, rows.Err()
}

// close releases the database's one connection, which frees the statements the
// connection prepared. See TheoryOfPreparedStatements.
func (u *sqlUpper) close() error {
	if u.db == nil {
		return nil
	}
	err := u.db.Close()
	u.db = nil
	return err
}

// prepareStatements fills the layer's statement set. It runs once, from the
// open path, on the layer's single connection, so no request ever waits for a
// preparation. A failure leaves the layer unopened rather than serving requests
// that would pay the parse the set exists to remove. The guard and the audit
// insert take their text from the layer's dialect.
// See TheoryOfPreparedStatements and TheoryOfSqlDialects.
func (u *sqlUpper) prepareStatements() error {
	for _, target := range []struct {
		stmt  **sql.Stmt
		query string
	}{
		{&u.stmts.selectRow, querySelectRow},
		{&u.stmts.selectUpper, querySelectUpper},
		{&u.stmts.selectDir, querySelectDir},
		{&u.stmts.selectChunks, querySelectChunks},
		{&u.stmts.selectChunk, querySelectChunk},
		{&u.stmts.putMeta, queryPutMeta},
		{&u.stmts.putOp, u.dialect.insertOp},
		{&u.stmts.putChunk, queryPutChunk},
		{&u.stmts.guardWrite, u.dialect.guardWrite},
		{&u.stmts.setSize, querySetSize},
		{&u.stmts.dropChunks, queryDropChunks},
		{&u.stmts.dropFrom, queryDropFrom},
		{&u.stmts.trimChunk, queryTrimChunk},
	} {
		stmt, err := u.db.Prepare(target.query)
		if err != nil {
			return fmt.Errorf("sandboxfs: %s prepare: %w", u.dialect.name, err)
		}
		*target.stmt = stmt
	}
	return nil
}
