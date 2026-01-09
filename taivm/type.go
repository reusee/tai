package taivm

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

type TypeKind uint8

const (
	KindInvalid TypeKind = iota
	KindBool
	KindInt
	KindInt8
	KindInt16
	KindInt32
	KindInt64
	KindUint
	KindUint8
	KindUint16
	KindUint32
	KindUint64
	KindUintptr
	KindFloat32
	KindFloat64
	KindComplex64
	KindComplex128
	KindArray
	KindChan
	KindFunc
	KindInterface
	KindMap
	KindPtr
	KindSlice
	KindString
	KindStruct
	KindUnsafePointer
	KindExternal
)

type Type struct {
	Kind     TypeKind
	Name     string
	Elem     *Type
	Key      *Type
	Len      int
	In       []*Type
	Out      []*Type
	Variadic bool
	Methods  map[string]*Type
	Fields   []StructField
	External reflect.Type
}

type StructField struct {
	Name      string
	Type      *Type
	Tag       string
	Anonymous bool
}

var (
	typeCache   sync.Map // reflect.Type -> *Type
	internCache sync.Map // string -> *Type
)

func intern(t *Type) *Type {
	if t == nil {
		return nil
	}
	// 具名类型或带有 External 引用的类型不参与基于结构的 intern
	if t.Name != "" || t.External != nil {
		return t
	}
	key := t.String()
	if v, ok := internCache.Load(key); ok {
		return v.(*Type)
	}
	v, _ := internCache.LoadOrStore(key, t)
	return v.(*Type)
}

func PointerTo(elem *Type) *Type {
	return intern(&Type{Kind: KindPtr, Elem: elem})
}

func SliceOf(elem *Type) *Type {
	return intern(&Type{Kind: KindSlice, Elem: elem})
}

func MapOf(key, elem *Type) *Type {
	return intern(&Type{Kind: KindMap, Key: key, Elem: elem})
}

func ArrayOf(elem *Type, length int) *Type {
	return intern(&Type{Kind: KindArray, Elem: elem, Len: length})
}

func ChanOf(elem *Type) *Type {
	return intern(&Type{Kind: KindChan, Elem: elem})
}

func FuncOf(in, out []*Type, variadic bool) *Type {
	return intern(&Type{Kind: KindFunc, In: in, Out: out, Variadic: variadic})
}

func StructOf(fields []StructField) *Type {
	return intern(&Type{Kind: KindStruct, Fields: fields})
}

func FromReflectType(t reflect.Type) *Type {
	if t == nil || t.Kind() == reflect.Invalid {
		return nil
	}
	if v, ok := typeCache.Load(t); ok {
		return v.(*Type)
	}
	return fromReflectType(t, make(map[reflect.Type]*Type))
}

