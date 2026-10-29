//go:build linux

package sandboxfs

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const TheoryOfOperationTiming = `
Operation timing theory:
- The filesystem's cost is the sum of its requests' costs, so the measurement
  attributes one request's duration and count to the request's opcode: one
  start stamp and one record per request in the server's worker loop and one
  map entry per opcode. Nothing is sampled, and nothing is recorded per byte or
  per cell, so the numbers answer which operation a workload spends its time in
  without changing what the workload does.
- The table and the flight window answer different questions from one
  measurement: the table names the operation that costs the most over a run,
  the window names the requests that led to the slowest one. They share the
  worker loop, the start stamp, and the lock that guards the aggregate, so a
  session pays one measurement, not two; the window's own model lives in
  TheoryOfFlightRecorder. The aggregate carries its own lock because several
  workers record at once. See TheoryOfFuseConcurrency.
- The measurement covers the server's own work: dispatch plus the reply
  write. Kernel-side work never reaches the server, so a lookup the kernel
  answers from its attribute cache, the caller's own syscalls, and the
  scheduling between them are invisible here. A run whose kernel hit rate is
  high therefore shows a small measured total, and the per-opcode average is
  the number to read: it is the cost each additional request would pay.
- A measured duration is the request's own work, never a wait for another
  request's turn: concurrency is what makes the two distinguishable, and a
  duration that grew with the queue would name the wrong bottleneck. See
  TheoryOfFuseConcurrency.
- Recording is always on because a flag would be missing exactly when nobody
  thought to enable it; the cost is negligible against the database round trip
  and the syscall that produced the request. Reporting is gated, because a
  report writes to a stream the session does not otherwise touch.
- A report is a snapshot: entries are copied under one lock, so a report never
  blocks a request for longer than a map scan, and the reporter runs beside the
  workers without synchronizing with them. Entries are ordered by
  accumulated time, so the first row names the bottleneck. The periodic
  reporter writes one snapshot per interval, and unmounting the mount writes
  the final one, so a session that ends before the first interval still
  reports the whole run's distribution instead of losing it.
`

// timingEnv enables the periodic operation-timing report when it holds a
// non-empty value. A value that parses as a positive duration sets the report
// interval, which lets a short run still produce a snapshot; any other value
// keeps defaultTimingReportInterval. See TheoryOfOperationTiming.
const timingEnv = "CAI_SANDBOX_TIMING"

// defaultTimingReportInterval is the report cadence when the timing
// environment value names no positive duration.
// See TheoryOfOperationTiming.
const defaultTimingReportInterval = 5 * time.Second

// OpTiming aggregates one opcode's requests. See TheoryOfOperationTiming.
type OpTiming struct {
	Op    string
	Count int64
	Total time.Duration
	Max   time.Duration
}

// Avg returns the mean duration of the opcode's requests, or zero when none
// was recorded.
func (t OpTiming) Avg() time.Duration {
	if t.Count == 0 {
		return 0
	}
	return t.Total / time.Duration(t.Count)
}

// OpTimings aggregates the per-opcode request timings of one FUSE server.
// All methods are safe for concurrent use. A nil aggregate records nothing,
// so a server built without the constructor runs unchanged.
// See TheoryOfOperationTiming and TheoryOfFlightRecorder.
type OpTimings struct {
	mu    sync.Mutex
	byOp  map[uint32]*OpTiming
	start time.Time

	// resolve names a node id for the flight window's report. It is assigned
	// before the request loop starts and read only while rendering a copied
	// window, never in the request path. See TheoryOfFlightRecorder.
	resolve func(nodeid uint64) string

	// flight is the ring of the most recent requests, allocated on the first
	// request and never grown, so recording one request costs one array
	// store. flightNext is the slot the next record takes; flightFilled
	// counts the slots that hold a record. window is the copy taken when a
	// request set a new slowest record above flightFloor, and its records are
	// immutable once taken, so a report reads the pointer and renders outside
	// the lock. See TheoryOfFlightRecorder.
	flight       []flightRecord
	flightNext   int
	flightFilled int
	window       *flightWindow
}

// newOpTimings returns an empty aggregate stamped with its creation time.
func newOpTimings() *OpTimings {
	return &OpTimings{
		byOp:  make(map[uint32]*OpTiming),
		start: time.Now(),
	}
}

// requestMark is one request's start stamp and the node it names. A nil
// aggregate yields the zero mark, which records nothing.
// See TheoryOfOperationTiming and TheoryOfFlightRecorder.
type requestMark struct {
	start  time.Time
	nodeid uint64
}

// markRequest stamps one request, carrying the node it names so the flight
// window's report can resolve a path. A nil aggregate yields the zero mark,
// which records nothing. See TheoryOfOperationTiming and
// TheoryOfFlightRecorder.
func (t *OpTimings) markRequest(nodeid uint64) requestMark {
	if t == nil {
		return requestMark{}
	}
	return requestMark{start: time.Now(), nodeid: nodeid}
}

// recordRequest attributes one request's duration to its opcode and appends
// the request to the flight ring, which takes the window when the request set
// a new slowest record. See TheoryOfOperationTiming and
// TheoryOfFlightRecorder.
func (t *OpTimings) recordRequest(opcode uint32, mark requestMark) {
	if t == nil || mark.start.IsZero() {
		return
	}
	end := time.Now()
	d := end.Sub(mark.start)
	t.mu.Lock()
	e := t.byOp[opcode]
	if e == nil {
		e = &OpTiming{Op: opcodeName(opcode)}
		t.byOp[opcode] = e
	}
	e.Count++
	e.Total += d
	e.Max = max(e.Max, d)
	t.pushWindowLocked(flightRecord{
		opcode:   opcode,
		nodeid:   mark.nodeid,
		duration: d,
		end:      end,
	})
	t.mu.Unlock()
}

