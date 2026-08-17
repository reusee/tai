package states

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfHandoff = `
When a generation round is truncated (no summary block) or errors after
producing partial output, a handoff summary is constructed before retrying.
The handoff condenses the valuable thinking from the interrupted output —
discoveries, insights, analysis, and decisions about the problem — into a
single self-contained text.

All changes are atomic: a truncated or failed round applies nothing, so
there is no completed work, no remaining work, and no next step to carry
forward. The handoff therefore never reports work status; claims about
what was completed or what remains are hallucinations, because the output
process failed and everything it produced was discarded.

The handoff's value is in the reasoning it preserves: it guides the
direction of the next generation round and mitigates the model's tendency
to overthink by carrying forward established insights and conclusions.
The handoff is reference material, not a substitute for thinking: the
next round must still reason about the problem and decide how to proceed,
using the handoff to avoid re-deriving preliminary analysis.

The handoff model follows HandoffModel, falling back to the fast model
and then the default model (see TheoryOfHandoffModel). The handoff prompt
instructs the model to produce a concise, plain-text summary without block
wrapping. Handoff generation is retried up to maxHandoffRetries times on
failure or empty response; a persistent failure is logged and the caller
retries with empty handoff content, so the run continues rather than
aborting.

Handoff generation streams to the HandoffWriter provider when one is
configured: the display writer receives the model's text and reasoning
thoughts as they are produced, so a TUI can show the handoff request in
progress. The captured handoff text is read from an inner buffer that
excludes thoughts, so the returned summary contains only the model's
final text. The HandoffObserver provider reports the handoff lifecycle —
HandoffStart before the first attempt and HandoffEnd after the last — so
a TUI can reflect the handoff state in its output tab title.

The fixed instructional prompt (HandoffSystemPrompt) is placed in the
system prompt; the user content carries only the dynamic incomplete
output. The same split applies to thought summarization.
`

// Handoff holds the outcome of summarizing interrupted or truncated output
// for a self-contained handoff to the next round. See TheoryOfHandoff.
type Handoff struct {
	// Summary is the summary of the truncated output, recorded in round statistics.
	Summary string
	// Prompt is the self-contained summary fed to the retry round as user input.
	Prompt string
}

// HandoffWriter receives the streamed output of a handoff generation
// request. The default provider returns nil, in which case the handoff
// output is not displayed. A display front-end (e.g., tai's TUI) forks
// this type to stream the handoff request's text and reasoning thoughts
// to its own display. See TheoryOfHandoff.
type HandoffWriter io.Writer

func (Module) HandoffWriter() HandoffWriter {
	return nil
}

// HandoffObserver reports the lifecycle of a handoff generation request.
// HandoffStart is called before the first attempt and HandoffEnd after
// the last attempt (success or failure). A display front-end (e.g., tai's
// TUI) implements this interface to reflect the handoff state in its
// output tab title. See TheoryOfHandoff.
type HandoffObserver interface {
	HandoffStart()
	HandoffEnd()
}

func (Module) HandoffObserver() HandoffObserver {
	return nil
}

// minHandoffLength is the minimum number of characters of incomplete output
// required to warrant a handoff summary. Output shorter than this is retried
// directly without handoff. See TheoryOfHandoff.
const minHandoffLength = 100

type HandoffRecorder interface {
	Enabled() bool
	SystemPrompt(prompt string)
	Content(content *generators.Content)
	Event(typ string, detail string)
}

