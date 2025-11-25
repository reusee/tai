package tailang

type Env struct {
	Parent *Env
	Vars   map[string]Value
}

func NewEnv() *Env {
	return &Env{
		Vars: map[string]Value{
			"printf": Printf{},
			"now":    Now{},
			"[":      List{},
			"join":   Join{},
			"def":    Def{},
			"func":   FuncDef{},
		},
	}
}

func (e *Env) Define(name string, val Value) {
	e.Vars[name] = val
}

func (e *Env) Lookup(name string) (Value, bool) {
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.Lookup(name)
	}
	return nil, false
}
