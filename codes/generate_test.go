package codes

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
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/loops"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/records"
	"github.com/reusee/tai/states"
)

// maxSummarizeRetries mirrors maxHandoffRetries from
// states/summarize_incomplete.go: the handoff generation is retried up to
// this many times on failure or an empty response before the run aborts.
// The constant is restated here because the states constant is unexported.
// See states.TheoryOfHandoff.
const maxSummarizeRetries = 3

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
		state, count, summary, err := handoffRetryState(partial, phaseErr, 1, func(text string) (*states.Handoff, error) {
			return &states.Handoff{Summary: "condensed", Prompt: "condensed"}, nil
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
		state, count, summary, err := handoffRetryState(partial, phaseErr, 1, func(text string) (*states.Handoff, error) {
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
		state, count, summary, err := handoffRetryState(base, phaseErr, 1, func(text string) (*states.Handoff, error) {
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

func TestHandoffSystemPromptSelfContainedAndReferenceOriented(t *testing.T) {
	// The handoff prompt must emphasize self-contained extraction,
	// task partitioning across rounds using continue blocks, and that
	// the handoff is reference material, not a substitute for
	// thinking: the next round must still reason about the problem and
	// decide how to proceed. It must also note that changes were not
	// applied to disk (atomic rollback). See states.TheoryOfHandoff.
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
		if !strings.Contains(states.HandoffSystemPrompt, want) {
			t.Fatalf("states.HandoffSystemPrompt must mention %q", want)
		}
	}
	// The handoff must not instruct the next round to act directly
	// without thinking, nor to re-read the filesystem: the context
	// already carries the latest state. See states.TheoryOfHandoff.
	if strings.Contains(states.HandoffSystemPrompt, "ACT DIRECTLY") {
		t.Fatal("states.HandoffSystemPrompt must not instruct acting directly without thinking")
	}
	if strings.Contains(states.HandoffSystemPrompt, "re-read") {
		t.Fatal("states.HandoffSystemPrompt must not instruct re-reading the filesystem")
	}
}

func TestHandoffSystemPromptRequiresHandoffBlock(t *testing.T) {
	// The handoff prompt must instruct the model to wrap the handoff
	// summary in a boundary-delimited block whose kind is the bare
	// function name "handoff" — written after the delimiter with no
	// parentheses and no parameters. Framing the kind as a named
	// parameter produced malformed headers carrying kind="handoff".
	// See states.TheoryOfHandoff.
	for _, want := range []string{
		"boundary-delimited block",
		`The block kind is "handoff"`,
		"no parentheses and no parameters",
		"block body must contain ONLY the handoff summary text",
	} {
		if !strings.Contains(states.HandoffSystemPrompt, want) {
			t.Fatalf("states.HandoffSystemPrompt must contain %q", want)
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
	var parts []generators.Part
	if g.calls < len(g.thoughts) && g.thoughts[g.calls] != "" {
		parts = append(parts, generators.Thought(g.thoughts[g.calls]))
	}
	parts = append(parts, generators.Text(response))
	g.calls++
	return state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: parts,
	})
}

func TestCreateHandoffErrorsAfterEmptyResponses(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{"", "", ""},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when all handoff attempts fail, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on failure, got %+v", handoff)
	}
	if gen.calls != maxSummarizeRetries {
		t.Fatalf("expected %d handoff calls (maxRetries), got %d", maxSummarizeRetries, gen.calls)
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
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
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

func TestCreateHandoffErrorsAfterGenerationFailures(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		errs: []error{
			errors.New("failure 1"),
			errors.New("failure 2"),
			errors.New("failure 3"),
		},
		responses: []string{"", "", ""},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when all handoff generations fail, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on failure, got %+v", handoff)
	}
	if gen.calls != maxSummarizeRetries {
		t.Fatalf("expected %d handoff calls, got %d", maxSummarizeRetries, gen.calls)
	}
}

func TestCreateHandoffLogsErrors(t *testing.T) {
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
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when all handoff generations fail, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on failure, got %+v", handoff)
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
		t.Fatalf("expected an error-level log for the final failure, got: %s", output)
	}
	if !strings.Contains(output, "handoff incomplete output failed") {
		t.Fatalf("expected the final failure message in the log, got: %s", output)
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
		func() states.GetHandoffGenerators {
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
		handoff, err := states.CreateHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
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
		_, err := states.CreateHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
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
		handoff, err := states.CreateHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
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

	t.Run("AllAttemptsFailRecordsFinalFailure", func(t *testing.T) {
		gen := &summarizeRetryMockGenerator{
			responses: []string{"", "", ""},
		}
		rec := &fakeRecorderForSummarize{enabled: true}
		handoff, err := states.CreateHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
		if err != nil {
			t.Fatalf("expected nil error when all handoff attempts fail, got %v", err)
		}
		if handoff != nil {
			t.Fatalf("expected nil handoff on failure, got %+v", handoff)
		}
		if len(rec.events) != maxSummarizeRetries+1 {
			t.Fatalf("expected %d decision events, got %d: %v",
				maxSummarizeRetries+1, len(rec.events), rec.events)
		}
		last := rec.events[len(rec.events)-1]
		if !strings.Contains(last, "handoff incomplete output failed after") {
			t.Fatalf("expected final failure event, got %s", last)
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
	handoff, err := states.CreateHandoff(context.Background(), logger, rec, []generators.Generator{gen}, longInput, nil, nil)
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
	// instructions. See states.TheoryOfHandoff.
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"this is plain text without a block",
			"more plain text still no block",
			"final attempt, still plain text",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error when all handoff attempts fail, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff when model emits plain text without block, got %+v", handoff)
	}
	if gen.calls != maxSummarizeRetries {
		t.Fatalf("expected %d handoff calls, got %d", maxSummarizeRetries, gen.calls)
	}
}

func TestCreateHandoffParsesHandoffBlockBody(t *testing.T) {
	// The handoff block body is parsed and trimmed as the handoff
	// content. Surrounding prose outside the block is ignored. See
	// states.TheoryOfHandoff.
	gen := &summarizeRetryMockGenerator{
		responses: []string{
			"I'll summarize now.\n<<黿鼍 handoff\nThis is the handoff content.\nWith multiple lines.\n黿鼍\nThat's all.",
		},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, nil)
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

func TestCreateHandoffSkipsShortOutput(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{"handoff text"},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	shortInput := "too short"
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, shortInput, nil, nil)
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

func TestCreateHandoffStreamsToWriter(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{"<<黿鼍 handoff\nhandoff prompt text\n黿鼍"},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	var buf bytes.Buffer
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handoff == nil {
		t.Fatal("expected handoff")
	}
	if !strings.Contains(buf.String(), "handoff prompt text") {
		t.Fatalf("expected handoff text in writer, got %q", buf.String())
	}
}

func TestCreateHandoffReportsLifecycle(t *testing.T) {
	gen := &summarizeRetryMockGenerator{
		responses: []string{"<<黿鼍 handoff\nhandoff prompt text\n黿鼍"},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	obs := &fakeHandoffObserver{}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, obs)
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
	gen := &summarizeRetryMockGenerator{
		responses: []string{"", "", ""},
	}
	logger := logs.Logger{slog.New(slog.NewTextHandler(io.Discard, nil))}
	obs := &fakeHandoffObserver{}
	longInput := strings.Repeat("long incomplete text ", 10)
	handoff, err := states.CreateHandoff(context.Background(), logger, nil, []generators.Generator{gen}, longInput, nil, obs)
	if err != nil {
		t.Fatalf("expected nil error when all handoff attempts fail, got %v", err)
	}
	if handoff != nil {
		t.Fatalf("expected nil handoff on failure, got %+v", handoff)
	}
	if obs.started != 1 || obs.ended != 1 {
		t.Fatalf("expected 1 start and 1 end even on failure, got %d start %d end", obs.started, obs.ended)
	}
}

func (f *fakeHandoffObserver) HandoffEnd() { f.ended++ }

func (f *fakeHandoffObserver) HandoffStart() { f.started++ }

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
	f.events = append(f.events, typ+": "+detail)
}

func (debugOutputMockGenerator) Spec() generators.Spec {
	return generators.Spec{ContextTokens: 100000}
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
		func() loops.Run {
			return func(ctx context.Context, opts loops.RunOptions, result *loops.Result) iter.Seq[error] {
				return func(yield func(error) bool) {}
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

func TestGenerateRoundStatsWrittenToRoundStatsWriter(t *testing.T) {
	// The round statistics table is written to the RoundStatsWriter
	// provider when one is configured, never to the generation output
	// writer: in TUI mode the output writer is the redirected null
	// device (see runWithTUI in cmd/tai/tui.go), so the deferred
	// PrintRoundStats in the codes pipeline needs the forked provider
	// to stay visible. The test forks the provider, runs one fake round
	// that collects a stat through OnRoundSuccess, and asserts the table
	// lands in the forked writer while the output writer stays without
	// it. See TheoryOfRoundStatistics.
	var statsBuf bytes.Buffer
	var outputBuf bytes.Buffer
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() flags.Chats { return flags.Chats{"hello"} },
		func() *records.Recorder { return nil },
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return &debugOutputMockGenerator{}, nil
			}
		},
		func() loops.Run {
			return func(ctx context.Context, opts loops.RunOptions, result *loops.Result) iter.Seq[error] {
				return func(yield func(error) bool) {
					// One successful round with no summaries: the loop
					// collects a zero-usage RoundStat, enough for
					// PrintRoundStats to render the table.
					if err := opts.OnRoundSuccess(opts.InitialState, nil); err != nil {
						t.Errorf("OnRoundSuccess: %v", err)
					}
				}
			}
		},
		func() RoundStatsWriter { return RoundStatsWriter(&statsBuf) },
	).Call(func(
		generateWithResultWithStats GenerateWithResultWithStats,
	) {
		if _, _, err := generateWithResultWithStats(context.Background(), &outputBuf); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(statsBuf.String(), "Generation Statistics") {
			t.Fatalf("expected the round statistics table in the stats writer, got: %q", statsBuf.String())
		}
		if strings.Contains(outputBuf.String(), "Generation Statistics") {
			t.Fatalf("expected the output writer to stay without the statistics table, got: %q", outputBuf.String())
		}
	})
}

func TestCollectRoundStats(t *testing.T) {
	t.Run("MultipleUsagePartsSingleRound", func(t *testing.T) {
		// Simulating Gemini streaming which emits multiple Usage parts in one round.
		// collectRoundStats must produce exactly 1 RoundStat entry with the last usage values.
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

		stats, nextCount := collectRoundStats(nil, state, 1, 500*time.Millisecond, "round 1 summary")
		if len(stats) != 1 {
			t.Fatalf("expected exactly 1 RoundStat, got %d", len(stats))
		}
		if stats[0].Round != 1 {
			t.Fatalf("expected Round 1, got %d", stats[0].Round)
		}
		if stats[0].PromptTokens != 100 || stats[0].CachedTokens != 10 || stats[0].CompletionTokens != 50 || stats[0].ThoughtTokens != 20 {
			t.Fatalf("expected final usage tokens (100, 10, 50, 20), got (%d, %d, %d, %d)",
				stats[0].PromptTokens, stats[0].CachedTokens, stats[0].CompletionTokens, stats[0].ThoughtTokens)
		}
		if stats[0].Summary != "round 1 summary" {
			t.Fatalf("expected summary 'round 1 summary', got %q", stats[0].Summary)
		}
		if stats[0].Duration != 500*time.Millisecond {
			t.Fatalf("expected duration 500ms, got %v", stats[0].Duration)
		}
		if nextCount != 6 {
			t.Fatalf("expected nextCount 6, got %d", nextCount)
		}
	})

	t.Run("MultipleRoundsSequential", func(t *testing.T) {
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("r1")}},
			{Role: generators.RoleLog, Parts: []generators.Part{generators.Usage{
				Prompt:     struct{ TokenCount, TokenCountCached int }{TokenCount: 100},
				Candidates: struct{ TokenCount int }{TokenCount: 30},
			}}},
		})
		stats, count1 := collectRoundStats(nil, state, 0, time.Second, "r1 summary")

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
		stats, count2 := collectRoundStats(stats, state, count1, 2*time.Second, "r2 summary")

		if len(stats) != 2 {
			t.Fatalf("expected 2 RoundStats, got %d", len(stats))
		}
		if stats[0].Round != 1 || stats[1].Round != 2 {
			t.Fatalf("expected Rounds 1 and 2, got %d and %d", stats[0].Round, stats[1].Round)
		}
		if stats[0].Summary != "r1 summary" || stats[1].Summary != "r2 summary" {
			t.Fatalf("unexpected summaries: %q, %q", stats[0].Summary, stats[1].Summary)
		}
		if count2 != 4 {
			t.Fatalf("expected count2 4, got %d", count2)
		}
	})

	t.Run("RoundWithoutUsage", func(t *testing.T) {
		var state generators.State = generators.NewPrompts("", []*generators.Content{
			{Role: generators.RoleUser, Parts: []generators.Part{generators.Text("no usage")}},
			{Role: generators.RoleAssistant, Parts: []generators.Part{generators.Text("reply")}},
		})
		stats, _ := collectRoundStats(nil, state, 0, time.Second, "no usage summary")
		if len(stats) != 1 {
			t.Fatalf("expected 1 RoundStat, got %d", len(stats))
		}
		if stats[0].Round != 1 {
			t.Fatalf("expected Round 1, got %d", stats[0].Round)
		}
		if stats[0].PromptTokens != 0 || stats[0].CompletionTokens != 0 {
			t.Fatalf("expected zero token counts, got prompt=%d completion=%d", stats[0].PromptTokens, stats[0].CompletionTokens)
		}
		if stats[0].Summary != "no usage summary" {
			t.Fatalf("expected summary 'no usage summary', got %q", stats[0].Summary)
		}
	})
}
