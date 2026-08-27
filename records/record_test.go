package records

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
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

func TestRecorderSessionAndTranscript(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test-command")
		recorder.SystemPrompt("the system prompt")
		recorder.AttemptStart()
		recorder.Content(&generators.Content{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text("user question"),
			},
		})
		recorder.Content(&generators.Content{
			Role: generators.RoleAssistant,
			Parts: []generators.Part{
				generators.Thought("thinking"),
				generators.Text("model answer"),
			},
		})
		recorder.AttemptCompleted([]string{"- done"})
		recorder.EndSession(nil)

		// The recorder must implement the pipeline.InteractionRecorder
		// contract used by the generation loop.
		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}

		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"=== Session", "test-command", "status: success",
			"context [system_prompt]", "the system prompt",
			"attempt 1 [attempt_start]",
			"attempt 1 [content_user]", "user question",
			"attempt 1 [content_assistant]", "thinking", "model answer",
			"attempt 1 [attempt_end]", "success", "- done",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
		}
	})
}

func TestRecorderBlockAndParseErrorEvents(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.AttemptStart()
		recorder.Block(blocks.Block{
			Kind:       "change",
			Boundary:   "abcdef",
			Attributes: map[string]string{"op": "MODIFY", "target": "Foo", "file-path": "x.go"},
			Body:       "func Foo() {}",
		})
		recorder.Block(blocks.Block{
			Boundary: "kindless",
			Body:     "body",
		})
		recorder.ParseError(&blocks.BlockParseError{
			BlockKind: "change",
			Boundary:  "bad",
			Reason:    "has an invalid or incomplete XML opening tag",
		})
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)

		var rows int
		if err := recorder.db.QueryRow(
			`SELECT COUNT(*) FROM events WHERE type IN ('block_change', 'block_unknown', 'parse_error')`,
		).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 3 {
			t.Fatalf("expected 3 events, got %d", rows)
		}
	})
}

func TestRecorderParseErrorRecordsFullContent(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.AttemptStart()
		largeBody := strings.Repeat("x", 200*1024)
		content := "<<徕珑龘 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\n" + largeBody
		recorder.ParseError(&blocks.BlockParseError{
			BlockKind: "change",
			Boundary:  "徕珑龘",
			Line:      1,
			Reason:    "has no matching closing line",
			Content:   content,
			Hints:     []string{"line 3: \"徕珑龘 extra\""},
		})
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, content) {
			t.Fatal("parse error content must be recorded in full without truncation")
		}
		if !strings.Contains(text, "line=1") {
			t.Fatal("parse error line must be recorded")
		}
		if !strings.Contains(text, "line 3: \"徕珑龘 extra\"") {
			t.Fatal("parse error hints must be recorded in full")
		}
	})
}

func TestRecorderRecordsFullDetailWithoutTruncation(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.AttemptStart()
		// Larger than the previous 100KB cap: the content must be
		// recorded in full without truncation.
		largeText := strings.Repeat("a", 200*1024)
		recorder.Content(&generators.Content{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text(largeText)},
		})
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, largeText) {
			t.Fatal("content must be recorded in full without truncation")
		}
		if strings.Contains(text, "truncated") {
			t.Fatal("recorded detail must not contain a truncation marker")
		}
	})
}

func TestRecorderDisabledWritesNothing(t *testing.T) {
	withRecorder(t, false, func(recorder *Recorder) {
		if recorder.Enabled() {
			t.Fatal("recorder should be disabled")
		}
		recorder.StartSession("test")
		recorder.AttemptStart()
		recorder.Content(&generators.Content{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text("hello")},
		})
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)

		var count int
		if err := recorder.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("disabled recorder must not create sessions, got %d", count)
		}
	})
}

func TestRecorderRecordsFileContent(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.AttemptStart()
		content := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
		recorder.Content(&generators.Content{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.FileContent{Content: content, MimeType: "image/png"},
			},
		})
		recorder.AttemptCompleted(nil)
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
			"[file content: image/png, base64]",
			base64.StdEncoding.EncodeToString(content),
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
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
		// RecordSession must be a no-op for nil recorder.
		done := RecordSession(recorder, "test")
		done()
	})
}

func TestRecordSessionPanicRecordsError(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		func() {
			defer func() { recover() }()
			defer RecordSession(recorder, "panic-test")()
			panic("boom")
		}()

		var status string
		if err := recorder.db.QueryRow(`SELECT status FROM sessions LIMIT 1`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "error" {
			t.Fatalf("expected error status, got %q", status)
		}
	})
}

func TestListSessions(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("first")
		recorder.AttemptStart()
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)
		recorder.StartSession("second")
		recorder.AttemptStart()
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)

		var buf bytes.Buffer
		if err := listSessions(recorder, 10, &buf); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		for _, want := range []string{"first", "second", "ID", "Command", "Status"} {
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

func TestRecorderEvent(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.AttemptStart()
		recorder.Event("api_call", "openai-compatible chat completion: model=test-model")
		recorder.Event("api_error", "openai http status 503: upstream unavailable")
		recorder.Event("decision", "missing completion (no summary block) triggered retry: attempt 1/3")
		recorder.AttemptCompleted(nil)
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
			"attempt 1 [api_call]", "model=test-model",
			"attempt 1 [api_error]", "openai http status 503",
			"attempt 1 [decision]", "retry: attempt 1/3",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
		}
	})
}

func TestRecorderMergesStreamedModelContents(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.SystemPrompt("prompt")
		recorder.AttemptStart()
		recorder.Content(&generators.Content{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text("user question")},
		})
		// Simulated streaming: many small model contents must be
		// recorded as one merged event, with adjacent Text parts
		// concatenated verbatim and Thought parts kept distinct.
		for _, chunk := range []string{"stream", "ed ", "answer"} {
			recorder.Content(&generators.Content{
				Role:  generators.RoleAssistant,
				Parts: []generators.Part{generators.Text(chunk)},
			})
		}
		recorder.Content(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Thought("reasoning")},
		})
		recorder.Content(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text("tail")},
		})
		recorder.AttemptCompleted([]string{"- done"})
		recorder.EndSession(nil)

		var count int
		if err := recorder.db.QueryRow(
			`SELECT COUNT(*) FROM events WHERE type = 'content_assistant'`,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected 1 merged content_assistant event, got %d", count)
		}

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		text, err := Transcript(recorder, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"streamed answer",
			"[thought]\nreasoning",
			"tail",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("transcript missing %q:\n%s", want, text)
			}
		}
	})
}

func TestRecorderModelMergeBreaksAtAttemptBoundary(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.AttemptStart()
		recorder.Content(&generators.Content{
			Role:  generators.RoleModel,
			Parts: []generators.Part{generators.Text("attempt 1 output")},
		})
		recorder.AttemptCompleted(nil)
		recorder.AttemptStart()
		recorder.Content(&generators.Content{
			Role:  generators.RoleModel,
			Parts: []generators.Part{generators.Text("attempt 2 output")},
		})
		recorder.AttemptCompleted(nil)
		recorder.EndSession(nil)

		rows, err := recorder.db.Query(
			`SELECT round FROM events WHERE type = 'content_model' ORDER BY id`,
		)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []int
		for rows.Next() {
			var attempt int
			if err := rows.Scan(&attempt); err != nil {
				t.Fatal(err)
			}
			got = append(got, attempt)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 content_model events (one per attempt), got %d", len(got))
		}
		if got[0] != 1 || got[1] != 2 {
			t.Fatalf("expected attempts 1 and 2, got %v", got)
		}
	})
}
