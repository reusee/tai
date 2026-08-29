package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfHandoff = `
When a generation attempt is truncated (no summary block) or errors after
producing partial output, a handoff summary is constructed before retrying.
Truncation often occurs because the model attempted too many changes at
once, exceeding output length limits. HandoffSystemPrompt is itself the
theory text for what the handoff must carry — the preserved thinking, the
atomicity acknowledgment, and the task-partitioning guidance — and it is
not repeated here.

All changes are atomic: a truncated or failed attempt applies nothing to
disk, so there is no completed work on disk and claims that changes took
effect are hallucinations. The handoff is therefore transient error
recovery, not history: it is injected into one retry request and never
persists as compressed context (see TheoryOfContextPhilosophy).

The handoff model is a single model specified by HandoffModel. When empty,
the fast model (FastModelName) is used if configured; otherwise the default
model (ModelName) is used. See TheoryOfHandoffModel. The handoff prompt
instructs the model to wrap the concise handoff summary in a
boundary-delimited block with kind "handoff". The system parses the block
body as the handoff content; if the model does not emit a valid handoff
block (missing, malformed, or unclosed), the response is treated as empty
and retried, preventing incorrect or incomplete content from being used as
handoff instructions. Handoff generation retries without an attempt limit
on failure or missing block; the loop exits only when a valid handoff block
is produced or the context is cancelled. Cancellation is logged and the
caller retries with empty handoff content, so the run continues rather than
aborting; a caller that wants a bound must supply a cancellable context.

The loop's event stream reports the handoff lifecycle as it happens:
EventHandoffStart is emitted immediately before the handoff request is
sent, and EventHandoff after the summary is produced, so a live consumer
sees the request in progress rather than waiting for its result. The
events carry the attempt attribution but no retry-budget figures:
handoff generation itself retries without an attempt limit, so a budget
display such as "attempt x/y" would misrepresent it. Handoff
generation also applies the HandoffStateDecorator provider to its state
when one is configured: the decorator observes every content part as it
is appended, so a display front-end receives the model's text and
reasoning thoughts carrying their roles and thinking state, and can
highlight the handoff request per part and per thought. The captured
handoff text is read from an inner buffer that excludes thoughts, so the
returned summary contains only the model's final text. The
HandoffObserver provider reports the handoff lifecycle — HandoffStart
before the first attempt and HandoffEnd after the last — so a TUI can
reflect the handoff state in its output tab title.

The fixed instructional prompt (HandoffSystemPrompt) is placed in the
system prompt; the user content carries only the dynamic incomplete
output. The same split applies to thought summarization.
`

// TheoryOfHandoffUsageAccounting explains how the handoff request's own
// token usage reaches attempt statistics; the constant body carries the
// mechanism and its boundaries. See also TheoryOfAttemptStatistics and
// TheoryOfUsageLogging.
const TheoryOfHandoffUsageAccounting = `Handoff usage accounting: the handoff summarizer runs on throwaway
per-attempt states, so its Usage parts never reach the collectors,
which scan only the main generation state. The spend travels with the
delivered Handoff value, accumulated across all generating attempts,
and the loop injects one RoleLog usage content before the attempt's
statistics are recorded, carrying the failed attempt's own last usage
plus the handoff usage. The injection window starts at the attempt's
pre-generation base (attemptBase), so a prior attempt's usage —
retained in the state on error retries — is never re-attributed, and
the retry base predates the injection, so the next attempt never
rescans it; last-Usage collection and RoleLog invisibility to the model
keep the accounting exact. Only a produced Handoff accounts usage: a
handoff failing on every attempt leaves its spend outside the
statistics.`

// Handoff holds the outcome of summarizing interrupted or truncated output
// for a self-contained handoff to the next generation. See TheoryOfHandoff.
type Handoff struct {
	// Summary is the summary of the truncated output, recorded in
	// attempt statistics.
	Summary string
	// Prompt is the self-contained summary fed to the retry attempt as
	// user input.
	Prompt string
	// Usage is the token usage of the handoff generation itself, summed
	// across all of its attempts. Callers inject it into the generation
	// state so attempt statistics include the handoff's spend.
	// See TheoryOfHandoffUsageAccounting.
	Usage generators.Usage
}

