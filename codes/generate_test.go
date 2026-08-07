package codes

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/loops"
)

func TestPrintRoundStats(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		var buf bytes.Buffer
		PrintRoundStats(&buf, nil)
		if buf.Len() != 0 {
			t.Fatalf("expected no output for empty stats, got: %s", buf.String())
		}
	})

	t.Run("SingleRound", func(t *testing.T) {
		var buf bytes.Buffer
		stats := []RoundStat{
			{Round: 1, PromptTokens: 1000, CompletionTokens: 500, ThoughtTokens: 200, CachedTokens: 100},
		}
		PrintRoundStats(&buf, stats)
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
		stats := []RoundStat{
			{Round: 1, PromptTokens: 111, CompletionTokens: 51, ThoughtTokens: 21, CachedTokens: 11},
			{Round: 2, PromptTokens: 222, CompletionTokens: 82, ThoughtTokens: 32, CachedTokens: 22},
			{Round: 3, PromptTokens: 333, CompletionTokens: 123, ThoughtTokens: 53, CachedTokens: 33},
		}
		PrintRoundStats(&buf, stats)
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
		state, count, summarized := summarizeRetryState(partial, phaseErr, 1, func(text string) (*loops.RetrySummary, error) {
			if text != "partial output" {
				t.Fatalf("expected partial output, got %q", text)
			}
			return &loops.RetrySummary{Summary: "condensed", RetryPrompt: "condensed"}, nil
		})
		if !summarized {
			t.Fatal("expected summarized=true")
		}
		if count != generators.CountContents(state) {
			t.Fatalf("expected count %d, got %d", generators.CountContents(state), count)
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
			t.Fatal("expected error in state")
		}
	})

	t.Run("SummarizeError", func(t *testing.T) {
		state, count, summarized := summarizeRetryState(partial, phaseErr, 1, func(text string) (*loops.RetrySummary, error) {
			return nil, errors.New("summarize failed")
		})
		if summarized {
			t.Fatal("expected summarized=false")
		}
		if count != generators.CountContents(state) {
			t.Fatalf("expected count %d, got %d", generators.CountContents(state), count)
		}
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if _, ok := part.(generators.Error); ok {
					foundError = true
				}
			}
		}
		if !foundError {
			t.Fatal("expected error in state")
		}
	})

	t.Run("NoPartial", func(t *testing.T) {
		state, count, summarized := summarizeRetryState(base, phaseErr, 1, func(text string) (*loops.RetrySummary, error) {
			t.Fatal("summarize should not be called")
			return nil, nil
		})
		if summarized {
			t.Fatal("expected summarized=false")
		}
		if count != generators.CountContents(state) {
			t.Fatalf("expected count %d, got %d", generators.CountContents(state), count)
		}
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if _, ok := part.(generators.Error); ok {
					foundError = true
				}
			}
		}
		if !foundError {
			t.Fatal("expected error in state")
		}
	})
}

func TestRetrySummarizationSystemPromptExtractsValuableContent(t *testing.T) {
	// The retry summarization prompt must instruct the summarizer to
	// extract the valuable conclusions of the truncated thinking — important
	// discoveries, decisions, and facts — rather than reproducing the
	// reasoning that led to them, so the retry round adopts the conclusions
	// instead of re-deriving them and needs less thinking. See
	// TheoryOfIncompleteOutputSummarization.
	for _, want := range []string{
		`(kind "summary")`,
		`(kind "continue")`,
		"discoveries",
		"decisions",
		"facts",
		"conclusions",
		"re-deriving",
	} {
		if !strings.Contains(retrySummarizationSystemPrompt, want) {
			t.Fatalf("retrySummarizationSystemPrompt must mention %q", want)
		}
	}
}

func TestPrintRoundStatsWithSummaries(t *testing.T) {
	var buf bytes.Buffer
	stats := []RoundStat{
		{Round: 1, PromptTokens: 1000, CompletionTokens: 500, Summary: "Analyzed the code."},
		{Round: 2, PromptTokens: 2000, CompletionTokens: 800, Summary: "Fixed the bug."},
	}
	PrintRoundStats(&buf, stats)
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
	stats := []RoundStat{
		{Round: 1, PromptTokens: 1000, CompletionTokens: 500},
	}
	PrintRoundStats(&buf, stats)
	output := buf.String()
	if strings.Contains(output, "=== Round Summaries ===") {
		t.Fatalf("should not print summaries section when no summaries exist, got: %s", output)
	}
}