const HandoffSystemPrompt = `You are a handoff assistant. The previous model generation was interrupted or truncated before completion. Because every change is applied atomically, nothing in the interrupted output was completed: the attempt's output was discarded and nothing it produced was applied, so there is no completed work, no remaining work, and no next step to carry forward. Claims about what was done or what remains are hallucinations — the output process failed, so nothing took effect.

Your task is to extract the valuable thinking from the interrupted output — the discoveries, insights, analysis, and decisions about the problem — and condense them into a concise, self-contained handoff that improves the next generation round. The handoff's value is in the reasoning it preserves, not in any status report: it is reference material for the next round, which must still think for itself and decide how to proceed.

CRITICAL CONSTRAINTS:
- Do NOT assume the next generation round can see the previous truncated output or its reasoning. The previous raw output is DISCARDED and will NOT appear in conversation history. Your handoff notes are the ONLY information passed forward.
- Everything necessary to continue must be SELF-CONTAINED in your summary.
- Do NOT report completed work, remaining work, or next steps: nothing was completed, and such status claims are hallucinations with no value.
- The handoff is reference material, not a substitute for thinking: the next round must still reason about the problem and apply its own judgment. State the established insights and conclusions clearly so the model does not repeat lengthy preliminary analysis or re-derive conclusions, but do not imply that it can act without thinking.

Prioritize:
- Important discoveries and insights established about the codebase or problem
- Key decisions and conclusions about the problem and approach
- Analysis that would otherwise be re-derived from scratch
- How the thinking should be improved or redirected in the next attempt

Output ONLY the concise handoff summary as plain text, with no preamble or extra commentary.`

// maxHandoffRetries bounds the number of attempts to generate a handoff summary
// when generation fails or produces an empty response. See TheoryOfHandoff.
const maxHandoffRetries = 3

func CreateHandoff(
	ctx context.Context,
	logger logs.Logger,
	recorder HandoffRecorder,
	generator generators.Generator,
	incompleteText string,
	writer io.Writer,
	observer HandoffObserver,
) (*Handoff, error) {
	if len(strings.TrimSpace(incompleteText)) < minHandoffLength {
		return nil, nil
	}

	if observer != nil {
		observer.HandoffStart()
		defer observer.HandoffEnd()
	}

	// recordContent and recordEvent write the handoff attempt's details
	// into the same session's interaction record.
	recordContent := func(content *generators.Content) {
		if recorder != nil && recorder.Enabled() {
			recorder.Content(content)
		}
	}
	recordEvent := func(format string, args ...any) {
		if recorder != nil && recorder.Enabled() {
			recorder.Event("decision", fmt.Sprintf(format, args...))
		}
	}
	recordSystemPrompt := func(prompt string) {
		if recorder != nil && recorder.Enabled() {
			recorder.SystemPrompt(prompt)
		}
	}

	// The fixed instructional prompt (HandoffSystemPrompt) is recorded as
	// the system prompt; the user content carries only the dynamic
	// incomplete output, so the transcript mirrors the actual generation.
	// See TheoryOfHandoff.
	recordSystemPrompt(HandoffSystemPrompt)

	var lastErr error
	for attempt := range maxHandoffRetries {
		recordContent(&generators.Content{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text(incompleteText),
			},
		})

		outputText, thoughts, err := runHandoffAttempt(ctx, generator, incompleteText, writer)
		if err != nil {
			lastErr = err
			logger.WarnContext(ctx, "handoff incomplete output: generation failed",
				"attempt", attempt+1,
				"max_attempts", maxHandoffRetries,
				"err", err,
			)
			recordContent(&generators.Content{
				Role: generators.RoleLog,
				Parts: []generators.Part{
					generators.Error{Error: err},
				},
			})
			recordEvent("handoff attempt %d/%d failed: generation error: %v",
				attempt+1, maxHandoffRetries, err)
			continue
		}

		recordContent(&generators.Content{
			Role: generators.RoleModel,
			Parts: []generators.Part{
				generators.Text(handoffResponseDetail(attempt+1, outputText, thoughts)),
			},
		})

		text := strings.TrimSpace(outputText)
		if text != "" {
			return &Handoff{
				Summary: text,
				Prompt:  text,
			}, nil
		}
		lastErr = fmt.Errorf("handoff response is empty")
		logger.WarnContext(ctx, "handoff incomplete output: response is empty",
			"attempt", attempt+1,
			"max_attempts", maxHandoffRetries,
		)
		recordEvent("handoff attempt %d/%d failed: response is empty",
			attempt+1, maxHandoffRetries)
	}
	if lastErr != nil {
		err := fmt.Errorf("handoff incomplete output failed after %d attempts: %w", maxHandoffRetries, lastErr)
		logger.ErrorContext(ctx, "handoff incomplete output failed",
			"max_attempts", maxHandoffRetries,
			"err", err,
		)
		recordEvent("handoff incomplete output failed after %d attempts: %v", maxHandoffRetries, lastErr)
	}
	// A persistent handoff failure is not fatal: the caller retries with
	// empty handoff content, so the run continues. The failure is logged
	// and recorded above for visibility. See TheoryOfHandoff.
	return nil, nil
}