// HandoffStateDecorator wraps the handoff generation's state before
// generation starts. The default provider is nil, in which case the
// handoff output is not displayed. A display front-end (e.g., tai's
// TUI) forks this type to observe the handoff request's contents, so
// every part reaches the display carrying its role and thinking state
// and part boundaries are preserved. See TheoryOfHandoff.
type HandoffStateDecorator func(generators.State) generators.State

func (Module) HandoffStateDecorator() HandoffStateDecorator {
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

BLOCK FORMAT REQUIREMENT (CRITICAL):
- Wrap the handoff summary in a boundary-delimited block. The block kind is "handoff": a bare function name written immediately after the delimiter in the opening header line, with no parentheses and no parameters. The kind is a function name, never a named parameter.
- The opening header line consists of exactly two words: your chosen two-character delimiter, then the word handoff.
- The block body must contain ONLY the handoff summary text.
- Do not output any prose before or after the block.
- If you fail to emit a valid, properly closed handoff block, the system will treat the response as empty and retry, so ensure the block is well-formed with a matched opening marker and closing line.`

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
// valid, which would feed wrong instructions to the next generation.
// See TheoryOfHandoff.
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

// createHandoff is the unexported package-level implementation behind the
// CreateHandoff dscope function type: it summarizes truncated or failed
// generation output into a self-contained handoff carried into the next
// generation. The implementation stays a plain function so tests can call
// it directly. See TheoryOfHandoff and TheoryOfDscopeBoundFunctions.
func createHandoff(
	ctx context.Context,
	logger logs.Logger,
	recorder HandoffRecorder,
	handoffGenerators []generators.Generator,
	incompleteText string,
	decorator HandoffStateDecorator,
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
	// totalUsage accumulates the token usage of every generating attempt,
	// including attempts whose response lacked a valid handoff block, so
	// the delivered Handoff reports the full spend of producing it.
	// See TheoryOfHandoffUsageAccounting.
	var totalUsage generators.Usage
	// attempts counts the started attempts; after the loop it is the
	// completed-attempt count reported by the abort log and event.
	attempts := 0
	// Retries are unbounded: the loop exits only when a valid handoff
	// block is produced or the context is cancelled, never on an
	// attempt count. See TheoryOfHandoff.
	for ctx.Err() == nil {
		attempt := attempts
		attempts++

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

		outputText, thoughts, attemptUsage, err := runHandoffAttempt(ctx, generator, incompleteText, decorator)
		if err != nil {
			lastErr = err
			logger.WarnContext(ctx, "handoff incomplete output: generation failed",
				"attempt", attempt+1,
				"model", generator.Spec().Model,
				"err", err,
			)
			recordContent(&generators.Content{
				Role: generators.RoleLog,
				Parts: []generators.Part{
					generators.Error{Error: err},
				},
			})
			recordEvent("handoff attempt %d failed: model=%s generation error: %v",
				attempt+1, generator.Spec().Model, err)
			continue
		}
		totalUsage.Prompt.TokenCount += attemptUsage.Prompt.TokenCount
		totalUsage.Prompt.TokenCountCached += attemptUsage.Prompt.TokenCountCached
		totalUsage.Candidates.TokenCount += attemptUsage.Candidates.TokenCount
		totalUsage.Thoughts.TokenCount += attemptUsage.Thoughts.TokenCount

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
				Usage:   totalUsage,
			}, nil
		}
		lastErr = fmt.Errorf("no valid handoff block found in response")
		logger.WarnContext(ctx, "handoff incomplete output: no valid handoff block found",
			"attempt", attempt+1,
			"model", generator.Spec().Model,
		)
		recordEvent("handoff attempt %d failed: model=%s no valid handoff block found",
			attempt+1, generator.Spec().Model)
	}
	if lastErr != nil {
		err := fmt.Errorf("handoff incomplete output aborted after %d attempts: %w", attempts, lastErr)
		logger.ErrorContext(ctx, "handoff incomplete output aborted",
			"attempts", attempts,
			"err", err,
		)
		recordEvent("handoff incomplete output aborted after %d attempts: %v", attempts, lastErr)
	}
	// A cancelled handoff is not fatal: the caller retries with empty
	// handoff content, so the run continues rather than aborting. The
	// cancellation is logged and recorded above for visibility. See
	// TheoryOfHandoff.
	return nil, nil
}

func runHandoffAttempt(
	ctx context.Context,
	generator generators.Generator,
	incompleteText string,
	decorator HandoffStateDecorator,
) (string, []string, generators.Usage, error) {
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
	// The decorator observes the state chain so a display front-end
	// receives every appended part with its role and thinking state,
	// instead of a byte stream that loses part boundaries. See
	// TheoryOfHandoff.
	if decorator != nil {
		state = decorator(state)
	}
	newState, err := generator.Generate(ctx, state, &generators.GenerateOptions{})
	if err != nil {
		return "", nil, generators.Usage{}, err
	}
	// The attempt's final usage is read from the throwaway state so the
	// spend can travel with the delivered Handoff. See
	// TheoryOfHandoffUsageAccounting.
	return buf.String(), extractModelThoughts(newState), extractLastUsage(newState, 0), nil
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

// extractLastUsage returns the last Usage part found in the state's
// contents at or after sinceContentCount, or the zero value when none
// exists. Generators append the final usage of a generation as a Usage
// part, so scanning the generated state yields the spend of that
// generation. Used to carry the handoff attempt's usage out of its
// throwaway state and to verify the injected sums in tests. See
// TheoryOfHandoffUsageAccounting.
func extractLastUsage(state generators.State, sinceContentCount int) generators.Usage {
	if state == nil {
		return generators.Usage{}
	}
	var usage generators.Usage
	i := 0
	for c := range state.Contents() {
		if i >= sinceContentCount {
			for _, p := range c.Parts {
				if u, ok := p.(generators.Usage); ok {
					usage = u
				}
			}
		}
		i++
	}
	return usage
}

// appendHandoffUsage appends one RoleLog content carrying the attempt's
// token usage including the handoff request's spend: the last usage
// already recorded since sinceContentCount (the generating attempt's
// final usage) plus handoffUsage, accumulated across the handoff's
// attempts. The statistics collectors take the last Usage part of the
// scanned window, so the sum becomes the attempt's accounted usage;
// RoleLog content is filtered out of API message assembly, so the
// injection is invisible to the model. States are immutable and the
// retry base predates the injection, so the following attempt's scan
// window never double counts it. An append failure skips the
// injection: usage accounting is best effort and must not fail the
// recovery path. A fully zero sum adds no information and is skipped,
// so an empty handoff usage cannot shadow a real one with zeros.
// See TheoryOfHandoffUsageAccounting.
func appendHandoffUsage(
	state generators.State,
	sinceContentCount int,
	handoffUsage generators.Usage,
) generators.State {
	total := extractLastUsage(state, sinceContentCount)
	total.Prompt.TokenCount += handoffUsage.Prompt.TokenCount
	total.Prompt.TokenCountCached += handoffUsage.Prompt.TokenCountCached
	total.Candidates.TokenCount += handoffUsage.Candidates.TokenCount
	total.Thoughts.TokenCount += handoffUsage.Thoughts.TokenCount
	if total.Prompt.TokenCount == 0 &&
		total.Prompt.TokenCountCached == 0 &&
		total.Candidates.TokenCount == 0 &&
		total.Thoughts.TokenCount == 0 {
		return state
	}
	newState, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleLog,
		Parts: []generators.Part{total},
	})
	if err != nil {
		return state
	}
	return newState
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
// block with a fresh delimiter, so the TUI's Events tab can display it
// as the attempt's completion signal.
func FormatSummaryBlock(summary string) string {
	delimiter := freshDelimiter()
	return "<<" + delimiter + " summary\n" + summary + "\n" + delimiter
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
