package generators

import (
	"context"
	"fmt"
	"strings"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

const TheoryOfThoughtsSummarize = `
ThoughtsSummarize is a State layer that periodically condenses accumulated
reasoning thoughts into concise summaries, writing them to a designated writer.
As models produce increasingly long reasoning traces, users struggle to extract
key information from raw thought streams. ThoughtsSummarize addresses this by
summarizing at a configurable interval (default 3 seconds), enabling users to
quickly assess whether the model's thinking direction is correct and interrupt
early if it diverges. When the interval elapses, only complete paragraphs are
summarized; any incomplete trailing text is retained for the next cycle to avoid
sending truncated sentences to the summarizer. When non-thought content (e.g.,
the model's final answer) arrives, all accumulated thoughts are flushed
immediately regardless of interval or paragraph boundaries, ensuring the summary
appears before the main text output rather than after it. A separate, typically
cheaper and faster generator is used for summarization to minimize latency and
cost. The GetDefaultSummarizer provider wires the default fast model (obtained via
GetDefaultFastModel) into a Summarizer, so production code can obtain a summarizer
without manually selecting a model. On Flush, any remaining accumulated thoughts
are summarized before propagating the flush upstream. The summarization system
prompt is designed to extract only the most important points and direction of
reasoning, not to reproduce the full thought content. The summary is formatted as
a bullet list of at most 2 key points, each item being a single concise sentence,
so the user can scan the reasoning trajectory quickly without reading a dense
paragraph.
`

const SummarizeSystemPrompt = `You are a reasoning thought summarizer. Your sole task is to condense the model's internal reasoning into an extremely concise summary that helps the user quickly assess whether the model's thinking is on the right track.

Output at most 2 bullet points. Each list item must be a single, short sentence capturing only the most important key point:
- What problem the model is currently working on
- The overall direction and approach of the reasoning

Pick only the most essential points. Do not be exhaustive. The user reads this to decide whether to let the model continue or interrupt — highlight any signs of wrong direction, circular reasoning, or irrelevant tangents. Do not reproduce the raw thoughts; extract only the essential trajectory.`

// ThoughtsSummarizeLanguage controls the output language for thought
// summaries. When empty (the default), no language hint is given to the
// summarizer. When set (e.g., "zh", "en"), the summarizer is instructed
// to output summaries in that language. It can be configured via the
// thoughts_summarize_language field in tai.cue or the
// -thoughts-summarize-language command-line flag.
type ThoughtsSummarizeLanguage string

var _ configs.Configurable = ThoughtsSummarizeLanguage("")

func (l ThoughtsSummarizeLanguage) TaigoConfigurable() {}

var _ flags.Flag = ThoughtsSummarizeLanguage("")

func (l ThoughtsSummarizeLanguage) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting language string, got empty")
	}
	return ThoughtsSummarizeLanguage(args[0]), args[1:], nil
}

func (l ThoughtsSummarizeLanguage) Keys() map[string]string {
	return map[string]string{
		"-thoughts-summarize-language": "Set the language for thought summaries (e.g., zh, en)",
	}
}

func (Module) ThoughtsSummarizeLanguage(
	loader configs.Loader,
) ThoughtsSummarizeLanguage {
	return configs.First[ThoughtsSummarizeLanguage](loader, "thoughts_summarize_language")
}

// Summarizer is a separate generator type dedicated to summarizing thoughts.
// It wraps an underlying Generator (typically a fast, cheap model) and
// provides a Summarize method that sends accumulated thoughts to the model
// with a purpose-built system prompt.
type Summarizer struct {
	generator Generator
	language  ThoughtsSummarizeLanguage
}

func NewSummarizer(generator Generator) *Summarizer {
	return &Summarizer{generator: generator}
}

type GetDefaultSummarizer func() (*Summarizer, error)

func (Module) GetDefaultSummarizer(
	getDefaultFastModel GetDefaultFastModel,
	enable SummarizeThoughts,
	language ThoughtsSummarizeLanguage,
) GetDefaultSummarizer {
	return func() (*Summarizer, error) {
		if !enable {
			return nil, nil
		}
		gen, err := getDefaultFastModel()
		if err != nil {
			return nil, err
		}
		return &Summarizer{
			generator: gen,
			language:  language,
		}, nil
	}
}

// Summarize sends the accumulated thoughts to the underlying generator with
// the summarization system prompt and returns the condensed summary text.
func (s *Summarizer) Summarize(ctx context.Context, thoughts string) (string, error) {
	systemPrompt := SummarizeSystemPrompt
	if s.language != "" {
		systemPrompt += "\n\nYou MUST output the summary in " + string(s.language) + "."
	}
	state := NewPrompts(systemPrompt, []*Content{
		{
			Role: RoleUser,
			Parts: []Part{
				Text(thoughts),
			},
		},
	})

	result, err := s.generator.Generate(ctx, state, &GenerateOptions{})
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
