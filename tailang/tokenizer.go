package tailang

import (
	"bufio"
	"bytes"
	"io"
	"unicode"
)

type Tokenizer struct {
	source  *bufio.Reader
	current *Token
}

func NewTokenizer(source io.Reader) *Tokenizer {
	return &Tokenizer{
		source: bufio.NewReader(source),
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

	r, _, err := t.source.ReadRune()
	if err == io.EOF {
		return &Token{Kind: TokenEOF}, nil
	}
	if err != nil {
		return nil, err
	}

	switch {
	case r == '.':
		return t.parseNamedParam()
	case r == '\'' || r == '"' || r == '`':
		return t.parseString(r)
	case unicode.IsDigit(r):
		t.source.UnreadRune()
		return t.parseNumber()
	case r == '[' || r == ']':
		return &Token{
			Kind: TokenSymbol,
			Text: string(r),
		}, nil
	}

	if unicode.IsGraphic(r) {
		t.source.UnreadRune()
		return t.parseIdentifier()
	}

	return &Token{Kind: TokenInvalid, Text: string(r)}, nil
}

func (t *Tokenizer) skipWhitespace() {
	for {
		r, _, err := t.source.ReadRune()
		if err != nil {
			return
		}
		if !unicode.IsSpace(r) {
			t.source.UnreadRune()
			return
		}
	}
}

func (t *Tokenizer) parseIdentifier() (*Token, error) {
	var buf bytes.Buffer
	for {
		r, _, err := t.source.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if unicode.IsSpace(r) || r == '[' || r == ']' || r == '\'' || r == '"' || r == '`' {
			t.source.UnreadRune()
			break
		}
		buf.WriteRune(r)
	}
	return &Token{
		Kind: TokenIdentifier,
		Text: buf.String(),
	}, nil
}

func (t *Tokenizer) parseNamedParam() (*Token, error) {
	ident, err := t.parseIdentifier()
	if err != nil {
		return nil, err
	}
	return &Token{
		Kind: TokenNamedParam,
		Text: "." + ident.Text,
	}, nil
}

func (t *Tokenizer) parseNumber() (*Token, error) {
	var buf bytes.Buffer
	hasDot := false
	for {
		r, _, err := t.source.ReadRune()
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
			t.source.UnreadRune()
			break
		}
	}
	return &Token{
		Kind: TokenNumber,
		Text: buf.String(),
	}, nil
}

func (t *Tokenizer) parseString(quote rune) (*Token, error) {
	var buf bytes.Buffer
	for {
		r, _, err := t.source.ReadRune()
		if err == io.EOF {
			// Unmatched quote
			return &Token{Kind: TokenInvalid, Text: buf.String()}, nil
		}
		if err != nil {
			return nil, err
		}
		if r == quote {
			break
		}
		buf.WriteRune(r)
	}
	return &Token{
		Kind: TokenString,
		Text: buf.String(),
	}, nil
}
