//go:build linux

package sandboxfs

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testPathResolver names a node the way a report renders it: the node table
// belongs to the server, and these tests exercise the window without one.
func testPathResolver(nodeid uint64) string {
	return fmt.Sprintf("/node%d", nodeid)
}

// TestFlightWindowCapturesSlowRequest checks the recorder's core: the window
// holds the requests leading up to the slowest one, the trigger last, and the
// report names the trigger's opcode and path. See TheoryOfFlightRecorder.
func TestFlightWindowCapturesSlowRequest(t *testing.T) {
	timings := newOpTimings()
	timings.resolve = testPathResolver
	now := time.Now()
	for i := 0; i < 5; i++ {
		timings.recordRequest(fuseOpLookup, requestMark{start: now.Add(-100 * time.Microsecond), nodeid: uint64(i)})
	}
	timings.recordRequest(fuseOpWrite, requestMark{start: now.Add(-2 * time.Millisecond), nodeid: 5})
	window := timings.window
	if window == nil {
		t.Fatal("no flight window was taken for a request above the floor")
	}
	if len(window.records) != 6 {
		t.Fatalf("window holds %d records, want the six recorded requests", len(window.records))
	}
	if last := window.records[5]; last.nodeid != 5 || last.opcode != fuseOpWrite {
		t.Fatalf("window ends with node %d op %d, want the trigger last", last.nodeid, last.opcode)
	}
	if window.trigger.duration < 2*time.Millisecond {
		t.Fatalf("trigger duration is %s, want the slow request's", window.trigger.duration)
	}
	text := timings.Report()
	if !strings.Contains(text, "flight window:") || !strings.Contains(text, "/node5") {
		t.Fatalf("report does not render the window with its path:\n%s", text)
	}
}

// TestFlightWindowKeepsSlowest checks the capture rule: a slower request
// replaces the window and a faster one leaves it. See TheoryOfFlightRecorder.
func TestFlightWindowKeepsSlowest(t *testing.T) {
	timings := newOpTimings()
	now := time.Now()
	timings.recordRequest(fuseOpRead, requestMark{start: now.Add(-3 * time.Millisecond)})
	first := timings.window
	if first == nil {
		t.Fatal("no window was taken for the first slow request")
	}
	timings.recordRequest(fuseOpRead, requestMark{start: now.Add(-time.Millisecond)})
	if timings.window != first {
		t.Fatal("a faster request replaced the window")
	}
	timings.recordRequest(fuseOpWrite, requestMark{start: now.Add(-9 * time.Millisecond)})
	if timings.window == first {
		t.Fatal("a slower request did not replace the window")
	}
	if text := timings.Report(); !strings.Contains(text, "flight window:") {
		t.Fatalf("report prints no flight section with a nil resolver:\n%s", text)
	}
}

// TestFlightWindowBelowFloor checks the floor: a run whose slowest request
// stays under it keeps no window, because the summary table answers for such
// requests. See TheoryOfFlightRecorder.
func TestFlightWindowBelowFloor(t *testing.T) {
	timings := newOpTimings()
	timings.recordRequest(fuseOpGetattr, requestMark{start: time.Now().Add(-time.Microsecond)})
	if timings.window != nil {
		t.Fatal("a request under the floor produced a window")
	}
	if text := timings.Report(); strings.Contains(text, "flight window") {
		t.Fatalf("report prints a flight section without a window:\n%s", text)
	}
}

// TestFlightWindowIsBounded checks the ring's bound: the window holds the most
// recent flightWindowRequests requests, not the whole run.
// See TheoryOfFlightRecorder.
func TestFlightWindowIsBounded(t *testing.T) {
	timings := newOpTimings()
	now := time.Now()
	total := flightWindowRequests + 100
	for i := 0; i < total; i++ {
		timings.recordRequest(fuseOpLookup, requestMark{start: now.Add(-10 * time.Microsecond), nodeid: uint64(i)})
	}
	timings.recordRequest(fuseOpWrite, requestMark{start: now.Add(-2 * time.Millisecond), nodeid: uint64(total)})
	window := timings.window
	if window == nil {
		t.Fatal("no window was taken")
	}
	if len(window.records) != flightWindowRequests {
		t.Fatalf("window holds %d records, want the ring's bound %d", len(window.records), flightWindowRequests)
	}
	wantFirst := uint64(total - flightWindowRequests + 1)
	if first := window.records[0].nodeid; first != wantFirst {
		t.Fatalf("window starts at node %d, want the oldest retained request %d", first, wantFirst)
	}
	if last := window.records[len(window.records)-1].nodeid; last != uint64(total) {
		t.Fatalf("window ends at node %d, want the trigger %d", last, total)
	}
}

// TestFlightWindowReportRowsAreCapped checks the rendered size: a full window
// renders flightReportRows rows, the ones closest to the trigger, and names
// the dropped ones. See TheoryOfFlightRecorder.
func TestFlightWindowReportRowsAreCapped(t *testing.T) {
	timings := newOpTimings()
	timings.resolve = testPathResolver
	now := time.Now()
	total := flightWindowRequests
	for i := 0; i < total; i++ {
		timings.recordRequest(fuseOpLookup, requestMark{start: now.Add(-100 * time.Microsecond), nodeid: uint64(i)})
	}
	timings.recordRequest(fuseOpWrite, requestMark{start: now.Add(-2 * time.Millisecond), nodeid: uint64(total)})
	text := timings.Report()
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "  ") && strings.Contains(line, "/node") {
			rows++
		}
	}
	if rows != flightReportRows {
		t.Fatalf("report renders %d window rows, want the cap %d:\n%s", rows, flightReportRows, text)
	}
	if !strings.Contains(text, "earlier requests dropped") {
		t.Fatalf("report does not name the dropped rows:\n%s", text)
	}
}

// TestFuseServerReportResolvesFlightPaths checks the flight window's
// integration with a real server: the aggregate the constructor installs
// resolves a node id through the server's node table, so the window's report
// names the path behind a slow request rather than an opaque id. The node id
// comes from the server's own allocator without a store round trip, so the
// test measures the resolver wiring alone. See TheoryOfFlightRecorder.
func TestFuseServerReportResolvesFlightPaths(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sandbox.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := newFuseServer(store, -1)
	if server.timings.resolve == nil {
		t.Fatal("the server installed no resolver for the flight window")
	}
	id, err := server.nodeOf("/testfile", false)
	if err != nil {
		t.Fatal(err)
	}
	server.timings.recordRequest(fuseOpLookup, requestMark{
		start:  time.Now().Add(-2 * time.Millisecond),
		nodeid: id,
	})
	report := server.timings.Report()
	if !strings.Contains(report, "the slowest request was lookup /testfile") {
		t.Fatalf("flight window does not name the trigger's resolved path:\n%s", report)
	}
}
