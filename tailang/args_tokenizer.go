package tailang

import (
	"strings"
	"unicode"
)

type ArgsTokenizer struct {
	args    []string
	idx     int
	current *Token
}

func NewArgsTokenizer(args []string) *ArgsTokenizer {
	return &ArgsTokenizer{
		args: args,
	}
}

func (t *ArgsTokenizer) Current() (*Token, error) {
	if t.current == nil {
		t.current = t.parse()
	}
	return t.current, nil
}

func (t *ArgsTokenizer) Consume() {
	t.current = nil
	t.idx++
}

func (t *ArgsTokenizer) parse() *Token {
	if t.idx >= len(t.args) {
		return &Token{Kind: TokenEOF}
	}
	txt := t.args[t.idx]

	// Symbol
	switch txt {
	case "[", "]", "(", ")", "{", "}", "|":
		return &Token{
			Kind: TokenSymbol,
			Text: txt,
			Pos:  Pos{Source: &Source{Name: "args"}},
		}
	}

	// Number
	isNumber := true
	hasDot := false
	for i, r := range txt {
		if unicode.IsDigit(r) {
			continue
		}
		if r == '.' && !hasDot && i > 0 && i < len(txt)-1 {
			hasDot = true
			continue
		}
		isNumber = false
		break
	}
	if isNumber && len(txt) > 0 {
		return &Token{
			Kind: TokenNumber,
			Text: txt,
			Pos:  Pos{Source: &Source{Name: "args"}},
		}
	}

	// NamedParam
	if strings.HasPrefix(txt, ".") && len(txt) > 1 {
		return &Token{
			Kind: TokenNamedParam,
			Text: txt,
			Pos:  Pos{Source: &Source{Name: "args"}},
		}
	}

	// Keyword
	if IsKeyword(txt) {
		return &Token{
			Kind: TokenIdentifier,
			Text: txt,
			Pos:  Pos{Source: &Source{Name: "args"}},
		}
	}

	// Default
	return &Token{
		Kind: TokenUnquotedString,
		Text: txt,
		Pos:  Pos{Source: &Source{Name: "args"}},
	}
}
