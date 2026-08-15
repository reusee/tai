package states

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

const TheoryOfIncompleteOutputSummarization = `
When a round is truncated (no summary block) or errors after producing partial
output, the incomplete output is summarized before retrying. The retry process has
two tasks: producing a summary of the truncated output (recorded as the truncated
round's summary in round statistics) and producing the content fed to the retry
round as user input, framed as a continue block. The summary provides context for
the retry; the continue block carries what the retry round should adopt.

Truncation most often happens when the model thinks too long. The truncated
reasoning is not wasted: it has already produced valuable results — discoveries,
decisions, and facts. Discarding these results and letting the retry round
re-derive them from scratch would spend the thinking budget a second time, risking
the same truncation. The retry summarization therefore extracts the thinking
results from the truncated output — the conclusions, not the reasoning that led to
them — and carries them into the continue block fed to the retry round. The
extraction prioritizes the most valuable content: important discoveries and
insights, important decisions, important facts about the codebase or task, the
state of completed work, and the next steps the model was about to take. The retry
round adopts these pre-established conclusions and continues from where the model
left off, so it needs less thinking than the truncated attempt.

The same extraction serves both retry paths: missing-completion retries (truncated
output) and error retries (partial output followed by an error). See
TheoryOfSummaryCompletionRetry and TheoryOfSummaryRetryOnError.

The summarization model follows SummarizeModel, falling back to the fast model
and then the default model (see states.TheoryOfSummarizeModel). The summary is
appended as user content with a system note explaining the retry.

The summarization system prompt (RetrySummarizationSystemPrompt) shows a complete
example of the summary and continue block format with concrete delimiters, so
the model emits parseable blocks rather than plain text.

The summarization itself is retried when its response cannot be parsed into
summary and continue blocks, or when the summarize generation fails: a malformed
or incomplete summarize response would otherwise leave the retry round without a
summary or with a degraded retry prompt, and a transient API error would leave
the round without any summary at all. The retry is bounded by maxSummarizeRetries;
when all attempts fail, the summarization returns an error instead of falling
back to the incomplete text. A fallback that substitutes the raw incomplete text
as both the summary and the retry prompt would feed the model unstructured,
possibly truncated reasoning as if it were a distilled summary, degrading the
retry prompt's quality and masking the summarization failure. The summarization
failure is a serious error, not a soft "no summary available" condition: it
propagates as a generation error and aborts the run. Continuing without a
synthesized summary would hide the truncation, leave the retry round without the
distilled conclusions it needs, and degrade the retry prompt's quality; the
operator must see the failure and intervene.

Each failed summarize attempt is logged with the attempt number and the error,
and the final failure is logged as an error, so the operator can diagnose why a
round aborted. Every attempt is also written into the same session's interaction
record: the request input and the raw model response — including the model's
thoughts, which the parse buffer excludes — are recorded as contents, and each
attempt's failure reason (generation error, parse error, or a missing
summary/continue block, with which blocks were found) plus the final failure are
recorded as decision events. When a response cannot be parsed into the two
blocks, the record therefore shows exactly what the summarize model produced,
so the summarization prompt can be optimized from real failures.
`

// RetrySummary holds the outcome of summarizing truncated output for retry.
// The retry process has two tasks: producing a summary of the truncated output
// (recorded as the truncated round's summary in round statistics) and producing
// the compressed content fed to the retry round as user input, framed as a
// continue block. See TheoryOfIncompleteOutputSummarization.
type RetrySummary struct {
	// Summary is the summary of the truncated output, used as the
	// truncated round's summary in round statistics.
	Summary string
	// RetryPrompt is the compressed version of the truncated output,
	// fed to the retry round as user input.
	RetryPrompt string
}

// SummarizeRecorder is the minimal recorder interface used by
// SummarizeIncomplete. The records package's Recorder implements it.
type SummarizeRecorder interface {
	Enabled() bool
	Content(content *generators.Content)
	Event(typ string, detail string)
}