func fromReflectType(t reflect.Type, cache map[reflect.Type]*Type) *Type {
	if t == nil || t.Kind() == reflect.Invalid {
		return nil
	}
	if v, ok := typeCache.Load(t); ok {
		return v.(*Type)
	}

	// 匿名复合类型通过工厂方法递归规范化
	if t.Name() == "" {
		switch t.Kind() {
		case reflect.Ptr:
			return PointerTo(fromReflectType(t.Elem(), cache))
		case reflect.Slice:
			return SliceOf(fromReflectType(t.Elem(), cache))
		case reflect.Map:
			return MapOf(fromReflectType(t.Key(), cache), fromReflectType(t.Elem(), cache))
		case reflect.Array:
			return ArrayOf(fromReflectType(t.Elem(), cache), t.Len())
		case reflect.Chan:
			return ChanOf(fromReflectType(t.Elem(), cache))
		case reflect.Func:
			in := make([]*Type, t.NumIn())
			for i := 0; i < t.NumIn(); i++ {
				in[i] = fromReflectType(t.In(i), cache)
			}
			out := make([]*Type, t.NumOut())
			for i := 0; i < t.NumOut(); i++ {
				out[i] = fromReflectType(t.Out(i), cache)
			}
			return FuncOf(in, out, t.IsVariadic())
		case reflect.Struct:
			fields := make([]StructField, t.NumField())
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				fields[i] = StructField{
					Name:      f.Name,
					Type:      fromReflectType(f.Type, cache),
					Tag:       string(f.Tag),
					Anonymous: f.Anonymous,
				}
			}
			return StructOf(fields)
		}
	}

	if res, ok := cache[t]; ok {
		return res
	}

	res := &Type{
		Name:     t.Name(),
		External: t,
	}
	cache[t] = res

	kind := t.Kind()
	switch kind {
	case reflect.Bool:
		res.Kind = KindBool
	case reflect.Int:
		res.Kind = KindInt
	case reflect.Int8:
		res.Kind = KindInt8
	case reflect.Int16:
		res.Kind = KindInt16
	case reflect.Int32:
		res.Kind = KindInt32
	case reflect.Int64:
		res.Kind = KindInt64
	case reflect.Uint:
		res.Kind = KindUint
	case reflect.Uint8:
		res.Kind = KindUint8
	case reflect.Uint16:
		res.Kind = KindUint16
	case reflect.Uint32:
		res.Kind = KindUint32
	case reflect.Uint64:
		res.Kind = KindUint64
	case reflect.Uintptr:
		res.Kind = KindUintptr
	case reflect.Float32:
		res.Kind = KindFloat32
	case reflect.Float64:
		res.Kind = KindFloat64
	case reflect.Complex64:
		res.Kind = KindComplex64
	case reflect.Complex128:
		res.Kind = KindComplex128
	case reflect.Array:
		res.Kind = KindArray
		res.Len = t.Len()
		res.Elem = fromReflectType(t.Elem(), cache)
	case reflect.Chan:
		res.Kind = KindChan
		res.Elem = fromReflectType(t.Elem(), cache)
	case reflect.Func:
		res.Kind = KindFunc
		res.In = make([]*Type, t.NumIn())
		for i := 0; i < t.NumIn(); i++ {
			res.In[i] = fromReflectType(t.In(i), cache)
		}
		res.Out = make([]*Type, t.NumOut())
		for i := 0; i < t.NumOut(); i++ {
			res.Out[i] = fromReflectType(t.Out(i), cache)
		}
		res.Variadic = t.IsVariadic()
	case reflect.Interface:
		res.Kind = KindInterface
	case reflect.Map:
		res.Kind = KindMap
		res.Key = fromReflectType(t.Key(), cache)
		res.Elem = fromReflectType(t.Elem(), cache)
	case reflect.Ptr:
		res.Kind = KindPtr
		res.Elem = fromReflectType(t.Elem(), cache)
	case reflect.Slice:
		res.Kind = KindSlice
		res.Elem = fromReflectType(t.Elem(), cache)
	case reflect.String:
		res.Kind = KindString
	case reflect.Struct:
		res.Kind = KindStruct
		res.Fields = make([]StructField, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			res.Fields[i] = StructField{
				Name:      f.Name,
				Type:      fromReflectType(f.Type, cache),
				Tag:       string(f.Tag),
				Anonymous: f.Anonymous,
			}
		}
	case reflect.UnsafePointer:
		res.Kind = KindUnsafePointer
	default:
		res.Kind = KindExternal
	}

	if t.NumMethod() > 0 {
		res.Methods = make(map[string]*Type)
		for i := 0; i < t.NumMethod(); i++ {
			m := t.Method(i)
			res.Methods[m.Name] = fromReflectType(m.Type, cache)
		}
	}

	actual, _ := typeCache.LoadOrStore(t, res)
	return actual.(*Type)
}

func (t *Type) ToReflectType() reflect.Type {
	if t == nil {
		return nil
	}
	if t.External != nil {
		return t.External
	}
	switch t.Kind {
	case KindBool:
		return reflect.TypeFor[bool]()
	case KindInt:
		return reflect.TypeFor[int]()
	case KindInt8:
		return reflect.TypeFor[int8]()
	case KindInt16:
		return reflect.TypeFor[int16]()
	case KindInt32:
		return reflect.TypeFor[int32]()
	case KindInt64:
		return reflect.TypeFor[int64]()
	case KindUint:
		return reflect.TypeFor[uint]()
	case KindUint8:
		return reflect.TypeFor[uint8]()
	case KindUint16:
		return reflect.TypeFor[uint16]()
	case KindUint32:
		return reflect.TypeFor[uint32]()
	case KindUint64:
		return reflect.TypeFor[uint64]()
	case KindUintptr:
		return reflect.TypeFor[uintptr]()
	case KindFloat32:
		return reflect.TypeFor[float32]()
	case KindFloat64:
		return reflect.TypeFor[float64]()
	case KindComplex64:
		return reflect.TypeFor[complex64]()
	case KindComplex128:
		return reflect.TypeFor[complex128]()
	case KindString:
		return reflect.TypeFor[string]()
	case KindUnsafePointer:
		return reflect.TypeFor[uintptr]()
	case KindSlice:
		if t.Elem == nil {
			return reflect.TypeFor[[]any]()
		}
		return reflect.SliceOf(t.Elem.ToReflectType())
	case KindArray:
		if t.Elem == nil {
			return reflect.ArrayOf(t.Len, reflect.TypeFor[any]())
		}
		return reflect.ArrayOf(t.Len, t.Elem.ToReflectType())
	case KindMap:
		if t.Key == nil || t.Elem == nil {
			return reflect.TypeFor[map[any]any]()
		}
		return reflect.MapOf(t.Key.ToReflectType(), t.Elem.ToReflectType())
	case KindPtr:
		if t.Elem == nil {
			return reflect.TypeFor[uintptr]()
		}
		return reflect.PointerTo(t.Elem.ToReflectType())
	case KindFunc:
		in := make([]reflect.Type, len(t.In))
		for i, v := range t.In {
			in[i] = v.ToReflectType()
		}
		out := make([]reflect.Type, len(t.Out))
		for i, v := range t.Out {
			out[i] = v.ToReflectType()
		}
		return reflect.FuncOf(in, out, t.Variadic)
	}
	return nil
}

