package blocks

import (
	"iter"
	"sync"

	"github.com/reusee/tai/generators"
)

const TheoryOfParserState = `
ParserState is a State decorator that incrementally parses boundary-delimited blocks
from streamed model output. It sits between the generator and the downstream consumer,
intercepting text parts appended by the model to extract structured blocks (e.g., change
and finish blocks) without losing non-block prose. Parsed blocks are buffered and can be
read or drained for downstream processing while generation continues.

Only Text parts are collected into the parse buffer; Thought parts (model reasoning) are
explicitly excluded because they may contain illustrative block markers that are not actual
block output, and parsing them would produce spurious blocks.

The parser is incremental: each AppendContent call appends new text to an internal buffer
and re-attempts to parse complete blocks. A block is only complete when a matching
:::end <boundary> marker is found at line start. A line-start :::end with a different
boundary is treated as body content and does not close the block. If no matching end
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

Parsed blocks can be consumed selectively by kind via PopBlocksByKind, so processing
one kind of block (e.g., request-context) does not discard blocks of other kinds (e.g.,
change) that must remain available for subsequent processing.
`

// ParserState wraps an upstream State and incrementally parses boundary-delimited
// blocks from streamed model output. As the model appends text parts, the
// accumulated text is scanned for complete blocks using ParseFirstBlock. Parsed
// blocks are buffered and can be read or drained for downstream processing.
type ParserState struct {
	upstream generators.State

	mu     sync.Mutex
	buf    []byte
	blocks []Block
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
	s.upstream = newUpstream

	// Only parse blocks from model-generated content, not from user or system input.
	if content.Role == generators.RoleAssistant || content.Role == generators.RoleModel {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, part := range content.Parts {
			// Thoughts are model reasoning, not block output. They may
			// contain illustrative block markers that must not be parsed
			// as real blocks. See TheoryOfParserState.
			if _, ok := part.(generators.Thought); ok {
				continue
			}
			if text, ok := part.(generators.Text); ok {
				s.buf = append(s.buf, string(text)...)
			}
		}
		if err := s.parseBlocks(false); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (s *ParserState) parseBlocks(final bool) error {
	for {
		block, _, end, ok, err := parseFirstBlock(s.buf, final)
		if err != nil {
			// Unclosed block: incomplete, wait for more output.
			break
		}
		if !ok {
			break
		}
		s.blocks = append(s.blocks, block)
		s.buf = s.buf[end:]
	}
	return nil
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
	s.upstream = newUpstream
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.parseBlocks(true); err != nil {
		return s, err
	}
	// Discard any remaining unparseable fragments so post-flush content
	// does not combine with pre-flush fragments. See TheoryOfParserState.
	s.buf = s.buf[:0]
	return s, nil
}

func (s *ParserState) Unwrap() generators.State {
	return s.upstream
}

func (s *ParserState) Blocks() iter.Seq[Block] {
	return func(yield func(Block) bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, b := range s.blocks {
			if !yield(b) {
				return
			}
		}
	}
}

func (s *ParserState) PopBlocks() []Block {
	s.mu.Lock()
	defer s.mu.Unlock()
	blocks := s.blocks
	s.blocks = nil
	return blocks
}

func (s *ParserState) PopBlocksByKind(kind string) []Block {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []Block
	var remaining []Block
	for _, b := range s.blocks {
		if b.Kind == kind {
			matched = append(matched, b)
		} else {
			remaining = append(remaining, b)
		}
	}
	s.blocks = remaining
	return matched
}

func (s *ParserState) PendingText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}
