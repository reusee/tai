package sandboxfs

import (
	"database/sql"
)

const TheoryOfPreparedStatements = `
Prepared statements theory:
- sqlite parses and plans a statement every time it executes SQL text. The
  profile of the write benchmark, taken before this set existed, put that
  parse at the top of the write path — the parser's reduce steps and their
  comparisons ahead of the blob copy and the commit — and the same cost stood
  under every stat, every row read, and every chunk deletion the request path
  runs. Executing text is what pays the parse; the operation is not.
- The store prepares the statements its request path runs, once, when it
  opens, and the request path executes the prepared forms. The set covers the
  stat select, the full-row select, the upper-row select, the directory
  listing select, the two chunk selects, the metadata upsert, the single-chunk
  upsert, the audit-row insert, the size guard, the size update, and the three
  chunk drop-and-trim statements. A preparation failure fails Open: a store
  that served requests without the set would pay the parse the set exists to
  remove, silently.
- Every statement the request path runs has a prepared form, the multi-chunk
  window included: a window spanning many chunks executes the prepared
  single-chunk upsert once per chunk. A multi-row INSERT whose VALUES the
  window builds pays a fresh parse of a statement whose length grows with the
  window. The two shapes measured within noise of each other — a prepared
  form's per-execute cost stands close to the parse it saves — so the loop is
  kept for its simpler code: one statement and one execution path instead of a
  statement the window builds.
- A prepared statement belongs to a connection. The store holds one
  connection (see OpenWithPolicy), so a transaction runs the same prepared
  statement through database/sql's own view of it, and the transaction keeps
  the exclusivity that makes concurrent callers safe: while it is open the
  one connection is checked out, so no other caller's statement can join it.
  The post-change profile reaches the driver through ctxDriverStmtExec — the
  prepared statement, not a re-preparation.
- The remaining top of the write profile is the driver's blob binding: the
  bound []byte is copied into sqlite's page cache. That is the storage
  engine's data movement, once per window, and no statement set removes it;
  the set's job ends with the parse.
- A statement that is nil falls back to its SQL text. The fallback costs the
  parse the prepared form avoids, and it keeps every helper total without a
  second code path; the open path prepares the set before the store serves a
  request, so the fallback never runs on a store that opened.
- The set lives as long as the store's one connection. Closing the database
  closes that connection, and the driver frees the statements the connection
  prepared, so the set needs no separate close on the open-and-close path the
  tests and the review command follow.
`

// querySelectDir is the directory listing's row select: a listing walks every
// entry under a prefix, and the listing is one request, so the select has a
// prepared form like every other request-path statement.
// See TheoryOfPreparedStatements.
const querySelectDir = `SELECT path, kind, mode, mtime, deleted, size, length(target)
		 FROM files WHERE path >= ? AND path < ?`

// queryPutOp is the audit-row insert: a batch shares one commit, and the
// insert itself is prepared so the batch pays no SQL parse per row.
// See TheoryOfPreparedStatements and TheoryOfAuditBatching.
const queryPutOp = `INSERT INTO ops (time, op, path, detail) VALUES (?, ?, ?, ?)`

// The SQL text of every statement the request path runs. A statement is
// prepared from its text and executed by its prepared form, so the text lives
// once, beside the set it fills. See TheoryOfPreparedStatements.
const (
	querySelectStat   = `SELECT kind, mode, mtime, deleted, size, length(target) FROM files WHERE path = ?`
	querySelectRow    = `SELECT path, kind, mode, mtime, deleted, size, target, original_exists, original_hash FROM files WHERE path = ?`
	querySelectUpper  = `SELECT kind, deleted, size FROM files WHERE path = ?`
	querySelectChunks = `SELECT idx, data FROM chunks WHERE path = ? AND idx >= ? AND idx <= ? ORDER BY idx`
	querySelectChunk  = `SELECT data FROM chunks WHERE path = ? AND idx = ?`
	queryPutMeta      = `INSERT INTO files (path, kind, mode, mtime, deleted, size, target, original_exists, original_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   kind = excluded.kind,
		   mode = excluded.mode,
		   mtime = excluded.mtime,
		   deleted = excluded.deleted,
		   size = excluded.size,
		   target = excluded.target,
		   original_exists = excluded.original_exists,
		   original_hash = excluded.original_hash`
	queryPutChunk = `INSERT INTO chunks (path, idx, data) VALUES (?, ?, ?)
			 ON CONFLICT(path, idx) DO UPDATE SET data = excluded.data`
	queryGuardWrite = `UPDATE files SET size = max(size, ?), mtime = ?
		 WHERE path = ? AND kind = ? AND deleted = 0`
	querySetSize    = `UPDATE files SET size = ?, mtime = ? WHERE path = ?`
	queryDropChunks = `DELETE FROM chunks WHERE path = ?`
	queryDropFrom   = `DELETE FROM chunks WHERE path = ? AND idx >= ?`
	queryTrimChunk  = `UPDATE chunks SET data = substr(data, 1, ?) WHERE path = ? AND idx = ?`
)

