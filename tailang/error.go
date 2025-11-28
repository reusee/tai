package tailang

import (
	"fmt"
	"strings"
)

type PosError struct {
	Err error
	Pos Pos
}

func (p PosError) Error() string {
	if p.Pos.Source == nil {
		return p.Err.Error()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s at %s:%d:%d\n", p.Err.Error(), p.Pos.Source.Name, p.Pos.Line, p.Pos.Column))

	// Line content
	lines := p.Pos.Source.Lines
	idx := p.Pos.Line - 1
	if idx >= 0 && idx < len(lines) {
		line := lines[idx]
		sb.WriteString(line)
		sb.WriteString("\n")

		// Caret
		pad := p.Pos.Column - 1
		if pad < 0 {
			pad = 0
		}
		for i := 0; i < pad; i++ {
			if i < len(line) && line[i] == '\t' {
				sb.WriteString("\t")
			} else {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("^\n")
	}

	return sb.String()
}

func (p PosError) Unwrap() error {
	return p.Err
}

func WithPos(err error, pos Pos) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(PosError); ok {
		return err
	}
	return PosError{
		Err: err,
		Pos: pos,
	}
}
