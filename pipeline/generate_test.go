package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
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
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
	"github.com/reusee/tai/records"
)

func TestPrintAttemptStats(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		var buf bytes.Buffer
		PrintAttemptStats(&buf, nil)
		if buf.Len() != 0 {
			t.Fatalf("expected no output for empty stats, got: %s", buf.String())
		}
	})

	t.Run("SingleAttempt", func(t *testing.T) {
		var buf bytes.Buffer
		stats := []AttemptStat{
			{Attempt: 1, PromptTokens: 1000, CompletionTokens: 500, ThoughtTokens: 200, CachedTokens: 100},
		}
		PrintAttemptStats(&buf, stats)
		output := buf.String()
		if !strings.Contains(output, "Total attempts: 1") {
			t.Fatalf("expected total attempts 1, got: %s", output)
		}
		if !strings.Contains(output, "1000") {
			t.Fatalf("expected prompt tokens 1000, got: %s", output)
		}
		if !strings.Contains(output, "500") {
			t.Fatalf("expected completion tokens 500, got: %s", output)
		}
	})

	t.Run("MultipleGenerationsWithTotals", func(t *testing.T) {
		var buf bytes.Buffer
		stats := []AttemptStat{
			{Attempt: 1, PromptTokens: 111, CompletionTokens: 51, ThoughtTokens: 21, CachedTokens: 11},
			{Attempt: 2, PromptTokens: 222, CompletionTokens: 82, ThoughtTokens: 32, CachedTokens: 22},
			{Attempt: 3, PromptTokens: 333, CompletionTokens: 123, ThoughtTokens: 53, CachedTokens: 33},
		}
		PrintAttemptStats(&buf, stats)
		output := buf.String()
		if !strings.Contains(output, "Total attempts: 3") {
			t.Fatalf("expected total attempts 3, got: %s", output)
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
		// Verify each attempt number appears
		for _, r := range []string{"1", "2", "3"} {
			if !strings.Contains(output, r) {
				t.Fatalf("expected attempt %s in output, got: %s", r, output)
			}
		}
	})
}

