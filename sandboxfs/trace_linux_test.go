//go:build linux

package sandboxfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime/trace"
	"testing"

	"golang.org/x/sys/unix"
)

const TheoryOfTraceWorkload = `
Trace workload theory:
- The workload drives the server through its kernel-facing loop, not by
  calling handlers directly: requests are written as FUSE frames on a
  SEQPACKET socketpair and replies are read back, so the measured cost is the
  whole request path — the read loop, the header decode, the handler, the
  reply encode, and the reply write. A harness that called handlers directly
  would bypass the request loop and the operation-timing aggregate, and the
  table would stay empty.
- The workload replays a toolchain's access pattern: a directory walk
  (lookup, getattr, opendir, readdirplus, releasedir), windowed reads of an
  unchanged base file, and create, write, and truncate on the upper layer.
- The operation-timing table and the execution trace answer different
  questions. The table attributes each request's total cost to its opcode and
  names the expensive operation; the trace splits one operation's cost into
  the runtime work inside it — reflection, allocation, garbage collection,
  sqlite calls, syscalls — so a fix targets the cost that dominates there.
- The workload repeats every phase for several rounds because a first round
  pays one-time costs (handle opens, absence-cache misses, sqlite warm-up)
  that a steady-state request never pays again. Steady-state cost is what a
  session's toolchain actually experiences.
`

const (
	// traceEnv names the trace file to write; empty writes into the test's
	// temporary directory.
	traceEnv = "TAI_SANDBOX_TRACE"

	traceWorkloadFiles    = 64
	traceWorkloadRounds   = 6
	traceWorkloadReads    = 8
	traceWorkloadWrites   = 3
	traceWorkloadFileSize = 96 << 10
	traceWindowSize       = 32 << 10
	traceWriteChunk       = 32 << 10
	traceReplyBufferSize  = fuseInHeaderSize + fuseOutHeaderSize + fuseMaxWriteMax
)

// TestTraceFuseWorkload drives the server through its request loop under
// runtime/trace and logs the per-opcode timing table. See
// TheoryOfTraceWorkload.
func TestTraceFuseWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("trace workload skipped in short mode")
	}
	base := t.TempDir()
	treeDir := filepath.Join(base, "tree")
	if err := os.MkdirAll(treeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("abcdefgh"), traceWorkloadFileSize/8)
	for i := 0; i < traceWorkloadFiles; i++ {
		p := filepath.Join(treeDir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(filepath.Join(t.TempDir(), "trace.db"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server, client := startTraceServer(t, store)

	traceFile := os.Getenv(traceEnv)
	if traceFile == "" {
		traceFile = filepath.Join(t.TempDir(), "fuse.trace")
	}
	f, err := os.OpenFile(traceFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := trace.Start(f); err != nil {
		t.Fatal(err)
	}
	runTraceWorkload(t, client)
	trace.Stop()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("execution trace: %s", traceFile)
	if report := server.timings.Report(); report != "" {
		t.Logf("operation timings:\n%s", report)
	}
}

// traceClient speaks the kernel protocol over one end of a SEQPACKET
// socketpair. See TheoryOfTraceWorkload.
type traceClient struct {
	t      *testing.T
	fd     int
	unique uint64
	buf    []byte
}

// startTraceServer wires a store to one end of a SEQPACKET socketpair and runs
// the server's request loop on the other end, so the workload measures the
// whole request path and the operation-timing aggregate records it.
func startTraceServer(t *testing.T, store *Store) (*fuseServer, *traceClient) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		t.Fatal(err)
	}
	size := fuseMaxWrite * 2
	_ = unix.SetsockoptInt(fds[0], unix.SOL_SOCKET, unix.SO_RCVBUF, size)
	_ = unix.SetsockoptInt(fds[1], unix.SOL_SOCKET, unix.SO_SNDBUF, size)
	server := newFuseServer(store, fds[0])
	go server.run()
	client := &traceClient{t: t, fd: fds[1], buf: make([]byte, traceReplyBufferSize)}
	t.Cleanup(func() {
		_ = unix.Close(fds[1])
		<-server.done
		_ = unix.Close(fds[0])
	})
	return server, client
}

// request sends one FUSE request and returns the reply body and errno. The
// request frame carries the header followed by the body; the reply is one
// datagram on the SEQPACKET socket, so one read returns one reply.
// See TheoryOfTraceWorkload.
func (c *traceClient) request(opcode uint32, nodeid uint64, body []byte) ([]byte, int32) {
	c.t.Helper()
	c.unique++
	req := make([]byte, fuseInHeaderSize+len(body))
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], opcode)
	binary.LittleEndian.PutUint64(req[8:16], c.unique)
	binary.LittleEndian.PutUint64(req[16:24], nodeid)
	copy(req[fuseInHeaderSize:], body)
	if _, err := unix.Write(c.fd, req); err != nil {
		c.t.Fatalf("write request op %d: %v", opcode, err)
	}
	n, err := unix.Read(c.fd, c.buf)
	if err != nil {
		c.t.Fatalf("read reply op %d: %v", opcode, err)
	}
	if n < fuseOutHeaderSize {
		c.t.Fatalf("short reply op %d: %d bytes", opcode, n)
	}
	if got := binary.LittleEndian.Uint64(c.buf[8:16]); got != c.unique {
		c.t.Fatalf("reply op %d carries unique %d, want %d", opcode, got, c.unique)
	}
	errno := int32(binary.LittleEndian.Uint32(c.buf[4:8]))
	out := make([]byte, n-fuseOutHeaderSize)
	copy(out, c.buf[fuseOutHeaderSize:n])
	return out, errno
}

