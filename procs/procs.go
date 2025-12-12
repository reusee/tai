package procs

type Procs[C any] []Proc[C]

var _ Proc[any] = Procs[any]{}

func (p Procs[C]) Run(ctx C) (Proc[C], error) {
	if len(p) == 0 {
		return nil, nil
	}
	proc, err := p[0].Run(ctx)
	if err != nil {
		return nil, err
	}
	if proc == nil {
		return p[1:], nil
	}
	p[0] = proc
	return p, nil
}
