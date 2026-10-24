package sandboxfs

import (
	"bytes"
	"path/filepath"
	"testing"
)

// TestSpliceFileUsesPreparedWrites checks the prepared write path: the store
// opens with its write statements prepared, the first content write lands, and
// a refused write leaves no transaction open, so the next write still succeeds.
// See TheoryOfPreparedStatements.
func TestSpliceFileUsesPreparedWrites(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "prepared.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.stmts.guardWrite == nil || store.stmts.putChunk == nil {
		t.Fatal("the store prepared no write statements")
	}
	data := bytes.Repeat([]byte("p"), 4096)
	if err := store.SpliceFile("/a.bin", 0, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadRange("/a.bin", 0, len(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("stored content holds %d bytes, want the written %d", len(got), len(data))
	}
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.SpliceFile("/d", 0, data, 0o644); err == nil {
		t.Fatal("a write to a directory succeeded")
	}
	if err := store.SpliceFile("/b.bin", 0, data[:16], 0o644); err != nil {
		t.Fatalf("a write after a refused write failed: %v", err)
	}
	if size, err := store.Stat("/b.bin"); err != nil || size.Size != 16 {
		t.Fatalf("second write stored %d bytes (err %v), want 16", size.Size, err)
	}
}

// TestOpenPreparesTheRequestPath checks the statement set the store opens with:
// every statement the request path runs is prepared, because a statement
// missing from the set would leave its operation paying the SQL parse the set
// exists to remove. See TheoryOfPreparedStatements.
func TestOpenPreparesTheRequestPath(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "statements.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	all := map[string]bool{
		"selectStat":   store.stmts.selectStat != nil,
		"selectRow":    store.stmts.selectRow != nil,
		"selectUpper":  store.stmts.selectUpper != nil,
		"selectChunks": store.stmts.selectChunks != nil,
		"selectChunk":  store.stmts.selectChunk != nil,
		"putMeta":      store.stmts.putMeta != nil,
		"putChunk":     store.stmts.putChunk != nil,
		"guardWrite":   store.stmts.guardWrite != nil,
		"setSize":      store.stmts.setSize != nil,
		"dropChunks":   store.stmts.dropChunks != nil,
		"dropFrom":     store.stmts.dropFrom != nil,
		"trimChunk":    store.stmts.trimChunk != nil,
	}
	for name, prepared := range all {
		if !prepared {
			t.Errorf("statement %q was not prepared", name)
		}
	}
}

// TestPreparedWriteKeepsMultiChunkWindow checks that a window crossing a chunk
// boundary still stores every byte it covers, so the prepared single-chunk
// shortcut never truncates the general shape. See TheoryOfPreparedWrites.
func TestPreparedWriteKeepsMultiChunkWindow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "prepared.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	data := bytes.Repeat([]byte("m"), 3*contentChunkSize/2)
	if err := store.SpliceFile("/big.bin", 0, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadRange("/big.bin", 0, len(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("multi-chunk window stored %d bytes, want %d", len(got), len(data))
	}
}
