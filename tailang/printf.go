package tailang

import "fmt"

type Printf struct {
}

func (p Printf) Call(format string, args ...any) error {
	_, err := fmt.Printf(format, args...)
	return err
}

var _ Function = Printf{}

func (p Printf) Name() string {
	return "printf"
}