func TestHandoffRetryState(t *testing.T) {
	base := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("question")}},
	})
	partial, err := base.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(strings.Repeat("partial output words ", 10))},
	})
	if err != nil {
		t.Fatal(err)
	}
	phaseErr := errors.New("boom")

	t.Run("HandoffSuccess", func(t *testing.T) {
		state, count, summary, err := handoffRetryState(partial, phaseErr, 1, func(text string) (*Handoff, error) {
			return &Handoff{Summary: "condensed", Prompt: "condensed"}, nil
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
		foundRetryPrompt := false
		foundError := false
		for c := range state.Contents() {
			for _, part := range c.Parts {
				if text, ok := part.(generators.Text); ok {
					if strings.Contains(string(text), "condensed") {
						foundRetryPrompt = true
					}
					if strings.Contains(string(text), "boom") {
						foundError = true
					}
				}
			}
		}
		if !foundRetryPrompt {
			t.Fatal("expected retry prompt in state")
		}
		if !foundError {
			t.Fatal("expected error in state")
		}
	})

	t.Run("HandoffError", func(t *testing.T) {
		state, count, summary, err := handoffRetryState(partial, phaseErr, 1, func(text string) (*Handoff, error) {
			return nil, errors.New("handoff failed")
		})
		if err == nil {
			t.Fatal("expected error when handoff fails")
		}
		if !strings.Contains(err.Error(), "handoff failed") {
			t.Fatalf("expected handoff error, got %v", err)
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
					if strings.Contains(string(text), "summary") && strings.Contains(string(text), "[Error: boom]") {
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
		state, count, summary, err := handoffRetryState(base, phaseErr, 1, func(text string) (*Handoff, error) {
			t.Fatal("handoff should not be called")
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
					if strings.Contains(string(text), "summary") && strings.Contains(string(text), "[Error: boom]") {
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

func TestHandoffRetryStateInjectsUsage(t *testing.T) {
	// The success path injects the handoff request's own spend into the
	// returned state: the last usage in the state equals the main
	// generation's usage plus the handoff usage, so the caller's
	// statistics scan accounts both. See
	// TheoryOfHandoffUsageAccounting.
	base := generators.NewPrompts("", []*generators.Content{
		{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("question")}},
		{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
			Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100, TokenCountCached: 20},
			Candidates: struct{ TokenCount int }{TokenCount: 50},
			Thoughts:   struct{ TokenCount int }{TokenCount: 10},
		}}},
	})
	partial, err := base.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(strings.Repeat("partial output words ", 10))},
	})
	if err != nil {
		t.Fatal(err)
	}
	handoffUsage := generators.Usage{}
	handoffUsage.Prompt.TokenCount = 7
	handoffUsage.Candidates.TokenCount = 3
	handoffUsage.Thoughts.TokenCount = 1

	state, _, _, err := handoffRetryState(partial, errors.New("boom"), 1, func(text string) (*Handoff, error) {
		return &Handoff{Summary: "condensed", Prompt: "condensed", Usage: handoffUsage}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := extractLastUsage(state, 0)
	if got.Prompt.TokenCount != 107 || got.Prompt.TokenCountCached != 20 ||
		got.Candidates.TokenCount != 53 || got.Thoughts.TokenCount != 11 {
		t.Fatalf("expected injected usage (107, 20, 53, 11), got (%d, %d, %d, %d)",
			got.Prompt.TokenCount, got.Prompt.TokenCountCached,
			got.Candidates.TokenCount, got.Thoughts.TokenCount)
	}
}

func TestHandoffSystemPromptSelfContainedAndReferenceOriented(t *testing.T) {
	// The handoff prompt must emphasize self-contained extraction,
	// task partitioning across rounds using continue blocks, and that
	// the handoff is reference material, not a substitute for
	// thinking: the next round must still reason about the problem and
	// decide how to proceed. It must also note that changes were not
	// applied to disk (atomic rollback). See TheoryOfHandoff.
	for _, want := range []string{
		"SELF-CONTAINED",
		"DISCARDED",
		"reference material",
		"think for itself",
		"discoveries",
		"decisions",
		"hallucinations",
		"nothing was completed",
		"partition",
		"continue block",
		"output limit",
		"boundary-delimited block",
	} {
		if !strings.Contains(HandoffSystemPrompt, want) {
			t.Fatalf("HandoffSystemPrompt must mention %q", want)
		}
	}
	// The handoff must not instruct the next round to act directly
	// without thinking, nor to re-read the filesystem: the context
	// already carries the latest state. See TheoryOfHandoff.
	if strings.Contains(HandoffSystemPrompt, "ACT DIRECTLY") {
		t.Fatal("HandoffSystemPrompt must not instruct acting directly without thinking")
	}
	if strings.Contains(HandoffSystemPrompt, "re-read") {
		t.Fatal("HandoffSystemPrompt must not instruct re-reading the filesystem")
	}
}

func TestHandoffSystemPromptRequiresHandoffBlock(t *testing.T) {
	// The handoff prompt must instruct the model to wrap the handoff
	// summary in a boundary-delimited block whose kind is the bare
	// function name "handoff" — written after the delimiter with no
	// parentheses and no parameters. Framing the kind as a named
	// parameter produced malformed headers carrying kind="handoff".
	// See TheoryOfHandoff.
	for _, want := range []string{
		"boundary-delimited block",
		`The block kind is "handoff"`,
		"no parentheses and no parameters",
		"block body must contain ONLY the handoff summary text",
	} {
		if !strings.Contains(HandoffSystemPrompt, want) {
			t.Fatalf("HandoffSystemPrompt must contain %q", want)
		}
	}
}

func TestPrintAttemptStatsWithSummaries(t *testing.T) {
	var buf bytes.Buffer
	stats := []AttemptStat{
		{Attempt: 1, PromptTokens: 1000, CompletionTokens: 500, Summary: "Analyzed the code."},
		{Attempt: 2, PromptTokens: 2000, CompletionTokens: 800, Summary: "Fixed the bug."},
	}
	PrintAttemptStats(&buf, stats)
	output := buf.String()
	if !strings.Contains(output, "=== Attempt Summaries ===") {
		t.Fatalf("expected summaries section, got: %s", output)
	}
	if !strings.Contains(output, "Attempt 1: Analyzed the code.") {
		t.Fatalf("expected attempt 1 summary, got: %s", output)
	}
	if !strings.Contains(output, "Attempt 2: Fixed the bug.") {
		t.Fatalf("expected attempt 2 summary, got: %s", output)
	}
}

func TestPrintAttemptStatsNoSummaries(t *testing.T) {
	var buf bytes.Buffer
	stats := []AttemptStat{
		{Attempt: 1, PromptTokens: 1000, CompletionTokens: 500},
	}
	PrintAttemptStats(&buf, stats)
	output := buf.String()
	if strings.Contains(output, "=== Attempt Summaries ===") {
		t.Fatalf("should not print summaries section when no summaries exist, got: %s", output)
	}
}

func TestPrintAttemptStatsWithDuration(t *testing.T) {
	var buf bytes.Buffer
	stats := []AttemptStat{
		{Attempt: 1, PromptTokens: 1000, CompletionTokens: 500, Duration: 3 * time.Second},
		{Attempt: 2, PromptTokens: 2000, CompletionTokens: 800, Duration: 1500 * time.Millisecond},
	}
	PrintAttemptStats(&buf, stats)
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

func TestPrintAttemptStatsWithLoopColumn(t *testing.T) {
	var buf bytes.Buffer
	stats := []AttemptStat{
		{Loop: 1, Attempt: 1, PromptTokens: 111, CompletionTokens: 51, Duration: time.Second, Summary: "first round"},
		{Loop: 1, Attempt: 2, PromptTokens: 222, CompletionTokens: 82, Duration: time.Second, Summary: "second round"},
	}
	PrintAttemptStats(&buf, stats, "Goal Loop Statistics")
	output := buf.String()
	if !strings.Contains(output, "=== Goal Loop Statistics ===") {
		t.Fatalf("expected custom title, got: %s", output)
	}
	if !strings.Contains(output, "Loop") {
		t.Fatalf("expected Loop column in output, got: %s", output)
	}
	if !strings.Contains(output, "Loop 1 Attempt 1: first round") {
		t.Fatalf("expected loop-aware summary for attempt 1, got: %s", output)
	}
	if !strings.Contains(output, "Loop 1 Attempt 2: second round") {
		t.Fatalf("expected loop-aware summary for attempt 2, got: %s", output)
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
				return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {
					generationInitiated = true
					return Result{}, nil, nil
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
				return func(ctx context.Context, output io.Writer) (Result, []AttemptStat, error) {
					reviewModel = string(modelName)
					return Result{}, nil, nil
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

// debugOutputMockGenerator is a generator stub that reports a usable
// context window so the token-budget computation in
// GenerateWithResultWithStats succeeds, and delegates CountTokens and
// Generate to the embedded summarizeRetryMockGenerator. The fake loop
// run in TestGenerateDebugPromptsWrittenToOutput never invokes
// Generate.
type debugOutputMockGenerator struct {
	summarizeRetryMockGenerator
}

type summarizeRetryMockGenerator struct {
	calls     int
	responses []string
	errs      []error
	thoughts  []string
	// onCall, when set, is invoked with the 0-based call number after
	// the call is counted. Tests use it to cancel the context mid-loop
	// and exercise createHandoff's unbounded retry loop through its
	// cancellation exit.
	onCall func(call int)
}

func (g *summarizeRetryMockGenerator) Spec() generators.Spec {
	return generators.Spec{}
}

func (g *summarizeRetryMockGenerator) CountTokens(string) (int, error) {
	return 0, nil
}

func (g *summarizeRetryMockGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	call := g.calls
	g.calls++
	if g.onCall != nil {
		g.onCall(call)
	}
	if call < len(g.errs) && g.errs[call] != nil {
		return state, g.errs[call]
	}
	response := g.responses[call]
	var parts []generators.Part
	if call < len(g.thoughts) && g.thoughts[call] != "" {
		parts = append(parts, generators.Thought(g.thoughts[call]))
	}
	parts = append(parts, generators.Text(response))
	return state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: parts,
	})
}

func TestCreateHandoffCancelledContext(t *testing.T) {
	// The unbounded retry loop exits only through success or context
	// cancellation. A pre-cancelled context stops the loop before any
	// attempt and returns nil — handoff stays non-fatal. See
	// TheoryOfHandoff.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gen := &summarizeRetryMockGenerator{
		responses: []string{"", "", ""},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(ctx, logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when the context is cancelled, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on cancellation, got %+v", handoff)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 handoff calls on a pre-cancelled context, got %d", gen.calls)
	}
}

func TestCreateHandoffRetriesOnGenerationFailure(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("handoff generation failed"),
		},
		responses: []string{
			"", // unused (first call errors)
			"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 handoff calls, got %d", gen.calls)
	}
	if handoff.Summary != "handoff prompt text" {
		t.Fatalf("expected summary 'handoff prompt text', got %q", handoff.Summary)
	}
	if handoff.Prompt != "handoff prompt text" {
		t.Fatalf("expected prompt 'handoff prompt text', got %q", handoff.Prompt)
	}
}

func TestCreateHandoffRetriesWithoutLimit(t *testing.T) {
	// Reproduction test for the unlimited retry policy: four generation
	// failures are followed by a fifth attempt that emits a valid
	// handoff block, and the loop must still deliver the summary. A
	// capped implementation stops at three attempts and returns nil.
	// See TheoryOfHandoff.
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("failure 1"),
			errors.New("failure 2"),
			errors.New("failure 3"),
			errors.New("failure 4"),
		},
		responses: []string{
			"", "", "", "",
			"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff after unbounded retries")
	}
	if gen.calls != 5 {
		t.Fatalf("expected 5 handoff calls, got %d", gen.calls)
	}
	if handoff.Summary != "handoff prompt text" {
		t.Fatalf("expected summary 'handoff prompt text', got %q", handoff.Summary)
	}
}

func TestCreateHandoffLogsErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("failure 1"),
			errors.New("failure 2"),
			errors.New("failure 3"),
		},
		responses: []string{"", "", ""},
		onCall: func(call int) {
			if call == 2 {
				cancel()
			}
		},
	}
	var buf bytes.Buffer
	logger := logs.Logger{slog.New(slog.NewTextHandler(&buf, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(ctx, logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when the context is cancelled, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on cancellation, got %+v", handoff)
	}
	output := buf.String()
	for _, want := range []string{
		"level=WARN",
		"handoff incomplete output: generation failed",
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
	if !strings.Contains(output, "level=ERROR") {
		t.Fatalf("expected an error-level log for the aborted handoff, got: %s", output)
	}
	if !strings.Contains(output, "handoff incomplete output aborted") {
		t.Fatalf("expected the abort message in the log, got: %s", output)
	}
}

func TestCreateHandoffProvider(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
		},
	}
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() GetHandoffGenerators {
			return func() ([]generators.Generator, error) {
				return []generators.Generator{gen}, nil
			}
		},
		func() *records.Recorder { return nil },
	).Call(func(
		createHandoff CreateHandoff,
	) {
		longInput := strings.Repeat("long incomplete text ", 10)
		handoff, err := createHandoff(context.Background(), longInput)
		if err != nil {
			t.Fatal(err)
		}
		if handoff.Summary != "handoff prompt text" {
			t.Fatalf("expected summary 'handoff prompt text', got %q", handoff.Summary)
		}
		if handoff.Prompt != "handoff prompt text" {
			t.Fatalf("expected prompt 'handoff prompt text', got %q", handoff.Prompt)
		}
	})
}

