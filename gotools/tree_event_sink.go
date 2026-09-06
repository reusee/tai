package gotools

import "sync"

// TreeEventSink buffers gotools' context-assembly diagnostics so they
// can join the session tree as event nodes. The token composition
// summaries are produced during PartsProvider.Parts and SimplifyFiles,
// which run before the generation loop opens the session tree, so the
// records are buffered here and drained by Module.Run at startup,
// replayed as context event nodes under the session root. The sink is
// a pointer value provided per scope: dscope caches the provider
// result, so every recorder in one scope shares one sink, and the goal
// runner's per-loop dscope.Reset installs a fresh sink per loop, so
// records never cross loop boundaries. All methods are nil-receiver
// safe, so call sites need no nil checks. See TheoryOfTokenComposition
// and pipeline.TheoryOfLoopEvents.
type TreeEventSink struct {
	mu      sync.Mutex
	records []string
}

// Record buffers one diagnostic record for the session tree. An empty
// detail is skipped.
func (s *TreeEventSink) Record(detail string) {
	if s == nil || detail == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, detail)
}

// Drain returns the buffered records and empties the sink.
func (s *TreeEventSink) Drain() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.records
	s.records = nil
	return records
}

// TreeEventSink provides the per-scope sink buffering gotools'
// context-assembly diagnostics for the session tree. See
// TheoryOfTokenComposition and pipeline.TheoryOfLoopEvents.
func (Module) TreeEventSink() *TreeEventSink {
	return &TreeEventSink{}
}
