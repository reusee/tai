package blocks

import (
	"iter"
	"slices"

	"github.com/reusee/tai/generators"
)

const TheoryOfParserState = `
ParserState is a State decorator that incrementally parses heredoc-delimited blocks
from streamed model output. It sits between the generator and the downstream
consumer, intercepting text parts appended by the model to extract structured blocks
(e.g., change and finish blocks) without losing non-block prose. Only Text parts are
collected into the parse buffer; Thought parts (model reasoning) are explicitly
excluded because they may contain illustrative block markers that are not actual
block output, and parsing them would produce spurious blocks. Parsed blocks are
passed to a BlockHandler callback that is responsible for all block management —
processing, storing, or discarding blocks. ParserState itself does not store blocks.
This ensures that all state modifications happen exclusively through the State
interface methods (AppendContent, Flush), with no extra methods that bypass the
immutable state chain.

ParserState is an immutable data structure: AppendContent and Flush return a new
*ParserState rather than mutating in place. This preserves snapshot integrity for
rollback and retry: the pre-generation State is unaffected by a failed attempt, so
retrying starts from a clean snapshot. Because blocks are managed externally by the
caller (via the handler closure), rollback consistency is achieved by resetting the
external block collection alongside other external state (e.g., MemoryStore) in the
retry callback.

The parser is incremental: each AppendContent call appends new text to the buffer
and re-attempts to parse complete blocks. A block is only complete when a matching
DELIMITER closing line is found. A line that does not match the delimiter is treated
as body content and does not close the block. If no matching closing line is found,
the block is unclosed (incomplete) and left in the buffer for the next AppendContent
call, because streaming output may arrive in fragments. Text preceding the first
block marker is prose and is discarded once a block is found, because ParserState's
purpose is block extraction, not prose preservation.

At Flush, an unclosed block (opening marker with no matching closing line) is
collected as a parse error (see TheoryOfParseErrorCollection) rather than treated as
a fatal error, because the caller can feed the error back to the model for
self-correction. Complete blocks remaining in the buffer are discarded (not stored),
because the handler is responsible for block management and should have been called
during AppendContent. Any remaining unparseable fragments are discarded so content
appended after Flush (e.g., from a subsequent generation cycle) is never combined
with pre-Flush content within the same block. Delimiters are parsed as the text
between << and the first whitespace or < character; this ensures the delimiter is
extracted correctly regardless of what follows it on the opening line.
`

const TheoryOfParseErrorCollection = `
Parse errors — blocks whose opening tag is malformed or whose closing delimiter is
missing or malformed — are collected during streaming rather than treated as fatal
generation errors. A block that cannot be parsed is malformed or truncated model
output, not a system failure: the model may correct it given the right feedback.
Collecting the error with the block kind, boundary, line number, partial content,
and collision hints and feeding it back as user content in the next round gives the
model a concrete target for self-correction, while successfully parsed blocks
continue to be processed normally. A line with a valid two-character Han delimiter
followed by an invalid or incomplete XML opening tag is a malformed block, not
prose: the delimiter marks the line as an intended block opening, so the model must
be told what went wrong rather than having its intended block silently dropped.
Complete blocks that follow a malformed block in the same buffer are not reachable
during AppendContent, because the parser stops at the first malformed block; Flush
handles them so valid blocks are never lost because a preceding block was malformed.
This self-healing capability is especially important in unattended tasks where no
human is available to intervene.

The correction budget is cumulative per run: the loop (see TheoryOfLoops) feeds
parse errors back only for a bounded number of rounds since the last clean round,
and the feedback states the attempt number so the model knows when it is on its
final attempt. When the budget is exhausted, feedback stops and the uncorrected
parse errors are surfaced via pipeline.Result.ParseErrors, so a persistently malformed
model cannot restart the correction cycle indefinitely and unattended callers can
detect silent change loss.

The partial content included in the error message is truncated when the block body
is large: a malformed block can contain an arbitrarily large body (e.g., a model
emitting a large file before being cut off), and including the full body would make
the error message enormous, wasting context in the self-correction round. The
truncated message keeps the head — the opening marker, which identifies the block —
and the tail — where the content ended, where the closing marker was expected — so
the model still has a concrete target for correction. The error also carries the
1-based line number of the opening marker, so the model can locate the malformed
block in its own output even when the content is truncated. The full content remains
available in BlockParseError.Content for programmatic inspection; only the formatted
error string is truncated.
`