// fakeRecorderForSummarize is a minimal InteractionRecorder for testing
// that summarize requests, responses, and decision events are recorded.
// Contents and events are tracked separately so tests can assert both.
type fakeRecorderForSummarize struct {
	enabled  bool
	contents []*generators.Content
	events   []string
}

func TestCreateHandoffRecords(t *testing.T) {
	longInput := strings.Repeat("long incomplete text ", 10)

	t.Run("Enabled", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			responses: []string{
				"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
			},
		}
		logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &fakeRecorderForSummarize{enabled: true}
		handoff, err := createHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if handoff == nil {
			t.Fatal("expected handoff summary")
		}
		if len(rec.contents) != 2 {
			t.Fatalf("expected 2 recorded contents, got %d", len(rec.contents))
		}
		if rec.contents[0].Role != generators.RoleUser {
			t.Fatalf("expected first content role user, got %s", rec.contents[0].Role)
		}
		if text, ok := rec.contents[0].Parts[0].(generators.Text); !ok || !strings.Contains(string(text), "long incomplete text") {
			t.Fatalf("expected first content to include the incomplete text, got %v", rec.contents[0].Parts[0])
		}
		if rec.contents[1].Role != generators.RoleModel {
			t.Fatalf("expected second content role model, got %s", rec.contents[1].Role)
		}
		if text, ok := rec.contents[1].Parts[0].(generators.Text); !ok || !strings.Contains(string(text), "handoff prompt text") {
			t.Fatalf("expected second content to include the handoff prompt text, got %v", rec.contents[1].Parts[0])
		}
		if len(rec.events) != 0 {
			t.Fatalf("expected no decision events on success, got %v", rec.events)
		}
	})

	t.Run("Disabled", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			responses: []string{
				"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
			},
		}
		logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &fakeRecorderForSummarize{enabled: false}
		_, err := createHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(rec.contents) != 0 {
			t.Fatalf("expected no recorded contents when disabled, got %d", len(rec.contents))
		}
		if len(rec.events) != 0 {
			t.Fatalf("expected no recorded events when disabled, got %v", rec.events)
		}
	})

	t.Run("RecordsFailure", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			errs: []error{
				errors.New("failure"),
			},
			responses: []string{
				"", // unused (first call errors)
				"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
			},
		}
		logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
		rec := &fakeRecorderForSummarize{enabled: true}
		handoff, err := createHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if handoff == nil {
			t.Fatal("expected handoff summary")
		}
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
		if len(rec.events) != 1 {
			t.Fatalf("expected 1 decision event, got %d: %v", len(rec.events), rec.events)
		}
		if !strings.Contains(rec.events[0], "generation error") || !strings.Contains(rec.events[0], "failure") {
			t.Fatalf("unexpected decision event: %s", rec.events[0])
		}
	})
}

