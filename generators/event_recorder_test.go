package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

// fakeEventRecorder is a caller-supplied EventRecorder for tests that
// assert on recorded events, mirroring a display front-end's or an
// external recorder's implementation.
type fakeEventRecorder struct {
	enabled bool
	events  []string
}

func (f *fakeEventRecorder) Enabled() bool { return f.enabled }

func (f *fakeEventRecorder) Event(typ string, detail string) {
	f.events = append(f.events, typ+": "+detail)
}

func TestEventRecorderDefaultIsSink(t *testing.T) {
	// The generators module provides the scope's EventSink as the
	// default EventRecorder, so generator events are captured by
	// default; the generation loop drains the sink and records every
	// buffered event as a session-tree event node. See
	// TheoryOfEventRecorder.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Call(func(rec EventRecorder, sink *EventSink) {
		if rec == nil {
			t.Fatal("expected the default recorder to be the scope's EventSink")
		}
		if !rec.Enabled() {
			t.Fatal("the default recorder must be enabled")
		}
		rec.Event("api_call", "openai chat completion: model=test-model")
		rec.Event("api_error", "openai http status 503: upstream unavailable")
		drained := sink.Drain()
		if len(drained) != 2 {
			t.Fatalf("expected 2 buffered events, got %d", len(drained))
		}
		if drained[0].Type != "api_call" || drained[1].Type != "api_error" {
			t.Fatalf("unexpected drained events: %+v", drained)
		}
		if drained[0].Detail != "openai chat completion: model=test-model" {
			t.Fatalf("unexpected detail: %q", drained[0].Detail)
		}
		if again := sink.Drain(); len(again) != 0 {
			t.Fatalf("Drain must empty the sink, got %+v", again)
		}
	})
}
