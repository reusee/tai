package generators

const TheoryOfEventRecorder = `
The event recorder receives API-level events (api_call, api_error) from
generator implementations and forwards them to the active interaction
recorder. Generators hold the recorder as a dscope.Inject[EventRecorder]
field, filled by dscope.InjectStruct when the generator is constructed, so
they never receive the recorder object and never depend on the loops or
records packages, which would create an import cycle. The per-scope
EventRecorder provider binds the recorder: the generators module provides
the nil default, and the tai command forks the provider with the
records.Recorder value, so every generator resolved in the forked scope
records API-level events. Recorder events are attributed to the round in
progress by the recorder's internal round counter, so an API error during
a retried attempt lands on the same round as the retry decision.
`

// EventRecorder records generation events such as API calls and API
// errors. The records package's Recorder implements this interface;
// generator implementations hold the recorder as a
// dscope.Inject[EventRecorder] field so they can record events without
// depending on the loops or records packages, which would create an
// import cycle. See TheoryOfEventRecorder.
type EventRecorder interface {
	Enabled() bool
	Event(typ string, detail string)
}

// EventRecorder provides the default recorder: none. Commands that run
// the generation pipeline fork this provider with the records.Recorder
// value, so generators resolved in the forked scope record API-level
// events without carrying the recorder through the context. See
// TheoryOfEventRecorder.
func (Module) EventRecorder() EventRecorder {
	return nil
}