func TestCreateHandoffRecordsEmptyResponses(t *testing.T) {
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)

	t.Run("CancelledContextRecordsFinalAbort", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		gen := &summarizeRetryMockGenerator{
			responses: []string{"", "", ""},
			onCall: func(call int) {
				if call == 2 {
					cancel()
				}
			},
		}
		rec := &fakeRecorderForSummarize{enabled: true}
		handoff, err := createHandoff(ctx, logger, rec, []generators.Generator{gen}, longInput, nil, nil)
		if err != nil {
			t.Fatalf("expected nil error when the context is cancelled, got %v", err)
		}
		if handoff != nil {
			t.Fatalf("expected nil handoff on cancellation, got %+v", handoff)
		}
		if len(rec.events) != 4 {
			t.Fatalf("expected 4 decision events (3 attempts + abort), got %d: %v",
				len(rec.events), rec.events)
		}
		last := rec.events[len(rec.events)-1]
		if !strings.Contains(last, "handoff incomplete output aborted") {
			t.Fatalf("expected final abort event, got %s", last)
		}
	})
}

func TestCreateHandoffRecordsThoughts(t *testing.T) {
	longInput := strings.Repeat("long incomplete text ", 10)
	gen := &summarizeRetryMockGenerator{
		thoughts: []string{"the model reasoned about the handoff here"},
		responses: []string{
			"",
			"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := &fakeRecorderForSummarize{enabled: true}
	handoff, err := createHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff")
	}
	if handoff.Summary != "handoff prompt text" {
		t.Fatalf("expected summary 'handoff prompt text', got %q", handoff.Summary)
	}
	if handoff.Prompt != "handoff prompt text" {
		t.Fatalf("expected prompt 'handoff prompt text', got %q", handoff.Prompt)
	}
	var firstResponse string
	for _, c := range rec.contents {
		if c.Role == generators.RoleModel {
			if text, ok := c.Parts[0].(generators.Text); ok {
				firstResponse = string(text)
			}
			break
		}
	}
	if !strings.Contains(firstResponse, "[thought]\nthe model reasoned about the handoff here") {
		t.Fatalf("expected model thoughts in recorded response, got %q", firstResponse)
	}
}

func TestCreateHandoffRejectsPlainTextWithoutBlock(t *testing.T) {
	// When the model emits plain text without a handoff block, the
	// response must be treated as empty and retried. This prevents
	// incorrect or incomplete content from being used as handoff
	// instructions. Retries are unbounded, so the test cancels the
	// context after three plain-text responses to end the loop. See
	// TheoryOfHandoff.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"this is plain text without a block",
			"more plain text still no block",
			"final attempt, still plain text",
		},
		onCall: func(call int) {
			if call == 2 {
				cancel()
			}
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(ctx, logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when the context is cancelled, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff when model emits plain text without block, got %+v", handoff)
	}
	if gen.calls != 3 {
		t.Fatalf("expected 3 handoff calls, got %d", gen.calls)
	}
}

func TestCreateHandoffParsesHandoffBlockBody(t *testing.T) {
	// The handoff block body is parsed and trimmed as the handoff
	// content. Surrounding prose outside the block is ignored. See
	// TheoryOfHandoff.
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"I'll summarize now.\n<<黿鼍 handoff\nThis is the handoff content.\nWith multiple lines.\n黿鼍\nThat's all.",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff")
	}
	want := "This is the handoff content.\nWith multiple lines."
	if handoff.Summary != want {
		t.Fatalf("expected summary %q, got %q", want, handoff.Summary)
	}
	if handoff.Prompt != want {
		t.Fatalf("expected prompt %q, got %q", want, handoff.Prompt)
	}
}