func TestPrintRoundStatsWithDuration(t *testing.T) {
	var buf bytes.Buffer
	stats := []RoundStat{
		{Round: 1, PromptTokens: 1000, CompletionTokens: 500, Duration: 3 * time.Second},
		{Round: 2, PromptTokens: 2000, CompletionTokens: 800, Duration: 1500 * time.Millisecond},
	}
	PrintRoundStats(&buf, stats)
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

func TestPrintRoundStatsWithLoopColumn(t *testing.T) {
	var buf bytes.Buffer
	stats := []RoundStat{
		{Loop: 1, Round: 1, PromptTokens: 111, CompletionTokens: 51, Duration: time.Second, Summary: "first round"},
		{Loop: 1, Round: 2, PromptTokens: 222, CompletionTokens: 82, Duration: time.Second, Summary: "second round"},
	}
	PrintRoundStats(&buf, stats, "Goal Loop Statistics")
	output := buf.String()
	if !strings.Contains(output, "=== Goal Loop Statistics ===") {
		t.Fatalf("expected custom title, got: %s", output)
	}
	if !strings.Contains(output, "Loop") {
		t.Fatalf("expected Loop column in output, got: %s", output)
	}
	if !strings.Contains(output, "Loop 1 Round 1: first round") {
		t.Fatalf("expected loop-aware summary for round 1, got: %s", output)
	}
	if !strings.Contains(output, "Loop 1 Round 2: second round") {
		t.Fatalf("expected loop-aware summary for round 2, got: %s", output)
	}
	// Total prompt tokens across all loops: 111 + 222 = 333
	if !strings.Contains(output, "333") {
		t.Fatalf("expected aggregated total prompt tokens, got: %s", output)
	}
}

func TestReviewModelsFlagAndConfig(t *testing.T) {
	f := ReviewModels(nil)
	newDef, remainArgs, err := f.Handle("-review-model", []string{"gemini-pro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remainArgs) != 0 {
		t.Fatalf("expected no remaining args, got %v", remainArgs)
	}
	ret, ok := newDef.(*ReviewModels)
	if !ok {
		t.Fatalf("expected *ReviewModels, got %T", newDef)
	}
	if len(*ret) != 1 || (*ret)[0] != "gemini-pro" {
		t.Fatalf("unexpected ReviewModels: %v", *ret)
	}
}

// reviewMockGenerator satisfies generators.Generator for review loop tests.
// See TestRunReviewSkipsWhenNoDiffs and TestRunReviewRunsWhenDiffsExist.
type reviewMockGenerator struct{}

func (reviewMockGenerator) Spec() generators.Spec {
	return generators.Spec{Model: "test-model"}
}

func (reviewMockGenerator) CountTokens(string) (int, error) {
	return 0, nil
}

func (reviewMockGenerator) Generate(context.Context, generators.State, *generators.GenerateOptions) (generators.State, error) {
	return nil, nil
}

func TestRunReviewSkipsWhenNoDiffs(t *testing.T) {
	// When no change blocks were produced (empty diffs), the review loop
	// must not initiate a generation session, even when the -review flag
	// is enabled. Reviewing an empty diff set would waste tokens and
	// produce review noise without any changes to review. The provider is
	// invoked directly (rather than resolved via dscope) so the assertion
	// does not depend on dscope's handling of the Reset type. See
	// TheoryOfReviewLoop.
	generationInitiated := false
	fakeReset := dscope.Reset(func() dscope.Scope {
		generationInitiated = true
		return dscope.New()
	})

	var m Module
	runReview := m.RunReview(
		fakeReset,
		true,
		nil,
		func() (generators.Generator, error) {
			generationInitiated = true
			return reviewMockGenerator{}, nil
		},
	)

	// nil diffs (the actual case when no change blocks were applied).
	if err := runReview(context.Background(), io.Discard, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty non-nil diffs.
	if err := runReview(context.Background(), io.Discard, []changes.FileDiff{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generationInitiated {
		t.Fatal("review loop must not initiate generation when no diffs exist")
	}
}

func TestRunReviewRunsWhenDiffsExist(t *testing.T) {
	// Positive control: when change blocks were produced (non-empty
	// diffs) and the -review flag is enabled, the review loop must
	// initiate a generation session with a fresh scope that re-reads the
	// current filesystem state. The provider is invoked directly (rather
	// than resolved via dscope) so the fake scope is guaranteed to be
	// used; this guards the skip test against a vacuous pass. See
	// TheoryOfReviewLoop.
	generationInitiated := false
	fakeReset := dscope.Reset(func() dscope.Scope {
		return dscope.New(
			func() GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (loops.Result, []RoundStat, error) {
					generationInitiated = true
					return loops.Result{}, nil, nil
				}
			},
		)
	})

	var m Module
	runReview := m.RunReview(
		fakeReset,
		true,
		nil,
		func() (generators.Generator, error) {
			return reviewMockGenerator{}, nil
		},
	)
	if err := runReview(context.Background(), io.Discard, []changes.FileDiff{
		{
			Path:           "test.go",
			Original:       []byte("old content"),
			OriginalExists: true,
			Current:        []byte("new content"),
			CurrentExists:  true,
		},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !generationInitiated {
		t.Fatal("review loop must initiate generation when diffs exist")
	}
}
