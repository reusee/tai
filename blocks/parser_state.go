package blocks

import (
	"iter"
	"slices"

	"github.com/reusee/tai/generators"
)

const TheoryOfParserState = `
ParserState is a State decorator that incrementally parses boundary-delimited blocks
from streamed model output. It sits between the generator and the downstream consumer,
intercepting text parts appended by the model to extract structured blocks (e.g., change
and finish blocks) without losing non-block prose. Parsed blocks are buffered and can be
read or drained for downstream processing while generation continues.

ParserState is an immutable data structure: AppendContent and Flush return a new
*ParserState rather than mutating in place. This preserves snapshot integrity for
rollback and retry (e.g., RedoCheckpoint.state0 is not corrupted by generation).
PopBlocks and PopBlocksByKind also return a new *ParserState alongside the popped
blocks, so consuming blocks does not mutate the original state. Callers that pop
blocks must thread the returned *ParserState through subsequent operations and
reconcile it with the outer state (via WithUpstream) before the next generation
round, so that consumed blocks are not reprocessed.

Only Text parts are collected into the parse buffer; Thought parts (model reasoning) are
explicitly excluded because they may contain illustrative block markers that are not actual
block output, and parsing them would produce spurious blocks.

The parser is incremental: each AppendContent call appends new text to the buffer and
re-attempts to parse complete blocks. A block is only complete when a matching
:::<boundary> </kind> marker is found at line start. A line-start :::<boundary> with a different
boundary is treated as body content and does not close the block. If no matching closing
marker is found, the block is unclosed (incomplete) and left in the buffer for the next
AppendContent call, because streaming output may arrive in fragments. Text preceding the
first block marker is prose and is discarded once a block is found, because ParserState's
purpose is block extraction, not prose preservation.

At Flush, the parser switches to final mode: an unclosed block is treated as ended rather
than left pending, so its body is finalized as all remaining buffered content and the buffer
is fully consumed. Any remaining unparseable fragments are discarded so content appended
after Flush (e.g., from a subsequent generation cycle) is never combined with pre-Flush
content within the same block. Boundary strings are parsed as leading Han (Chinese)
ideographs only; a non-Han character terminates the boundary so trailing model-added
content does not corrupt block matching.

Parsed blocks can be consumed selectively by kind via PopBlocksByKind, which returns
the matched blocks alongside a new *ParserState with those blocks removed, so processing
one kind of block (e.g., request-context) does not discard blocks of other kinds (e.g.,
change) that must remain available for subsequent processing.
`

// ParserState wraps an upstream State and incrementally parses boundary-delimited
// blocks from streamed model output. As the model appends text parts, the
// accumulated text is scanned for complete blocks using ParseFirstBlock. Parsed
// blocks are buffered and can be read or drained for downstream processing.
//
// ParserState is immutable: AppendContent, Flush, PopBlocks, and PopBlocksByKind
// all return a new *ParserState rather than mutating in place. See TheoryOfParserState.
type ParserState struct {
	upstream generators.State
	buf      []byte
	blocks   []Block
}

// NewParserState creates a ParserState that wraps the given upstream State.
func NewParserState(upstream generators.State) *ParserState {
	return &ParserState{
		upstream: upstream,
	}
}

var _ generators.State = (*ParserState)(nil)

func (s *ParserState) AppendContent(content *generators.Content) (generators.State, error) {
	newUpstream, err := s.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}

	// Only parse blocks from model-generated content, not from user or system input.
	if content.Role != generators.RoleAssistant && content.Role != generators.RoleModel {
		return &ParserState{
			upstream: newUpstream,
			buf:      s.buf,
			blocks:   s.blocks,
		}, nil
	}

	newBuf := slices.Clone(s.buf)
	for _, part := range content.Parts {
		// Thoughts are model reasoning, not block output. They may
		// contain illustrative block markers that must not be parsed
		// as real blocks. See TheoryOfParserState.
		if _, ok := part.(generators.Thought); ok {
			continue
		}
		if text, ok := part.(generators.Text); ok {
			newBuf = append(newBuf, string(text)...)
		}
	}

	blocks := slices.Clone(s.blocks)
	buf := newBuf
	for {
		block, _, end, ok, err := parseFirstBlock(buf, false)
		if err != nil {
			// Unclosed block: incomplete, wait for more output.
			break
		}
		if !ok {
			break
		}
		blocks = append(blocks, block)
		buf = buf[end:]
	}

	return &ParserState{
		upstream: newUpstream,
		buf:      buf,
		blocks:   blocks,
	}, nil
}

func (s *ParserState) Contents() iter.Seq[*generators.Content] {
	return s.upstream.Contents()
}

func (s *ParserState) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s *ParserState) Functions() iter.Seq[*generators.Function] {
	return s.upstream.Functions()
}

func (s *ParserState) Flush() (generators.State, error) {
	newUpstream, err := s.upstream.Flush()
	if err != nil {
		return nil, err
	}

	blocks := slices.Clone(s.blocks)
	buf := slices.Clone(s.buf)
	for {
		block, _, end, ok, err := parseFirstBlock(buf, true)
		if err != nil {
			break
		}
		if !ok {
			break
		}
		blocks = append(blocks, block)
		buf = buf[end:]
	}

	// Discard any remaining unparseable fragments so post-flush content
	// does not combine with pre-flush fragments. See TheoryOfParserState.
	return &ParserState{
		upstream: newUpstream,
		buf:      nil,
		blocks:   blocks,
	}, nil
}

func (s *ParserState) Unwrap() generators.State {
	return s.upstream
}

// WithUpstream returns a new ParserState with the same blocks and buffer
// but a different upstream state. Used to reconcile block processing
// (which removes blocks) with content appending (which updates the upstream)
// before the next generation round. See TheoryOfParserState.
func (s *ParserState) WithUpstream(upstream generators.State) *ParserState {
	return &ParserState{
		upstream: upstream,
		buf:      s.buf,
		blocks:   s.blocks,
	}
}

func (s *ParserState) Blocks() iter.Seq[Block] {
	return func(yield func(Block) bool) {
		for _, b := range s.blocks {
			if !yield(b) {
				return
			}
		}
	}
}

// HasCompletionBlock reports whether the parser state contains any block
// that signals normal round completion (summary or finish). Both block
// kinds indicate the model intentionally ended the round, as opposed to
// truncated output where no completion block is present.
// See TheoryOfSummaryCompletionRetry in codes/generate.go.
func (s *ParserState) HasCompletionBlock() bool {
	if s == nil {
		return false
	}
	for block := range s.Blocks() {
		if block.Kind == "summary" || block.Kind == "finish" {
			return true
		}
	}
	return false
}

// PopBlocks returns all buffered blocks and a new *ParserState with no blocks.
// The original state is not modified. See TheoryOfParserState.
func (s *ParserState) PopBlocks() ([]Block, *ParserState) {
	blocks := s.blocks
	return blocks, &ParserState{
		upstream: s.upstream,
		buf:      s.buf,
		blocks:   nil,
	}
}

// PopBlocksByKind returns blocks matching the given kind and a new *ParserState
// with those blocks removed. The original state is not modified.
// See TheoryOfParserState.
func (s *ParserState) PopBlocksByKind(kind string) ([]Block, *ParserState) {
	var matched []Block
	var remaining []Block
	for _, b := range s.blocks {
		if b.Kind == kind {
			matched = append(matched, b)
		} else {
			remaining = append(remaining, b)
		}
	}
	return matched, &ParserState{
		upstream: s.upstream,
		buf:      s.buf,
		blocks:   remaining,
	}
}

func (s *ParserState) PendingText() string {
	return string(s.buf)
}
