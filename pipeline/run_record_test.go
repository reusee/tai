package pipeline

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/tree"
)

// withRecorderRun resolves the module graph with a recorder backed by a
// temporary database, so the recording tests exercise the real
// recording path without touching the user config directory. The extra
// definitions are forked on top of the base scope (e.g., a session tree
// continuation) and may be empty. See
// records.TheoryOfInteractionRecording.
func withRecorderRun(t *testing.T, extra []any, fn func(run Run, recorder *records.Recorder)) {
	t.Helper()
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() records.DBPath {
			return records.DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		func() records.Enabled { return records.Enabled(true) },
	)
	if len(extra) > 0 {
		scope = scope.Fork(extra...)
	}
	scope.Call(func(run Run, recorder *records.Recorder) {
		if recorder == nil {
			t.Fatal("recorder is nil")
		}
		fn(run, recorder)
	})
}

func TestRunRecordsFreshSession(t *testing.T) {
	// A fresh run owns its recording session: it opens the session
	// through the resolved recorder, attaches the sink to the run's
	// tree, and ends the session with the run's outcome. The recorded
	// stream is the run's tree operations, so the transcript is the
	// replayed tree with each node rendered in full. See
	// records.TheoryOfInteractionRecording.
	withRecorderRun(t, nil, func(run Run, recorder *records.Recorder) {
		result, err := runOnce(run, RunOptions{
			Generator: nil,
			InitialState: generators.NewPrompts("sys prompt", []*generators.Content{
				{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("task")}},
			}),
			Command: "test-command",
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.SessionTree == nil {
			t.Fatal("expected a session tree")
		}
		// A fresh temporary database numbers the first session 1.
		text, err := records.Transcript(recorder, 1)
		if err != nil {
			t.Fatalf("transcript: %v", err)
		}
		for _, want := range []string{
			"=== Session 1: test-command ===",
			"command line: ",
			"status: success",
			"root [root]",
			"system-1 [system/program]",
			"| sys prompt",
			"attempt-1 [attempt/program]",
			"user-1 [user/user]",
			"| task",
			"model-1 [model/model]",
			"summary-1 [summary/model]",
			"| Done.",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
		}
	})
}

func TestRunContinuedSessionLeavesRecordingToRunner(t *testing.T) {
	// A continued run — a goal loop — receives the runner's continuation
	// and leaves session ownership to the runner: it writes into the
	// runner's tree and never opens a recorder session itself, so one
	// goal run records as one session owned by the runner. See
	// records.TheoryOfInteractionRecording.
	withRecorderRun(t, []any{
		func() SessionTreeContinuation {
			return SessionTreeContinuation{Tree: tree.New(), Parent: "root"}
		},
	}, func(run Run, recorder *records.Recorder) {
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.SessionTree == nil {
			t.Fatal("expected the continued tree")
		}
		if _, ok := result.SessionTree.Node("model-1"); !ok {
			t.Fatal("expected the loop's nodes in the continued tree")
		}
		if _, err := records.Transcript(recorder, 1); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("a continued run must not open a recorder session, got: %v", err)
		}
	})
}
