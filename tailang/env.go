package tailang

import (
	"bytes"
	"math/big"
	"reflect"
	"runtime"
	"strings"
	"unicode"
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

	// Reflect
	for name, fn := range map[string]any{
		"slice_of":   reflect.SliceOf,
		"map_of":     reflect.MapOf,
		"array_of":   reflect.ArrayOf,
		"chan_of":    reflect.ChanOf,
		"pointer_to": reflect.PointerTo,
		"func_of":    reflect.FuncOf,
	} {
		e.Define(name, GoFunc{
			Name: name,
			Func: fn,
		})
	}
	e.Define("recv_dir", reflect.RecvDir)
	e.Define("send_dir", reflect.SendDir)
	e.Define("both_dir", reflect.BothDir)

	RegisterStdLib(e)
	return e
}

func (e *Env) Define(name string, val any) {
	if val != nil && reflect.TypeOf(val).Kind() == reflect.Func {
		if _, ok := val.(Function); !ok {
			val = GoFunc{
				Name: funcName(val),
				Func: val,
			}
		}
	}
	e.Vars[name] = val
}

func funcName(fn any) string {
	v := reflect.ValueOf(fn)
	fullName := runtime.FuncForPC(v.Pointer()).Name()
	parts := strings.Split(fullName, "/")
	last := parts[len(parts)-1]
	dotParts := strings.Split(last, ".")
	if len(dotParts) < 2 {
		return ""
	}
	pkg := dotParts[len(dotParts)-2]
	name := dotParts[len(dotParts)-1]
	return pkg + "." + toSnake(name)
}

func toSnake(s string) string {
	var buf bytes.Buffer
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 &&
				(unicode.IsLower(rune(s[i-1])) ||
					(i+1 < len(s) && unicode.IsLower(rune(s[i+1])))) {
				buf.WriteRune('_')
			}
			buf.WriteRune(unicode.ToLower(r))
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
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
