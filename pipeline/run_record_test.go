package pipeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

type fakeInteractionRecorder struct {
	enabled bool
	events  []string
}

func (f *fakeInteractionRecorder) Enabled() bool { return f.enabled }

func (f *fakeInteractionRecorder) StartSession(command string) {
	f.events = append(f.events, "session_start:"+command)
}

func (f *fakeInteractionRecorder) EndSession(err error) {
	f.events = append(f.events, "session_end")
}

func (f *fakeInteractionRecorder) SystemPrompt(prompt string) {
	f.events = append(f.events, "system_prompt")
}
func (f *fakeInteractionRecorder) AttemptStart() {
	f.events = append(f.events, "attempt_start")
}
func (f *fakeInteractionRecorder) AttemptCompleted(summaries []string) {
	f.events = append(f.events, "attempt_completed")
}
func (f *fakeInteractionRecorder) AttemptTruncated() {
	f.events = append(f.events, "attempt_truncated")
}
func (f *fakeInteractionRecorder) AttemptError(err error) {
	f.events = append(f.events, "attempt_error:"+err.Error())
}
func (f *fakeInteractionRecorder) Content(content *generators.Content) {
	f.events = append(f.events, "content_"+string(content.Role))
}
func (f *fakeInteractionRecorder) Block(block blocks.Block) {
	f.events = append(f.events, "block_"+block.Kind)
}
func (f *fakeInteractionRecorder) ParseError(parseErr *blocks.BlockParseError) {
	f.events = append(f.events, "parse_error")
}

func (f *fakeInteractionRecorder) Event(typ string, detail string) {
	f.events = append(f.events, "event_"+typ)
}

func TestRunRecordsAttempt(t *testing.T) {
	withRun(t, func(run Run) {
		rec := &fakeInteractionRecorder{enabled: true}
		_, err := runOnce(run, RunOptions{
			Generator:           nil,
			InitialState:        generators.NewPrompts("my system prompt", nil),
			Components:          nil,
			InteractionRecorder: rec,
			Command:             "test-command",
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(rec.events, ",")
		for _, want := range []string{
			"session_start:test-command",
			"system_prompt",
			"attempt_start",
			"content_assistant",
			"block_summary",
			"attempt_completed",
			"session_end",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("events missing %q: %s", want, joined)
			}
		}
	})
}

func TestRunRecordsDisabled(t *testing.T) {
	withRun(t, func(run Run) {
		rec := &fakeInteractionRecorder{enabled: false}
		_, err := runOnce(run, RunOptions{
			Generator:           nil,
			InitialState:        generators.NewPrompts("", nil),
			Components:          nil,
			InteractionRecorder: rec,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rec.events) != 0 {
			t.Fatalf("disabled recorder must not receive events, got %v", rec.events)
		}
	})
}

func TestRunRecordsAttemptError(t *testing.T) {
	withRun(t, func(run Run) {
		rec := &fakeInteractionRecorder{enabled: true}
		_, err := runOnce(run, RunOptions{
			Generator:           nil,
			InitialState:        generators.NewPrompts("", nil),
			Components:          nil,
			InteractionRecorder: rec,
			PhaseBuilder: func(g generators.Generator) generators.Phase {
				return errorPhase(errors.New("boom"))
			},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		joined := strings.Join(rec.events, ",")
		if !strings.Contains(joined, "attempt_error:boom") {
			t.Fatalf("expected attempt_error:boom, got %s", joined)
		}
	})
}

func TestRunRecordsTruncationRetry(t *testing.T) {
	withRun(t, func(run Run) {
		rec := &fakeInteractionRecorder{enabled: true}
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("no summary")
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			InteractionRecorder:      rec,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(rec.events, ",")
		if !strings.Contains(joined, "attempt_truncated") {
			t.Fatalf("expected attempt_truncated, got %s", joined)
		}
		if !strings.Contains(joined, "attempt_completed") {
			t.Fatalf("expected attempt_completed, got %s", joined)
		}
	})
}

func TestRunRecordsParseError(t *testing.T) {
	withRun(t, func(run Run) {
		rec := &fakeInteractionRecorder{enabled: true}
		phaseBuilder := func(g generators.Generator) generators.Phase {
			return appendPhaseWithFlush("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")
		}

		_, err := runOnce(run, RunOptions{
			Generator:           nil,
			InitialState:        generators.NewPrompts("", nil),
			Components:          nil,
			InteractionRecorder: rec,
			PhaseBuilder:        phaseBuilder,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(rec.events, ",")
		if !strings.Contains(joined, "parse_error") {
			t.Fatalf("expected parse_error event, got %s", joined)
		}
	})
}

func TestRunRecordsDecisionEvents(t *testing.T) {
	withRun(t, func(run Run) {
		rec := &fakeInteractionRecorder{enabled: true}
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("incomplete output without summary")
			}
			return appendPhase("<<龘靐 summary\nDone.\n龘靐\n")
		}

		_, err := runOnce(run, RunOptions{
			Generator:                nil,
			InitialState:             generators.NewPrompts("", nil),
			Components:               nil,
			InteractionRecorder:      rec,
			PhaseBuilder:             phaseBuilder,
			RetryOnMissingCompletion: true,
			MaxRetries:               3,
			Handoff: func(text string) (*Handoff, error) {
				return &Handoff{Summary: "summary", Prompt: "retry prompt"}, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (retry once), got %d", callCount)
		}
		joined := strings.Join(rec.events, ",")
		if !strings.Contains(joined, "event_decision") {
			t.Fatalf("expected decision events, got: %s", joined)
		}
		if !strings.Contains(joined, "attempt_truncated") {
			t.Fatalf("expected attempt_truncated event, got: %s", joined)
		}
	})
}
