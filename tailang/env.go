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
			"set":  Set{},
			"func": FuncDef{},

			"if":      If{},
			"while":   While{},
			"switch":  Switch{},
			"repeat":  Repeat{},
			"foreach": Foreach{},

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

			"==": GoFunc{
				Name: "==",
				Func: Eq,
			},
			"!=": GoFunc{
				Name: "!=",
				Func: Ne,
			},
			"<": GoFunc{
				Name: "<",
				Func: Lt,
			},
			"<=": GoFunc{
				Name: "<=",
				Func: Le,
			},
			">": GoFunc{
				Name: ">",
				Func: Gt,
			},
			">=": GoFunc{
				Name: ">=",
				Func: Ge,
			},

			"int":     reflect.TypeFor[int](),
			"int8":    reflect.TypeFor[int8](),
			"int16":   reflect.TypeFor[int16](),
			"int32":   reflect.TypeFor[int32](),
			"int64":   reflect.TypeFor[int64](),
			"uint":    reflect.TypeFor[uint](),
			"uint8":   reflect.TypeFor[uint8](),
			"uint16":  reflect.TypeFor[uint16](),
			"uint32":  reflect.TypeFor[uint32](),
			"uint64":  reflect.TypeFor[uint64](),
			"float32": reflect.TypeFor[float32](),
			"float64": reflect.TypeFor[float64](),
			"bool":    reflect.TypeFor[bool](),
			"string":  reflect.TypeFor[string](),
			"byte":    reflect.TypeFor[byte](),
			"rune":    reflect.TypeFor[rune](),
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
