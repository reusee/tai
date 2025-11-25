package tailang

import "io"

type Tokenizer struct {
	source  io.Reader
	current *Token
}

func (t *Tokenizer) Current() (*Token, error) {
	if t.current == nil {
		//TODO parse next
	}
	return t.current, nil
}

func (t *Tokenizer) Consume() {
	t.current = nil
}
