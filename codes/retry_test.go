package codes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
)

func TestRunPhaseWithRetry(t *testing.T) {
	summaryBlock := ":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n"
	noSummaryText := "some output without any blocks\n"
	logger := logs.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	t.Run("SucceedsOnFirstAttempt", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		var callCount int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			callCount++
			newState, err := state.AppendContent(&generators.Content{
				Role:  generators.RoleAssistant,
				Parts: []generators.Part{generators.Text(summaryBlock)},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, phaseErr, summaries, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, nil,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 call, got %d", callCount)
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
	})

	t.Run("RetriesOnMissingSummary", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		var callCount int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			callCount++
			var text string
			if callCount == 1 {
				text = noSummaryText
			} else {
				text = summaryBlock
			}
			newState, err := state.AppendContent(&generators.Content{
				Role:  generators.RoleAssistant,
				Parts: []generators.Part{generators.Text(text)},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, phaseErr, summaries, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, nil,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 calls (retry once), got %d", callCount)
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
	})

	t.Run("RetriesFromOriginalState", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		var statesSeen []int
		var callCount int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			callCount++
			statesSeen = append(statesSeen, countContents(state))
			var text string
			if callCount == 1 {
				text = noSummaryText
			} else {
				text = summaryBlock
			}
			newState, err := state.AppendContent(&generators.Content{
				Role:  generators.RoleAssistant,
				Parts: []generators.Part{generators.Text(text)},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, _, summaries, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, nil,
		)
		if len(statesSeen) != 2 {
			t.Fatalf("expected 2 state observations, got %d", len(statesSeen))
		}
		if statesSeen[0] != statesSeen[1] {
			t.Fatalf("retry should start from original state: first=%d, second=%d",
				statesSeen[0], statesSeen[1])
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
	})

	t.Run("GivesUpAfterMaxRetries", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		var callCount int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			callCount++
			newState, err := state.AppendContent(&generators.Content{
				Role:  generators.RoleAssistant,
				Parts: []generators.Part{generators.Text(noSummaryText)},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, phaseErr, summaries, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, nil,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if callCount != maxRetriesForMissingSummary+1 {
			t.Fatalf("expected %d calls, got %d", maxRetriesForMissingSummary+1, callCount)
		}
		if len(summaries) != 0 {
			t.Fatalf("expected 0 summaries, got %d", len(summaries))
		}
	})

	t.Run("PropagatesPhaseError", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		expectedErr := fmt.Errorf("phase error")
		var callCount int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			callCount++
			return nil, state, expectedErr
		}

		_, _, phaseErr, _, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, nil,
		)
		if phaseErr != expectedErr {
			t.Fatalf("expected error %v, got %v", expectedErr, phaseErr)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 call, got %d", callCount)
		}
	})
}

func TestRunPhaseWithRetryFinishBlock(t *testing.T) {
	logger := logs.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	baseState := generators.NewPrompts("", nil)
	initialParserState := blocks.NewParserState(baseState)

	finishBlock := ":::徕珑 <finish>\nDone.\n:::徕珑 </finish>\n"
	var callCount int
	phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		callCount++
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(finishBlock)},
		})
		if err != nil {
			return nil, state, err
		}
		return nil, newState, nil
	}

	_, _, phaseErr, _, _ := runPhaseWithRetry(
		context.Background(), phase, initialParserState, initialParserState, logger, nil,
	)
	if phaseErr != nil {
		t.Fatalf("unexpected error: %v", phaseErr)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call (no retry when finish block present), got %d", callCount)
	}
}

func TestRunPhaseWithRetrySummarization(t *testing.T) {
	logger := logs.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	t.Run("SummarizeAndRetry", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		var summarizeCalls []string
		summarize := func(incompleteText string) (string, error) {
			summarizeCalls = append(summarizeCalls, incompleteText)
			return "summary of incomplete output", nil
		}

		var phaseCalls int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			phaseCalls++
			// First call: return incomplete output (no summary block)
			if phaseCalls == 1 {
				newState, err := state.AppendContent(&generators.Content{
					Role: generators.RoleAssistant,
					Parts: []generators.Part{
						generators.Text("partial output without summary"),
					},
				})
				if err != nil {
					return nil, state, err
				}
				return nil, newState, nil
			}
			// Second call: return a summary block to signal completion
			newState, err := state.AppendContent(&generators.Content{
				Role: generators.RoleAssistant,
				Parts: []generators.Part{
					generators.Text(":::徕珑 <summary>\nDone after retry.\n:::徕珑 </summary>\n"),
				},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, phaseErr, summaries, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, summarize,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if phaseCalls != 2 {
			t.Fatalf("expected 2 phase calls, got %d", phaseCalls)
		}
		if len(summarizeCalls) != 1 {
			t.Fatalf("expected 1 summarize call, got %d", len(summarizeCalls))
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
	})

	t.Run("SummarizeAddsToState", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		var stateReceivedOnRetry generators.State
		summarize := func(incompleteText string) (string, error) {
			return "the summary", nil
		}

		var phaseCalls int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			phaseCalls++
			if phaseCalls == 1 {
				newState, err := state.AppendContent(&generators.Content{
					Role: generators.RoleAssistant,
					Parts: []generators.Part{
						generators.Text("incomplete"),
					},
				})
				if err != nil {
					return nil, state, err
				}
				return nil, newState, nil
			}
			stateReceivedOnRetry = state
			newState, err := state.AppendContent(&generators.Content{
				Role: generators.RoleAssistant,
				Parts: []generators.Part{
					generators.Text(":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n"),
				},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, phaseErr, _, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, summarize,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if phaseCalls != 2 {
			t.Fatalf("expected 2 phase calls, got %d", phaseCalls)
		}
		// The state on retry should contain the summary prefix and the summary text.
		foundPrefix := false
		foundSummary := false
		for c := range stateReceivedOnRetry.Contents() {
			for _, p := range c.Parts {
				if t, ok := p.(generators.Text); ok {
					if strings.Contains(string(t), incompleteOutputSummaryPrefix) {
						foundPrefix = true
					}
					if strings.Contains(string(t), "the summary") {
						foundSummary = true
					}
				}
			}
		}
		if !foundPrefix {
			t.Fatal("state on retry should contain the summary prefix")
		}
		if !foundSummary {
			t.Fatal("state on retry should contain the summary text")
		}
	})

	t.Run("SummarizeErrorFallsBack", func(t *testing.T) {
		baseState := generators.NewPrompts("", nil)
		initialParserState := blocks.NewParserState(baseState)

		summarize := func(incompleteText string) (string, error) {
			return "", fmt.Errorf("summarization failed")
		}

		var phaseCalls int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			phaseCalls++
			if phaseCalls == 1 {
				newState, err := state.AppendContent(&generators.Content{
					Role: generators.RoleAssistant,
					Parts: []generators.Part{
						generators.Text("incomplete"),
					},
				})
				if err != nil {
					return nil, state, err
				}
				return nil, newState, nil
			}
			newState, err := state.AppendContent(&generators.Content{
				Role: generators.RoleAssistant,
				Parts: []generators.Part{
					generators.Text(":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n"),
				},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		_, _, phaseErr, _, _ := runPhaseWithRetry(
			context.Background(), phase, initialParserState, initialParserState, logger, summarize,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if phaseCalls != 2 {
			t.Fatalf("expected 2 phase calls (retry without summary), got %d", phaseCalls)
		}
	})
}
