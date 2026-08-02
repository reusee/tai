package phases

import (
	"context"
	"errors"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/generators"
)

type alwaysRetryableGenerator struct {
	calls *int
}

func (g *alwaysRetryableGenerator) Spec() generators.Spec           { return generators.Spec{} }
func (g *alwaysRetryableGenerator) CountTokens(string) (int, error) { return 0, nil }
func (g *alwaysRetryableGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	*g.calls++
	return nil, errors.Join(errors.New("no output"), generators.ErrRetryable)
}

type retryThenSuccessGenerator struct {
	calls     *int
	succeedAt int
}

func (g *retryThenSuccessGenerator) Spec() generators.Spec           { return generators.Spec{} }
func (g *retryThenSuccessGenerator) CountTokens(string) (int, error) { return 0, nil }
func (g *retryThenSuccessGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	*g.calls++
	if *g.calls >= g.succeedAt {
		return state, nil
	}
	return nil, errors.Join(errors.New("no output"), generators.ErrRetryable)
}

func TestBuildGenerateRetryLimit(t *testing.T) {
	calls := 0
	gen := &alwaysRetryableGenerator{calls: &calls}

	// phases.Module's BuildChatPhase provider requires logs.Logger and
	// debugs.Tap. Include debugs.Module (which embeds logs.Module) so
	// both are resolvable. See the user's instruction to use real dscope
	// instances with Call rather than direct method calls.
	dscope.New(
		new(Module),
		new(debugs.Module),
	).Call(func(
		buildGenerate BuildGenerate,
	) {
		phase := buildGenerate(gen, nil)(nil)

		_, _, err := phase(context.Background(), generators.NewPrompts("", nil))
		if err == nil {
			t.Fatal("expected error after retry limit exhausted")
		}
		if calls != 3 {
			t.Fatalf("expected 3 generate calls (maxRetries), got %d", calls)
		}
	})
}

func TestBuildGenerateRetryThenSuccess(t *testing.T) {
	calls := 0
	gen := &retryThenSuccessGenerator{calls: &calls, succeedAt: 2}

	dscope.New(
		new(Module),
		new(debugs.Module),
	).Call(func(
		buildGenerate BuildGenerate,
	) {
		phase := buildGenerate(gen, nil)(nil)

		nextPhase, _, err := phase(context.Background(), generators.NewPrompts("", nil))
		if err != nil {
			t.Fatalf("expected success on second attempt, got: %v", err)
		}
		if nextPhase != nil {
			t.Fatal("expected nil next phase when cont is nil")
		}
		if calls != 2 {
			t.Fatalf("expected 2 generate calls, got %d", calls)
		}
	})
}

type nonRetryableErrorGenerator struct{}

func (g *nonRetryableErrorGenerator) Spec() generators.Spec           { return generators.Spec{} }
func (g *nonRetryableErrorGenerator) CountTokens(string) (int, error) { return 0, nil }
func (g *nonRetryableErrorGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	return nil, errors.New("fatal error")
}

func TestBuildGenerateNonRetryableErrorReturnsState(t *testing.T) {
	// When generator.Generate returns a non-retryable error, the phase
	// must return the input state (not nil) so that callers like loops.Run
	// can pass a valid state to OnPhaseError.
	gen := &nonRetryableErrorGenerator{}

	dscope.New(
		new(Module),
		new(debugs.Module),
	).Call(func(
		buildGenerate BuildGenerate,
	) {
		phase := buildGenerate(gen, nil)(nil)

		initialState := generators.NewPrompts("", nil)
		_, state, err := phase(context.Background(), initialState)
		if err == nil {
			t.Fatal("expected error")
		}
		if state == nil {
			t.Fatal("expected non-nil state on non-retryable error, got nil")
		}
	})
}
