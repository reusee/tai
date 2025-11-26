package tailang

import "reflect"

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

			"type": GoFunc{
				Name: "type",
				Func: TypeOf,
			},

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

			"int":     reflect.TypeFor[int](),
			"float64": reflect.TypeFor[float64](),
			"bool":    reflect.TypeFor[bool](),
			"string":  reflect.TypeFor[string](),
			"any":     reflect.TypeFor[any](),
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
