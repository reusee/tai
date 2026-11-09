//go:build linux

package sandboxfs

import (
	"encoding/binary"
	"errors"
	"syscall"
	"testing"
)

// TestInitNegotiatesMajorVersion protects TheoryOfInitNegotiation: a kernel
// that names a newer protocol major is told the server's own major and
// reopens the handshake at that version; a kernel that names an older major
// speaks a protocol this server cannot serve.
func TestInitNegotiatesMajorVersion(t *testing.T) {
	s := newInitTestServer(t)
	out, err := s.handleInit(encodeStruct(&fuseInitIn{Major: 8, Minor: 0}))
	if err != nil {
		t.Fatalf("kernel major 8: %v", err)
	}
	if got := binary.LittleEndian.Uint32(out[0:]); got != fuseProtocolMajor {
		t.Errorf("response major for kernel 8: got %d, want %d", got, fuseProtocolMajor)
	}
	if _, err := s.handleInit(encodeStruct(&fuseInitIn{Major: 6, Minor: 0})); !errors.Is(err, syscall.EPROTO) {
		t.Errorf("kernel major 6: got %v, want EPROTO", err)
	}
	// A kernel that speaks a newer minor is capped, not echoed back: the
	// server answers at the last field definition it knows.
	out, err = s.handleInit(encodeStruct(&fuseInitIn{Major: 7, Minor: 99}))
	if err != nil {
		t.Fatalf("kernel major 7: %v", err)
	}
	if got := binary.LittleEndian.Uint32(out[4:]); got != fuseProtocolMinor {
		t.Errorf("response minor for kernel 99: got %d, want %d", got, fuseProtocolMinor)
	}
}
