package states

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfHandoff = `
When a generation round is truncated (no summary block) or errors after
producing partial output, a handoff summary is constructed before retrying.
Truncation often occurs because the model attempted too many changes at
once, exceeding output length limits. The handoff condenses the valuable
thinking from the interrupted output — discoveries, insights, analysis,
decisions about the problem, and specific attempted code modifications —
into a single self-contained text.

All changes are atomic: a truncated or failed round applies nothing to disk,
so there is no completed work on disk and claims that changes took effect are
hallucinations. However, summarizing what changes were being attempted and
evaluating whether they are sound is crucial: it informs the next round so
it can complete a manageable initial subset of those changes first, using a
continue block to partition the remaining work across subsequent rounds
instead of repeatedly attempting an oversized emission that triggers
truncation again.

The handoff's value is in the reasoning and structural plan it preserves:
it guides the direction of the next generation round, prevents repeating
preliminary analysis, and directs task partitioning. The handoff is
reference material, not a substitute for thinking: the next round must
still reason about the problem and decide how to partition its work.

The handoff model is a single model specified by HandoffModels. When empty,
the fast model (FastModelName) is used if configured; otherwise the default
model (ModelName) is used. See TheoryOfHandoffModel. The handoff
prompt instructs the model to wrap the concise handoff summary in a
boundary-delimited block with kind "handoff". The system parses the block
body as the handoff content; if the model does not emit a valid handoff
block (missing, malformed, or unclosed), the response is treated as empty
and retried, preventing incorrect or incomplete content from being used as
handoff instructions. Handoff generation is retried up to maxHandoffRetries
times on failure or missing block; a persistent failure is logged and the
caller retries with empty handoff content, so the run continues rather than
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

const HandoffSystemPrompt = `You are a handoff assistant. The previous model generation was interrupted or truncated before completion. Truncation often happens because too many changes were attempted at once, exceeding the model's output limit. Because changes are applied atomically, nothing in the interrupted output was applied to disk: the attempt's output was discarded and nothing was completed, so there is no completed work on disk. Claims that changes took effect are hallucinations — the output process failed, so nothing took effect.

Your task is to extract the valuable thinking from the interrupted output — discoveries, insights, analysis, decisions, and attempted code modifications — and condense them into a concise, self-contained handoff that guides the next generation round to partition its work effectively.

CRITICAL CONSTRAINTS:
- Do NOT assume the next generation round can see the previous truncated output or its reasoning. The previous raw output is DISCARDED and will NOT appear in conversation history. Your handoff notes are the ONLY information passed forward.
- Everything necessary to continue must be SELF-CONTAINED in your summary.
- Acknowledge that changes were not applied to disk (atomic rollback), but identify what code changes were attempted or generated so the next round knows what modifications were underway.
- Guide task partitioning: if the previous round was truncated due to output volume or attempting too many changes, explicitly identify what changes were attempted, evaluate whether they are sound, and advise the next round to complete an initial manageable subset of those changes first and use a continue block to partition the remaining work across subsequent rounds, rather than repeatedly emitting an oversized response that triggers truncation again.
- The handoff is reference material, not a substitute for thinking: the next round must still think for itself, reason about the problem, and apply its own judgment. State established insights and conclusions clearly so the model does not repeat lengthy preliminary analysis or re-derive conclusions.

Prioritize:
- Important discoveries and insights established about the codebase or problem
- Key decisions and conclusions about the problem and approach
- Specific code modifications and change blocks that were being produced or attempted
- Guidance on task partitioning: which changes to complete first in the upcoming round and how to use continue blocks for remaining work to avoid output truncation

