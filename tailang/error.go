package tailang

import (
	"fmt"
	"strings"
)

type StackError struct {
	Name string
	Args []any
	Err  error
}

func (s *StackError) Error() string {
	var sb strings.Builder
	sb.WriteString(s.Name)
	if len(s.Args) > 0 {
		sb.WriteString("(")
		for i, arg := range s.Args {
			if i > 0 {
				sb.WriteString(" ")
			}
			fmt.Fprintf(&sb, "%+v", arg)
		}
		sb.WriteString(")")
	}
	sb.WriteString("\n  ")
	sb.WriteString(strings.ReplaceAll(s.Err.Error(), "\n", "\n  "))
	return sb.String()
}

func (s *StackError) Unwrap() error {
	return s.Err
}
