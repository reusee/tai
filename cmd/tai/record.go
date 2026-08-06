package main

import (
	"context"
	"os"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/records"
)

const TheoryOfRecordCommand = `
The record subcommand exposes the self-improvement mechanism: it lists
recorded interaction sessions, renders a session transcript, and — with
-analyze — feeds the transcript to the configured model for improvement
analysis.

- tai record            -> list recent sessions
- tai record -session 5 -> show the full transcript of session 5
- tai record -analyze [-session 5] -> analyze session 5 (or the most
  recent session) with the model

Recording itself is enabled by the -record flag; every generation command
(go, any, ai, next, goal) records through the unified generation loop. The
record subcommand works regardless of whether recording is enabled, because
the interaction database is opened on every run.
See records.TheoryOfInteractionRecording.
`

var RecordCommand = Command{
	Defs: []any{
		modes.ForProduction(),
	},
	Main: func(
		recorder *records.Recorder,
		sessionID records.SessionID,
		analyze records.Analyze,
		limit records.SessionLimit,
		getDefaultGenerator generators.GetDefaultGenerator,
		buildGenerate phases.BuildGenerate,
	) {
		ctx := context.Background()

		if bool(analyze) {
			generator, err := getDefaultGenerator()
			ce(err)
			ce(records.RunAnalysis(ctx, generator, buildGenerate, recorder, int64(sessionID), os.Stdout))
			return
		}

		if int64(sessionID) != 0 {
			ce(records.ShowSession(recorder, int64(sessionID), os.Stdout))
			return
		}

		ce(records.ListSessions(recorder, int(limit), os.Stdout))
	},
}
