package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
)

// TestRunUnknownBlockKindFeedback verifies that a well-formed block of a
// kind the session cannot process is reported immediately after the
// attempt and fed back as a correction error — naming the kind and the
// boundary, forbidding re-emission, requiring the original task to
// continue — and that the correction round runs as the next generation.
// See TheoryOfUnknownBlockKinds.
func TestRunUnknownBlockKindFeedback(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<永樂 mystery\nsome content\n永樂\n<<崇禎 summary\nRound 1 done.\n崇禎\n")
			}
			return appendPhase("<<崇禎 summary\nDone.\n崇禎\n")
		}
		comps := components.ComponentSet{
			{Kind: "summary", PromptSection: "summary prompt"},
		}
		result, err := runOnce(run, RunOptions{
			Generator:       nil,
			InitialState:    generators.NewPrompts("", nil),
			Components:      comps,
			PhaseBuilder:    phaseBuilder,
			KnownBlockKinds: comps.KnownKinds(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 rounds (1 unknown-kind correction round), got %d", callCount)
		}
		foundFeedback := false
		for c := range result.FinalState.Contents() {
			if c.Role == generators.RoleUser {
				for _, p := range c.Parts {
					if text, ok := p.(generators.Text); ok {
						if strings.Contains(string(text), "NOT available in this session") &&
							strings.Contains(string(text), `kind "mystery"`) &&
							strings.Contains(string(text), "CONTINUE the original task") {
							foundFeedback = true
						}
					}
				}
			}
		}
		if !foundFeedback {
			t.Fatal("expected unknown-kind feedback in state")
		}
		// The unprocessable block stays in the remaining blocks.
		foundRemaining := false
		for _, block := range result.RemainingBlocks {
			if block.Kind == "mystery" {
				foundRemaining = true
			}
		}
		if !foundRemaining {
			t.Fatal("expected the unknown-kind block to remain in RemainingBlocks")
		}
	})
}

// TestRunUnknownBlockKindKnownPasses verifies that a collected block of
// a kind the predicate accepts produces no correction round, and that a
// nil predicate (the default) disables the check entirely.
func TestRunUnknownBlockKindKnownPasses(t *testing.T) {
	withRun(t, func(run Run) {
		t.Run("known kind", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				return appendPhase("<<永樂 change\nbody\n永樂\n<<崇禎 summary\nDone.\n崇禎\n")
			}
			comps := components.ComponentSet{
				{Kind: "change", PromptSection: "change prompt"},
			}
			result, err := runOnce(run, RunOptions{
				Generator:       nil,
				InitialState:    generators.NewPrompts("", nil),
				Components:      comps,
				PhaseBuilder:    phaseBuilder,
				KnownBlockKinds: comps.KnownKinds(),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != 1 {
				t.Fatalf("expected 1 round, got %d", callCount)
			}
			for c := range result.FinalState.Contents() {
				if c.Role == generators.RoleUser {
					for _, p := range c.Parts {
						if text, ok := p.(generators.Text); ok {
							if strings.Contains(string(text), "NOT available in this session") {
								t.Fatal("known kind must not trigger feedback")
							}
						}
					}
				}
			}
		})
		t.Run("nil predicate disables check", func(t *testing.T) {
			callCount := 0
			phaseBuilder := func(g generators.Generator) generators.Phase {
				callCount++
				return appendPhase("<<永樂 mystery\nbody\n永樂\n<<崇禎 summary\nDone.\n崇禎\n")
			}
			comps := components.ComponentSet{
				{Kind: "summary", PromptSection: "summary prompt"},
			}
			_, err := runOnce(run, RunOptions{
				Generator:    nil,
				InitialState: generators.NewPrompts("", nil),
				Components:   comps,
				PhaseBuilder: phaseBuilder,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != 1 {
				t.Fatalf("expected 1 round, got %d", callCount)
			}
		})
	})
}

// TestRunUnknownBlockKindBudgetExhausted verifies that the shared
// correction budget bounds the feedback: after the budget is spent, an
// unavailable kind no longer schedules a generation and the run ends
// with the blocks accumulated in the remaining blocks.
func TestRunUnknownBlockKindBudgetExhausted(t *testing.T) {
	withRun(t, func(run Run) {
		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			return appendPhase("<<永樂 mystery\nbody\n永樂\n<<崇禎 summary\nRound done.\n崇禎\n")
		}
		comps := components.ComponentSet{
			{Kind: "summary", PromptSection: "summary prompt"},
		}
		result, err := runOnce(run, RunOptions{
			Generator:       nil,
			InitialState:    generators.NewPrompts("", nil),
			Components:      comps,
			PhaseBuilder:    phaseBuilder,
			KnownBlockKinds: comps.KnownKinds(),
			MaxGenerations:  10,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != maxParseErrorCorrections+1 {
			t.Fatalf("expected %d rounds (budget %d corrections), got %d", maxParseErrorCorrections+1, maxParseErrorCorrections, callCount)
		}
		if len(result.RemainingBlocks) != maxParseErrorCorrections+1 {
			t.Fatalf("expected %d remaining blocks, got %d", maxParseErrorCorrections+1, len(result.RemainingBlocks))
		}
	})
}
