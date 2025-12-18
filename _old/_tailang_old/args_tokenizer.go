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
	hasExp := false
	hasDigit := false
	start := 0
	if len(txt) > 0 && (txt[0] == '-' || txt[0] == '+') {
		start = 1
	}

	if start == len(txt) {
		isNumber = false
	} else {
		for i := start; i < len(txt); i++ {
			r := rune(txt[i])
			if r == '_' {
				continue
			}
			if unicode.IsDigit(r) {
				hasDigit = true
				continue
			}
			if r == '.' && !hasDot && !hasExp {
				hasDot = true
				continue
			}
			if (r == 'e' || r == 'E') && !hasExp {
				hasExp = true
				if i+1 < len(txt) && (txt[i+1] == '+' || txt[i+1] == '-') {
					i++
				}
				continue
			}
			isNumber = false
			break
		}
	}

	if isNumber && len(txt) > 0 && hasDigit {
		text := strings.ReplaceAll(txt, "_", "")
		val, err := parseNumberValue(text)
		if err == nil {
			return &Token{
				Kind:  TokenNumber,
				Text:  text,
				Pos:   Pos{Source: &Source{Name: "args"}},
				Value: val,
			}
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
