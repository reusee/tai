package codes

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
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
		state, count, summary, err := summarizeRetryState(partial, phaseErr, 1, func(text string) (*loops.RetrySummary, error) {
			if text != "partial output" {
				t.Fatalf("expected partial output, got %q", text)
			}
			return &loops.RetrySummary{Summary: "condensed", RetryPrompt: "condensed"}, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summary != "condensed" {
			t.Fatalf("expected summary 'condensed', got %q", summary)
		}
		if count != generators.CountContents(state) {
			t.Fatalf("expected count %d, got %d", generators.CountContents(state), count)
		}
		foundSummaryBlock := false
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "<summary>") && strings.Contains(string(text), "condensed") {
						foundSummaryBlock = true
					}
					if strings.Contains(string(text), "boom") {
						foundError = true
					}
				}
			}
		}
		if !foundSummaryBlock {
			t.Fatal("expected summary block in state")
		}
		if !foundError {
			t.Fatal("expected error in state")
		}
	})

	t.Run("SummarizeError", func(t *testing.T) {
		state, count, summary, err := summarizeRetryState(partial, phaseErr, 1, func(text string) (*loops.RetrySummary, error) {
			return nil, errors.New("summarize failed")
		})
		if err == nil {
			t.Fatal("expected error when summarize fails")
		}
		if !strings.Contains(err.Error(), "summarize failed") {
			t.Fatalf("expected summarize error, got %v", err)
		}
		if summary != "[Error: boom]" {
			t.Fatalf("expected summary '[Error: boom]', got %q", summary)
		}
		if count != generators.CountContents(state) {
			t.Fatalf("expected count %d, got %d", generators.CountContents(state), count)
		}
		foundSummaryBlock := false
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "<summary>") && strings.Contains(string(text), "[Error: boom]") {
						foundSummaryBlock = true
					}
				}
				if _, ok := part.(generators.Error); ok {
					foundError = true
				}
			}
		}
		if !foundSummaryBlock {
			t.Fatal("expected summary block in state")
		}
		if !foundError {
			t.Fatal("expected error in state")
		}
	})

	t.Run("NoPartial", func(t *testing.T) {
		state, count, summary, err := summarizeRetryState(base, phaseErr, 1, func(text string) (*loops.RetrySummary, error) {
			t.Fatal("summarize should not be called")
			return nil, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if summary != "[Error: boom]" {
			t.Fatalf("expected summary '[Error: boom]', got %q", summary)
		}
		if count != generators.CountContents(state) {
			t.Fatalf("expected count %d, got %d", generators.CountContents(state), count)
		}
		foundSummaryBlock := false
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "<summary>") && strings.Contains(string(text), "[Error: boom]") {
						foundSummaryBlock = true
					}
				}
				if _, ok := part.(generators.Error); ok {
					foundError = true
				}
			}
		}
		if !foundSummaryBlock {
			t.Fatal("expected summary block in state")
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

func TestRetrySummarizationSystemPromptShowsBlockFormat(t *testing.T) {
	// The summarization prompt must show a complete example of the
	// heredoc-delimited block format with concrete delimiters. Without
	// an example, the model does not know the format and produces plain
	// text that cannot be parsed into summary and continue blocks,
	// causing the retry to proceed without a synthesized summary. See
	// TheoryOfIncompleteOutputSummarization.
	if strings.Contains(retrySummarizationSystemPrompt, "<<DELIMITER") {
		t.Fatal("retrySummarizationSystemPrompt must not display the literal template marker '<<DELIMITER'")
	}
	if !strings.Contains(retrySummarizationSystemPrompt, "<summary>") {
		t.Fatal("retrySummarizationSystemPrompt must show a summary block example")
	}
	if !strings.Contains(retrySummarizationSystemPrompt, "<continue>") {
		t.Fatal("retrySummarizationSystemPrompt must show a continue block example")
	}
	if !strings.Contains(retrySummarizationSystemPrompt, "uncommon Chinese characters") {
		t.Fatal("retrySummarizationSystemPrompt must mandate the three-uncommon-Chinese-characters delimiter policy")
	}
}

func TestRetrySummarizationSystemPromptRequiresNonEmptyBlocks(t *testing.T) {
	// The retry summarization prompt must require non-empty block bodies,
	// distinct delimiters per block, delimiter-body disjointness, and a
	// response containing only the two blocks. These requirements make the
	// summarizer's parse robust: an empty or missing block, a reused
	// delimiter, or surrounding prose would fail to produce a usable
	// retry summary. See TheoryOfIncompleteOutputSummarization.
	for _, want := range []string{
		"Both blocks MUST have non-empty bodies",
		"DIFFERENT trio for each block",
		"The delimiter MUST NOT appear anywhere in the block body",
		"Output ONLY these two blocks as your final text",
	} {
		if !strings.Contains(retrySummarizationSystemPrompt, want) {
			t.Fatalf("retrySummarizationSystemPrompt must contain %q", want)
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

func TestBuildUserPromptText(t *testing.T) {
	// buildUserPromptText must concatenate only Text parts, in order,
	// ignoring non-text parts, and produce exactly the string that repeated
	// += over the Text parts would produce. See buildUserPromptText.
	parts := []generators.Part{
		generators.Text("``` begin of file a.go\n"),
		generators.Thought("reasoning is not user prompt context"),
		generators.Text("package a\n"),
		generators.Text("``` end of file a.go\n"),
	}
	want := "``` begin of file a.go\npackage a\n``` end of file a.go\n"
	if got := buildUserPromptText(parts); got != want {
		t.Fatalf("buildUserPromptText() = %q, want %q", got, want)
	}
	if got := buildUserPromptText(nil); got != "" {
		t.Fatalf("buildUserPromptText(nil) = %q, want empty", got)
	}
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
		flags.ModelName("test-model"),
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
		flags.ModelName("test-model"),
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

func TestRunReviewUsesModelFlagValue(t *testing.T) {
	// When no -review-model is configured, the review loop must reuse the
	// model name from the -model flag, not the resolved generator's Spec.
	// Built-in shortcuts (flash, gemini, ...) and the ollama shorthand do
	// not set Spec.Name, and their Spec.Model values are not resolvable
	// model names, so deriving the review model from the Spec produced
	// "invalid model" errors. See TheoryOfReviewLoop.
	var reviewModel string
	fakeReset := dscope.Reset(func() dscope.Scope {
		return dscope.New(
			// dscope validates provider dependencies at registration
			// time, so the fake scope must provide flags.ModelName
			// before the GenerateWithResultWithStats provider that
			// depends on it. RunReview forks its chosen model value
			// over this placeholder.
			func() flags.ModelName { return "" },
			func(modelName flags.ModelName) GenerateWithResultWithStats {
				return func(ctx context.Context, output io.Writer) (loops.Result, []RoundStat, error) {
					reviewModel = string(modelName)
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
		flags.ModelName("gemini-flash"),
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
	if reviewModel != "gemini-flash" {
		t.Fatalf("review must use the -model flag value, got %q", reviewModel)
	}
}

type summarizeRetryMockGenerator struct {
	calls     int
	responses []string
	errs      []error
}

func (g *summarizeRetryMockGenerator) Spec() generators.Spec {
	return generators.Spec{}
}

func (g *summarizeRetryMockGenerator) CountTokens(string) (int, error) {
	return 0, nil
}

func (g *summarizeRetryMockGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	if g.calls < len(g.errs) && g.errs[g.calls] != nil {
		err := g.errs[g.calls]
		g.calls++
		return state, err
	}
	response := g.responses[g.calls]
	g.calls++
	return state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text(response),
		},
	})
}

func TestSummarizeIncompleteOutputRetriesOnParseFailure(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"unparseable response without blocks",
			"<<徕珑龘 <summary>\nsummary\n徕珑龘\n<<龘靐齉 <continue>\nretry prompt\n龘靐齉\n",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, nil, gen, "incomplete text")
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 summarize calls, got %d", gen.calls)
	}
	if retrySummary.Summary != "summary" {
		t.Fatalf("expected summary 'summary', got %q", retrySummary.Summary)
	}
	if retrySummary.RetryPrompt != "retry prompt" {
		t.Fatalf("expected retry prompt 'retry prompt', got %q", retrySummary.RetryPrompt)
	}
}

func TestSummarizeIncompleteOutputErrorsAfterMaxRetries(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"unparseable 1",
			"unparseable 2",
			"unparseable 3",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, nil, gen, "incomplete text")
	if err == nil {
		t.Fatal("expected error after all summarize attempts fail")
	}
	if retrySummary != nil {
		t.Fatalf("expected nil summary on failure, got %+v", retrySummary)
	}
	if gen.calls != maxSummarizeRetries {
		t.Fatalf("expected %d summarize calls, got %d", maxSummarizeRetries, gen.calls)
	}
	if !strings.Contains(err.Error(), "summarize incomplete output failed") {
		t.Fatalf("expected failure message, got: %v", err)
	}
}

func TestSummarizeIncompleteOutputRetriesOnGenerationFailure(t *testing.T) {
	// A failed summarize generation is a failed summarize attempt: it
	// must be retried like a parsing failure, so a transient API error
	// does not leave the round without a summary. See
	// TheoryOfIncompleteOutputSummarization.
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("summarize generation failed"),
		},
		responses: []string{
			"", // unused (first call errors)
			"<<徕珑龘 <summary>\nsummary\n徕珑龘\n<<龘靐齉 <continue>\nretry prompt\n龘靐齉\n",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, nil, gen, "incomplete text")
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 summarize calls, got %d", gen.calls)
	}
	if retrySummary.Summary != "summary" {
		t.Fatalf("expected summary 'summary', got %q", retrySummary.Summary)
	}
	if retrySummary.RetryPrompt != "retry prompt" {
		t.Fatalf("expected retry prompt 'retry prompt', got %q", retrySummary.RetryPrompt)
	}
}

func TestSummarizeIncompleteOutputErrorsAfterGenerationFailures(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("failure 1"),
			errors.New("failure 2"),
			errors.New("failure 3"),
		},
		responses: []string{"", "", ""},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, nil, gen, "incomplete text")
	if err == nil {
		t.Fatal("expected error after all summarize generations fail")
	}
	if retrySummary != nil {
		t.Fatalf("expected nil summary on failure, got %+v", retrySummary)
	}
	if gen.calls != maxSummarizeRetries {
		t.Fatalf("expected %d summarize calls, got %d", maxSummarizeRetries, gen.calls)
	}
	if !strings.Contains(err.Error(), "summarize incomplete output failed") {
		t.Fatalf("expected failure message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failure 3") {
		t.Fatalf("expected the last failure in the error message, got: %v", err)
	}
}

func TestSummarizeIncompleteOutputLogsErrors(t *testing.T) {
	// Each failed summarize attempt must be logged with the attempt
	// number and the error, and the final failure must be logged as an
	// error, so the operator can diagnose why a round lacks a
	// synthesized summary. See TheoryOfIncompleteOutputSummarization.
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("failure 1"),
			errors.New("failure 2"),
			errors.New("failure 3"),
		},
		responses: []string{"", "", ""},
	}
	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, nil, gen, "incomplete text")
	if err == nil {
		t.Fatal("expected error after all summarize generations fail")
	}
	if retrySummary != nil {
		t.Fatalf("expected nil summary on failure, got %+v", retrySummary)
	}
	output := buf.String()
	// Each failed attempt is logged as a warning with the attempt number.
	for _, want := range []string{
		"level=WARN",
		"summarize incomplete output: generation failed",
		"attempt=1",
		"attempt=2",
		"attempt=3",
		"failure 1",
		"failure 2",
		"failure 3",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in log output, got: %s", want, output)
		}
	}
	// The final failure is logged as an error.
	if !strings.Contains(output, "level=ERROR") {
		t.Fatalf("expected an error-level log for the final failure, got: %s", output)
	}
	if !strings.Contains(output, "summarize incomplete output failed") {
		t.Fatalf("expected the final failure message in the log, got: %s", output)
	}
}

