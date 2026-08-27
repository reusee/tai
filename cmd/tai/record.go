package main

import (
	"context"

	"github.com/reusee/tai/modes"
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
(go, any, ai, next) records through the unified generation loop. The
record subcommand works regardless of whether recording is enabled, because
the interaction database is opened on every run.
See records.TheoryOfInteractionRecording.
`

var RecordCommand = Command{
	Defs: []any{
		modes.ForProduction(),
	},
	Main: func(
		output Output,
		sessionID records.SessionID,
		analyze records.Analyze,
		limit records.SessionLimit,
		runAnalysis records.RunAnalysis,
		showSession records.ShowSession,
		listSessions records.ListSessions,
	) {
		ctx := context.Background()

		if bool(analyze) {
			ce(runAnalysis(ctx, int64(sessionID), output))
			return
		}

		if int64(sessionID) != 0 {
			ce(showSession(int64(sessionID), output))
			return
		}

		ce(listSessions(int(limit), output))
	},
}
