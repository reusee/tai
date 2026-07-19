package phases

import (
	"iter"

	"github.com/reusee/tai/generators"
)

type RedoCheckpoint struct {
	upstream  generators.State
	state0    generators.State
	generator generators.Generator
}

var _ generators.State = RedoCheckpoint{}

func (r RedoCheckpoint) AppendContent(content *generators.Content) (generators.State, error) {
	upstream, err := r.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}
	return RedoCheckpoint{
		upstream:  upstream,
		state0:    r.state0,    // Preserve state0
		generator: r.generator, // Preserve generator
	}, nil
}

func (r RedoCheckpoint) Contents() iter.Seq[*generators.Content] {
	return r.upstream.Contents()
}

func (r RedoCheckpoint) Flush() (generators.State, error) {
	upstream, err := r.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return RedoCheckpoint{
		upstream:  upstream,
		state0:    r.state0,    // Preserve state0
		generator: r.generator, // Preserve generator
	}, nil
}

func (r RedoCheckpoint) Functions() iter.Seq[*generators.Function] {
	return r.upstream.Functions()
}

func (r RedoCheckpoint) SystemPrompt() string {
	return r.upstream.SystemPrompt()
}

func (r RedoCheckpoint) Unwrap() generators.State {
	return r.upstream
}

// WithUpstream returns a new RedoCheckpoint with the same state0 and generator
// but a different upstream state. Used to reconcile block processing (which
// updates the *ParserState inside upstream) with content appending (which
// also updates the upstream) before the next generation round.
func (r RedoCheckpoint) WithUpstream(upstream generators.State) RedoCheckpoint {
	return RedoCheckpoint{
		upstream:  upstream,
		state0:    r.state0,
		generator: r.generator,
	}
}
