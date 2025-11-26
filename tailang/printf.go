package tailang

import "fmt"

type Printf struct {
}

var _ Function = Printf{}

func (p Printf) FunctionName() string {
	return "printf"
}

func (p Printf) Call(format string, args ...any) (int, error) {
	return fmt.Printf(format, args...)
}