func runHandoffAttempt(
	ctx context.Context,
	generator generators.Generator,
	incompleteText string,
	writer io.Writer,
) (string, []string, error) {
	var state generators.State
	state = generators.NewPrompts(HandoffSystemPrompt, []*generators.Content{
		{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text(incompleteText),
			},
		},
	})
	var buf bytes.Buffer
	state = generators.NewOutput(state, &buf, false)
	if writer != nil {
		state = generators.NewOutput(state, writer, true)
	}
	newState, err := generator.Generate(ctx, state, &generators.GenerateOptions{})
	if err != nil {
		return "", nil, err
	}
	return buf.String(), extractModelThoughts(newState), nil
}

// extractModelThoughts collects the Thought parts of the model contents
// produced by one summarize attempt. The Output capture buffer excludes
// thoughts (showThoughts=false), so the recorded response needs them
// appended separately; when a response fails to parse, the thoughts are
// often the only signal of what the model actually produced.
// See TheoryOfIncompleteOutputSummarization.
func extractModelThoughts(state generators.State) []string {
	if state == nil {
		return nil
	}
	var thoughts []string
	for content := range state.Contents() {
		if content.Role != generators.RoleModel && content.Role != generators.RoleAssistant {
			continue
		}
		for _, part := range content.Parts {
			if thought, ok := part.(generators.Thought); ok && len(thought) > 0 {
				thoughts = append(thoughts, string(thought))
			}
		}
	}
	return thoughts
}

// handoffResponseDetail renders the recorded detail of one handoff attempt.
func handoffResponseDetail(attempt int, outputText string, thoughts []string) string {
	var detail strings.Builder
	fmt.Fprintf(&detail, "Handoff response (attempt %d):\n\n%s", attempt, outputText)
	for _, thought := range thoughts {
		detail.WriteString("\n[thought]\n")
		detail.WriteString(thought)
	}
	return detail.String()
}

// FormatSummaryBlock wraps a summary in a boundary-delimited summary
// block with a fresh delimiter, so the TUI's Round tab can display it
// as the round's completion signal.
func FormatSummaryBlock(summary string) string {
	delimiter := freshDelimiter()
	return "<<" + delimiter + " <summary>\n" + summary + "\n" + delimiter
}

// FormatHandoffPrompt formats the retry user prompt with the handoff content.
// See TheoryOfHandoff.
func FormatHandoffPrompt(prefix, handoffPrompt string) string {
	return prefix + handoffPrompt
}

// freshDelimiter returns a fresh pair of uncommon Chinese characters for
// use as a block delimiter in system-generated blocks. The delimiter is
// chosen randomly from a set of uncommon Chinese characters so it is
// unlikely to appear in the block body.
func freshDelimiter() string {
	const uncommonChars = "龘靐齉爩麤黿鼍爨灪虋齾齑靁齌齍齎齏爞齔齕"
	chars := []rune(uncommonChars)
	return string([]rune{
		chars[rand.IntN(len(chars))],
		chars[rand.IntN(len(chars))],
	})
}
