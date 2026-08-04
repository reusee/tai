package codes

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/reusee/tai/generators"
)

func TestPrintRoundStats(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		var buf bytes.Buffer
		printRoundStats(&buf, nil)
		if buf.Len() != 0 {
			t.Fatalf("expected no output for empty stats, got: %s", buf.String())
		}
	})

	t.Run("SingleRound", func(t *testing.T) {
		var buf bytes.Buffer
		stats := []roundStat{
			{Round: 1, PromptTokens: 1000, CompletionTokens: 500, ThoughtTokens: 200, CachedTokens: 100},
		}
		printRoundStats(&buf, stats)
		output := buf.String()
		if !strings.Contains(output, "Total rounds: 1") {
			t.Fatalf("expected total rounds 1, got: %s", output)
		}
		if !strings.Contains(output, "1000") {
			t.Fatalf("expected prompt tokens 1000, got: %s", output)
		}
		if !strings.Contains(output, "500") {
			t.Fatalf("expected completion tokens 500, got: %s", output)
		}
	})

	t.Run("MultipleRoundsWithTotals", func(t *testing.T) {
		var buf bytes.Buffer
		stats := []roundStat{
			{Round: 1, PromptTokens: 111, CompletionTokens: 51, ThoughtTokens: 21, CachedTokens: 11},
			{Round: 2, PromptTokens: 222, CompletionTokens: 82, ThoughtTokens: 32, CachedTokens: 22},
			{Round: 3, PromptTokens: 333, CompletionTokens: 123, ThoughtTokens: 53, CachedTokens: 33},
		}
		printRoundStats(&buf, stats)
		output := buf.String()
		if !strings.Contains(output, "Total rounds: 3") {
			t.Fatalf("expected total rounds 3, got: %s", output)
		}
		// Totals: 111+222+333=666, 51+82+123=256, 21+32+53=106, 11+22+33=66
		if !strings.Contains(output, "666") {
			t.Fatalf("expected total prompt 666, got: %s", output)
		}
		if !strings.Contains(output, "256") {
			t.Fatalf("expected total completion 256, got: %s", output)
		}
		if !strings.Contains(output, "106") {
			t.Fatalf("expected total thoughts 106, got: %s", output)
		}
		if !strings.Contains(output, "66") {
			t.Fatalf("expected total cached 66, got: %s", output)
		}
		// Verify each round number appears
		for _, r := range []string{"1", "2", "3"} {
			if !strings.Contains(output, r) {
				t.Fatalf("expected round %s in output, got: %s", r, output)
			}
		}
	})
}

func TestCountContents(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		state := generators.NewPrompts("", nil)
		if count := countContents(state); count != 0 {
			t.Fatalf("expected 0 contents, got %d", count)
		}
	})

	t.Run("Multiple", func(t *testing.T) {
		state := generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("hello")}},
			{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("hi")}},
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("bye")}},
		})
		if count := countContents(state); count != 3 {
			t.Fatalf("expected 3 contents, got %d", count)
		}
	})
}

func TestExtractPartialOutput(t *testing.T) {
	state := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("question")}},
		{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("base answer")}},
		{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("partial answer"), generators.Thought("thinking...")}},
	})

	t.Run("SkipBase", func(t *testing.T) {
		got := extractPartialOutput(state, 2)
		if !strings.Contains(got, "partial answer") || !strings.Contains(got, "thinking...") {
			t.Fatalf("expected partial answer and thoughts, got %q", got)
		}
		if strings.Contains(got, "base answer") {
			t.Fatalf("base answer should be skipped, got %q", got)
		}
	})

	t.Run("NoSkip", func(t *testing.T) {
		got := extractPartialOutput(state, 0)
		if !strings.Contains(got, "base answer") {
			t.Fatalf("expected base answer when no skip, got %q", got)
		}
	})

	t.Run("SkipAll", func(t *testing.T) {
		got := extractPartialOutput(state, 3)
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})
}

