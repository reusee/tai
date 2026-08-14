package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

type fakeEventRecorder struct {
	enabled bool
	events  []string
}

func (f *fakeEventRecorder) Enabled() bool { return f.enabled }

func (f *fakeEventRecorder) Event(typ string, detail string) {
	f.events = append(f.events, typ+": "+detail)
}

func TestEventRecorderDefaultIsNil(t *testing.T) {
	// The generators module provides the nil default EventRecorder so a
	// scope without recording wiring resolves the dependency. The tai
	// command forks the provider with the records.Recorder value, so
	// generators resolved in a command scope record API-level events
	// without carrying the recorder through the context. See
	// TheoryOfEventRecorder.
	loader := configs.NewLoader(nil, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Call(func(rec EventRecorder) {
		if rec != nil {
			t.Fatal("expected the default recorder to be nil")
		}
	})
}
