package flags

type Flag interface {
	Key() string
	Handle(args []string) (newValue any, remainArgs []string, err error)
}
