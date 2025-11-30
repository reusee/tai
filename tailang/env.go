package tailang

import (
	"math/big"
	"reflect"
)

type Env struct {
	Parent      *Env
	Vars        map[string]any
	Defers      []func()
	IsFuncFrame bool
}

func NewEnv() *Env {
	e := &Env{
		Vars: map[string]any{
			"true":  true,
			"false": false,

			"[":    List{},
			"{":    BlockDef{},
			"def":  Def{},
			"set":  Set{},
			"func": FuncDef{},
			"do":   Do{},

			"if":      If{},
			"while":   While{},
			"switch":  Switch{},
			"repeat":  Repeat{},
			"foreach": Foreach{},
			"select":  Select{},

			"break":    Break{},
			"continue": Continue{},
			"return":   Return{},
			"defer":    Defer{},
			"go":       Go{},

			"type": GoFunc{
				Name: "type",
				Func: TypeOf,
			},

			"len":       Len,
			"cap":       Cap,
			"make":      Make,
			"new":       New,
			"append":    Append,
			"copy":      Copy,
			"delete":    Delete,
			"close":     Close,
			"panic":     Panic,
			"recover":   Recover,
			"complex":   Complex,
			"real":      Real,
			"imag":      Imag,
			"index":     Index,
			"slice":     Slice,
			"set_index": SetIndex,
			"send":      Send,
			"recv":      Recv,
		},
	}

	// Ops
	for name, fn := range map[string]any{
		"+":  Plus,
		"-":  Minus,
		"*":  Multiply,
		"/":  Divide,
		"%":  Mod,
		"==": Eq,
		"!=": Ne,
		"<":  Lt,
		"<=": Le,
		">":  Gt,
		">=": Ge,

		"&":       BitAnd,
		"bit_or":  BitOr,
		"^":       BitXor,
		"&^":      BitClear,
		"<<":      LShift,
		">>":      RShift,
		"bit_not": BitNot,

		"!":  Not,
		"&&": LogicAnd,
		"||": LogicOr,
	} {
		e.Define(name, GoFunc{
			Name: name,
			Func: fn,
		})
	}

	// Types
	for name, t := range map[string]reflect.Type{
		"int":      reflect.TypeFor[int](),
		"int8":     reflect.TypeFor[int8](),
		"int16":    reflect.TypeFor[int16](),
		"int32":    reflect.TypeFor[int32](),
		"int64":    reflect.TypeFor[int64](),
		"uint":     reflect.TypeFor[uint](),
		"uint8":    reflect.TypeFor[uint8](),
		"uint16":   reflect.TypeFor[uint16](),
		"uint32":   reflect.TypeFor[uint32](),
		"uint64":   reflect.TypeFor[uint64](),
		"float32":  reflect.TypeFor[float32](),
		"float64":  reflect.TypeFor[float64](),
		"bool":     reflect.TypeFor[bool](),
		"string":   reflect.TypeFor[string](),
		"byte":     reflect.TypeFor[byte](),
		"rune":     reflect.TypeFor[rune](),
		"any":      reflect.TypeFor[any](),
		"block":    reflect.TypeFor[*Block](),
		"bigint":   reflect.TypeFor[*big.Int](),
		"bigfloat": reflect.TypeFor[*big.Float](),
	} {
		e.Define(name, t)
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

func (e *Env) NewScope() *Env {
	return &Env{
		Parent: e,
		Vars:   make(map[string]any),
	}
}

func IsKeyword(name string) bool {
	switch name {
	case "def", "set", "func", "if", "else", "do", "while",
		"switch", "repeat", "foreach", "true", "false", "nil",
		"break", "continue", "return", "default", "end":
		return true
	}
	return false
}
