package generators

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"
)

// ThoughtsSummarize is a State layer that periodically summarizes accumulated
// Thought parts and writes the summary to a designated writer. This helps users
// track the model's reasoning direction without reading every raw thought.
// See TheoryOfThoughtsSummarize for design rationale.
type ThoughtsSummarize struct {
	upstream      State
	summarizer    *Summarizer
	writer        io.Writer
	interval      time.Duration
	accumulated   string
	lastSummarize time.Time
	ctx           context.Context
}

// NewThoughtsSummarize creates a ThoughtsSummarize state layer. The interval
// controls how often accumulated thoughts are summarized; if not specified,
// it defaults to 3 seconds. The ctx is used for summarization API calls
// since AppendContent does not receive a context.
func NewThoughtsSummarize(
	ctx context.Context,
	upstream State,
	summarizer *Summarizer,
	writer io.Writer,
	interval ...time.Duration,
) ThoughtsSummarize {
	i := 3 * time.Second
	if len(interval) > 0 && interval[0] > 0 {
		i = interval[0]
	}
	return ThoughtsSummarize{
		upstream:      upstream,
		summarizer:    summarizer,
		writer:        writer,
		interval:      i,
		lastSummarize: time.Now(),
		ctx:           ctx,
	}
}

var _ State = ThoughtsSummarize{}

func (s ThoughtsSummarize) AppendContent(content *Content) (State, error) {
	if s.summarizer == nil {
		ret := s
		var err error
		ret.upstream, err = s.upstream.AppendContent(content)
		if err != nil {
			return ret, err
		}
		return ret, nil
	}

	ret := s // copy

	// Check if the incoming content contains any Thought parts, and
	// accumulate Thought parts. Non-thought parts pass through unchanged
	// to upstream.
	hasThought := false
	for _, part := range content.Parts {
		if thought, ok := part.(Thought); ok {
			hasThought = true
			ret.accumulated += string(thought)
		}
	}

	// When the incoming content does not contain thoughts and there are
	// accumulated thoughts from previous content, flush the accumulated
	// thoughts immediately. This ensures the thought summary appears
	// before the main text output, not after it. All accumulated text is
	// summarized (not just complete paragraphs) because the thought
	// stream has ended.
	if !hasThought && len(ret.accumulated) > 0 {
		summary, err := ret.summarizer.Summarize(ret.ctx, ret.accumulated)
		if err != nil {
			return ret, err
		}
		if _, err := fmt.Fprintf(ret.writer, "\n[Thought Summary]:\n%s\n\n", summary); err != nil {
			return ret, err
		}
		ret.accumulated = ""
		ret.lastSummarize = time.Now()
	}

	// Periodically summarize accumulated thoughts. The interval check
	// ensures summarization happens at most once per interval, preventing
	// excessive API calls during fast streaming. Only complete paragraphs
	// are summarized to avoid sending truncated sentences to the
	// summarizer; any incomplete trailing text is retained for the next
	// cycle.
	if len(ret.accumulated) > 0 && time.Since(ret.lastSummarize) >= ret.interval {
		complete, remaining := splitAtLastCompleteParagraph(ret.accumulated)
		if complete != "" {
			summary, err := ret.summarizer.Summarize(ret.ctx, complete)
			if err != nil {
				return ret, err
			}
			if _, err := fmt.Fprintf(ret.writer, "\n[Thought Summary]:\n%s\n\n", summary); err != nil {
				return ret, err
			}
			ret.accumulated = remaining
			ret.lastSummarize = time.Now()
		}
	}

	// Propagate content to upstream. Thoughts are still passed through so
	// that downstream layers (e.g., Output) can display them if desired.
	var err error
	ret.upstream, err = s.upstream.AppendContent(content)
	if err != nil {
		return ret, err
	}

	return ret, nil
}

func (s ThoughtsSummarize) Contents() iter.Seq[*Content] {
	return s.upstream.Contents()
}

func (s ThoughtsSummarize) Functions() iter.Seq[*Function] {
	return s.upstream.Functions()
}

func (s ThoughtsSummarize) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s ThoughtsSummarize) Flush() (State, error) {
	if s.summarizer == nil {
		ret := s
		var err error
		ret.upstream, err = s.upstream.Flush()
		if err != nil {
			return ret, err
		}
		return ret, nil
	}

	ret := s // copy

	// Summarize any remaining accumulated thoughts before flushing.
	// This ensures no thoughts are lost when a turn ends between
	// summarization intervals. On flush, all remaining text is
	// summarized regardless of paragraph boundaries since the turn
	// is ending.
	if len(ret.accumulated) > 0 {
		summary, err := ret.summarizer.Summarize(ret.ctx, ret.accumulated)
		if err != nil {
			return ret, err
		}
		if _, err := fmt.Fprintf(ret.writer, "\n[Thought Summary]:\n%s\n\n", summary); err != nil {
			return ret, err
		}
		ret.accumulated = ""
	}

	var err error
	ret.upstream, err = s.upstream.Flush()
	if err != nil {
		return ret, err
	}

	return ret, nil
}

func (s ThoughtsSummarize) Unwrap() State {
	return s.upstream
}

// splitAtLastCompleteParagraph splits accumulated text at the last
// complete paragraph boundary. It returns the complete portion (safe
// to summarize) and the remaining incomplete portion (to keep for the
// next cycle). Paragraph boundaries are double newlines; single newlines
// are used as a secondary boundary. If no boundary is found, complete is
// empty and remaining is the full text, indicating summarization should
// be deferred until more content arrives.
func splitAtLastCompleteParagraph(text string) (complete, remaining string) {
	// Try paragraph boundary (double newline) first
	if idx := strings.LastIndex(text, "\n\n"); idx >= 0 {
		return text[:idx+2], text[idx+2:]
	}
	// Try single newline as a secondary boundary
	if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		return text[:idx+1], text[idx+1:]
	}
	// No suitable boundary found; defer summarization
	return "", text
}
