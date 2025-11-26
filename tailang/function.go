package tailang

type Function interface {
	Value
	FunctionName() string
}
