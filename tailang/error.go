package tailang

import "strings"

type StackError struct {
	Name string
	Err  error
}

func (s *StackError) Error() string {
	return s.Name + "\n  " + strings.ReplaceAll(s.Err.Error(), "\n", "\n  ")
}

func (s *StackError) Unwrap() error {
	return s.Err
}
