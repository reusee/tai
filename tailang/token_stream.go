package tailang

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
