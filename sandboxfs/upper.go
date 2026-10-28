package sandboxfs

import "time"

const TheoryOfUpperLayer = `
The upper layer is one contract with three implementations, and the store's
union logic — capture of the base state, kind checks, the size sentinel,
whiteouts, conflict detection, apply — is written once against it. A session's
semantics therefore do not depend on where its mutations live.

The memory layer is the default: the rows and content live in maps, the session
leaves no file behind, nothing survives the process, and a mutation pays no
disk round trip. A default session therefore needs no record and no decision
about where one lives — a run whose changes reach the host as it proceeds, a
test, a filesystem with no room for a record all get the cheap layer by asking
for nothing.

The SQL layer is durable and opt-in: one database file records the session,
outlives the process, and the review command browses and applies it later. A
caller selects it by naming the record. Its dialect decides the engine that
holds the file — sqlite, or DuckDB when the binary that selects it links a
driver (see TheoryOfSqlDialects).

The directory layer is durable and opt-in too, and the thinnest of the three:
one host directory records the session as ordinary files, and the host
filesystem does the storing, the windowing, and the truncation. It needs no
driver, no schema, and no chunk layout, so a project that wants a durable
record without a database gets one. Its own model lives in TheoryOfDirUpper and
its selection in TheoryOfDirRecord.

The memory and SQL layers keep content in fixed-size chunks, so one write
window costs its own payload in either one; the layout is
TheoryOfChunkedContent's subject. The directory layer keeps each path's content
in one file and lets the host do the same job. The memory layer copies every
chunk it stores: the caller's buffer may be a reused request buffer, and the
layer must own the bytes it answers with.

The contract speaks rows and content windows, never SQL and never files. A row
is metadata alone. The composite operations — write one row, write one row with
its whole content, create with content, splice, truncate, drop — are atomic
inside the layer: the SQL implementation reaches atomicity through a
transaction on its one connection, the memory implementation through its mutex,
and the directory implementation through its mutex and a rename over the
committed file. Atomicity is the layer's own business because only the layer
knows where a row and its content both live.

The negative-lookup cache and the prepared statement set are the SQL layer's
own: a map lookup needs no absence cache, a failed file open costs one host
lookup, and neither other layer executes SQL. A caller of the contract sees
neither.
`

// upperStore is the sandbox's mutable layer: the rows and content the session
// wrote, and the audit trail of what it touched. See TheoryOfUpperLayer.
type upperStore interface {
	// row returns one path's row, live or deleted, and whether the layer
	// holds it.
	row(p string) (fileRow, bool, error)

	// window returns one path's content window [off, off+size) and the
	// path's recorded kind. found reports that the layer answers the window
	// with content: a false result with a nil error means the layer holds no
	// row, or a metadata-only row whose content still lives in the base, so
	// the caller serves the base. A whiteout reports ErrNotExist. A window
	// past the stored end yields an empty slice. A row of another kind
	// reports found true with no data, and the caller answers by the kind.
	window(p string, off int64, size int) (data []byte, kind Kind, found bool, err error)

	// content returns one live regular row's content window. The caller
	// holds the row, so the layer needs no second metadata lookup.
	content(p string, off int64, size int) ([]byte, error)

	// rowsUnder returns the rows of one directory's direct entries, in
	// unspecified order: a listing merges them beside the base's own entries.
	rowsUnder(dir string) ([]fileRow, error)

	// allRows returns every row, ordered by path.
	allRows() ([]fileRow, error)

	// putRow writes one metadata row, dropping the content of a row that is
	// deleted or no longer regular.
	putRow(r fileRow) error

	// replaceContent writes one row together with its whole content, in one
	// atomic step, so a reader never sees a size and a content that disagree.
	replaceContent(r fileRow, content []byte) error

	// createContent writes one row and its content only while the path holds
	// no live regular row that already carries content: such a row is another
	// writer's committed bytes, and replacing it would drop what that writer
	// stored. A metadata-only row is not that row, so this call replaces it.
	// created reports whether this call wrote the row.
	createContent(r fileRow, content []byte) (created bool, err error)

	// splice writes one payload into a live regular row's content at off,
	// extending the row's size and recording mtime, in one atomic step.
	// errNoLiveRegularRow reports a path without such a row, so the caller
	// materialises it and retries.
	splice(p string, off int64, data []byte, mtime time.Time) error

	// truncate shapes one live regular row's content to size and records the
	// new length and mtime, in one atomic step.
	truncate(p string, size int64, mtime time.Time) error

	// drop removes one path's row and its content, so a repeated apply is a
	// no-op and the next diff is exact.
	drop(p string) error

	// writeAudit records audit rows. It is best-effort like the trail itself:
	// a failure never fails the operation that recorded it.
	writeAudit(ops []Op) error

	// auditRows returns the audit trail in application order.
	auditRows() ([]Op, error)

	// close releases the layer's resources.
	close() error
}

// attrOfRow renders one row's attributes. A regular row whose size is
// negative records metadata alone, so the base holds its length and the caller
// resolves that with one Lstat; a link reports its target's length, the value
// a listing and a stat both show. See TheoryOfReadPath.
func attrOfRow(r fileRow) Attr {
	a := Attr{Kind: r.kind, Mode: r.mode, ModTime: r.mtime}
	switch r.kind {
	case KindRegular:
		a.Size = r.size
	case KindSymlink:
		a.Size = int64(len(r.target))
	}
	return a
}