// preparedStatements is the store's statement set: one prepared form per
// statement the request path runs. A zero value holds no statement, and every
// helper that runs one falls back to its SQL text, so a Store built without the
// set still works. See TheoryOfPreparedStatements.
type preparedStatements struct {
	selectStat   *sql.Stmt
	selectRow    *sql.Stmt
	selectUpper  *sql.Stmt
	selectDir    *sql.Stmt
	selectChunks *sql.Stmt
	selectChunk  *sql.Stmt
	putMeta      *sql.Stmt
	putOp        *sql.Stmt
	putChunk     *sql.Stmt
	guardWrite   *sql.Stmt
	setSize      *sql.Stmt
	dropChunks   *sql.Stmt
	dropFrom     *sql.Stmt
	trimChunk    *sql.Stmt
}

// prepareStatements fills the store's statement set. It runs once, from the
// open path, on the store's single connection, so no request ever waits for a
// preparation. A failure leaves the store unopened rather than serving requests
// that would pay the parse the set exists to remove.
// See TheoryOfPreparedStatements.
func (s *Store) prepareStatements() error {
	for _, target := range []struct {
		stmt  **sql.Stmt
		query string
	}{
		{&s.stmts.selectStat, querySelectStat},
		{&s.stmts.selectRow, querySelectRow},
		{&s.stmts.selectUpper, querySelectUpper},
		{&s.stmts.selectDir, querySelectDir},
		{&s.stmts.selectChunks, querySelectChunks},
		{&s.stmts.selectChunk, querySelectChunk},
		{&s.stmts.putMeta, queryPutMeta},
		{&s.stmts.putOp, queryPutOp},
		{&s.stmts.putChunk, queryPutChunk},
		{&s.stmts.guardWrite, queryGuardWrite},
		{&s.stmts.setSize, querySetSize},
		{&s.stmts.dropChunks, queryDropChunks},
		{&s.stmts.dropFrom, queryDropFrom},
		{&s.stmts.trimChunk, queryTrimChunk},
	} {
		stmt, err := s.db.Prepare(target.query)
		if err != nil {
			return err
		}
		*target.stmt = stmt
	}
	return nil
}

// runStmt runs one prepared statement through an execer: a transaction runs its
// own view of the statement, a direct connection runs it as is, and a nil
// statement falls back to its SQL text. See TheoryOfPreparedStatements.
func runStmt(ex dbExecer, stmt *sql.Stmt, query string, args ...any) (sql.Result, error) {
	if stmt == nil {
		return ex.Exec(query, args...)
	}
	if tx, ok := ex.(*sql.Tx); ok {
		return tx.Stmt(stmt).Exec(args...)
	}
	return stmt.Exec(args...)
}

// queryStmt runs one prepared query through an execer, with the same
// transaction view and text fallback as runStmt. See TheoryOfPreparedStatements.
func queryStmt(ex dbExecer, stmt *sql.Stmt, query string, args ...any) (*sql.Rows, error) {
	if stmt == nil {
		return ex.Query(query, args...)
	}
	if tx, ok := ex.(*sql.Tx); ok {
		return tx.Stmt(stmt).Query(args...)
	}
	return stmt.Query(args...)
}

// queryRowStmt runs one prepared single-row query through an execer, with the
// same transaction view and text fallback as runStmt.
// See TheoryOfPreparedStatements.
func queryRowStmt(ex dbExecer, stmt *sql.Stmt, query string, args ...any) *sql.Row {
	if stmt == nil {
		return ex.QueryRow(query, args...)
	}
	if tx, ok := ex.(*sql.Tx); ok {
		return tx.Stmt(stmt).QueryRow(args...)
	}
	return stmt.QueryRow(args...)
}