// handoffUsageMockGenerator emits canned responses, each followed by a
// fixed usage part, so createHandoff's cross-attempt usage accumulation
// can be verified deterministically.
type handoffUsageMockGenerator struct {
	responses []string
	usage     generators.Usage
	calls     int
}

func (g *handoffUsageMockGenerator) Spec() generators.Spec {
	return generators.Spec{Name: "handoff-usage-mock", Model: "mock-handoff"}
}

func (g *handoffUsageMockGenerator) CountTokens(text string) (int, error) {
	return len(text), nil
}

func (g *handoffUsageMockGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	if g.calls >= len(g.responses) {
		return state, errors.New("no more responses")
	}
	response := g.responses[g.calls]
	g.calls++
	newState, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text(response)},
	})
	if err != nil {
		return state, err
	}
	return newState.AppendContent(&generators.Content{
		Role:  generators.RoleLog,
		Parts: []generators.Part{g.usage},
	})
}

func TestCreateHandoffAccumulatesUsageAcrossAttempts(t *testing.T) {
	// The delivered Handoff must report the token spend of every
	// generating attempt: the first response lacks a valid handoff
	// block and is retried, and the second succeeds. The accumulated
	// usage equals the sum of both attempts. See
	// TheoryOfHandoffUsageAccounting.
	gen := &handoffUsageMockGenerator{
		responses: []string{
			"I cannot produce a valid block.",
			"<<黿鼍 handoff\nThe condensed handoff content.\n黿鼍",
		},
		usage: generators.Usage{
			Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 11, TokenCountCached: 2},
			Candidates: struct{ TokenCount int }{TokenCount: 5},
			Thoughts:   struct{ TokenCount int }{TokenCount: 1},
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	input := strings.Repeat("long incomplete text ", 10)

	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, input, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff")
	}
	if handoff.Summary != "The condensed handoff content." {
		t.Fatalf("unexpected summary %q", handoff.Summary)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 generating attempts, got %d", gen.calls)
	}
	want := generators.Usage{
		Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 22, TokenCountCached: 4},
		Candidates: struct{ TokenCount int }{TokenCount: 10},
		Thoughts:   struct{ TokenCount int }{TokenCount: 2},
	}
	if handoff.Usage != want {
		t.Fatalf("expected accumulated usage %+v, got %+v", want, handoff.Usage)
	}
}

func TestCreateHandoffSkipsShortOutput(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{"handoff text"},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	shortInput := "too short"
	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, shortInput, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff for short output, got %+v", handoff)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 generator calls for short output, got %d", gen.calls)
	}
}

