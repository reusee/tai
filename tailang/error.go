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
		runes := []rune(line)
		col := p.Pos.Column - 1
		for i, r := range runes {
			if i >= col {
				break
			}
			if r == '\t' {
				sb.WriteString("\t")
			} else {
				w := runeWidth(r)
				for k := 0; k < w; k++ {
					sb.WriteString(" ")
				}
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

func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if r >= 0x1100 &&
		(r <= 0x115f || r == 0x2329 || r == 0x232a ||
			(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
			(r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe10 && r <= 0xfe19) ||
			(r >= 0xfe30 && r <= 0xfe6f) ||
			(r >= 0xff00 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6)) {
		return 2
	}
	return 1
}
