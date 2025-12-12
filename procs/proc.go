package procs

type Proc[C any] interface {
	Run(ctx C) (Proc[C], error)
}