// fakeRecorderForSummarize is a minimal InteractionRecorder for testing
// that summarize requests are recorded. It captures contents appended via
// Content.
type fakeRecorderForSummarize struct {
	enabled  bool
	contents []*generators.Content
}

func TestSummarizeIncompleteOutputRecords(t *testing.T) {
	t.Run("Enabled", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			responses: []string{
				"<<徕珑龘 <summary>\nsummary\n徕珑龘\n<<龘靐齉 <continue>\nretry prompt\n龘靐齉\n",
			},
		}
		logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &fakeRecorderForSummarize{enabled: true}
		retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, rec, gen, "incomplete text")
		if err != nil {
			t.Fatal(err)
		}
		if retrySummary == nil {
			t.Fatal("expected retry summary")
		}
		if len(rec.contents) != 2 {
			t.Fatalf("expected 2 recorded contents, got %d", len(rec.contents))
		}
		if rec.contents[0].Role != generators.RoleUser {
			t.Fatalf("expected first content role user, got %s", rec.contents[0].Role)
		}
		if text, ok := rec.contents[0].Parts[0].(generators.Text); !ok || !strings.Contains(string(text), "incomplete text") {
			t.Fatalf("expected first content to include the incomplete text, got %v", rec.contents[0].Parts[0])
		}
		if rec.contents[1].Role != generators.RoleModel {
			t.Fatalf("expected second content role model, got %s", rec.contents[1].Role)
		}
		if text, ok := rec.contents[1].Parts[0].(generators.Text); !ok || !strings.Contains(string(text), "summary") {
			t.Fatalf("expected second content to include the summary response, got %v", rec.contents[1].Parts[0])
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			responses: []string{
				"<<徕珑龘 <summary>\nsummary\n徕珑龘\n<<龘靐齉 <continue>\nretry prompt\n龘靐齉\n",
			},
		}
		logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &fakeRecorderForSummarize{enabled: false}
		_, err := summarizeIncompleteOutput(context.Background(), logger, rec, gen, "incomplete text")
		if err != nil {
			t.Fatal(err)
		}
		if len(rec.contents) != 0 {
			t.Fatalf("expected no recorded contents when disabled, got %d", len(rec.contents))
		}
	})

	t.Run("RecordsFailure", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			errs: []error{
				errors.New("failure"),
			},
			responses: []string{
				"", // unused (first call errors)
				"<<徕珑龘 <summary>\nsummary\n徕珑龘\n<<龘靐齉 <continue>\nretry prompt\n龘靐齉\n",
			},
		}
		logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &fakeRecorderForSummarize{enabled: true}
		retrySummary, err := summarizeIncompleteOutput(context.Background(), logger, rec, gen, "incomplete text")
		if err != nil {
			t.Fatal(err)
		}
		if retrySummary == nil {
			t.Fatal("expected retry summary")
		}
		// Attempt 1: user input, then a failure log. Attempt 2: user
		// input, then the successful model response. Total: 4 contents.
		if len(rec.contents) != 4 {
			t.Fatalf("expected 4 recorded contents, got %d", len(rec.contents))
		}
		if rec.contents[0].Role != generators.RoleUser {
			t.Fatalf("expected content 0 role user, got %s", rec.contents[0].Role)
		}
		if rec.contents[1].Role != generators.RoleLog {
			t.Fatalf("expected content 1 role log, got %s", rec.contents[1].Role)
		}
		if rec.contents[2].Role != generators.RoleUser {
			t.Fatalf("expected content 2 role user, got %s", rec.contents[2].Role)
		}
		if rec.contents[3].Role != generators.RoleModel {
			t.Fatalf("expected content 3 role model, got %s", rec.contents[3].Role)
		}
	})
}

func (f *fakeRecorderForSummarize) Enabled() bool { return f.enabled }

func (f *fakeRecorderForSummarize) StartSession(string) {}

func (f *fakeRecorderForSummarize) EndSession(error) {}

func (f *fakeRecorderForSummarize) SystemPrompt(string) {}

func (f *fakeRecorderForSummarize) RoundStart() {}

func (f *fakeRecorderForSummarize) RoundSuccess([]string) {}

func (f *fakeRecorderForSummarize) RoundTruncated() {}

func (f *fakeRecorderForSummarize) RoundError(error) {}

func (f *fakeRecorderForSummarize) Content(content *generators.Content) {
	f.contents = append(f.contents, content)
}

func (f *fakeRecorderForSummarize) Block(blocks.Block) {}

func (f *fakeRecorderForSummarize) ParseError(*blocks.BlockParseError) {}

func (f *fakeRecorderForSummarize) Event(typ string, detail string) {
	f.contents = append(f.contents, &generators.Content{
		Role:  generators.RoleLog,
		Parts: []generators.Part{generators.Text("[" + typ + "] " + detail)},
	})
}
