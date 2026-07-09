package phases

import (
	"context"
	"errors"
	"testing"

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

	module := Module{}
	buildGenerate := module.BuildGenerate()
	phase := buildGenerate(gen, nil)(nil)

	_, _, err := phase(context.Background(), generators.NewPrompts("", nil))
	if err == nil {
		t.Fatal("expected error after retry limit exhausted")
	}
	if calls != 3 {
		t.Fatalf("expected 3 generate calls (maxRetries), got %d", calls)
	}
}

func TestBuildGenerateRetryThenSuccess(t *testing.T) {
	calls := 0
	gen := &retryThenSuccessGenerator{calls: &calls, succeedAt: 2}

	module := Module{}
	buildGenerate := module.BuildGenerate()
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
}
