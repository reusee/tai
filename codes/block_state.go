package codes

import (
	"iter"
	"sync"

	"github.com/reusee/tai/generators"
)

const TheoryOfBlockState = `
BlockState is a State decorator that incrementally parses boundary-delimited blocks
from streamed model output. It sits between the generator and the downstream consumer,
intercepting text parts appended by the model to extract structured blocks (e.g., change
and finish blocks) without losing non-block prose. Parsed blocks are buffered and can be
read or drained for downstream processing while generation continues.

The parser is incremental: each AppendContent call appends new text to an internal buffer
and re-attempts to parse complete blocks. An unclosed block (opening marker found but no
matching closing marker) is treated as incomplete rather than malformed, so streaming
output that arrives in fragments is handled correctly. Text preceding the first block
marker is prose and is discarded once a block is found, because BlockState's purpose is
block extraction, not prose preservation.
`

// BlockState wraps an upstream State and incrementally parses boundary-delimited
// blocks from streamed model output. As the model appends text parts, the
// accumulated text is scanned for complete blocks using ParseFirstBlock. Parsed
// blocks are buffered and can be read or drained for downstream processing.
type BlockState struct {
	upstream generators.State

	mu     sync.Mutex
	buf    []byte
	blocks []Block
}

// NewBlockState creates a BlockState that wraps the given upstream State.
func NewBlockState(upstream generators.State) *BlockState {
	return &BlockState{
		upstream: upstream,
	}
}

var _ generators.State = (*BlockState)(nil)

// AppendContent passes content to the upstream state and extracts text parts
// from model-generated content (assistant or model role) for incremental block
// parsing.
func (s *BlockState) AppendContent(content *generators.Content) (generators.State, error) {
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
			if text, ok := part.(generators.Text); ok {
				s.buf = append(s.buf, string(text)...)
			}
		}
		s.parseBlocks()
	}
	return s, nil
}

// parseBlocks repeatedly parses complete blocks from the internal buffer.
// An unclosed block (error from ParseFirstBlock) is treated as incomplete
// and left in the buffer for the next AppendContent call.
func (s *BlockState) parseBlocks() {
	for {
		block, _, end, ok, err := ParseFirstBlock(s.buf)
		if err != nil || !ok {
			break
		}
		s.blocks = append(s.blocks, block)
		s.buf = s.buf[end:]
	}
}

// Contents returns the upstream state's content iterator.
func (s *BlockState) Contents() iter.Seq[*generators.Content] {
	return s.upstream.Contents()
}

// SystemPrompt returns the upstream state's system prompt.
func (s *BlockState) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

// Functions returns the upstream state's function iterator.
func (s *BlockState) Functions() iter.Seq[*generators.Function] {
	return s.upstream.Functions()
}

// Flush flushes the upstream state and performs a final block parse attempt.
func (s *BlockState) Flush() (generators.State, error) {
	newUpstream, err := s.upstream.Flush()
	if err != nil {
		return nil, err
	}
	s.upstream = newUpstream
	s.mu.Lock()
	s.parseBlocks()
	s.mu.Unlock()
	return s, nil
}

// Unwrap returns the upstream state.
func (s *BlockState) Unwrap() generators.State {
	return s.upstream
}

// Blocks returns an iterator over all parsed blocks without consuming them.
func (s *BlockState) Blocks() iter.Seq[Block] {
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

// PopBlocks returns all parsed blocks and clears the internal block buffer.
// This allows downstream processing to consume blocks incrementally as they arrive.
func (s *BlockState) PopBlocks() []Block {
	s.mu.Lock()
	defer s.mu.Unlock()
	blocks := s.blocks
	s.blocks = nil
	return blocks
}

// PendingText returns the remaining unparsed text in the buffer that has not yet
// formed a complete block. This is useful for debugging or for processing partial
// output after generation completes.
func (s *BlockState) PendingText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}