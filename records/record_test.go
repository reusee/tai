package records

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

// stubGetDefaultGenerator satisfies dscope.New validation for
// records.Module's RunAnalysis provider, which depends on
// generators.GetDefaultGenerator. The stub returns a nil generator; it is
// never invoked by non-analysis tests, which only need the dependency to
// resolve during scope construction. See TheoryOfInteractionRecording.
func stubGetDefaultGenerator() generators.GetDefaultGenerator {
	return func() (generators.Generator, error) {
		return nil, nil
	}
}

// stubBuildGenerate satisfies dscope.New validation for records.Module's
// RunAnalysis provider, which depends on generators.BuildGenerate. The stub
// returns a pass-through phase; it is never invoked by non-analysis
// tests. See TheoryOfInteractionRecording.
func stubBuildGenerate() generators.BuildGenerate {
	return func(generator generators.Generator, options *generators.GenerateOptions) generators.PhaseBuilder {
		return func(cont generators.Phase) generators.Phase {
			return cont
		}
	}
}

func withRecorder(t *testing.T, enabled bool, fn func(*Recorder)) {
	t.Helper()
	dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		func() Enabled {
			return Enabled(enabled)
		},
	).Call(func(recorder *Recorder) {
		if recorder == nil {
			t.Fatal("recorder is nil")
		}
		fn(recorder)
	})
}

func TestRecorderWritesTreeOperations(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test-command")
		tr := tree.New().WithOpSink(recorder.Sink())
		var err error
		tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello\nworld")
		if err != nil {
			t.Fatal(err)
		}
		tr, _, err = tr.WriteAuto("user-1", "model", tree.TypeModel, tree.AuthorModel, "the answer")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Modify("user-1", "hello again"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}

		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"=== Session", "command=test-command", "status=success",
			"operations=3",
			"root type=root author= parent= time=",
			"user-1 type=user author=user parent=root time=", "| hello again",
			"model-1 type=model author=model parent=user-1 time=", "| the answer",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
		}
	})
}

func TestRecorderWritesCommandLine(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.EndSession(nil)
		var commandLine string
		if err := recorder.db.QueryRow(`SELECT command_line FROM sessions LIMIT 1`).Scan(&commandLine); err != nil {
			t.Fatal(err)
		}
		if commandLine == "" {
			t.Fatal("the process command line must be recorded with the session")
		}
	})
}

func TestRecorderDisabledWritesNothing(t *testing.T) {
	withRecorder(t, false, func(recorder *Recorder) {
		if recorder.Enabled() {
			t.Fatal("recorder should be disabled")
		}
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		if _, err := tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var count int
		if err := recorder.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("disabled recorder must not create sessions, got %d", count)
		}
		if err := recorder.db.QueryRow(`SELECT COUNT(*) FROM tree_ops`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("disabled recorder must not write tree operations, got %d", count)
		}
	})
}

func TestRecorderNilWhenDBUnavailable(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
		stubGetDefaultGenerator,
		stubBuildGenerate,
	).Fork(
		func() DBPath { return "" },
		func() Enabled { return Enabled(true) },
	).Call(func(recorder *Recorder) {
		if recorder != nil {
			t.Fatal("expected nil recorder when db path is empty")
		}
		if recorder.Sink() != nil {
			t.Fatal("a nil recorder must yield a nil sink")
		}
	})
}

func TestRecorderRecordsFullContentWithoutTruncation(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		largeBody := strings.Repeat("x", 200*1024)
		if _, err := tr.Write("root", "model-1", tree.TypeModel, tree.AuthorModel, largeBody); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, largeBody) {
			t.Fatal("content must be recorded in full without truncation")
		}
		if strings.Contains(text, "truncated") {
			t.Fatal("recorded detail must not contain a truncation marker")
		}
	})
}

func TestListSessions(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("first")
		tr := tree.New().WithOpSink(recorder.Sink())
		if _, err := tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "hello"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)
		recorder.StartSession("second")
		recorder.EndSession(nil)

		var buf bytes.Buffer
		if err := listSessions(recorder, 10, &buf); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		for _, want := range []string{
			"id=1", "command=first", "status=success", "operations=1",
			"id=2", "command=second", "operations=0",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("listing missing %q:\n%s", want, out)
			}
		}
	})
}

func TestLatestSessionID(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		id, err := latestSessionID(recorder)
		if err != nil {
			t.Fatal(err)
		}
		if id != 0 {
			t.Fatalf("expected 0 for empty database, got %d", id)
		}
		recorder.StartSession("test")
		recorder.EndSession(nil)
		id, err = latestSessionID(recorder)
		if err != nil {
			t.Fatal(err)
		}
		if id != 1 {
			t.Fatalf("expected 1, got %d", id)
		}
	})
}

func TestSessionNotFound(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		err := showSession(recorder, 999, &bytes.Buffer{})
		if err == nil {
			t.Fatal("expected error for nonexistent session")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestTranscriptRendersDeletedSubtrees(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		var err error
		tr, err = tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "kept")
		if err != nil {
			t.Fatal(err)
		}
		tr, err = tr.Write("root", "user-2", tree.TypeUser, tree.AuthorUser, "removed")
		if err != nil {
			t.Fatal(err)
		}
		tr, err = tr.Delete("user-2")
		if err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "user-1 type=user author=user") || !strings.Contains(text, "| kept") {
			t.Fatalf("transcript must carry the surviving node, got:\n%s", text)
		}
		if strings.Contains(text, "user-2 ") || strings.Contains(text, "| removed") {
			t.Fatalf("the deleted subtree must not appear in the replayed tree, got:\n%s", text)
		}
	})
}

// TestTranscriptCarriesNodeMetadata verifies that the transcript renders
// each node's complete metadata: the write's insert time survives the
// record-replay round trip and appears on the node line alongside the
// parent, type, and author.
func TestTranscriptCarriesNodeMetadata(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		insertTime := time.Date(2024, 5, 1, 12, 30, 0, 0, time.UTC)
		tr, err := tr.WriteAll(tree.WriteOp{
			Parent:     "root",
			Name:       "user-1",
			Type:       tree.TypeUser,
			Author:     tree.AuthorUser,
			Content:    "hello",
			InsertTime: insertTime,
		})
		if err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		want := "user-1 type=user author=user parent=root time=" + insertTime.Format(time.RFC3339Nano)
		if !strings.Contains(text, want) {
			t.Fatalf("transcript must carry the node's complete metadata, got:\n%s", text)
		}
	})
}
