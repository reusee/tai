package generators

type State interface {
	Contents() []*Content
	AppendContent(*Content) (State, error)
	SystemPrompt() string
	FuncMap() map[string]*Func
	Flush() (State, error)
	Unwrap() State
}
