package blocks

const TheoryOfSummaryBlocks = `
Summary blocks allow the model to emit a brief description of each generation
round's content, including reasoning. One summary block is emitted per round,
before any continue or finish block. The summaries are collected after
generation ends and displayed alongside the round statistics, providing a
human-readable narrative of the generation session without interfering with
block processing or state management. Summary blocks are always enabled because
they have no side effects and provide value in every session: they help the
user understand what the model did and thought in each round without reading
the full output.
`

const SummaryBlockSystemPrompt = `**Summary Block Kind:**

The "summary" kind provides a brief description of the current generation round's content, including your reasoning and actions taken. One summary block MUST be emitted at the end of each generation round, before any continue or finish block.

**Summary Block Format:**

:::<boundary> <summary>
<brief description of this round's content and reasoning>
:::<boundary> </summary>

**Rules:**
- Emit exactly one summary block per generation round.
- The summary block MUST appear before any continue or finish block in the response.
- The body is a brief description of what was done and thought in this round.
- The summary is displayed to the user after generation ends, alongside round statistics.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.
`

// ProcessSummaryBlocks pops all summary blocks from parserState and returns
// their body texts alongside a new *ParserState with those blocks removed.
// The original parserState is not modified. Summaries are collected for
// terminal display after generation ends, not appended to the state.
// See TheoryOfSummaryBlocks and TheoryOfParserState.
func ProcessSummaryBlocks(parserState *ParserState) ([]string, *ParserState) {
	if parserState == nil {
		return nil, nil
	}
	blocks, newParserState := parserState.PopBlocksByKind("summary")
	if len(blocks) == 0 {
		return nil, newParserState
	}
	var summaries []string
	for _, block := range blocks {
		summaries = append(summaries, block.Body)
	}
	return summaries, newParserState
}