// Snapshot returns one entry per recorded opcode, ordered by accumulated time
// descending and by name for equal times. See TheoryOfOperationTiming.
func (t *OpTimings) Snapshot() []OpTiming {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	entries := make([]OpTiming, 0, len(t.byOp))
	for _, e := range t.byOp {
		entries = append(entries, *e)
	}
	t.mu.Unlock()
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Total != entries[j].Total {
			return entries[i].Total > entries[j].Total
		}
		return entries[i].Op < entries[j].Op
	})
	return entries
}

// Total returns the time the server spent answering requests.
func (t *OpTimings) Total() time.Duration {
	var total time.Duration
	for _, e := range t.Snapshot() {
		total += e.Total
	}
	return total
}

// Report renders the aggregate as a table ordered by accumulated time
// descending, so the first row names the bottleneck, followed by the flight
// window when a request set a slowest record. The report ends with a blank
// line so a periodic report stays separated from what precedes it.
// An aggregate that recorded nothing renders the empty string.
// See TheoryOfOperationTiming and TheoryOfFlightRecorder.
func (t *OpTimings) Report() string {
	if t == nil {
		return ""
	}
	entries := t.Snapshot()
	if len(entries) == 0 {
		return ""
	}
	var count int64
	var total time.Duration
	for _, e := range entries {
		count += e.Count
		total += e.Total
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"sandboxfs operation timings: %d requests, %s in requests, %s since start\n",
		count,
		total.Round(time.Millisecond),
		time.Since(t.start).Round(time.Millisecond),
	)
	fmt.Fprintf(&b, "  %8s %10s %10s %10s  %s\n", "count", "total", "avg", "max", "op")
	for _, e := range entries {
		fmt.Fprintf(&b, "  %8d %10s %10s %10s  %s\n",
			e.Count,
			e.Total.Round(time.Millisecond),
			e.Avg().Round(time.Microsecond),
			e.Max.Round(time.Microsecond),
			e.Op,
		)
	}
	// The window's records are immutable once taken, so the report renders
	// them outside the aggregate's lock: the resolver reads the server's node
	// table under its own lock, and the two locks never nest.
	// See TheoryOfFlightRecorder.
	t.mu.Lock()
	window, resolve := t.window, t.resolve
	t.mu.Unlock()
	if text := window.render(resolve); text != "" {
		b.WriteString(text)
	}
	b.WriteString("\n")
	return b.String()
}

// StartReporter writes one report every interval to w until stop is called.
// A nil aggregate, a nil writer, or a non-positive interval degrades
// gracefully, and the returned stop function is idempotent. The stop function
// also waits for an in-flight report to finish before it returns: a final
// snapshot written after stop therefore never interleaves with a periodic one,
// so the two snapshots stay whole and readable.
// See TheoryOfOperationTiming.
func (t *OpTimings) StartReporter(interval time.Duration, w io.Writer) (stop func()) {
	if t == nil || w == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = defaultTimingReportInterval
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	var once sync.Once
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_, _ = io.WriteString(w, t.Report())
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
		wg.Wait()
	}
}

// timingIntervalOf interprets the timing environment value: a positive
// duration sets the report interval, and every other value keeps the default,
// so CAI_SANDBOX_TIMING=1s makes a short run report.
// See TheoryOfOperationTiming.
func timingIntervalOf(value string) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(value)); err == nil && d > 0 {
		return d
	}
	return defaultTimingReportInterval
}

// opcodeNames names the request opcodes the server answers, so a report reads
// as operations rather than as numbers. An opcode without an entry renders as
// "op-<number>", so a newer kernel opcode still reaches the report.
var opcodeNames = map[uint32]string{
	fuseOpLookup:      "lookup",
	fuseOpForget:      "forget",
	fuseOpGetattr:     "getattr",
	fuseOpSetattr:     "setattr",
	fuseOpReadlink:    "readlink",
	fuseOpSymlink:     "symlink",
	fuseOpMknod:       "mknod",
	fuseOpMkdir:       "mkdir",
	fuseOpUnlink:      "unlink",
	fuseOpRmdir:       "rmdir",
	fuseOpRename:      "rename",
	fuseOpLink:        "link",
	fuseOpOpen:        "open",
	fuseOpRead:        "read",
	fuseOpWrite:       "write",
	fuseOpStatfs:      "statfs",
	fuseOpRelease:     "release",
	fuseOpFsync:       "fsync",
	fuseOpFlush:       "flush",
	fuseOpInit:        "init",
	fuseOpOpendir:     "opendir",
	fuseOpReaddir:     "readdir",
	fuseOpReleasedir:  "releasedir",
	fuseOpAccess:      "access",
	fuseOpCreate:      "create",
	fuseOpInterrupt:   "interrupt",
	fuseOpDestroy:     "destroy",
	fuseOpBatchForget: "batch_forget",
	fuseOpRename2:     "rename2",
	fuseOpFsyncdir:    "fsyncdir",
	fuseOpReaddirplus: "readdirplus",
}

// opcodeName returns the display name of one request opcode.
func opcodeName(opcode uint32) string {
	if name, ok := opcodeNames[opcode]; ok {
		return name
	}
	return "op-" + strconv.FormatUint(uint64(opcode), 10)
}
