package tailang

type Function interface {
	Value
	Name() string
}
