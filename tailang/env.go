package tailang

type Env struct {
	Parent *Env
	Vars   map[string]any
}

func NewEnv() *Env {
	e := &Env{
		Vars: map[string]any{
			"[":    List{},
			"def":  Def{},
			"func": FuncDef{},

			"+": GoFunc{
				Name: "+",
				Func: Plus,
			},
			"-": GoFunc{
				Name: "-",
				Func: Minus,
			},
			"*": GoFunc{
				Name: "*",
				Func: Multiply,
			},
			"/": GoFunc{
				Name: "/",
				Func: Divide,
			},
			"%": GoFunc{
				Name: "%",
				Func: Mod,
			},
		},
	}
	RegisterStdLib(e)
	return e
}

func (e *Env) Define(name string, val any) {
	e.Vars[name] = val
}

func (e *Env) Lookup(name string) (any, bool) {
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.Lookup(name)
	}
	return nil, false
}
