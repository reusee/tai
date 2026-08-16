package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
)

func TestEffortFlagInjection(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() flags.Effort {
			return flags.Effort("high")
		},
	).Call(func(
		newGemini NewGemini,
		newOpenAI NewOpenAI,
	) {
		g := newGemini(Spec{
			Model:           "test-model",
			ReasoningEffort: "low",
		})
		if got := string(g.Effort()); got != "high" {
			t.Errorf("expected injected effort 'high' to override spec, got %q", got)
		}

		o := newOpenAI(Spec{
			Model:           "test-model",
			ReasoningEffort: "low",
		}, "test-key")
		if got := string(o.Effort()); got != "high" {
			t.Errorf("expected injected effort 'high' to override spec, got %q", got)
		}
	})
}
