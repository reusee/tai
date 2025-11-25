package tailang

type List struct{}

func (l List) Name() string {
	return "["
}

func (l List) Call(args ...any) []any {
	return args
}
