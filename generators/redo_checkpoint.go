package generators

import "iter"

// RedoCheckpoint is a State wrapper that preserves the pre-generation state
// and the generator that produced the wrapped state, so the chat prompt's
// /regen command can restart generation from the checkpoint. State0 and
// Generator are exported because the chat prompt (pipeline.BuildChatPhase,
// pipeline.BuildChatIdle) reads them across package boundaries.
type RedoCheckpoint struct {
	upstream State
	// State0 is the pre-generation state, preserved for regeneration.
	State0 State
	// Generator is the generator that produced the wrapped state.
	Generator Generator
}

var _ State = RedoCheckpoint{}

func (r RedoCheckpoint) AppendContent(content *Content) (State, error) {
	upstream, err := r.upstream.AppendContent(content)
	if err != nil {
		// The error path still returns a checkpoint: the state carries
		// the partial upstream result and the checkpoint fields, so the
		// caller's retry gate can read the content increase. A nil state
		// would discard the session. See TheoryOfStateImmutability.
		if upstream == nil {
			upstream = r.upstream
		}
		return RedoCheckpoint{
			upstream:  upstream,
			State0:    r.State0,
			Generator: r.Generator,
		}, err
	}
	return RedoCheckpoint{
		upstream:  upstream,
		State0:    r.State0,
		Generator: r.Generator,
	}, nil
}

func (r RedoCheckpoint) Contents() iter.Seq[*Content] {
	return r.upstream.Contents()
}

func (r RedoCheckpoint) Flush() (State, error) {
	upstream, err := r.upstream.Flush()
	if err != nil {
		// The error path still returns a checkpoint: the state carries
		// the partial upstream result and the checkpoint fields, so the
		// caller never loses the session to an upstream failure. A nil
		// state would discard it. See TheoryOfStateImmutability.
		if upstream == nil {
			upstream = r.upstream
		}
		return RedoCheckpoint{
			upstream:  upstream,
			State0:    r.State0,
			Generator: r.Generator,
		}, err
	}
	return RedoCheckpoint{
		upstream:  upstream,
		State0:    r.State0,
		Generator: r.Generator,
	}, nil
}

func (r RedoCheckpoint) Functions() iter.Seq[*Function] {
	return r.upstream.Functions()
}

func (r RedoCheckpoint) SystemPrompt() string {
	return r.upstream.SystemPrompt()
}

func (r RedoCheckpoint) Unwrap() State {
	return r.upstream
}
