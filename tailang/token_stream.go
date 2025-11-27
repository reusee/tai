package tailang

import "fmt"

type TokenStream interface {
	Current() (*Token, error)
	Consume()
}

type SliceTokenStream struct {
	tokens []*Token
	idx    int
}

func NewSliceTokenStream(tokens []*Token) *SliceTokenStream {
	return &SliceTokenStream{
		tokens: tokens,
	}
}

func (s *SliceTokenStream) Current() (*Token, error) {
	if s.idx >= len(s.tokens) {
		return &Token{Kind: TokenEOF}, nil
	}
	return s.tokens[s.idx], nil
}

func (s *SliceTokenStream) Consume() {
	if s.idx < len(s.tokens) {
		s.idx++
	}
}

func ParseBlock(stream TokenStream) (*Block, error) {
	tok, err := stream.Current()
	if err != nil {
		return nil, err
	}
	if tok.Text != "{" {
		return nil, fmt.Errorf("expected { for block")
	}
	stream.Consume()

	return ParseBlockBody(stream)
}

func ParseBlockBody(stream TokenStream) (*Block, error) {
	var body []*Token
	depth := 1
	for depth > 0 {
		tok, err := stream.Current()
		if err != nil {
			return nil, err
		}
		if tok.Kind == TokenEOF {
			return nil, fmt.Errorf("unexpected EOF in block")
		}

		if tok.Text == "{" {
			depth++
		} else if tok.Text == "}" {
			depth--
		}

		if depth > 0 {
			body = append(body, tok)
		}
		stream.Consume()
	}
	return &Block{Body: body}, nil
}
