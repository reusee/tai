//go:build linux

package sandboxfs

import (
	"encoding/binary"
	"testing"
)

// TestInitOutWireLayoutMatchesTheKernel protects TheoryOfInitNegotiation: the
// kernel parses fuse_init_out by its own struct offsets, where max_pages is a
// 16-bit field and map_alignment and flags2 follow it. A wider max_pages
// shifts the two later fields by two bytes, and the kernel reads an alignment
// and a flag set the server never announced. Both fields are zero on every
// real reply, so the defect stays invisible until the encoder is asked for a
// nonzero value: this test asks.
func TestInitOutWireLayoutMatchesTheKernel(t *testing.T) {
	wire := encodeStruct(&fuseInitOut{
		MaxWrite:     fuseMaxWrite,
		MaxPages:     fuseMaxPages,
		MapAlignment: 5,
		Flags2:       7,
	})
	if len(wire) != 64 {
		t.Fatalf("fuse_init_out size: got %d, want 64", len(wire))
	}
	if got := binary.LittleEndian.Uint32(wire[20:]); got != fuseMaxWrite {
		t.Errorf("max_write at offset 20: got %d, want %d", got, fuseMaxWrite)
	}
	if got := binary.LittleEndian.Uint16(wire[28:]); got != fuseMaxPages {
		t.Errorf("max_pages at offset 28: got %d, want %d", got, fuseMaxPages)
	}
	if got := binary.LittleEndian.Uint16(wire[30:]); got != 5 {
		t.Errorf("map_alignment at offset 30: got %d, want 5", got)
	}
	if got := binary.LittleEndian.Uint32(wire[32:]); got != 7 {
		t.Errorf("flags2 at offset 32: got %d, want 7", got)
	}
}
