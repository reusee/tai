package tailang

type PipedStream struct {
	TokenStream
	Value    any
	HasValue bool
}

func (p *PipedStream) Current() (*Token, error) {
	return p.TokenStream.Current()
}

func (p *PipedStream) Consume() {
	p.TokenStream.Consume()
}