func TestSummarizeRetryState(t *testing.T) {
	base := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("question")}},
	})
	partial, err := base.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("partial output")},
	})
	if err != nil {
		t.Fatal(err)
	}
	phaseErr := errors.New("boom")

	t.Run("Summarized", func(t *testing.T) {
		state, count, summarized := summarizeRetryState(partial, phaseErr, 1, func(text string) (string, error) {
			if text != "partial output" {
				t.Fatalf("expected partial output, got %q", text)
			}
			return "condensed", nil
		})
		if !summarized {
			t.Fatal("expected summarized=true")
		}
		if count != countContents(state) {
			t.Fatalf("expected count %d, got %d", countContents(state), count)
		}
		foundSummary := false
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "condensed") {
						foundSummary = true
					}
					if strings.Contains(string(text), "boom") {
						foundError = true
					}
				}
			}
		}
		if !foundSummary {
			t.Fatal("expected summary in state")
		}
		if !foundError {
			t.Fatal("expected error message in summary retry state")
		}
	})

	t.Run("NoPartial", func(t *testing.T) {
		state, count, summarized := summarizeRetryState(base, phaseErr, 1, func(text string) (string, error) {
			t.Fatal("summarize should not be called")
			return "", nil
		})
		if summarized {
			t.Fatal("expected summarized=false")
		}
		if count != countContents(state) {
			t.Fatalf("expected count %d, got %d", countContents(state), count)
		}
		// Only examine contents appended by summarizeRetryState (after the
		// prevContentCount base contents); the base state may contain user
		// contents of its own.
		foundUser := false
		foundErrorLog := false
		contentIndex := 0
		for c := range state.Contents() {
			if contentIndex < 1 {
				contentIndex++
				continue
			}
			if c.Role == generators.RoleUser {
				foundUser = true
			}
			for _, part := range c.Parts {
				if _, ok := part.(generators.Error); ok {
					foundErrorLog = true
				}
			}
		}
		if foundUser {
			t.Fatal("expected no user summary content when no partial output")
		}
		if !foundErrorLog {
			t.Fatal("expected error appended as log content when no partial output")
		}
	})

	t.Run("SummarizeError", func(t *testing.T) {
		state, count, summarized := summarizeRetryState(partial, phaseErr, 1, func(text string) (string, error) {
			return "", errors.New("summarize failed")
		})
		if summarized {
			t.Fatal("expected summarized=false on summarize error")
		}
		if count != countContents(state) {
			t.Fatalf("expected count %d, got %d", countContents(state), count)
		}
	})
}

func TestPrintRoundStatsWithSummaries(t *testing.T) {
	var buf bytes.Buffer
	stats := []roundStat{
		{Round: 1, PromptTokens: 1000, CompletionTokens: 500, Summary: "Analyzed the code."},
		{Round: 2, PromptTokens: 2000, CompletionTokens: 800, Summary: "Fixed the bug."},
	}
	printRoundStats(&buf, stats)
	output := buf.String()
	if !strings.Contains(output, "=== Round Summaries ===") {
		t.Fatalf("expected summaries section, got: %s", output)
	}
	if !strings.Contains(output, "Round 1: Analyzed the code.") {
		t.Fatalf("expected round 1 summary, got: %s", output)
	}
	if !strings.Contains(output, "Round 2: Fixed the bug.") {
		t.Fatalf("expected round 2 summary, got: %s", output)
	}
}

func TestPrintRoundStatsNoSummaries(t *testing.T) {
	var buf bytes.Buffer
	stats := []roundStat{
		{Round: 1, PromptTokens: 1000, CompletionTokens: 500},
	}
	printRoundStats(&buf, stats)
	output := buf.String()
	if strings.Contains(output, "=== Round Summaries ===") {
		t.Fatalf("should not print summaries section when no summaries exist, got: %s", output)
	}
}

func TestPrintRoundStatsWithDuration(t *testing.T) {
	var buf bytes.Buffer
	stats := []roundStat{
		{Round: 1, PromptTokens: 1000, CompletionTokens: 500, Duration: 3 * time.Second},
		{Round: 2, PromptTokens: 2000, CompletionTokens: 800, Duration: 1500 * time.Millisecond},
	}
	printRoundStats(&buf, stats)
	output := buf.String()
	if !strings.Contains(output, "Duration") {
		t.Fatalf("expected Duration column header, got: %s", output)
	}
	if !strings.Contains(output, "3s") {
		t.Fatalf("expected duration '3s' in output, got: %s", output)
	}
	if !strings.Contains(output, "1.5s") {
		t.Fatalf("expected duration '1.5s' in output, got: %s", output)
	}
	// Total duration: 3s + 1.5s = 4.5s
	if !strings.Contains(output, "4.5s") {
		t.Fatalf("expected total duration '4.5s' in output, got: %s", output)
	}
}
