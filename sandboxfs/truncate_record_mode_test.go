package sandboxfs

import (
	"os"
	"testing"
)

// TestTruncateRepairsTheRecordContentMode protects TheoryOfDirUpper: the record
// is the layer's private data, so a content file whose own bits refuse the
// layer's write must not refuse the session's truncation. The audit trail's
// "openat <hash>.data: permission denied" on a truncate row is the failure this
// test reproduces.
func TestTruncateRepairsTheRecordContentMode(t *testing.T) {
	dir := t.TempDir()
	record := dir + "/record"
	store, err := OpenDir(record, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/f", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The record's content file loses the owner's write bit: the shape a record
	// an older build wrote, a copied record, or a host-side tool leaves behind.
	if err := os.Chmod(record+"/"+dataName("/f"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := store.TruncateFile("/f", 4, 0o644); err != nil {
		t.Fatalf("truncate through a record content file that refused the write: %v", err)
	}
	content, err := store.ReadFile("/f")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "0123" {
		t.Fatalf("after the truncate: got %q, want %q", content, "0123")
	}
}
