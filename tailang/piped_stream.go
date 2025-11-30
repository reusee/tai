package tailang

type PipedStream struct {
	TokenStream
	Value     any
	HasValue  bool
	PipeLast  bool
	PipeIndex int
}

func (p *PipedStream) Current() (*Token, error) {
	return p.TokenStream.Current()
}

func (p *PipedStream) Consume() {
	p.TokenStream.Consume()
}
