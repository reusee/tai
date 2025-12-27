package taivm

type Env struct {
	Parent *Env
	Vars   map[string]any
}

func (e *Env) Get(name string) (any, bool) {
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	return nil, false
}

func (e *Env) Def(name string, val any) {
	if e.Vars == nil {
		e.Vars = make(map[string]any)
	}
	e.Vars[name] = val
}

func (e *Env) Set(name string, val any) bool {
	if _, ok := e.Vars[name]; ok {
		e.Vars[name] = val
		return true
	}
	if e.Parent != nil {
		return e.Parent.Set(name, val)
	}
	return false
}

func (e *Env) NewChild() *Env {
	return &Env{
		Parent: e,
	}
}
