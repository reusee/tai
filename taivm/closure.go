package taivm

type Closure struct {
	Fun         *Function
	Env         *Env
	ParamSyms   []Symbol
	MaxParamSym int
	Defaults    []any
}
