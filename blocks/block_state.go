package blocks

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
and re-attempts to parse complete blocks. A block is only complete when a matching
:::end <boundary> marker is found at line start. A line-start :::end with a different
boundary is treated as body content and does not close the block. If no matching end
marker is found, the block is unclosed (incomplete) and left in the buffer for the next
AppendContent call, because streaming output may arrive in fragments. Text preceding the
first block marker is prose and is discarded once a block is found, because BlockState's
purpose is block extraction, not prose preservation.
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
// parsing. An unclosed (incomplete) block is kept in the buffer for later chunks.
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
		if err := s.parseBlocks(); err != nil {
			return s, err
		}
	}
	return s, nil
}

// parseBlocks repeatedly parses complete blocks from the internal buffer.
// It must be called with s.mu held. An unclosed block (no matching end
// marker found) is treated as incomplete and left in the buffer for the
// next AppendContent call, because streaming output may arrive in fragments.
// A line-start :::end with a different boundary is treated as body content,
// not an error.
func (s *BlockState) parseBlocks() error {
	for {
		block, _, end, ok, err := ParseFirstBlock(s.buf)
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
	defer s.mu.Unlock()
	if err := s.parseBlocks(); err != nil {
		return s, err
	}
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