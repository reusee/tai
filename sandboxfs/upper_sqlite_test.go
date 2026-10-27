package sandboxfs

import "testing"

// sqliteOf returns the store's sqlite upper layer, so a test reaches the
// statements, caches, and connection the layer owns. A memory-backed store
// carries no sqlite layer, so the test that asks for one is testing the wrong
// backend. See TheoryOfUpperLayer.
func sqliteOf(t *testing.T, store *Store) *sqliteUpper {
	t.Helper()
	u, ok := store.upper.(*sqliteUpper)
	if !ok {
		t.Fatalf("store upper layer is %T, want *sqliteUpper", store.upper)
	}
	return u
}
