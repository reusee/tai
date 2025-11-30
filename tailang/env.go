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
		Vars: make(map[string]any),
	}

	e.Define("true", true)
	e.Define("false", false)

	e.Define("[", List{})
	e.Define("{", BlockDef{})
	e.Define("def", Def{})
	e.Define("set", Set{})
	e.Define("func", FuncDef{})
	e.Define("do", Do{})

	e.Define("if", If{})
	e.Define("while", While{})
	e.Define("switch", Switch{})
	e.Define("repeat", Repeat{})
	e.Define("foreach", Foreach{})
	e.Define("select", Select{})

	e.Define("break", Break{})
	e.Define("continue", Continue{})
	e.Define("return", Return{})
	e.Define("defer", Defer{})
	e.Define("go", Go{})

	e.Define("type", TypeOf)

	e.Define("len", Len)
	e.Define("cap", Cap)
	e.Define("make", Make)
	e.Define("new", New)
	e.Define("append", Append)
	e.Define("copy", Copy)
	e.Define("delete", Delete)
	e.Define("close", Close)
	e.Define("panic", Panic)
	e.Define("recover", Recover)
	e.Define("complex", Complex)
	e.Define("real", Real)
	e.Define("imag", Imag)
	e.Define("index", Index)
	e.Define("slice", Slice)
	e.Define("set_index", SetIndex)
	e.Define("send", Send)
	e.Define("recv", Recv)

	e.Define("+", Plus)
	e.Define("-", Minus)
	e.Define("*", Multiply)
	e.Define("/", Divide)
	e.Define("%", Mod)
	e.Define("==", Eq)
	e.Define("!=", Ne)
	e.Define("<", Lt)
	e.Define("<=", Le)
	e.Define(">", Gt)
	e.Define(">=", Ge)

	e.Define("&", BitAnd)
	e.Define("bit_or", BitOr)
	e.Define("^", BitXor)
	e.Define("&^", BitClear)
	e.Define("<<", LShift)
	e.Define(">>", RShift)
	e.Define("bit_not", BitNot)

	e.Define("!", Not)
	e.Define("&&", LogicAnd)
	e.Define("||", LogicOr)

	e.Define("int", reflect.TypeFor[int]())
	e.Define("int8", reflect.TypeFor[int8]())
	e.Define("int16", reflect.TypeFor[int16]())
	e.Define("int32", reflect.TypeFor[int32]())
	e.Define("int64", reflect.TypeFor[int64]())
	e.Define("uint", reflect.TypeFor[uint]())
	e.Define("uint8", reflect.TypeFor[uint8]())
	e.Define("uint16", reflect.TypeFor[uint16]())
	e.Define("uint32", reflect.TypeFor[uint32]())
	e.Define("uint64", reflect.TypeFor[uint64]())
	e.Define("float32", reflect.TypeFor[float32]())
	e.Define("float64", reflect.TypeFor[float64]())
	e.Define("bool", reflect.TypeFor[bool]())
	e.Define("string", reflect.TypeFor[string]())
	e.Define("byte", reflect.TypeFor[byte]())
	e.Define("rune", reflect.TypeFor[rune]())
	e.Define("any", reflect.TypeFor[any]())
	e.Define("block", reflect.TypeFor[*Block]())
	e.Define("bigint", reflect.TypeFor[*big.Int]())
	e.Define("bigfloat", reflect.TypeFor[*big.Float]())

	e.Define("slice_of", reflect.SliceOf)
	e.Define("map_of", reflect.MapOf)
	e.Define("array_of", reflect.ArrayOf)
	e.Define("chan_of", reflect.ChanOf)
	e.Define("pointer_to", reflect.PointerTo)
	e.Define("func_of", reflect.FuncOf)

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