Wrap the concise handoff summary in a boundary-delimited block with kind "handoff". The block body must contain ONLY the handoff summary text. Do not output any prose before or after the block. If you fail to emit a valid, properly closed handoff block, the system will treat the response as empty and retry, so ensure the block is well-formed with a matched opening marker and closing line.`

// maxHandoffRetries bounds the number of attempts to generate a handoff summary
// when generation fails or produces an empty response. See TheoryOfHandoff.
const maxHandoffRetries = 3

// fullHandoffSystemPrompt combines the handoff-specific instructions with the
// unified block format prompt so the model knows both what to produce (a
// handoff block) and how to format it (the heredoc-delimited block format).
// The block format prompt is appended after the handoff instructions so the
// handoff guidance stays in the stable prefix and the format rules follow.
// See TheoryOfHandoff.
func fullHandoffSystemPrompt() string {
	return HandoffSystemPrompt + "\n\n" + blocks.BlockFormatSystemPrompt
}

// parseHandoffBlock extracts the body of a handoff block from the model's
// output. It parses the boundary-delimited blocks and returns the first
// block whose kind is "handoff". If no valid handoff block is found —
// either because the model did not emit a block, the block was malformed
// or unclosed, or the body is empty — the function returns false, and
// the caller treats the response as empty, triggering a retry. This
// prevents using incorrect or incomplete handoff content as if it were
// valid, which would feed wrong instructions to the next generation
// round. See TheoryOfHandoff.
func parseHandoffBlock(text string) (string, bool) {
	parsedBlocks, err := blocks.ParseBlocks([]byte(text))
	if err != nil {
		return "", false
	}
	for _, block := range parsedBlocks {
		if block.Kind == "handoff" {
			body := strings.TrimSpace(block.Body)
			if body != "" {
				return body, true
			}
		}
	}
	return "", false
}

func CreateHandoff(
	ctx context.Context,
	logger logs.Logger,
	recorder HandoffRecorder,
	handoffGenerators []generators.Generator,
	incompleteText string,
	writer io.Writer,
	observer HandoffObserver,
) (*Handoff, error) {
	if len(strings.TrimSpace(incompleteText)) < minHandoffLength {
		return nil, nil
	}

	// An empty handoff generator list cannot produce a summary; return
	// nil so the caller retries without handoff content rather than
	// panicking on a modulo-by-zero. In normal operation the provider
	// always resolves at least one generator (the default model), so this
	// guards only direct calls with an empty slice (e.g., tests).
	if len(handoffGenerators) == 0 {
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

	// The fixed instructional prompt (HandoffSystemPrompt) combined with
	// the unified block format prompt is recorded as the system prompt;
	// the user content carries only the dynamic incomplete output, so
	// the transcript mirrors the actual generation.
	// See TheoryOfHandoff.
	recordSystemPrompt(fullHandoffSystemPrompt())

	var lastErr error
	for attempt := range maxHandoffRetries {
		// The handoff model is a single model; the generator slice has
		// one element. The modulo is retained for safety but always
		// resolves to index 0.
		generator := handoffGenerators[attempt%len(handoffGenerators)]

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
				"model", generator.Spec().Model,
				"err", err,
			)
			recordContent(&generators.Content{
				Role: generators.RoleLog,
				Parts: []generators.Part{
					generators.Error{Error: err},
				},
			})
			recordEvent("handoff attempt %d/%d failed: model=%s generation error: %v",
				attempt+1, maxHandoffRetries, generator.Spec().Model, err)
			continue
		}

		recordContent(&generators.Content{
			Role: generators.RoleModel,
			Parts: []generators.Part{
				generators.Text(handoffResponseDetail(attempt+1, outputText, thoughts)),
			},
		})

		// Parse the handoff block from the model's output. The model is
		// instructed to wrap the handoff summary in a boundary-delimited
		// block with kind "handoff". If no valid block is found (missing,
		// malformed, or unclosed), the response is treated as empty and
		// retried, preventing incorrect or incomplete content from being
		// used as handoff instructions. See TheoryOfHandoff.
		handoffText, ok := parseHandoffBlock(outputText)
		if ok {
			return &Handoff{
				Summary: handoffText,
				Prompt:  handoffText,
			}, nil
		}
		lastErr = fmt.Errorf("no valid handoff block found in response")
		logger.WarnContext(ctx, "handoff incomplete output: no valid handoff block found",
			"attempt", attempt+1,
			"max_attempts", maxHandoffRetries,
			"model", generator.Spec().Model,
		)
		recordEvent("handoff attempt %d/%d failed: model=%s no valid handoff block found",
			attempt+1, maxHandoffRetries, generator.Spec().Model)
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
	state = generators.NewPrompts(fullHandoffSystemPrompt(), []*generators.Content{
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
	return "<<" + delimiter + " summary\n" + summary + "\n" + delimiter
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