// BlockHandler is called when a new block is parsed during AppendContent.
// The handler processes the block however it wishes — apply it, store it
// externally, or discard it. If err is non-nil, AppendContent returns the
// error immediately, stopping streaming.
type BlockHandler func(block Block) error

type ParserState struct {
	upstream    generators.State
	buf         []byte
	handler     BlockHandler
	parseErrors []*BlockParseError
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
			upstream:    newUpstream,
			buf:         s.buf,
			handler:     s.handler,
			parseErrors: s.parseErrors,
		}, nil
	}

	// Build the new parse buffer in a single exact-capacity allocation.
	// The previous buffer is never mutated after construction, so copying
	// its bytes once into the new allocation preserves the immutability of
	// previously returned states. slices.Clone followed by append would
	// copy the buffer twice per chunk: Clone yields a slice with cap == len,
	// so the first append reallocates and copies again.
	incomingLen := 0
	for _, part := range content.Parts {
		if _, ok := part.(generators.Thought); ok {
			continue
		}
		if text, ok := part.(generators.Text); ok {
			incomingLen += len(text)
		}
	}
	newBuf := make([]byte, 0, len(s.buf)+incomingLen)
	newBuf = append(newBuf, s.buf...)
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
					upstream:    newUpstream,
					buf:         buf,
					handler:     s.handler,
					parseErrors: s.parseErrors,
				}, handlerErr
			}
		}

		buf = buf[end:]
	}

	return &ParserState{
		upstream:    newUpstream,
		buf:         buf,
		handler:     s.handler,
		parseErrors: s.parseErrors,
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

	// During Flush, a malformed block (an unclosed block with no matching
	// end marker, or an invalid opening tag) is a parse error, not a fatal
	// error. Parse errors are collected via ParseErrors instead of being
	// returned, so the generation flow continues and the caller can feed
	// them back to the model for self-correction. The scanner skips past
	// each malformed block's opening marker so subsequent block markers
	// are still found.
	// A complete block remaining in the buffer at Flush was not handled
	// during AppendContent: the parser stops at the first malformed block,
	// so complete blocks following it are not reached until Flush. The
	// handler is called for them so valid blocks are never lost because a
	// preceding block was malformed. See TheoryOfParseErrorCollection.
	// Remaining unparseable fragments are discarded so post-flush content
	// does not combine with pre-flush fragments.
	// See TheoryOfParseErrorCollection.
	buf := slices.Clone(s.buf)
	parseErrors := slices.Clone(s.parseErrors)
	for {
		block, _, end, ok, err := parseFirstBlock(buf)
		if err != nil {
			// Malformed block: collect the parse error and skip past
			// its opening marker so subsequent block markers are still
			// found. If the marker cannot be skipped, stop scanning to
			// avoid an infinite loop.
			// See TheoryOfParseErrorCollection.
			if parseErr, ok := err.(*BlockParseError); ok {
				parseErrors = append(parseErrors, parseErr)
			}
			if end > 0 && end <= len(buf) {
				buf = buf[end:]
				continue
			}
			break
		}
		if !ok {
			break
		}
		// A complete block remaining in the buffer at Flush was not
		// handled during AppendContent (the parser stopped at a
		// preceding malformed block). Handle it now so valid blocks are
		// not lost. See TheoryOfParseErrorCollection.
		if s.handler != nil {
			if handlerErr := s.handler(block); handlerErr != nil {
				return nil, handlerErr
			}
		}
		buf = buf[end:]
	}

	return &ParserState{
		upstream:    newUpstream,
		buf:         nil,
		handler:     s.handler,
		parseErrors: parseErrors,
	}, nil
}

// ParseErrors returns the parse errors collected during Flush. Unclosed
// blocks (opening markers with no matching closing line) are collected
// here instead of aborting the generation flow, so the caller can feed
// them back to the model for self-correction. The returned slice is a
// copy; modifying it does not affect the ParserState. See
// TheoryOfParseErrorCollection.
func (s *ParserState) ParseErrors() []*BlockParseError {
	return slices.Clone(s.parseErrors)
}

func (s *ParserState) Unwrap() generators.State {
	return s.upstream
}

// PendingText returns the unparsed text remaining in the parse buffer.
// This is a read-only method for debugging; it does not modify state.
func (s *ParserState) PendingText() string {
	return string(s.buf)
}
