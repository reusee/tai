package sandboxfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDuckDBDialectSpellsWhatTheEnginesDisagreeOn protects TheoryOfSqlDialects:
// the guard's scalar maximum and the audit id are the two statements a shared
// text cannot carry, so a silent change to either would produce a statement one
// engine accepts and the other rejects at the session's first write. The two
// dialects must not have been flattened into one.
func TestDuckDBDialectSpellsWhatTheEnginesDisagreeOn(t *testing.T) {
	if duckDBDialect.driver != "duckdb" {
		t.Errorf("driver is %q, want duckdb", duckDBDialect.driver)
	}
	if got := duckDBDialect.dsn("/proj/x.db"); got != "/proj/x.db" {
		t.Errorf("DSN is %q, want the record path itself", got)
	}
	if !strings.Contains(duckDBDialect.guardWrite, "greatest(") {
		t.Errorf("guard does not use DuckDB's scalar maximum: %s", duckDBDialect.guardWrite)
	}
	if !strings.Contains(duckDBDialect.insertOp, "nextval(") {
		t.Errorf("audit insert does not name the sequence: %s", duckDBDialect.insertOp)
	}
	if schema := strings.Join(duckDBDialect.schema, "\n"); !strings.Contains(schema, "CREATE SEQUENCE") {
		t.Error("schema creates no sequence for the audit id")
	}
	if !strings.Contains(sqliteDialect.guardWrite, "max(size, ?)") {
		t.Errorf("sqlite guard lost its scalar maximum: %s", sqliteDialect.guardWrite)
	}
	if strings.Contains(sqliteDialect.insertOp, "nextval(") {
		t.Errorf("sqlite insert names a sequence: %s", sqliteDialect.insertOp)
	}
	if !strings.HasPrefix(sqliteDialect.dsn("/proj/x.db"), "file:") {
		t.Errorf("sqlite DSN is not a file DSN: %s", sqliteDialect.dsn("/proj/x.db"))
	}
}

// TestOpenDuckDBSelectsTheDialect checks the constructor's wiring: a DuckDB
// store carries the DuckDB dialect and its record path, whether the binary
// links a driver or not. Without one the open reports the unknown driver and
// names the dialect instead of falling back to sqlite, which is the honest
// answer: the record would otherwise land in a database of another engine.
// See TheoryOfSqlDialects.
func TestOpenDuckDBSelectsTheDialect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "session.duckdb")
	store, err := OpenDuckDB(dbPath, dir)
	if err != nil {
		if !strings.Contains(err.Error(), "duckdb") {
			t.Fatalf("open error does not name the dialect: %v", err)
		}
		return
	}
	defer store.Close()
	u := sqlLayerOf(t, store)
	if u.dialect.name != duckDBDialect.name {
		t.Fatalf("store dialect is %q, want %q", u.dialect.name, duckDBDialect.name)
	}
	if u.dbPath != dbPath {
		t.Errorf("record path is %q, want %q", u.dbPath, dbPath)
	}
}

// TestSQLUpperRejectsAnUnknownDriver protects the open path's report: a dialect
// whose driver nothing registered fails the open and names the dialect, so a
// binary that forgot its driver import learns why at the first mount instead of
// reading a record another engine wrote. See TheoryOfSqlDialects.
func TestSQLUpperRejectsAnUnknownDriver(t *testing.T) {
	dialect := sqlDialect{
		name:   "nodriver",
		driver: "tai-no-such-driver",
		dsn:    func(string) string { return ":memory:" },
		schema: []string{ddlFiles},
	}
	if _, err := openSQLUpper(dialect, filepath.Join(t.TempDir(), "x.db")); err == nil ||
		!strings.Contains(err.Error(), "nodriver") {
		t.Fatalf("open with an unregistered driver: err = %v", err)
	}
}

