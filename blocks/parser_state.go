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
and finish blocks) without losing non-block prose. Parsed blocks are passed to a
BlockHandler callback that is responsible for all block management — processing,
storing, or discarding blocks. ParserState itself does not store blocks, eliminating
the need for block reconciliation (WithUpstream, PopBlocks, PopBlocksByKind) when
the state chain is modified by other layers. This ensures that all state
modifications happen exclusively through the State interface methods (AppendContent,
Flush), with no extra methods that bypass the immutable state chain.

ParserState is an immutable data structure: AppendContent and Flush return a new
*ParserState rather than mutating in place. This preserves snapshot integrity for
rollback and retry: the pre-generation State is unaffected by a failed attempt, so
retrying starts from a clean snapshot. Because blocks are managed externally by the
caller (via the handler closure), rollback consistency is achieved by resetting the
external block collection alongside other external state (e.g., MemoryStore) in the
retry callback, rather than relying on state-chain reconciliation.

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

At Flush, an unclosed block (opening marker with no matching end marker) is treated as an
error rather than being finalized, because an unclosed block indicates incomplete or
truncated output. Complete blocks remaining in the buffer are discarded (not stored),
because the handler is responsible for block management and should have been called during
AppendContent. Any remaining unparseable fragments are discarded so content appended after
Flush (e.g., from a subsequent generation cycle) is never combined with pre-Flush content
within the same block. Boundary strings are parsed as leading Han (Chinese) ideographs
only; a non-Han character terminates the boundary so trailing model-added content does
not corrupt block matching.

A BlockHandler callback may be set at construction time to receive blocks as they are
parsed during AppendContent. When the handler returns an error, AppendContent returns the
error immediately, stopping streaming. The handler is not called during Flush because
only unclosed (incomplete) blocks or already-parsed complete blocks remain at that point.
The handler is propagated to all new ParserState instances created by AppendContent and
Flush, so it remains active across the entire generation session. The handler signature
is func(block Block) error — the handler processes the block however it wishes (apply it,
store it externally, or discard it) and returns nil on success or an error to stop
streaming. This design makes the handler the sole authority over block lifecycle
management, keeping ParserState focused on parsing and the State chain focused on
content storage.
`

// BlockHandler is called when a new block is parsed during AppendContent.
// The handler processes the block however it wishes — apply it, store it
// externally, or discard it. If err is non-nil, AppendContent returns the
// error immediately, stopping streaming.
type BlockHandler func(block Block) error

// ParserState wraps an upstream State and incrementally parses boundary-delimited
// blocks from streamed model output. As the model appends text parts, the
// accumulated text is scanned for complete blocks using ParseFirstBlock. Parsed
// blocks are passed to a BlockHandler callback; ParserState does not store blocks
// itself. The handler is the sole authority over block lifecycle management.
//
// ParserState is immutable: AppendContent and Flush return a new *ParserState
// rather than mutating in place. See TheoryOfParserState.
type ParserState struct {
	upstream generators.State
	buf      []byte
	handler  BlockHandler
}

// NewParserState creates a ParserState that wraps the given upstream State.
// An optional BlockHandler may be provided to receive blocks as they are
// parsed during streaming. See TheoryOfParserState.
func NewParserState(upstream generators.State, handler ...BlockHandler) *ParserState {
	var h BlockHandler
	if len(handler) > 0 {
		h = handler[0]
	}
	return &ParserState{
		upstream: upstream,
		handler:  h,
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
			handler:  s.handler,
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

	buf := newBuf
	for {
		block, _, end, ok, err := parseFirstBlock(buf)
		if err != nil {
			// Unclosed block: incomplete, wait for more output.
			break
		}
		if !ok {
			break
		}

		if s.handler != nil {
			if handlerErr := s.handler(block); handlerErr != nil {
				// Handler error: advance past the block and return
				// the error to stop streaming immediately.
				// See TheoryOfParserState.
				buf = buf[end:]
				return &ParserState{
					upstream: newUpstream,
					buf:      buf,
					handler:  s.handler,
				}, handlerErr
			}
		}

		buf = buf[end:]
	}

	return &ParserState{
		upstream: newUpstream,
		buf:      buf,
		handler:  s.handler,
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

	// During Flush, an unclosed block (opening marker with no matching
	// end marker) is an error, not a finalized block. An unclosed block
	// indicates incomplete or truncated output. Complete blocks should
	// have been parsed and handled during AppendContent; any complete
	// blocks remaining in the buffer (e.g., from a handler error that
	// stopped parsing early) are discarded because the handler is the
	// sole authority over block management. Remaining unparseable
	// fragments are discarded so post-flush content does not combine
	// with pre-flush fragments. See TheoryOfParserState.
	buf := slices.Clone(s.buf)
	for {
		_, _, end, ok, err := parseFirstBlock(buf)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		// Skip complete block (already handled during AppendContent).
		buf = buf[end:]
	}

	return &ParserState{
		upstream: newUpstream,
		buf:      nil,
		handler:  s.handler,
	}, nil
}

func (s *ParserState) Unwrap() generators.State {
	return s.upstream
}

// PendingText returns the unparsed text remaining in the parse buffer.
// This is a read-only method for debugging; it does not modify state.
func (s *ParserState) PendingText() string {
	return string(s.buf)
}
