package tailang

import (
	"bytes"
	"io"
	"unicode"
)

type Tokenizer struct {
	source  *Source
	runes   []rune
	cursor  int
	current *Token

	currPos Pos
	prevPos Pos
}

func NewTokenizer(source io.Reader) *Tokenizer {
	content, err := io.ReadAll(source)
	if err != nil {
		panic(err)
	}
	src := NewSource("", string(content))
	return &Tokenizer{
		source: src,
		runes:  []rune(src.Content),
		currPos: Pos{
			Source: src,
			Line:   1,
			Column: 1,
		},
	}
}

func (t *Tokenizer) readRune() (rune, error) {
	if t.cursor >= len(t.runes) {
		return 0, io.EOF
	}
	r := t.runes[t.cursor]
	t.cursor++

	t.prevPos = t.currPos
	if r == '\n' {
		t.currPos.Line++
		t.currPos.Column = 1
	} else {
		t.currPos.Column++
	}

	return r, nil
}

func (t *Tokenizer) unreadRune() {
	if t.cursor > 0 {
		t.cursor--
		r := t.runes[t.cursor]
		if r == '\n' {
			t.currPos.Line--
			col := 1
			for i := t.cursor - 1; i >= 0; i-- {
				if t.runes[i] == '\n' {
					break
				}
				col++
			}
			t.currPos.Column = col
		} else {
			t.currPos.Column--
		}
	}
}

func (t *Tokenizer) Current() (*Token, error) {
	if t.current == nil {
		var err error
		t.current, err = t.parseNext()
		if err != nil {
			return nil, err
		}
	}
	return t.current, nil
}

func (t *Tokenizer) Consume() {
	t.current = nil
}

func (t *Tokenizer) parseNext() (*Token, error) {
	t.skipWhitespace()
	startPos := t.currPos

	r, err := t.readRune()
	if err == io.EOF {
		return &Token{Kind: TokenEOF, Pos: startPos}, nil
	}
	if err != nil {
		return nil, err
	}

	switch {
	case r == '#':
		t.skipComment()
		return t.parseNext()
	case r == '.':
		return t.parseNamedParam(startPos)
	case r == '\'' || r == '"' || r == '`':
		return t.parseString(r, startPos)
	case unicode.IsDigit(r):
		t.unreadRune()
		return t.parseNumber()
	case r == '[' || r == ']' || r == '(' || r == ')' || r == '{' || r == '}' || r == '|':
		return &Token{
			Kind: TokenSymbol,
			Text: string(r),
			Pos:  startPos,
		}, nil
	}

	if unicode.IsGraphic(r) {
		t.unreadRune()
		return t.parseIdentifier()
	}

	return &Token{Kind: TokenInvalid, Text: string(r), Pos: startPos}, nil
}

func (t *Tokenizer) skipWhitespace() {
	for {
		r, err := t.readRune()
		if err != nil {
			return
		}
		if !unicode.IsSpace(r) {
			t.unreadRune()
			return
		}
	}
}

func (t *Tokenizer) skipComment() {
	for {
		r, err := t.readRune()
		if err != nil {
			return
		}
		if r == '\n' {
			return
		}
	}
}

func (t *Tokenizer) parseIdentifier() (*Token, error) {
	startPos := t.currPos
	var buf bytes.Buffer
	for {
		r, err := t.readRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if unicode.IsSpace(r) || isDelimiter(r) {
			t.unreadRune()
			break
		}
		buf.WriteRune(r)
	}
	return &Token{
		Kind: TokenIdentifier,
		Text: buf.String(),
		Pos:  startPos,
	}, nil
}

func isDelimiter(r rune) bool {
	switch r {
	case '[', ']', '(', ')', '{', '}', '\'', '"', '`', '|':
		return true
	}
	return false
}

func (t *Tokenizer) parseNamedParam(startPos Pos) (*Token, error) {
	ident, err := t.parseIdentifier()
	if err != nil {
		return nil, err
	}
	return &Token{
		Kind: TokenNamedParam,
		Text: "." + ident.Text,
		Pos:  startPos,
	}, nil
}

func (t *Tokenizer) parseNumber() (*Token, error) {
	startPos := t.currPos
	var buf bytes.Buffer
	hasDot := false
	for {
		r, err := t.readRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if unicode.IsDigit(r) {
			buf.WriteRune(r)
		} else if r == '.' && !hasDot {
			hasDot = true
			buf.WriteRune(r)
		} else {
			t.unreadRune()
			break
		}
	}
	return &Token{
		Kind: TokenNumber,
		Text: buf.String(),
		Pos:  startPos,
	}, nil
}

func (t *Tokenizer) parseString(quote rune, startPos Pos) (*Token, error) {
	var buf bytes.Buffer
	for {
		r, err := t.readRune()
		if err == io.EOF {
			// Unmatched quote
			return &Token{Kind: TokenInvalid, Text: buf.String(), Pos: startPos}, nil
		}
		if err != nil {
			return nil, err
		}
		if r == quote {
			break
		}

		if quote != '`' && r == '\\' {
			next, err := t.readRune()
			if err == io.EOF {
				buf.WriteRune(r)
				break
			}
			if err != nil {
				return nil, err
			}
			switch next {
			case 'n':
				buf.WriteRune('\n')
			case 'r':
				buf.WriteRune('\r')
			case 't':
				buf.WriteRune('\t')
			case '\\':
				buf.WriteRune('\\')
			case '"':
				buf.WriteRune('"')
			case '\'':
				buf.WriteRune('\'')
			default:
				buf.WriteRune('\\')
				buf.WriteRune(next)
			}
		} else {
			buf.WriteRune(r)
		}
	}
	return &Token{
		Kind: TokenString,
		Text: buf.String(),
		Pos:  startPos,
	}, nil
}