// TestValidateMountOptionsDuckDB checks the mount's dialect selection: the
// DuckDB dialect needs its record path like sqlite does, the two upper layers
// cannot be combined, and the dialect alone is a valid request.
// See TheoryOfSqlDialects and TheoryOfUpperLayer.
func TestValidateMountOptionsDuckDB(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    MountOptions
		wantErr string
	}{
		{
			name:    "dialect needs a record",
			opts:    MountOptions{Mountpoint: "/tmp/mnt", DuckDB: true},
			wantErr: "empty database path",
		},
		{
			name:    "dialect and memory are exclusive",
			opts:    MountOptions{Mountpoint: "/tmp/mnt", DuckDB: true, DBPath: "/tmp/x", Memory: true},
			wantErr: "different upper layers",
		},
		{
			name: "dialect alone is accepted",
			opts: MountOptions{Mountpoint: "/tmp/mnt", DuckDB: true, DBPath: "/tmp/x"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMountOptions(tc.opts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("request rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

// TestOwnRecordCoversTheDuckDBWAL protects the store's refusal to mutate its own
// record: DuckDB keeps its write-ahead log in a .wal file beside the record, so
// a session write to that name would corrupt the record the review depends on.
// The sqlite siblings keep their own names.
// See TheoryOfSandboxFilesystem and TheoryOfSqlDialects.
func TestOwnRecordCoversTheDuckDBWAL(t *testing.T) {
	s, err := OpenMemory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.dbPath = "/proj/.tai-sandbox.db"
	for _, p := range []string{
		"/proj/.tai-sandbox.db",
		"/proj/.tai-sandbox.db-wal",
		"/proj/.tai-sandbox.db-shm",
		"/proj/.tai-sandbox.db.wal",
	} {
		if !s.isOwnDBPath(p) {
			t.Errorf("%s is not refused as the store's own record", p)
		}
	}
	if s.isOwnDBPath("/proj/.tai-sandbox.db.other") {
		t.Error("a sibling no backend writes is refused as the record")
	}
}

// TestRecordEngineRoutesByMagic protects the review command's record discovery:
// the routing reads each dialect's own magic, so a session record opens with
// the engine that wrote it whatever path the caller names, an absent path
// still opens as the sqlite default, and an unrecognized header opens as
// sqlite so its own error names a file that is no record.
// See TheoryOfSqlDialects.
func TestRecordEngineRoutesByMagic(t *testing.T) {
	dir := t.TempDir()
	// A real sqlite record gives the file a header the check will read back.
	sqlitePath := filepath.Join(dir, "session.db")
	s, err := Open(sqlitePath, dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if engine, err := recordEngine(sqlitePath); err != nil || engine != sqliteDialect.name {
		t.Fatalf("sqlite record routed to %q (err %v)", engine, err)
	}
	// A DuckDB header routes to the DuckDB dialect whether or not the binary
	// links its driver: the routing is the fact under test.
	duckPath := filepath.Join(dir, "session.duckdb")
	if err := os.WriteFile(duckPath, []byte(duckDBDialect.magic+"\x01\x00\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if engine, err := recordEngine(duckPath); err != nil || engine != duckDBDialect.name {
		t.Fatalf("DuckDB record routed to %q (err %v)", engine, err)
	}
	// An absent path carries no header, so it opens as the sqlite default.
	missingPath := filepath.Join(dir, "missing.db")
	if engine, err := recordEngine(missingPath); err != nil || engine != sqliteDialect.name {
		t.Fatalf("missing path routed to %q (err %v)", engine, err)
	}
	// An unrecognized header routes to sqlite, whose open names the fault.
	unknownPath := filepath.Join(dir, "unknown.bin")
	if err := os.WriteFile(unknownPath, []byte("not a record"), 0o644); err != nil {
		t.Fatal(err)
	}
	if engine, err := recordEngine(unknownPath); err != nil || engine != sqliteDialect.name {
		t.Fatalf("unrecognized record routed to %q (err %v)", engine, err)
	}
}
