package taivm

type Frame struct {
	Fun      *Function
	ReturnIP int
	Env      *Env
	BaseSP   int
	BP       int
	Defers   []*Closure // stack of deferred functions for this frame
}
