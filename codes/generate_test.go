package codes

import (
	"bytes"
	"strings"
	"testing"

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