// must succeeds or fails the test with the request's opcode and errno.
func (c *traceClient) must(opcode uint32, nodeid uint64, body []byte) []byte {
	c.t.Helper()
	out, errno := c.request(opcode, nodeid, body)
	if errno != 0 {
		c.t.Fatalf("op %d node %d: errno %d", opcode, nodeid, errno)
	}
	return out
}

func (c *traceClient) init() {
	c.must(fuseOpInit, 0, encodeStruct(&fuseInitIn{
		Major:        7,
		Minor:        fuseProtocolMinor,
		MaxReadahead: 128 << 10,
		Flags:        fuseInitWanted,
	}))
}

// entryNodeid decodes the node id of a fuse_entry_out reply.
func entryNodeid(body []byte) uint64 {
	return binary.LittleEndian.Uint64(body[0:8])
}

// openHandle decodes the file handle of a fuse_open_out or fuse_create_out
// reply.
func openHandle(body []byte, create bool) uint64 {
	if create {
		return binary.LittleEndian.Uint64(body[fuseEntryOutSize:])
	}
	return binary.LittleEndian.Uint64(body[0:8])
}

func runTraceWorkload(t *testing.T, c *traceClient) {
	t.Helper()
	c.init()
	rootID := uint64(fuseRootID)
	treeID := entryNodeid(c.must(fuseOpLookup, rootID, []byte("tree\x00")))
	names := make([][]byte, traceWorkloadFiles)
	ids := make([]uint64, traceWorkloadFiles)
	for i := range names {
		names[i] = []byte(fmt.Sprintf("f%03d.txt\x00", i))
	}
	for round := 0; round < traceWorkloadRounds; round++ {
		t.Logf("round %d", round)
		walkTree(t, c, treeID, names, ids)
		readWindows(t, c, ids, round)
		writeFiles(t, c, round)
	}
}

func walkTree(t *testing.T, c *traceClient, treeID uint64, names [][]byte, ids []uint64) {
	t.Helper()
	for i, name := range names {
		ids[i] = entryNodeid(c.must(fuseOpLookup, treeID, name))
		c.must(fuseOpGetattr, ids[i], nil)
	}
	dirFh := openHandle(c.must(fuseOpOpendir, treeID, encodeStruct(&fuseOpenIn{})), false)
	c.must(fuseOpReaddirplus, treeID, encodeStruct(&fuseReadIn{
		Fh:   dirFh,
		Size: traceWindowSize,
	}))
	c.must(fuseOpReleasedir, treeID, encodeStruct(&fuseReleaseIn{Fh: dirFh}))
}

func readWindows(t *testing.T, c *traceClient, ids []uint64, round int) {
	t.Helper()
	for i := 0; i < traceWorkloadReads; i++ {
		node := ids[(round*traceWorkloadReads+i)%len(ids)]
		fh := openHandle(c.must(fuseOpOpen, node, encodeStruct(&fuseOpenIn{})), false)
		var off int64
		for off < traceWorkloadFileSize {
			out := c.must(fuseOpRead, node, encodeStruct(&fuseReadIn{
				Fh:     fh,
				Offset: uint64(off),
				Size:   traceWindowSize,
			}))
			if len(out) == 0 {
				break
			}
			off += int64(len(out))
		}
		c.must(fuseOpRelease, node, encodeStruct(&fuseReleaseIn{Fh: fh}))
	}
}

func writeFiles(t *testing.T, c *traceClient, round int) {
	t.Helper()
	payload := bytes.Repeat([]byte("w"), traceWriteChunk)
	for i := 0; i < traceWorkloadWrites; i++ {
		name := []byte(fmt.Sprintf("w%02d-%02d.bin\x00", round, i))
		created := c.must(fuseOpCreate, uint64(fuseRootID), append(encodeStruct(&fuseCreateIn{
			Flags: uint32(unix.O_CREAT | unix.O_WRONLY),
			Mode:  0o644,
		}), name...))
		fh := openHandle(created, true)
		node := entryNodeid(created)
		for off := 0; off < traceWorkloadFileSize; off += traceWriteChunk {
			body := append(encodeStruct(&fuseWriteIn{
				Fh:     fh,
				Offset: uint64(off),
				Size:   uint32(len(payload)),
			}), payload...)
			c.must(fuseOpWrite, node, body)
		}
		c.must(fuseOpSetattr, node, encodeStruct(&fuseSetattrIn{
			Valid: fattrSize,
			Size:  traceWorkloadFileSize / 2,
		}))
		c.must(fuseOpRelease, node, encodeStruct(&fuseReleaseIn{Fh: fh}))
	}
}
