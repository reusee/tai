package sandboxfs

import "testing"

// sqlLayerOf returns the store's SQL upper layer, so a test reaches the
// statements, caches, and connection the layer owns whatever dialect serves it.
// A memory-backed store carries no SQL layer, so the test that asks for one is
// testing the wrong backend. See TheoryOfUpperLayer.
func sqlLayerOf(t *testing.T, store *Store) *sqlUpper {
	t.Helper()
	u, ok := store.upper.(*sqlUpper)
	if !ok {
		t.Fatalf("store upper layer is %T, want *sqlUpper", store.upper)
	}
	return u
}

// sqliteOf returns the SQL upper layer of a store built by the sqlite
// constructor, and fails when the store carries another dialect: a test that
// reaches the sqlite layer must not silently read a DuckDB one.
// See TheoryOfSqlDialects.
func sqliteOf(t *testing.T, store *Store) *sqlUpper {
	t.Helper()
	u := sqlLayerOf(t, store)
	if u.dialect.name != sqliteDialect.name {
		t.Fatalf("store dialect is %q, want %q", u.dialect.name, sqliteDialect.name)
	}
	return u
}
