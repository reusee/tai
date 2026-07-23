package flags

type Flag interface {
	Keys() []string
	Handle(key string, args []string) (newValue any, remainArgs []string, err error)
}
