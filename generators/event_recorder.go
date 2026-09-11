package generators

import "sync"

const TheoryOfEventRecorder = `
The event recorder carries generator-level API error events
(api_error) from generator implementations into the session tree.
Generators hold the recorder as a dscope.Inject[EventRecorder] field,
filled by dscope.InjectStruct when the generator is constructed, so
they never receive a recorder object and never depend on the pipeline
or records packages, which would create an import cycle.

The recorder is a sink: the generator writes an event (type plus
free-form detail) and the sink buffers it. The per-scope EventSink is
the default EventRecorder, so generator events are captured by default;
dscope caches the provider, so every generator in one scope writes into
the same sink, and a goal loop's scope Reset installs a fresh sink per
loop. The generation loop drains the sink after every round and records
each buffered event as a session-tree node of the same type, so
API-level failures join the same operation stream as the rest of the
session — the recorder persists the tree, and the tree carries the
events.
`

// Event is one recorded generator-level event: a type (api_error)
// and a free-form detail. See TheoryOfEventRecorder.
type Event struct {
	Type   string
	Detail string
}

// EventRecorder records generation events such as API calls and API
// errors. Generator implementations hold the recorder as a
// dscope.Inject[EventRecorder] field so they can record events without
// depending on the pipeline or records packages, which would create an
// import cycle. See TheoryOfEventRecorder.
type EventRecorder interface {
	Enabled() bool
	Event(typ string, detail string)
}

// EventSink is the default EventRecorder: a per-scope, drainable buffer
// of generator events. The generation loop drains it after every round
// and records each buffered event as a session-tree event node. See
// TheoryOfEventRecorder.
type EventSink struct {
	mu     sync.Mutex
	events []Event
}

func NewEventSink() *EventSink {
	return &EventSink{}
}

func (s *EventSink) Enabled() bool {
	return s != nil
}

func (s *EventSink) Event(typ string, detail string) {
	if s == nil || detail == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, Event{Type: typ, Detail: detail})
}

// Drain returns the buffered events and empties the sink, so a
// drain-and-record pass never re-records an event. See
// TheoryOfEventRecorder.
func (s *EventSink) Drain() []Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.events
	s.events = nil
	return events
}

// EventSink provides the per-scope buffer generator implementations
// write their events into. dscope caches the provider per scope, so
// every generator in one scope writes into the same sink; a goal loop's
// scope Reset installs a fresh sink per loop. See TheoryOfEventRecorder.
func (Module) EventSink() *EventSink {
	return NewEventSink()
}

// EventRecorder provides the default recorder: the scope's EventSink.
// Generator events are therefore captured by default and reach the
// session tree through the generation loop's drain. See
// TheoryOfEventRecorder.
func (Module) EventRecorder(sink *EventSink) EventRecorder {
	return sink
}