// RetrySummarizationSystemPrompt is the system prompt for the retry
// summarization model. It shows a complete example of the summary and
// continue block format with concrete delimiters, so the model emits
// parseable blocks rather than plain text.
const RetrySummarizationSystemPrompt = `You are a summarization assistant. The previous model output was truncated before completion. Produce exactly two blocks, and ONLY these two blocks:

1. A summary block (kind "summary") whose body is a concise summary of the truncated output: what the model was doing, what it had produced, and where it was interrupted.

2. A continue block (kind "continue") whose body is the retry prompt: the essence of the truncated output that the next round needs to continue from where the model left off. Truncation most often happens when thinking is too long, and the truncated thinking has already produced valuable results. The retry prompt must carry these results over — the conclusions, not the reasoning that led to them — so the next round adopts them instead of re-deriving them and needs less thinking.

Both blocks MUST have non-empty bodies. The continue block body MUST carry the valuable conclusions from the truncated thinking whenever any exist — discoveries, decisions, facts, completed work, next steps.

Prioritize the following valuable content in the retry prompt:
- Important discoveries and insights the model reached
- Important decisions the model made
- Important facts the model established about the codebase, the task, or the environment
- The state of the work: what was completed and what remains
- The next steps the model was about to take

` + blocks.BlockFormatSystemPrompt + `

Output ONLY these two blocks as your final text, with no other text before or after them.`

// maxSummarizeRetries bounds the number of attempts to summarize
// incomplete output when the summarize response cannot be parsed into
// summary and continue blocks. When all attempts fail, the summarization
// returns an error instead of falling back to the incomplete text. See
// TheoryOfIncompleteOutputSummarization.
const maxSummarizeRetries = 3

// SummarizeIncomplete summarizes truncated or failed generation output
// before retry. Each attempt's request input, raw model response
// (including thoughts), and failure reason are written into the same
// session's interaction record, so an unparseable response is diagnosable
// from the transcript alone. See TheoryOfIncompleteOutputSummarization.
func SummarizeIncomplete(
	ctx context.Context,
	logger logs.Logger,
	recorder SummarizeRecorder,
	generator generators.Generator,
	incompleteText string,
) (*RetrySummary, error) {
	if incompleteText == "" {
		return nil, nil
	}

	// recordContent and recordEvent write the summarize attempt's details
	// into the same session's interaction record. The recorder is the one
	// bound to the running session, so the entries land in the round that
	// triggered the summarization.
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

	var lastErr error
	for attempt := range maxSummarizeRetries {
		// Record the summarize request. The input is recorded per attempt so
		// the interaction transcript shows each retry of the summarization.
		recordContent(&generators.Content{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text(fmt.Sprintf("Summarize request (attempt %d):\n\n%s", attempt+1, incompleteText)),
			},
		})

		outputText, thoughts, err := runSummarizeAttempt(ctx, generator, incompleteText)
		if err != nil {
			lastErr = err
			logger.WarnContext(ctx, "summarize incomplete output: generation failed",
				"attempt", attempt+1,
				"max_attempts", maxSummarizeRetries,
				"err", err,
			)
			// Record the failure so the transcript shows why the attempt failed.
			recordContent(&generators.Content{
				Role: generators.RoleLog,
				Parts: []generators.Part{
					generators.Error{Error: err},
				},
			})
			recordEvent("summarize attempt %d/%d failed: generation error: %v",
				attempt+1, maxSummarizeRetries, err)
			continue
		}

		// Record the summarize response. The raw output is recorded before
		// parsing, so even a malformed response is visible in the transcript.
		// The model's thoughts are appended because the capture buffer
		// excludes them; when the response fails to parse they are often the
		// only signal of what the model actually produced.
		recordContent(&generators.Content{
			Role: generators.RoleModel,
			Parts: []generators.Part{
				generators.Text(summarizeResponseDetail(attempt+1, outputText, thoughts)),
			},
		})

		parsedBlocks, err := blocks.ParseBlocks([]byte(outputText))
		if err != nil {
			lastErr = err
			logger.WarnContext(ctx, "summarize incomplete output: parse failed",
				"attempt", attempt+1,
				"max_attempts", maxSummarizeRetries,
				"err", err,
			)
			recordEvent("summarize attempt %d/%d failed: parse error: %v",
				attempt+1, maxSummarizeRetries, err)
			continue
		}
		var summary, continueContent string
		for _, block := range parsedBlocks {
			switch block.Kind {
			case "summary":
				summary = block.Body
			case "continue":
				continueContent = block.Body
			}
		}
		if summary != "" && continueContent != "" {
			return &RetrySummary{
				Summary:     summary,
				RetryPrompt: continueContent,
			}, nil
		}
		lastErr = fmt.Errorf("summarize response missing summary or continue block")
		logger.WarnContext(ctx, "summarize incomplete output: response missing summary or continue block",
			"attempt", attempt+1,
			"max_attempts", maxSummarizeRetries,
		)
		recordEvent("summarize attempt %d/%d failed: response missing summary or continue block (summary block found: %v, continue block found: %v)",
			attempt+1, maxSummarizeRetries, summary != "", continueContent != "")
	}
	if lastErr != nil {
		err := fmt.Errorf("summarize incomplete output failed after %d attempts: %w", maxSummarizeRetries, lastErr)
		logger.ErrorContext(ctx, "summarize incomplete output failed",
			"max_attempts", maxSummarizeRetries,
			"err", err,
		)
		recordEvent("summarize incomplete output failed after %d attempts: %v", maxSummarizeRetries, lastErr)
		return nil, err
	}
	return nil, fmt.Errorf("summarize incomplete output failed after %d attempts", maxSummarizeRetries)
}

