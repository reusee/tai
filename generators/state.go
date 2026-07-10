package generators

import "iter"

type State interface {
	Contents() iter.Seq[*Content]
	AppendContent(*Content) (State, error)
	SystemPrompt() string
	Functions() iter.Seq[*Function]
	Flush() (State, error)
	Unwrap() State
}
