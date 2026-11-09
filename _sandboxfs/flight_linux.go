//go:build linux

package sandboxfs

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const TheoryOfFlightRecorder = `
Flight recorder theory:
- The table cannot answer what happened around one slow request: it keeps a
  count and a maximum per opcode and discards the order, so the sequence that
  led to a hundred-millisecond request is gone by the time anyone reads it.
- The recorder keeps that sequence. A bounded ring holds the most recent
  requests — opcode, node, duration, completion time — and a request that
  sets a new slowest record above a floor copies the ring. The copy is the
  flight window: the lead-up to the slowest request, held in memory.
- The ring costs one array store per request and is allocated once, so it
  never grows and never allocates in the request path. The copy costs one
  bounded array copy per new record, which a run that never slows down never
  pays.
- A request under the floor is not a problem: the table answers for it. The
  window exists for the request a user would call slow, so a new maximum
  under the floor replaces nothing.
- The window shows requests, not kernel events, for the same reason the table
  does: the server sees requests. Its value is the order and the shape —
  which request came first, which one stalled, what the workload asked for
  around it — and the node identifiers resolve to the paths the review
  already speaks.
`

const (
	// flightWindowRequests is the number of recent requests the flight ring
	// holds: enough to cover the lead-up to a slow request in a directory
	// walk or a file write. See TheoryOfFlightRecorder.
	flightWindowRequests = 512

	// flightFloor is the duration under which a request is not treated as a
	// problem. The summary table answers for such a request, and a window
	// copied for every sub-millisecond maximum would be noise.
	// See TheoryOfFlightRecorder.
	flightFloor = time.Millisecond

	// flightReportRows caps the rendered window, so one report stays
	// readable; the rows closest to the trigger survive.
	// See TheoryOfFlightRecorder.
	flightReportRows = 40
)

// flightRecord is one request in the flight ring: what the server answered,
// which node it named, how long the answer took, and when it finished. The
// ring holds the record by value, so a request costs one array store and no
// allocation. See TheoryOfFlightRecorder.
type flightRecord struct {
	opcode   uint32
	nodeid   uint64
	duration time.Duration
	end      time.Time
}

// flightWindow is the copy of the flight ring taken when a request set a new
// slowest record: the requests leading up to it, oldest first with the
// trigger last. The records are immutable once taken, so a report renders
// them without the aggregate's lock. See TheoryOfFlightRecorder.
type flightWindow struct {
	records []flightRecord
	trigger flightRecord
	covered time.Duration
}

// pushWindowLocked appends one request to the flight ring and, when the
// request set a new slowest record at or above flightFloor, copies the ring
// into the flight window. The copy is bounded by flightWindowRequests and
// happens only when a request is slower than every request before it, so a
// run that never slows down never pays for one. The caller holds the lock.
// See TheoryOfFlightRecorder.
func (t *OpTimings) pushWindowLocked(r flightRecord) {
	if t.flight == nil {
		t.flight = make([]flightRecord, flightWindowRequests)
	}
	t.flight[t.flightNext] = r
	t.flightNext = (t.flightNext + 1) % len(t.flight)
	if t.flightFilled < len(t.flight) {
		t.flightFilled++
	}
	if r.duration < flightFloor {
		return
	}
	if t.window != nil && r.duration <= t.window.trigger.duration {
		return
	}
	t.window = t.takeWindowLocked(r)
}

// takeWindowLocked copies the ring into a flight window: the records in
// completion order, the trigger last. The records slice is allocated once
// per snapshot and never mutated, so a report renders it outside the lock.
// The caller holds the lock. See TheoryOfFlightRecorder.
func (t *OpTimings) takeWindowLocked(trigger flightRecord) *flightWindow {
	records := make([]flightRecord, 0, t.flightFilled)
	for i := 0; i < t.flightFilled; i++ {
		slot := (t.flightNext - t.flightFilled + i + len(t.flight)) % len(t.flight)
		records = append(records, t.flight[slot])
	}
	w := &flightWindow{
		records: records,
		trigger: trigger,
	}
	if len(records) > 1 {
		w.covered = trigger.end.Sub(records[0].end)
	}
	return w
}

// render draws the flight window: the trigger's identity first, then the
// requests leading up to it, one row each in completion order with the offset
// from the window's first request. Rows past flightReportRows drop from the
// front, so the moments closest to the trigger stay readable. resolve names a
// node id, and a nil resolver leaves the path out. See TheoryOfFlightRecorder.
func (w *flightWindow) render(resolve func(nodeid uint64) string) string {
	if w == nil || len(w.records) == 0 {
		return ""
	}
	name := func(nodeid uint64) string {
		if resolve == nil || nodeid == 0 {
			return ""
		}
		return resolve(nodeid)
	}
	target := opcodeName(w.trigger.opcode)
	if path := name(w.trigger.nodeid); path != "" {
		target += " " + path
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"flight window: the slowest request was %s in %s; the recorded lead-up covers %s\n",
		target,
		w.trigger.duration.Round(time.Microsecond),
		w.covered.Round(time.Microsecond),
	)
	counts := make(map[string]int, len(w.records))
	for _, r := range w.records {
		counts[opcodeName(r.opcode)]++
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s %d", n, counts[n]))
	}
	fmt.Fprintf(&b, "  window composition: %s\n", strings.Join(parts, ", "))
	rows := w.records
	dropped := 0
	if len(rows) > flightReportRows {
		dropped = len(rows) - flightReportRows
		rows = rows[len(rows)-flightReportRows:]
	}
	base := w.records[0].end
	fmt.Fprintf(&b, "  %8s  %10s  %-12s  %s\n", "offset", "duration", "op", "path")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %8s  %10s  %-12s  %s\n",
			r.end.Sub(base).Round(time.Microsecond),
			r.duration.Round(time.Microsecond),
			opcodeName(r.opcode),
			name(r.nodeid),
		)
	}
	if dropped > 0 {
		fmt.Fprintf(&b, "  (%d earlier requests dropped)\n", dropped)
	}
	return b.String()
}