type fakeHandoffObserver struct {
	started int
	ended   int
}

// decoratorCapture is a test State that records appended contents and
// forwards them upstream, mirroring a display decorator. See
// TestCreateHandoffStreamsToDecorator.
type decoratorCapture struct {
	generators.State
	captured *[]*generators.Content
}

func (s decoratorCapture) AppendContent(content *generators.Content) (generators.State, error) {
	*s.captured = append(*s.captured, content)
	newUpstream, err := s.State.AppendContent(content)
	if err != nil {
		return nil, err
	}
	return decoratorCapture{State: newUpstream, captured: s.captured}, nil
}

func TestCreateHandoffStreamsToDecorator(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		thoughts: []string{"handoff thinking"},
		responses: []string{
			"<<黿鼍 handoff\nhandoff prompt text\n黿鼍",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	// The decorator observes every appended content, so a display
	// front-end receives the thought and text parts separately with
	// their roles and thinking state, instead of a byte stream that
	// loses part boundaries. See TheoryOfHandoff.
	var captured []*generators.Content
	decorator := func(state generators.State) generators.State {
		return decoratorCapture{State: state, captured: &captured}
	}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, decorator, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff")
	}
	if handoff.Summary != "handoff prompt text" {
		t.Fatalf("expected handoff text in summary, got %q", handoff.Summary)
	}
	var sawThought, sawText bool
	for _, c := range captured {
		for _, p := range c.Parts {
			switch p := p.(type) {
			case generators.Thought:
				sawThought = true
			case generators.Text:
				if strings.Contains(string(p), "handoff prompt text") {
					sawText = true
				}
			}
		}
	}
	if !sawThought || !sawText {
		t.Fatalf("expected the decorator to observe thought and text parts, got thought=%v text=%v", sawThought, sawText)
	}
}

func TestCreateHandoffReportsLifecycle(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{"<<黿鼍 handoff\nhandoff prompt text\n黿鼍"},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	obs := &fakeHandoffObserver{}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, obs)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff")
	}
	if obs.started != 1 || obs.ended != 1 {
		t.Fatalf("expected 1 start and 1 end, got %d start %d end", obs.started, obs.ended)
	}
}

func TestCreateHandoffReportsLifecycleOnFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gen := &summarizeRetryMockGenerator{
		responses: []string{"", "", ""},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	obs := &fakeHandoffObserver{}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := createHandoff(ctx, logger, nil, []generators.Generator{gen}, longInput, nil, obs)
	if err != nil {
		t.Fatalf("expected nil error when the context is cancelled, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on cancellation, got %+v", handoff)
	}
	if obs.started != 1 || obs.ended != 1 {
		t.Fatalf("expected 1 start and 1 end even on cancellation, got %d start %d end", obs.started, obs.ended)
	}
}

func (f *fakeHandoffObserver) HandoffEnd() { f.ended++ }

func (f *fakeHandoffObserver) HandoffStart() { f.started++ }

func (f *fakeRecorderForSummarize) Enabled() bool { return f.enabled }

func (f *fakeRecorderForSummarize) StartSession(string) {}

func (f *fakeRecorderForSummarize) EndSession(error) {}

func (f *fakeRecorderForSummarize) SystemPrompt(string) {}

func (f *fakeRecorderForSummarize) AttemptStart() {}

func (f *fakeRecorderForSummarize) AttemptCompleted([]string) {}

func (f *fakeRecorderForSummarize) AttemptTruncated() {}

func (f *fakeRecorderForSummarize) AttemptError(error) {}

func (f *fakeRecorderForSummarize) Content(content *generators.Content) {
	f.contents = append(f.contents, content)
}

func (f *fakeRecorderForSummarize) Block(blocks.Block) {}

func (f *fakeRecorderForSummarize) ParseError(*blocks.BlockParseError) {}

func (f *fakeRecorderForSummarize) Event(typ string, detail string) {
	f.events = append(f.events, typ+": "+detail)
}

func (debugOutputMockGenerator) Spec() generators.Spec {
	return generators.Spec{ContextTokens: 100000}
}

// chatBracketPartsProvider is a codetypes.PartsProvider returning one fixed
// context part, so bracketing tests can assert the position of the chat
// input relative to the provider content. See TheoryOfChatBracketing.
type chatBracketPartsProvider struct{}

func (chatBracketPartsProvider) Parts(
	maxTokens int,
	countTokens func(string) (int, error),
	patterns []string,
) (
	parts []generators.Part,
	err error,
) {
	return []generators.Part{generators.Text("CHAT BRACKET CONTEXT\n\n")}, nil
}

