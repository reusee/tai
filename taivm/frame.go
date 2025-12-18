package taivm

type Frame struct {
	Fun      *Function
	ReturnIP int
	Env      *Env
	BaseSP   int
	BP       int
}
