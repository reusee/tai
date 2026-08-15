package states

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfHandoff = `
When a generation round is truncated (no summary block) or errors after
producing partial output, a handoff summary is constructed before retrying.
The handoff condenses valuable conclusions from the interrupted thinking
(discoveries, decisions, established facts, completed work, and next steps)
into a single self-contained text.

The handoff must not assume the next round can see the interrupted output,
as the raw partial output is discarded and will not appear in conversation
history. The handoff notes must therefore be completely self-contained.
The handoff focuses on guiding the direction of the next generation round
and mitigating the model's tendency to overthink: by providing clear
settled conclusions and concrete next steps, the next round can proceed
directly to action rather than re-deriving preliminary analysis.

Handoff is executed only when the model has produced a non-trivial amount
of output (at least minHandoffLength characters). If output is very short or
empty, handoff is skipped and a direct retry is performed.

The handoff model follows HandoffModel, falling back to the fast model
and then the default model (see TheoryOfHandoffModel). The handoff prompt
instructs the model to produce a concise, plain-text summary without block
wrapping. Handoff generation is retried up to maxHandoffRetries times on
failure or empty response; a persistent failure aborts the run to ensure
the failure is visible.

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

const HandoffSystemPrompt = `You are a handoff assistant. The previous model generation was interrupted or truncated before completion. Your task is to produce a concise, self-contained handoff summary that guides the next generation round to take over and complete the task efficiently.

CRITICAL CONSTRAINTS:
- Do NOT assume the next generation round can see the previous truncated output or its reasoning. The previous raw output is DISCARDED and will NOT appear in conversation history. Your handoff notes are the ONLY information passed forward.
- Everything necessary to continue must be SELF-CONTAINED in your summary.
- Guide the next round to ACT DIRECTLY and avoid overthinking. State established facts, decisions, and next steps clearly so the model does not repeat lengthy preliminary analysis or re-derive conclusions.

Prioritize:
- Important discoveries and insights established about the codebase or problem
- Key decisions made
- State of work: what was completed and what remains
- Actionable next steps: concrete, direct instructions for immediate execution

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
) (*Handoff, error) {
	if len(strings.TrimSpace(incompleteText)) < minHandoffLength {
		return nil, nil
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

		outputText, thoughts, err := runHandoffAttempt(ctx, generator, incompleteText)
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
		return nil, err
	}
	return nil, fmt.Errorf("handoff incomplete output failed after %d attempts", maxHandoffRetries)
}

// runHandoffAttempt executes one handoff generation attempt.
func runHandoffAttempt(
	ctx context.Context,
	generator generators.Generator,
	incompleteText string,
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
	newState, err := generator.Generate(ctx, state, &generators.GenerateOptions{
		NonStreaming: true,
	})
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