func (t *Type) AssignableTo(u *Type) bool {
	if t == nil || u == nil {
		return t == u
	}
	if t.External != nil && u.External != nil {
		return t.External.AssignableTo(u.External)
	}
	rt := t.ToReflectType()
	ru := u.ToReflectType()
	if rt == nil || ru == nil {
		return false
	}
	return rt.AssignableTo(ru)
}

func (t *Type) Zero() any {
	if t == nil {
		return nil
	}
	if t.External != nil {
		return reflect.Zero(t.External).Interface()
	}
	switch t.Kind {
	case KindPtr, KindSlice, KindMap, KindFunc, KindInterface, KindChan:
		return nil
	case KindStruct:
		return &Struct{TypeName: t.Name, Fields: make(map[string]any)}
	}
	rt := t.ToReflectType()
	if rt == nil {
		return nil
	}
	return reflect.Zero(rt).Interface()
}

func (t *Type) Match(val any) bool {
	if t == nil {
		return val == nil
	}
	if t.External != nil {
		if val == nil {
			k := t.External.Kind()
			return k == reflect.Interface || k == reflect.Ptr || k == reflect.Slice || k == reflect.Map || k == reflect.Func || k == reflect.Chan
		}
		return reflect.TypeOf(val).AssignableTo(t.External)
	}
	if val == nil {
		switch t.Kind {
		case KindPtr, KindSlice, KindMap, KindFunc, KindInterface, KindChan:
			return true
		}
		return false
	}
	if t.Kind == KindStruct {
		if s, ok := val.(*Struct); ok {
			if t.Name != "" && s.TypeName != "" && t.Name != s.TypeName {
				return false
			}
			return true
		}
	}
	rt := t.ToReflectType()
	if rt == nil {
		return false
	}
	return reflect.TypeOf(val).AssignableTo(rt)
}

func (t *Type) String() string {
	if t == nil {
		return "nil"
	}
	if t.Name != "" {
		return t.Name
	}
	if t.External != nil {
		return t.External.String()
	}
	switch t.Kind {
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	case KindInt8:
		return "int8"
	case KindInt16:
		return "int16"
	case KindInt32:
		return "int32"
	case KindInt64:
		return "int64"
	case KindUint:
		return "uint"
	case KindUint8:
		return "uint8"
	case KindUint16:
		return "uint16"
	case KindUint32:
		return "uint32"
	case KindUint64:
		return "uint64"
	case KindUintptr:
		return "uintptr"
	case KindFloat32:
		return "float32"
	case KindFloat64:
		return "float64"
	case KindComplex64:
		return "complex64"
	case KindComplex128:
		return "complex128"
	case KindString:
		return "string"
	case KindUnsafePointer:
		return "unsafe.Pointer"
	case KindChan:
		return "chan " + t.Elem.String()
	case KindPtr:
		return "*" + t.Elem.String()
	case KindSlice:
		return "[]" + t.Elem.String()
	case KindArray:
		return fmt.Sprintf("[%d]%s", t.Len, t.Elem.String())
	case KindMap:
		return fmt.Sprintf("map[%s]%s", t.Key.String(), t.Elem.String())
	case KindFunc:
		return t.funcString()
	case KindInterface:
		return t.interfaceString()
	case KindStruct:
		var sb strings.Builder
		sb.WriteString("struct { ")
		for i, f := range t.Fields {
			if i > 0 {
				sb.WriteString("; ")
			}
			if !f.Anonymous {
				sb.WriteString(f.Name)
				sb.WriteString(" ")
			}
			sb.WriteString(f.Type.String())
			if f.Tag != "" {
				sb.WriteString(fmt.Sprintf(" %q", f.Tag))
			}
		}
		sb.WriteString(" }")
		return sb.String()
	}
	return "unknown"
}

func (t *Type) funcString() string {
	var sb strings.Builder
	sb.WriteString("func(")
	for i, v := range t.In {
		if i > 0 {
			sb.WriteString(", ")
		}
		if t.Variadic && i == len(t.In)-1 {
			sb.WriteString("...")
			if v != nil && v.Kind == KindSlice {
				sb.WriteString(v.Elem.String())
			} else {
				sb.WriteString(v.String())
			}
		} else {
			sb.WriteString(v.String())
		}
	}
	sb.WriteString(")")
	t.appendResults(&sb)
	return sb.String()
}

func (t *Type) appendResults(sb *strings.Builder) {
	if len(t.Out) == 1 {
		sb.WriteString(" ")
		sb.WriteString(t.Out[0].String())
	} else if len(t.Out) > 1 {
		sb.WriteString(" (")
		for i, v := range t.Out {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(v.String())
		}
		sb.WriteString(")")
	}
}

func (t *Type) interfaceString() string {
	if len(t.Methods) == 0 {
		return "interface{}"
	}
	var names []string
	for n := range t.Methods {
		names = append(names, n)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("interface { ")
	for i, n := range names {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(n)
		sb.WriteString(strings.TrimPrefix(t.Methods[n].String(), "func"))
	}
	sb.WriteString(" }")
	return sb.String()
}

