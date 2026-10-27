package sandboxfs

import "net/url"

const TheoryOfSqlDialects = `
The durable upper layer is one SQL implementation over database/sql; a
dialect supplies what the backend engines spell differently. Three things
differ, and each carries a reason the shared text cannot.

The driver and its DSN. sqlite's driver is linked by this package because it is
pure Go; DuckDB has no pure-Go driver, so the binary that wants the DuckDB
dialect imports one itself and this package names the driver "duckdb". That is
database/sql's own registration boundary, and it keeps the sandbox free of a cgo
and native-library dependency an environment that never selects DuckDB must not
pay. A binary without the driver reports the open error and names the dialect;
nothing falls back silently.

The size guard's scalar maximum. sqlite spells max(a, b); DuckDB's max is an
aggregate and spells the scalar greatest(a, b). The guard is one statement
because it both admits the delta and extends the row, so a dialect carries its
own text (see TheoryOfWriteSplice).

The audit id. sqlite declares AUTOINCREMENT and its insert omits the id; DuckDB
has no auto-increment column, so its schema creates a sequence and its insert
names nextval. The schema and the insert travel together because the two halves
must agree.

Everything else — the row and chunk selects, both upserts, the deletes, the
chunk trim, and the diff's full scan — is one text for both dialects, written
with constructs the two engines share. A further divergence becomes another
dialect field rather than a branch in the shared code.

The record file also names its engine by its header, and that fact belongs to
the dialect beside the driver it selects: the review command routes an
arbitrary record to the dialect that wrote it by the dialect's magic field, so
a session that selected DuckDB stays reviewable without the user remembering
which engine wrote the file. A file whose header names neither engine takes
sqlite, whose own error names a path that is no record. The DuckDB route still
requires the binary's driver import, exactly as the mount does.

The schema runs one statement at a time: a driver that refuses a
multi-statement Exec is still served, and a dialect's sequence is created
before the table whose insert names it.

The layer cannot be exercised end to end without the dialect's native library.
Its verification is the shared code path's sqlite coverage plus the dialect
data's own checks: a guard that silently lost its scalar maximum, or an insert
that lost its sequence call, would produce a statement one engine accepts and
the other rejects at a session's first write.
`

// sqlDialect is one SQL backend of the durable upper layer: the driver that
// opens it, the magic that names its record file, the DSN it opens with, the
// schema that holds its rows, and the two statements whose syntax the engines
// spell differently. Every other statement is shared. See TheoryOfSqlDialects.
type sqlDialect struct {
	name   string
	driver string
	// magic is the file header that names this engine, empty when the engine
	// declares none. The review command routes an arbitrary record to the
	// dialect that wrote it by this header, so a user never has to remember
	// which engine wrote the file. See TheoryOfSqlDialects.
	magic  string
	dsn    func(dbPath string) string
	schema []string
	// guardWrite extends one live regular row's size to the window's end,
	// records mtime, and matches only a live regular row. Its scalar maximum
	// is dialect syntax. See TheoryOfSqlDialects and TheoryOfWriteSplice.
	guardWrite string
	// insertOp writes one audit row. A dialect without an auto-increment
	// column names the id its schema's sequence produces.
	// See TheoryOfSqlDialects and TheoryOfAuditBatching.
	insertOp string
}

// The shared table definitions: both dialects create the same columns with the
// same names, so the shared statements select and upsert them unchanged. A
// dialect adds only what its own key or storage layout needs.
const (
	ddlFiles = `CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    kind INTEGER NOT NULL,
    mode BIGINT NOT NULL,
    mtime BIGINT NOT NULL,
    deleted INTEGER NOT NULL DEFAULT 0,
    size BIGINT NOT NULL DEFAULT 0,
    target TEXT NOT NULL DEFAULT '',
    original_exists INTEGER NOT NULL DEFAULT 0,
    original_hash TEXT NOT NULL DEFAULT ''
)`
	ddlChunks = `CREATE TABLE IF NOT EXISTS chunks (
    path TEXT NOT NULL,
    idx BIGINT NOT NULL,
    data BLOB NOT NULL,
    PRIMARY KEY (path, idx)
)`
	ddlChunksWithoutRowID = ddlChunks + ` WITHOUT ROWID`
	ddlOpsAuto            = `CREATE TABLE IF NOT EXISTS ops (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    time TEXT NOT NULL,
    op TEXT NOT NULL,
    path TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
)`
	ddlOpsSequence = `CREATE SEQUENCE IF NOT EXISTS ops_id START 1`
	ddlOpsKeyed    = `CREATE TABLE IF NOT EXISTS ops (
    id BIGINT PRIMARY KEY,
    time TEXT NOT NULL,
    op TEXT NOT NULL,
    path TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
)`
	ddlOpsIndex = `CREATE INDEX IF NOT EXISTS idx_ops_path ON ops(path)`
)

// sqliteDialect is the default dialect: the pure-Go sqlite driver this package
// links, its WAL record and pragmas, an auto-increment audit id, and the scalar
// maximum spelled max. See TheoryOfSqlDialects.
var sqliteDialect = sqlDialect{
	name:   "sqlite",
	driver: "sqlite",
	magic:  "SQLite format 3\x00",
	dsn: func(dbPath string) string {
		return "file:" + url.PathEscape(dbPath) +
			"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)" +
			// synchronous(NORMAL) keeps every commit durable across an
			// application crash and drops the per-commit fsync a machine crash
			// would need. The sandbox is a session record, reviewed while the
			// session runs: losing the last commits of a machine crash costs
			// nothing a review can use, while an fsync per write and per audit
			// row costs the write path a disk round trip each time.
			"&_pragma=synchronous(NORMAL)" +
			// cache_size(-32000) lifts sqlite's page cache to 32 MiB, so the
			// layer's own pages and the audit trail's stay resident instead of
			// being re-read from the database per request.
			"&_pragma=cache_size(-32000)"
	},
	schema: []string{ddlFiles, ddlChunksWithoutRowID, ddlOpsAuto, ddlOpsIndex},
	guardWrite: `UPDATE files SET size = max(size, ?), mtime = ?
		 WHERE path = ? AND kind = ? AND deleted = 0 AND size >= 0`,
	insertOp: `INSERT INTO ops (time, op, path, detail) VALUES (?, ?, ?, ?)`,
}

// duckDBDialect is the DuckDB dialect: the application-registered driver, the
// record path as its DSN, a sequence for the audit id, and the scalar maximum
// spelled greatest because DuckDB's max aggregates. See TheoryOfSqlDialects.
var duckDBDialect = sqlDialect{
	name:   "duckdb",
	driver: "duckdb",
	magic:  "DUCK",
	dsn:    func(dbPath string) string { return dbPath },
	schema: []string{ddlOpsSequence, ddlFiles, ddlChunks, ddlOpsKeyed, ddlOpsIndex},
	guardWrite: `UPDATE files SET size = greatest(size, ?), mtime = ?
		 WHERE path = ? AND kind = ? AND deleted = 0 AND size >= 0`,
	insertOp: `INSERT INTO ops (id, time, op, path, detail)
		 VALUES (nextval('ops_id'), ?, ?, ?, ?)`,
}