func TestGenerateDebugPromptsWrittenToOutput(t *testing.T) {
	// With -debug-codes, the assembled system and user prompts are dumped
	// to the generation output writer, never to os.Stdout directly: a
	// direct stdout write would bypass the writer that TUI mode forks,
	// and a library cannot know whether stdout is the terminal, a pipe,
	// or the null device. The test captures the provided writer and
	// asserts the dump lands there; before the fix the dump went to
	// os.Stdout and the captured writer stayed empty. See TheoryOfTUI
	// in cmd/tai/tui.go.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() flags.Chats { return flags.Chats{"hello"} },
		func() Debug { return Debug(true) },
		func() *records.Recorder { return nil },
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return &debugOutputMockGenerator{}, nil
			}
		},
		func() Run {
			return func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[Event, error] {
				return func(yield func(Event, error) bool) {}
			}
		},
	).Call(func(
		generateWithResultWithStats GenerateWithResultWithStats,
	) {
		var buf bytes.Buffer
		_, _, err := generateWithResultWithStats(context.Background(), &buf)
		if err != nil {
			t.Fatal(err)
		}
		output := buf.String()
		if !strings.Contains(output, "system prompt:") {
			t.Fatalf("expected the system prompt in the debug output, got: %q", output)
		}
		if !strings.Contains(output, "user prompt:") {
			t.Fatalf("expected the user prompt in the debug output, got: %q", output)
		}
	})
}

func TestGenerateChatInputBracketsContext(t *testing.T) {
	// The chat input must bracket the parts provider content: the initial
	// user content starts with a copy of the joined -chat arguments
	// (ending with a blank line) before the provider parts, and the chat
	// input itself is appended after the restate as the freshest input.
	// Content.Merge concatenates every adjacent Text part — the initial
	// content's parts and the appended chat content alike — so the state
	// carries one user content with one text part whose byte order is the
	// bracketing order: chat copy, provider context, restate, chat input.
	// See TheoryOfChatBracketing.
	var capturedState generators.State
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return chatBracketPartsProvider{} },
		func() flags.Chats { return flags.Chats{"do the task"} },
		func() *records.Recorder { return nil },
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return &debugOutputMockGenerator{}, nil
			}
		},
		func() Run {
			return func(ctx context.Context, opts RunOptions, result *Result) iter.Seq2[Event, error] {
				capturedState = opts.InitialState
				return func(yield func(Event, error) bool) {}
			}
		},
	).Call(func(
		generateWithResultWithStats GenerateWithResultWithStats,
	) {
		_, _, err := generateWithResultWithStats(context.Background(), &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
	})

	var contents []*generators.Content
	for content := range capturedState.Contents() {
		contents = append(contents, content)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 merged user content, got %d", len(contents))
	}
	if len(contents[0].Parts) != 1 {
		t.Fatalf("expected the merged content to hold one text part, got %d parts", len(contents[0].Parts))
	}
	text, ok := contents[0].Parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected a text part, got %T", contents[0].Parts[0])
	}

	// The chat copy opens the user content so the model reads the task
	// before the context.
	if !strings.HasPrefix(string(text), "do the task\n\n") {
		t.Fatalf("user content must start with the chat copy, got %q", string(text))
	}
	// The provider context follows, then the restate, then the chat input
	// as the freshest content at the very end.
	contextIdx := strings.Index(string(text), "CHAT BRACKET CONTEXT\n\n")
	restateIdx := strings.Index(string(text), "[System note:")
	if contextIdx < 0 || restateIdx < 0 || contextIdx >= restateIdx {
		t.Fatalf("provider context must precede the restate, got %q", string(text))
	}
	if !strings.HasSuffix(string(text), "do the task") {
		t.Fatalf("user content must end with the chat input, got %q", string(text))
	}
}

