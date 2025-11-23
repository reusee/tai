package phases

import "github.com/reusee/tai/generators"

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

func (r RedoCheckpoint) Contents() []*generators.Content {
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

func (r RedoCheckpoint) FuncMap() map[string]*generators.Func {
	return r.upstream.FuncMap()
}

func (r RedoCheckpoint) SystemPrompt() string {
	return r.upstream.SystemPrompt()
}

func (r RedoCheckpoint) Unwrap() generators.State {
	return r.upstream
}
