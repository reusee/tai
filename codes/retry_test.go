package codes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/changes"
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
		var collectedBlocks []blocks.Block

		// Handler collects blocks
		handler := func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		}

		// Wrap state with ParserState that has the handler
		ps := blocks.NewParserState(baseState, handler)

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

		_, _, phaseErr, summaries := runPhaseWithRetry(
			context.Background(), phase, ps, logger, nil,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		handler := func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		}

		ps := blocks.NewParserState(baseState, handler)

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

		_, _, phaseErr, summaries := runPhaseWithRetry(
			context.Background(), phase, ps, logger, nil,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		handler := func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		}

		ps := blocks.NewParserState(baseState, handler)

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

		_, _, _, summaries := runPhaseWithRetry(
			context.Background(), phase, ps, logger, nil,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		handler := func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		}

		ps := blocks.NewParserState(baseState, handler)

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

		_, _, phaseErr, summaries := runPhaseWithRetry(
			context.Background(), phase, ps, logger, nil,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		ps := blocks.NewParserState(baseState, func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		})

		expectedErr := fmt.Errorf("phase error")
		var callCount int
		phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
			callCount++
			return nil, state, expectedErr
		}

		_, _, phaseErr, _ := runPhaseWithRetry(
			context.Background(), phase, ps, logger, nil,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
	var collectedBlocks []blocks.Block

	finishBlock := ":::徕珑 <finish>\nDone.\n:::徕珑 </finish>\n"
	ps := blocks.NewParserState(baseState, func(block blocks.Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

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

	_, _, phaseErr, _ := runPhaseWithRetry(
		context.Background(), phase, ps, logger, nil,
		func() { collectedBlocks = nil },
		&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		ps := blocks.NewParserState(baseState, func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		})

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

		_, _, phaseErr, summaries := runPhaseWithRetry(
			context.Background(), phase, ps, logger, summarize,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		ps := blocks.NewParserState(baseState, func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		})

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

		_, _, phaseErr, _ := runPhaseWithRetry(
			context.Background(), phase, ps, logger, summarize,
			func() { collectedBlocks = nil },
			&collectedBlocks,
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
		var collectedBlocks []blocks.Block

		ps := blocks.NewParserState(baseState, func(block blocks.Block) error {
			collectedBlocks = append(collectedBlocks, block)
			return nil
		})

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

		_, _, phaseErr, _ := runPhaseWithRetry(
			context.Background(), phase, ps, logger, summarize,
			func() { collectedBlocks = nil },
			&collectedBlocks,
		)
		if phaseErr != nil {
			t.Fatalf("unexpected error: %v", phaseErr)
		}
		if phaseCalls != 2 {
			t.Fatalf("expected 2 phase calls (retry without summary), got %d", phaseCalls)
		}
	})
}

func TestRunPhaseWithRetryCallsOnPhaseStart(t *testing.T) {
	logger := logs.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	baseState := generators.NewPrompts("", nil)
	var collectedBlocks []blocks.Block

	ps := blocks.NewParserState(baseState, func(block blocks.Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	var onPhaseStartCalls int
	onPhaseStart := func() {
		onPhaseStartCalls++
	}

	noSummaryText := "some output without any blocks\n"
	phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(noSummaryText)},
		})
		if err != nil {
			return nil, state, err
		}
		return nil, newState, nil
	}

	_, _, _, _ = runPhaseWithRetry(
		context.Background(), phase, ps, logger, nil, onPhaseStart,
		&collectedBlocks,
	)

	// onPhaseStart should be called once per attempt: initial + maxRetriesForMissingSummary
	expectedCalls := maxRetriesForMissingSummary + 1
	if onPhaseStartCalls != expectedCalls {
		t.Fatalf("expected %d onPhaseStart calls (one per attempt including retries), got %d", expectedCalls, onPhaseStartCalls)
	}
}

func TestRunPhaseWithRetryMemoryStoreConsistency(t *testing.T) {
	logger := logs.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// Create initial file on disk
	original := "package x\n\nfunc Old() {}\n"
	if err := root.WriteFile("test.go", []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	memStore := changes.NewMemoryStore(changes.NewRootStore(root))
	var collectedBlocks []blocks.Block

	ps := blocks.NewParserState(generators.NewPrompts("", nil), func(block blocks.Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Track which attempt we're on
	var attempt int
	phase := func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		attempt++

		// Simulate applying a change block to memStore during generation.
		newFile := fmt.Sprintf("package x\n\nfunc New%d() {}\n", attempt)
		if err := memStore.WriteFile(fmt.Sprintf("file%d.go", attempt), []byte(newFile), 0644); err != nil {
			return nil, state, err
		}

		if attempt <= maxRetriesForMissingSummary {
			// No completion block (truncated output)
			newState, err := state.AppendContent(&generators.Content{
				Role:  generators.RoleAssistant,
				Parts: []generators.Part{generators.Text("incomplete")},
			})
			if err != nil {
				return nil, state, err
			}
			return nil, newState, nil
		}

		// Final attempt: include a completion block
		newState, err := state.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(":::徕珑 <summary>\nDone.\n:::徕珑 </summary>\n")},
		})
		if err != nil {
			return nil, state, err
		}
		return nil, newState, nil
	}

	onPhaseStart := func() {
		memStore.Reset()
		collectedBlocks = nil
	}

	_, _, _, _ = runPhaseWithRetry(
		context.Background(), phase, ps, logger, nil, onPhaseStart,
		&collectedBlocks,
	)

	// After all retries, the MemoryStore should ONLY have the file from
	// the LAST (successful) attempt.
	lastAttempt := maxRetriesForMissingSummary + 1

	lastFile := fmt.Sprintf("file%d.go", lastAttempt)
	lastContent := fmt.Sprintf("package x\n\nfunc New%d() {}\n", lastAttempt)
	got, err := memStore.ReadFile(lastFile)
	if err != nil {
		t.Fatalf("expected last attempt's file %s in memStore: %v", lastFile, err)
	}
	if string(got) != lastContent {
		t.Fatalf("expected last attempt's content %q, got %q", lastContent, string(got))
	}

	// Files from failed attempts should NOT exist in memStore.
	for i := 1; i < lastAttempt; i++ {
		failedFile := fmt.Sprintf("file%d.go", i)
		_, err := memStore.ReadFile(failedFile)
		if err == nil {
			t.Fatalf("file %s from failed attempt %d should have been cleared by Reset(), but is still in memStore", failedFile, i)
		}
	}

	// Disk should still have only the original file (Flush was not called
	// by runPhaseWithRetry; it is called by the caller after success).
	diskContent, err := root.ReadFile("test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(diskContent) != original {
		t.Fatalf("disk should have original content (Flush not called), got %q", string(diskContent))
	}
}
