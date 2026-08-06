package records

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func withRecorder(t *testing.T, enabled bool, fn func(*Recorder)) {
	t.Helper()
	dscope.New(
		modes.ForTest(t),
		new(Module),
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
		recorder.RoundStart()
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
		recorder.RoundSuccess([]string{"- done"})
		recorder.EndSession(nil)

		// The recorder must implement the loops.InteractionRecorder
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
			"round 1 [round_start]",
			"round 1 [content_user]", "user question",
			"round 1 [content_assistant]", "thinking", "model answer",
			"round 1 [round_end]", "success", "- done",
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
		recorder.RoundStart()
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
		recorder.RoundSuccess(nil)
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

func TestRecorderDisabledWritesNothing(t *testing.T) {
	withRecorder(t, false, func(recorder *Recorder) {
		if recorder.Enabled() {
			t.Fatal("recorder should be disabled")
		}
		recorder.StartSession("test")
		recorder.RoundStart()
		recorder.Content(&generators.Content{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text("hello")},
		})
		recorder.RoundSuccess(nil)
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

func TestRecorderNilWhenDBUnavailable(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
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
		recorder.RoundStart()
		recorder.RoundSuccess(nil)
		recorder.EndSession(nil)
		recorder.StartSession("second")
		recorder.RoundStart()
		recorder.RoundSuccess(nil)
		recorder.EndSession(nil)

		var buf bytes.Buffer
		if err := ListSessions(recorder, 10, &buf); err != nil {
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

func TestLimitDetail(t *testing.T) {
	if got := limitDetail("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a", maxDetailBytes+100)
	got := limitDetail(long)
	if len(got) > maxDetailBytes+len("\n...[truncated]...") {
		t.Fatalf("truncated length %d exceeds cap", len(got))
	}
	if !strings.HasSuffix(got, "...[truncated]...") {
		t.Fatalf("missing truncation marker: %q", got[len(got)-20:])
	}

	// Multi-byte runes must not be split mid-rune.
	chinese := strings.Repeat("世界", maxDetailBytes)
	if got := limitDetail(chinese); !strings.HasPrefix(got, "世") {
		t.Fatal("truncated content should be valid UTF-8")
	}
}

func TestSessionNotFound(t *testing.T) {
	withRecorder(t, true, func(recorder *Recorder) {
		err := ShowSession(recorder, 999, &bytes.Buffer{})
		if err == nil {
			t.Fatal("expected error for nonexistent session")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
