package main

import (
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/records"
)

func (Module) Generator(
	getDefaultGenerator generators.GetDefaultGenerator,
) generators.Generator {
	ret, err := getDefaultGenerator()
	ce(err)
	return ret
}

// eventRecorderDef binds the interaction recorder to the generators-level
// EventRecorder interface. It is forked into the base scope by main() so
// every generator resolved in a command scope records API-level events
// (api_call, api_error) without carrying the recorder through the
// context. The generators module provides the nil default; this def
// overrides it with the records.Recorder value, which stays nil when
// recording is disabled or unavailable. See
// generators.TheoryOfEventRecorder.
func eventRecorderDef(recorder *records.Recorder) generators.EventRecorder {
	return recorder
}
