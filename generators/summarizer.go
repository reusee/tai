package generators

import (
	"context"
	"strings"
)

const TheoryOfThoughtsSummarize = `
ThoughtsSummarize is a State layer that periodically condenses accumulated
reasoning thoughts into concise summaries, writing them to a designated writer.
As models produce increasingly long reasoning traces, users struggle to extract
key information from raw thought streams. ThoughtsSummarize addresses this by
summarizing at a configurable interval (default 3 seconds), enabling users to
quickly assess whether the model's thinking direction is correct and interrupt
early if it diverges. A separate, typically cheaper and faster generator is used
for summarization to minimize latency and cost. On Flush, any remaining
accumulated thoughts are summarized before propagating the flush upstream.
The summarization system prompt is designed to extract the key points and
direction of reasoning, not to reproduce the full thought content.
`

const SummarizeSystemPrompt = `You are a reasoning thought summarizer. Your sole task is to condense the model's internal reasoning into a brief, actionable summary that helps the user quickly assess whether the model's thinking is on the right track.

Focus on:
1. What problem the model is currently working on
2. Key intermediate conclusions or decisions
3. The overall direction and approach of the reasoning

Keep the summary to 2-3 sentences. Be concise and direct. The user reads this to decide whether to let the model continue or interrupt — highlight any signs of wrong direction, circular reasoning, or irrelevant tangents. Do not reproduce the raw thoughts; extract only the essential trajectory.`

// Summarizer is a separate generator type dedicated to summarizing thoughts.
// It wraps an underlying Generator (typically a fast, cheap model) and
// provides a Summarize method that sends accumulated thoughts to the model
// with a purpose-built system prompt.
type Summarizer struct {
	generator Generator
}

func NewSummarizer(generator Generator) *Summarizer {
	return &Summarizer{generator: generator}
}

// Summarize sends the accumulated thoughts to the underlying generator with
// the summarization system prompt and returns the condensed summary text.
// Non-streaming mode is used because summaries are short and the complete
// result is needed before writing to the output writer.
func (s *Summarizer) Summarize(ctx context.Context, thoughts string) (string, error) {
	state := NewPrompts(SummarizeSystemPrompt, []*Content{
		{
			Role: RoleUser,
			Parts: []Part{
				Text(thoughts),
			},
		},
	})

	result, err := s.generator.Generate(ctx, state, &GenerateOptions{
		NonStreaming: true,
	})
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for content := range result.Contents() {
		if content.Role != RoleModel && content.Role != RoleAssistant {
			continue
		}
		for _, part := range content.Parts {
			if text, ok := part.(Text); ok {
				sb.WriteString(string(text))
			}
		}
	}
	return sb.String(), nil
}
