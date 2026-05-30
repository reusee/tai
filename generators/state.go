package generators

import "iter"

type State interface {
	Contents() iter.Seq[*Content]
	AppendContent(*Content) (State, error)
	SystemPrompt() string
	FuncMap() iter.Seq2[string, *Func]
	Flush() (State, error)
	Unwrap() State
}