package states

import (
	"context"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
)

const TheoryOfThoughtsSummarize = `
ThoughtsSummarize is a State layer that periodically condenses accumulated
reasoning thoughts into concise summaries, writing them to a designated writer.
The designated writer is the ThoughtSummaryWriter provider when non-nil,
otherwise the generation output writer — the same stream the raw thoughts would
have used. A display front-end such as tai's TUI forks ThoughtSummaryWriter to
route the summaries to its own display. As models produce increasingly long
reasoning traces, users struggle to extract key information from raw thought
streams. ThoughtsSummarize addresses this by summarizing at a configurable
interval (default 3 seconds), enabling users to quickly assess whether the
model's thinking direction is correct and interrupt early if it diverges.
When the interval elapses, only complete paragraphs are summarized; any
incomplete trailing text is retained for the next cycle to avoid sending
truncated sentences to the summarizer. When content containing non-thought parts
(e.g., the model's final answer, or a streaming chunk that mixes reasoning with
answer text) arrives, all accumulated thoughts — including any thoughts in that
same content — are flushed immediately before the content is propagated to
upstream, regardless of interval or paragraph boundaries, ensuring the summary
appears before the main text output rather than after it. The flush is triggered
by the presence of non-thought parts, so a mixed content with both Thought and
Text parts correctly flushes before the text is printed. The summarization model
follows SummarizeModel, falling back to the fast model and then the default model
(see TheoryOfSummarizeModel). The GetDefaultSummarizer provider wires the selected
generator into a Summarizer. On Flush, any remaining accumulated thoughts are
summarized before propagating the flush upstream. The summarization system prompt
is designed to extract only the most important points and direction of reasoning,
not to reproduce the full thought content. The summary is formatted as a bullet
list of at most 2 key points, each item being a single concise sentence. The
Summarize method prompts the summarization model to wrap its output in a
boundary-delimited summary block and parses the block body via
blocks.ParseFirstBlock, ensuring the returned text contains only the clean
bullet-list summary without model preamble or trailing prose; if no block is
found the raw text is returned as a fallback.

Thought summarization serves user readability, not context compression.
Summaries go to the output writer for the human reader; they are never fed
back into the model. The system does not compress dialogue history. See
TheoryOfContextPhilosophy in loops/run.go.
`

const SummarizeSystemPrompt = `Condense the model's internal reasoning into an extremely concise summary that helps the user quickly assess whether the model's thinking is on the right track.

Output at most 2 bullet points. Each list item must be a single, short sentence capturing only the most important key point:
- What problem the model is currently working on
- The overall direction and approach of the reasoning

Pick only the most essential points. Do not be exhaustive. The user reads this to decide whether to let the model continue or interrupt — highlight any signs of wrong direction, circular reasoning, or irrelevant tangents. Do not reproduce the raw thoughts; extract only the essential trajectory.

Wrap the output in a summary block whose body is the bullet list. Output ONLY the block, no other text before or after.

` + blocks.BlockFormatSystemPrompt

// ThoughtsSummarizeLanguage is an alias for flags.ThoughtsSummarizeLanguage.
type ThoughtsSummarizeLanguage = flags.ThoughtsSummarizeLanguage

// Summarizer is a separate generator type dedicated to summarizing thoughts.
// It wraps an underlying Generator (typically a fast, cheap model) and
// provides a Summarize method that sends accumulated thoughts to the model
// with a purpose-built system prompt.
type Summarizer struct {
	generator generators.Generator
	language  ThoughtsSummarizeLanguage
}

func NewSummarizer(generator generators.Generator) *Summarizer {
	return &Summarizer{generator: generator}
}

type GetDefaultSummarizer func() (*Summarizer, error)

func (Module) GetDefaultSummarizer(
	getSummarizeGenerator GetSummarizeGenerator,
	enable SummarizeThoughts,
	language ThoughtsSummarizeLanguage,
) GetDefaultSummarizer {
	return func() (*Summarizer, error) {
		if !enable {
			return nil, nil
		}
		gen, err := getSummarizeGenerator()
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
// The system prompt instructs the model to wrap its output in a
// boundary-delimited summary block; the block body is parsed and returned
// as the clean summary text. When the model emits a block without the XML
// opening tag, the block has no kind and can only be found by iterating all
// blocks; in that case the first block is used as a fallback.
// See TheoryOfKindlessBlocks.
func (s *Summarizer) Summarize(ctx context.Context, thoughts string) (string, error) {
	systemPrompt := SummarizeSystemPrompt
	if s.language != "" {
		systemPrompt += "\n\nYou MUST output the summary in " + string(s.language) + "."
	}
	state := generators.NewPrompts(systemPrompt, []*generators.Content{
		{
			Role: generators.RoleUser,
			Parts: []generators.Part{
				generators.Text(thoughts),
			},
		},
	})

	result, err := s.generator.Generate(ctx, state, &generators.GenerateOptions{})
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for content := range result.Contents() {
		if content.Role != generators.RoleModel && content.Role != generators.RoleAssistant {
			continue
		}
		for _, part := range content.Parts {
			if text, ok := part.(generators.Text); ok {
				sb.WriteString(string(text))
			}
		}
	}

	// Parse the summary block from the model output. The system prompt
	// instructs the model to wrap its output in a boundary-delimited
	// summary block; extracting the block body yields the clean
	// bullet-list summary without model preamble or trailing prose.
	// The model may emit a block without the XML opening tag; such a
	// block has no kind and can only be found by iterating all blocks.
	// Look for a summary block first, then fall back to the first block.
	// See TheoryOfKindlessBlocks.
	parsedBlocks, err := blocks.ParseBlocks([]byte(sb.String()))
	if err == nil && len(parsedBlocks) > 0 {
		for _, block := range parsedBlocks {
			if block.Kind == "summary" {
				return strings.TrimSpace(block.Body), nil
			}
		}
		return strings.TrimSpace(parsedBlocks[0].Body), nil
	}
	// Fallback: return raw text if no block was found or on parse error
	return sb.String(), nil
}