// runSummarizeAttempt executes one summarize generation and returns the
// captured output text together with the model's thoughts.
func runSummarizeAttempt(
	ctx context.Context,
	generator generators.Generator,
	incompleteText string,
) (string, []string, error) {
	var state generators.State
	state = generators.NewPrompts(RetrySummarizationSystemPrompt, []*generators.Content{
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

// summarizeResponseDetail renders the recorded detail of one summarize
// attempt: the raw output text followed by the model's thoughts under a
// [thought] marker. The thoughts are appended only when present, so a
// successful plain-text response records exactly as before.
func summarizeResponseDetail(attempt int, outputText string, thoughts []string) string {
	var detail strings.Builder
	fmt.Fprintf(&detail, "Summarize response (attempt %d):\n\n%s", attempt, outputText)
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

// FormatRetryPrompt formats the retry user prompt: the prefix (a system
// note explaining why the retry is happening), the summary block (when a
// summary exists), and the continue block carrying the retry content.
// The summary block carries the synthesized summary so the TUI's Summary
// tab can display it as the truncated or failed round's completion
// signal. See TheoryOfIncompleteOutputSummarization.
func FormatRetryPrompt(prefix, summary, retryPrompt string) string {
	delimiter := freshDelimiter()
	msg := prefix
	if summary != "" {
		msg += FormatSummaryBlock(summary) + "\n\n"
	}
	return msg +
		"<<" + delimiter + " <continue>\n" + retryPrompt + "\n" + delimiter
}

// freshDelimiter returns a fresh trio of uncommon Chinese characters for
// use as a block delimiter in system-generated blocks. The delimiter is
// chosen randomly from a set of uncommon Chinese characters so it is
// unlikely to appear in the block body.
func freshDelimiter() string {
	const uncommonChars = "龘靐齉爩麤黿鼍爨灪虋齾齑靁齌齍齎齏爞齔齕"
	chars := []rune(uncommonChars)
	return string([]rune{
		chars[rand.IntN(len(chars))],
		chars[rand.IntN(len(chars))],
		chars[rand.IntN(len(chars))],
	})
}
