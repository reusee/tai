//go:build linux

package sandboxfs

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writerFunc adapts a function to io.Writer for the reporter test.
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestOpTimingsRecordAndSnapshot checks the aggregate the read loop feeds:
// counts, per-opcode ordering by accumulated time, and the derived average.
// See TheoryOfOperationTiming.
func TestOpTimingsRecordAndSnapshot(t *testing.T) {
	timings := newOpTimings()
	now := time.Now()
	timings.recordRequest(fuseOpLookup, requestMark{start: now.Add(-10 * time.Millisecond)})
	timings.recordRequest(fuseOpRead, requestMark{start: now.Add(-2 * time.Millisecond)})
	timings.recordRequest(fuseOpLookup, requestMark{start: now.Add(-30 * time.Millisecond)})

	entries := timings.Snapshot()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want one per recorded opcode (2)", len(entries))
	}
	if entries[0].Op != "lookup" || entries[1].Op != "read" {
		t.Fatalf("entries are %q and %q, want lookup ordered before read by accumulated time",
			entries[0].Op, entries[1].Op)
	}
	if entries[0].Count != 2 {
		t.Fatalf("lookup count is %d, want 2", entries[0].Count)
	}
	if entries[0].Max < 30*time.Millisecond {
		t.Fatalf("lookup max is %s, want the slowest request recorded", entries[0].Max)
	}
	if avg := entries[0].Avg(); avg < 20*time.Millisecond {
		t.Fatalf("lookup average is %s, want the mean of both requests", avg)
	}
	if total := timings.Total(); total < 40*time.Millisecond {
		t.Fatalf("total is %s, want the sum of every request", total)
	}
}

// TestOpTimingsNilReceiverIsInert checks that a server built without the
// constructor measures nothing instead of panicking: the read loop calls both
// hooks unconditionally. See TheoryOfOperationTiming.
func TestOpTimingsNilReceiverIsInert(t *testing.T) {
	var timings *OpTimings
	if mark := timings.markRequest(0); !mark.start.IsZero() {
		t.Fatal("nil aggregate stamped a request")
	}
	timings.recordRequest(fuseOpRead, requestMark{start: time.Now()})
	if entries := timings.Snapshot(); entries != nil {
		t.Fatalf("nil aggregate reported %d entries", len(entries))
	}
	if text := timings.Report(); text != "" {
		t.Fatalf("nil aggregate reported %q", text)
	}
	if total := timings.Total(); total != 0 {
		t.Fatalf("nil aggregate totalled %s", total)
	}
	stop := timings.StartReporter(time.Millisecond, io.Discard)
	stop()
	stop()
}

// TestOpTimingsReportOrdersByTotal checks the report's shape: a summary line
// first, the bottleneck's row next, and a trailing blank line so a periodic
// report stays separated from what precedes it.
func TestOpTimingsReportOrdersByTotal(t *testing.T) {
	timings := newOpTimings()
	now := time.Now()
	timings.recordRequest(fuseOpWrite, requestMark{start: now.Add(-time.Millisecond)})
	timings.recordRequest(fuseOpLookup, requestMark{start: now.Add(-50 * time.Millisecond)})

	text := timings.Report()
	if !strings.HasPrefix(text, "sandboxfs operation timings:") {
		t.Fatalf("report does not open with its summary line: %q", text)
	}
	if !strings.HasSuffix(text, "\n\n") {
		t.Fatalf("report does not end with a blank line: %q", text)
	}
	lookup, write := strings.Index(text, " lookup"), strings.Index(text, " write")
	if lookup < 0 || write < 0 {
		t.Fatalf("report does not name both opcodes: %q", text)
	}
	if lookup > write {
		t.Fatalf("report does not put the larger accumulated time first: %q", text)
	}
}

// TestOpTimingsStartReporterWritesSnapshots checks the periodic report: it
// writes a snapshot on its interval, and the snapshot carries the recorded
// distribution.
func TestOpTimingsStartReporterWritesSnapshots(t *testing.T) {
	timings := newOpTimings()
	timings.recordRequest(fuseOpLookup, requestMark{start: time.Now().Add(-time.Millisecond)})
	wrote := make(chan string, 4)
	stop := timings.StartReporter(time.Millisecond, writerFunc(func(p []byte) (int, error) {
		select {
		case wrote <- string(p):
		default:
		}
		return len(p), nil
	}))
	defer stop()
	select {
	case text := <-wrote:
		if !strings.Contains(text, "lookup") {
			t.Fatalf("snapshot does not name the recorded opcode: %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reporter wrote no snapshot")
	}
}

// TestTimingIntervalOf checks the environment value's interpretation: a
// positive duration sets the interval and every other value keeps the
// default, so CAI_SANDBOX_TIMING=1s makes a short run report.
func TestTimingIntervalOf(t *testing.T) {
	if got := timingIntervalOf(""); got != defaultTimingReportInterval {
		t.Fatalf("empty value interval is %s, want the default", got)
	}
	if got := timingIntervalOf("not a duration"); got != defaultTimingReportInterval {
		t.Fatalf("unparsable value interval is %s, want the default", got)
	}
	if got := timingIntervalOf("-1s"); got != defaultTimingReportInterval {
		t.Fatalf("negative value interval is %s, want the default", got)
	}
	if got := timingIntervalOf(" 250ms "); got != 250*time.Millisecond {
		t.Fatalf("parsable value interval is %s, want 250ms", got)
	}
}

// TestNewFuseServerStartsWithTimings checks the wiring every server comes
// from: the constructor installs the aggregate the read loop feeds.
func TestNewFuseServerStartsWithTimings(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if server := newFuseServer(store, -1); server.timings == nil {
		t.Fatal("server carries no timing aggregate; the read loop would measure nothing")
	}
}

// TestMountedFSReportTimingsFinalSnapshot checks the final report path: a
// session that ends before a report interval still reports where its time
// went, the snapshot is written once, and a mount without the timing
// environment writes nothing. See TheoryOfOperationTiming.
func TestMountedFSReportTimingsFinalSnapshot(t *testing.T) {
	timings := newOpTimings()
	timings.recordRequest(fuseOpGetattr, requestMark{start: time.Now().Add(-time.Millisecond)})
	stopped := false
	m := &MountedFS{
		server:     &fuseServer{timings: timings},
		stopTiming: func() { stopped = true },
	}
	var b strings.Builder
	m.reportTimings(&b)
	if !stopped {
		t.Fatal("the periodic reporter was not stopped before the final snapshot")
	}
	if !strings.Contains(b.String(), "getattr") {
		t.Fatalf("final snapshot does not name the recorded opcode: %q", b.String())
	}
	b.Reset()
	m.reportTimings(&b)
	if b.Len() != 0 {
		t.Fatalf("a second final snapshot was written: %q", b.String())
	}
	var none MountedFS
	none.reportTimings(&b)
	if b.Len() != 0 {
		t.Fatalf("a mount without the timing environment wrote a snapshot: %q", b.String())
	}
}
