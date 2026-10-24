package sandboxfs

import (
	"bytes"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

const TheoryOfConcurrentStoreAccess = `
concurrent store access theory:
- The FUSE server answers one request at a time, so concurrency lives
  in the store, not in the mount: the concurrent benchmarks drive the
  store directly. See TheoryOfFuseMount.
- The store answers every method concurrently. A read of a path the
  session never wrote costs one lock-free absence lookup, one lock-free
  first-read check, and one base window read, so parallel readers run
  beside each other without a lock between them.
- Every mutation reaches the one sqlite connection the store holds, so
  concurrent writers serialize there. The design accepts that
  serialization: one connection keeps a session's reads and writes
  consistent without a second locking layer, and a coding task spends
  far more time reading than writing. The writers-only rate divides
  that serialized engine throughput by the write amplification of a
  chunk-shaped partial write (see TheoryOfChunkedContent); the chunk
  size's balance is what keeps the partial write near the transaction's
  own cost instead of at the chunk's size.
- A read of a path the session wrote reaches that same connection, so a
  mixed workload measures both sides at once — the contention a
  toolchain produces when its readers and writers run together. The
  writers-only benchmark is the ceiling the single connection puts on
  the write side, and the readers-only benchmark states how far the read
  path scales. Read all three against BenchmarkReadRangeBasePath, the
  single-threaded cost of one read. See TheoryOfUpperAbsentCache and
  TheoryOfChunkedContent.
- The mixed benchmark must run readers and writers at the same time,
  not in alternating all-read and all-write phases. Every worker that
  alternates from the same iteration index starts on the same side, and
  the phases meet only after the workers drift apart; the workers
  therefore take a start parity from one counter, so half begin with a
  read and half with a write, and the two loads overlap from the first
  iteration.
- The reported rates stay separately meaningful: the readers-only and
  writers-only rates each divide by one page of one direction, while the
  mixed benchmark reports its iteration cost alone, because a rate
  over two directions per iteration would be comparable to neither.
`

const (
	// concurrentPageSize is the payload of one splice and the size of one
	// read window: one page, the window a kernel write carries most often.
	concurrentPageSize = 4 << 10

	// concurrentWindow bounds the offsets the benchmarks cycle through, so
	// both files keep the same size on every iteration.
	concurrentWindow = concurrentPageSize * 16
)

// newConcurrentBenchStore builds the store the concurrent benchmarks share:
// one base file the readers serve and one empty upper-layer file the writers
// splice into. The payload is the page both directions carry.
func newConcurrentBenchStore(b *testing.B) (*Store, []byte) {
	b.Helper()
	base := b.TempDir()
	content := bytes.Repeat([]byte("x"), concurrentWindow)
	if err := os.WriteFile(filepath.Join(base, "a.bin"), content, 0o644); err != nil {
		b.Fatal(err)
	}
	store, err := Open(filepath.Join(b.TempDir(), "sandbox.db"), base)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	if err := store.WriteFile("/w.bin", nil, 0o644); err != nil {
		b.Fatal(err)
	}
	return store, bytes.Repeat([]byte("y"), concurrentPageSize)
}

// concurrentOffset maps an iteration onto one page of the window, so both
// files stay bounded and every operation lands on a real window.
func concurrentOffset(i int) int64 {
	return int64(i%(concurrentWindow/concurrentPageSize)) * concurrentPageSize
}

// concurrentPhase returns a worker's start parity from one counter, so
// neighbouring workers begin on opposite sides of the read/write alternation.
// Every worker that started from the same iteration index would run an
// all-read phase followed by an all-write phase, and the two loads would meet
// only after the workers drifted apart. See TheoryOfConcurrentStoreAccess.
func concurrentPhase(counter *atomic.Int64) int {
	return int(counter.Add(1)-1) & 1
}

// BenchmarkConcurrentRead measures reader parallelism: every parallel
// goroutine reads one page of a base file, so the workload states how far the
// read path scales when a toolchain drives many readers at once. The rate
// divides by the one page the iteration reads. See
// TheoryOfConcurrentStoreAccess.
func BenchmarkConcurrentRead(b *testing.B) {
	store, _ := newConcurrentBenchStore(b)
	b.SetBytes(concurrentPageSize)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if _, err := store.ReadRange("/a.bin", concurrentOffset(i), concurrentPageSize); err != nil {
				b.Errorf("read: %v", err)
				return
			}
		}
	})
}

// BenchmarkConcurrentReadWrite measures the mixed workload a toolchain
// produces: every parallel goroutine alternates a base read with a splice, and
// the workers start on opposite sides, so the readers are served by the base
// while the writers queue on the store's one connection. No rate is reported:
// one iteration carries a page in each direction. See
// TheoryOfConcurrentStoreAccess.
func BenchmarkConcurrentReadWrite(b *testing.B) {
	store, payload := newConcurrentBenchStore(b)
	var workers atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		phase := concurrentPhase(&workers)
		for i := 0; pb.Next(); i++ {
			if (i+phase)%2 == 0 {
				if _, err := store.ReadRange("/a.bin", concurrentOffset(i), concurrentPageSize); err != nil {
					b.Errorf("read: %v", err)
					return
				}
				continue
			}
			if err := store.SpliceFile("/w.bin", concurrentOffset(i), payload, 0o644); err != nil {
				b.Errorf("splice: %v", err)
				return
			}
		}
	})
}

// BenchmarkConcurrentSplice measures the writers-only ceiling: every iteration
// splices a page, so the iterations queue on the store's one connection and
// the result states what the write side cannot exceed. The rate divides by the
// one page the iteration writes. See TheoryOfConcurrentStoreAccess.
func BenchmarkConcurrentSplice(b *testing.B) {
	store, payload := newConcurrentBenchStore(b)
	b.SetBytes(concurrentPageSize)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if err := store.SpliceFile("/w.bin", concurrentOffset(i), payload, 0o644); err != nil {
				b.Errorf("splice: %v", err)
				return
			}
		}
	})
}
