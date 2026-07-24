package generators

import (
	"context"
	"fmt"
	"io"
	"iter"
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
	ret := s // copy

	// Accumulate Thought parts from incoming content. Non-thought parts
	// pass through unchanged to upstream.
	for _, part := range content.Parts {
		if thought, ok := part.(Thought); ok {
			ret.accumulated += string(thought)
		}
	}

	// Periodically summarize accumulated thoughts. The interval check
	// ensures summarization happens at most once per interval, preventing
	// excessive API calls during fast streaming.
	if len(ret.accumulated) > 0 && time.Since(ret.lastSummarize) >= ret.interval {
		summary, err := ret.summarizer.Summarize(ret.ctx, ret.accumulated)
		if err != nil {
			return ret, err
		}
		if _, err := fmt.Fprintf(ret.writer, "\n[Thought Summary]: %s\n\n", summary); err != nil {
			return ret, err
		}
		ret.accumulated = ""
		ret.lastSummarize = time.Now()
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
	ret := s // copy

	// Summarize any remaining accumulated thoughts before flushing.
	// This ensures no thoughts are lost when a turn ends between
	// summarization intervals.
	if len(ret.accumulated) > 0 {
		summary, err := ret.summarizer.Summarize(ret.ctx, ret.accumulated)
		if err != nil {
			return ret, err
		}
		if _, err := fmt.Fprintf(ret.writer, "\n[Thought Summary]: %s\n\n", summary); err != nil {
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
