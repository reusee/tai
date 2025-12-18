package taivm

type Closure struct {
	Fun *Function
	Env *Env
}