func TestCollectAttemptStats(t *testing.T) {
	t.Run("MultipleUsagePartsSingleAttempt", func(t *testing.T) {
		// Simulating Gemini streaming which emits multiple Usage parts in one attempt.
		// collectAttemptStats must produce exactly 1 AttemptStat entry with the last usage values.
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("hi")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100, TokenCountCached: 10},
				Candidates: struct{ TokenCount int }{TokenCount: 5},
				Thoughts:   struct{ TokenCount int }{TokenCount: 2},
			}}},
			{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("chunk 1")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100, TokenCountCached: 10},
				Candidates: struct{ TokenCount int }{TokenCount: 25},
				Thoughts:   struct{ TokenCount int }{TokenCount: 10},
			}}},
			{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("chunk 2")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100, TokenCountCached: 10},
				Candidates: struct{ TokenCount int }{TokenCount: 50},
				Thoughts:   struct{ TokenCount int }{TokenCount: 20},
			}}},
		})

		stats, nextCount := collectAttemptStats(nil, state, 1, 500*time.Millisecond, "attempt 1 summary")
		if len(stats) != 1 {
			t.Fatalf("expected exactly 1 AttemptStat, got %d", len(stats))
		}
		if stats[0].Attempt != 1 {
			t.Fatalf("expected Attempt 1, got %d", stats[0].Attempt)
		}
		if stats[0].PromptTokens != 100 || stats[0].CachedTokens != 10 || stats[0].CompletionTokens != 50 || stats[0].ThoughtTokens != 20 {
			t.Fatalf("expected final usage tokens (100, 10, 50, 20), got (%d, %d, %d, %d)",
				stats[0].PromptTokens, stats[0].CachedTokens, stats[0].CompletionTokens, stats[0].ThoughtTokens)
		}
		if stats[0].Summary != "attempt 1 summary" {
			t.Fatalf("expected summary 'attempt 1 summary', got %q", stats[0].Summary)
		}
		if stats[0].Duration != 500*time.Millisecond {
			t.Fatalf("expected duration 500ms, got %v", stats[0].Duration)
		}
		if nextCount != 6 {
			t.Fatalf("expected nextCount 6, got %d", nextCount)
		}
	})

	t.Run("MultipleGenerationsSequential", func(t *testing.T) {
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("r1")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100},
				Candidates: struct{ TokenCount int }{TokenCount: 30},
			}}},
		})
		stats, count1 := collectAttemptStats(nil, state, 0, time.Second, "r1 summary")

		state, _ = state.AppendContent(&generators.Content{
			Role:  generators.RoleUser,
			Parts: []generators.Part{generators.Text("r2")},
		})
		state, _ = state.AppendContent(&generators.Content{
			Role: generators.RoleLog,
			Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 200},
				Candidates: struct{ TokenCount int }{TokenCount: 60},
			}},
		})
		stats, count2 := collectAttemptStats(stats, state, count1, 2*time.Second, "r2 summary")

		if len(stats) != 2 {
			t.Fatalf("expected 2 AttemptStats, got %d", len(stats))
		}
		if stats[0].Attempt != 1 || stats[1].Attempt != 2 {
			t.Fatalf("expected Attempts 1 and 2, got %d and %d", stats[0].Attempt, stats[1].Attempt)
		}
		if stats[0].Summary != "r1 summary" || stats[1].Summary != "r2 summary" {
			t.Fatalf("unexpected summaries: %q, %q", stats[0].Summary, stats[1].Summary)
		}
		if count2 != 4 {
			t.Fatalf("expected count2 4, got %d", count2)
		}
	})

	t.Run("AttemptWithoutUsage", func(t *testing.T) {
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("no usage")}},
			{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("reply")}},
		})
		stats, _ := collectAttemptStats(nil, state, 0, time.Second, "no usage summary")
		if len(stats) != 1 {
			t.Fatalf("expected 1 AttemptStat, got %d", len(stats))
		}
		if stats[0].Attempt != 1 {
			t.Fatalf("expected Attempt 1, got %d", stats[0].Attempt)
		}
		if stats[0].PromptTokens != 0 || stats[0].CompletionTokens != 0 {
			t.Fatalf("expected zero token counts, got prompt=%d completion=%d", stats[0].PromptTokens, stats[0].CompletionTokens)
		}
		if stats[0].Summary != "no usage summary" {
			t.Fatalf("expected summary 'no usage summary', got %q", stats[0].Summary)
		}
	})
}

func TestAppendHandoffUsageSumsLastAndHandoff(t *testing.T) {
	t.Run("SumsLastAndHandoff", func(t *testing.T) {
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("hi")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100, TokenCountCached: 10},
				Candidates: struct{ TokenCount int }{TokenCount: 50},
				Thoughts:   struct{ TokenCount int }{TokenCount: 20},
			}}},
		})
		handoffUsage := generators.Usage{}
		handoffUsage.Prompt.TokenCount = 7
		handoffUsage.Candidates.TokenCount = 3
		handoffUsage.Thoughts.TokenCount = 1

		newState := appendHandoffUsage(state, 0, handoffUsage)
		got := extractLastUsage(newState, 0)
		if got.Prompt.TokenCount != 107 || got.Prompt.TokenCountCached != 10 ||
			got.Candidates.TokenCount != 53 || got.Thoughts.TokenCount != 21 {
			t.Fatalf("expected summed usage (107, 10, 53, 21), got (%d, %d, %d, %d)",
				got.Prompt.TokenCount, got.Prompt.TokenCountCached,
				got.Candidates.TokenCount, got.Thoughts.TokenCount)
		}
	})

	t.Run("ZeroSumSkipped", func(t *testing.T) {
		state := generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("hi")}},
		})
		newState := appendHandoffUsage(state, 0, generators.Usage{})
		if generators.CountContents(newState) != generators.CountContents(state) {
			t.Fatal("expected no injected content for a zero-sum usage")
		}
	})

	t.Run("WindowBaselineExcludesEarlierUsage", func(t *testing.T) {
		// A usage part before the baseline window must not be picked up:
		// the window starts at sinceContentCount, so an earlier round's
		// usage cannot leak into the injected sum.
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("hi")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt: struct{ TokenCount, TokenCountCached int }{TokenCount: 999},
			}}},
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("next")}},
		})
		newState := appendHandoffUsage(state, 2, generators.Usage{})
		if generators.CountContents(newState) != generators.CountContents(state) {
			t.Fatal("expected no injection when the window holds no usage")
		}
	})
}
