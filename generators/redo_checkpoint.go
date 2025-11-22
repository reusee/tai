package generators

type RedoCheckpoint struct {
	upstream  State
	state0    State
	generator Generator
}

var _ State = RedoCheckpoint{}

func (r RedoCheckpoint) AppendContent(content *Content) (State, error) {
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

func (r RedoCheckpoint) Contents() []*Content {
	return r.upstream.Contents()
}

func (r RedoCheckpoint) Flush() (State, error) {
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

func (r RedoCheckpoint) FuncMap() map[string]*Func {
	return r.upstream.FuncMap()
}

func (r RedoCheckpoint) SystemPrompt() string {
	return r.upstream.SystemPrompt()
}

func (r RedoCheckpoint) Unwrap() State {
	return r.upstream
}
