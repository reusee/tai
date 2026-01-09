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

// Type is an interface representing a type, like reflect.Type.
// Type values can be compared with == to check type identity.
type Type interface {
	Kind() TypeKind
	Name() string
	Elem() Type
	Key() Type
	Len() int
	In() []Type
	Out() []Type
	Variadic() bool
	Methods() map[string]Type
	Fields() []StructField
	ToReflectType() reflect.Type
	AssignableTo(u Type) bool
	Zero() any
	Match(val any) bool
	String() string
	private() // to prevent external implementations
}

type typeImpl struct {
	kind     TypeKind
	name     string
	elem     Type
	key      Type
	len      int
	in       []Type
	out      []Type
	variadic bool
	methods  map[string]Type
	fields   []StructField
	external reflect.Type
}

func (t *typeImpl) Kind() TypeKind     { return t.kind }
func (t *typeImpl) Name() string        { return t.name }
func (t *typeImpl) Elem() Type          { return t.elem }
func (t *typeImpl) Key() Type           { return t.key }
func (t *typeImpl) Len() int            { return t.len }
func (t *typeImpl) In() []Type          { return t.in }
func (t *typeImpl) Out() []Type         { return t.out }
func (t *typeImpl) Variadic() bool      { return t.variadic }
func (t *typeImpl) Methods() map[string]Type {
	if t.methods == nil {
		return nil
	}
	// Return a copy to prevent modification
	m := make(map[string]Type, len(t.methods))
	for k, v := range t.methods {
		m[k] = v
	}
	return m
}
func (t *typeImpl) Fields() []StructField { return t.fields }
func (t *typeImpl) private()            {}

func (t *typeImpl) getExternal() reflect.Type { return t.external }

type StructField struct {
	Name      string
	Type      Type
	Tag       string
	Anonymous bool
}

var typeCache = sync.Map{} // map[reflect.Type]Type

func FromReflectType(rt reflect.Type) Type {
	if rt == nil || rt.Kind() == reflect.Invalid {
		return nil
	}
	// Check cache first
	if cached, ok := typeCache.Load(rt); ok {
		return cached.(Type)
	}
	return fromReflectType(rt, make(map[reflect.Type]Type))
}

func fromReflectType(rt reflect.Type, cache map[reflect.Type]Type) Type {
	if rt == nil || rt.Kind() == reflect.Invalid {
		return nil
	}
	if res, ok := cache[rt]; ok {
		return res
	}

	// Check global cache
	if cached, ok := typeCache.Load(rt); ok {
		cache[rt] = cached.(Type)
		return cached.(Type)
	}

	impl := &typeImpl{
		name:     rt.Name(),
		external: rt,
	}
	cache[rt] = impl

	kind := rt.Kind()
	switch kind {
	case reflect.Bool:
		impl.kind = KindBool
	case reflect.Int:
		impl.kind = KindInt
	case reflect.Int8:
		impl.kind = KindInt8
	case reflect.Int16:
		impl.kind = KindInt16
	case reflect.Int32:
		impl.kind = KindInt32
	case reflect.Int64:
		impl.kind = KindInt64
	case reflect.Uint:
		impl.kind = KindUint
	case reflect.Uint8:
		impl.kind = KindUint8
	case reflect.Uint16:
		impl.kind = KindUint16
	case reflect.Uint32:
		impl.kind = KindUint32
	case reflect.Uint64:
		impl.kind = KindUint64
	case reflect.Uintptr:
		impl.kind = KindUintptr
	case reflect.Float32:
		impl.kind = KindFloat32
	case reflect.Float64:
		impl.kind = KindFloat64
	case reflect.Complex64:
		impl.kind = KindComplex64
	case reflect.Complex128:
		impl.kind = KindComplex128
	case reflect.Array:
		impl.kind = KindArray
		impl.len = rt.Len()
		impl.elem = fromReflectType(rt.Elem(), cache)
	case reflect.Chan:
		impl.kind = KindChan
		impl.elem = fromReflectType(rt.Elem(), cache)
	case reflect.Func:
		impl.kind = KindFunc
		impl.in = make([]Type, rt.NumIn())
		for i := 0; i < rt.NumIn(); i++ {
			impl.in[i] = fromReflectType(rt.In(i), cache)
		}
		impl.out = make([]Type, rt.NumOut())
		for i := 0; i < rt.NumOut(); i++ {
			impl.out[i] = fromReflectType(rt.Out(i), cache)
		}
		impl.variadic = rt.IsVariadic()
	case reflect.Interface:
		impl.kind = KindInterface
	case reflect.Map:
		impl.kind = KindMap
		impl.key = fromReflectType(rt.Key(), cache)
		impl.elem = fromReflectType(rt.Elem(), cache)
	case reflect.Ptr:
		impl.kind = KindPtr
		impl.elem = fromReflectType(rt.Elem(), cache)
	case reflect.Slice:
		impl.kind = KindSlice
		impl.elem = fromReflectType(rt.Elem(), cache)
	case reflect.String:
		impl.kind = KindString
	case reflect.Struct:
		impl.kind = KindStruct
		impl.fields = make([]StructField, rt.NumField())
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			impl.fields[i] = StructField{
				Name:      f.Name,
				Type:      fromReflectType(f.Type, cache),
				Tag:       string(f.Tag),
				Anonymous: f.Anonymous,
			}
		}
	case reflect.UnsafePointer:
		impl.kind = KindUnsafePointer
	default:
		impl.kind = KindExternal
	}

	if rt.NumMethod() > 0 {
		impl.methods = make(map[string]Type)
		for i := 0; i < rt.NumMethod(); i++ {
			m := rt.Method(i)
			impl.methods[m.Name] = fromReflectType(m.Type, cache)
		}
	}

	// Store in global cache
	typeCache.Store(rt, impl)

	return impl
}

func (t *typeImpl) ToReflectType() reflect.Type {
	if t == nil {
		return nil
	}
	if t.external != nil {
		return t.external
	}
	switch t.kind {
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
		if t.elem == nil {
			return reflect.TypeFor[[]any]()
		}
		return reflect.SliceOf(t.elem.ToReflectType())
	case KindArray:
		if t.elem == nil {
			return reflect.ArrayOf(t.len, reflect.TypeFor[any]())
		}
		return reflect.ArrayOf(t.len, t.elem.ToReflectType())
	case KindMap:
		if t.key == nil || t.elem == nil {
			return reflect.TypeFor[map[any]any]()
		}
		return reflect.MapOf(t.key.ToReflectType(), t.elem.ToReflectType())
	case KindPtr:
		if t.elem == nil {
			return reflect.TypeFor[uintptr]()
		}
		return reflect.PointerTo(t.elem.ToReflectType())
	case KindFunc:
		in := make([]reflect.Type, len(t.in))
		for i, v := range t.in {
			in[i] = v.ToReflectType()
		}
		out := make([]reflect.Type, len(t.out))
		for i, v := range t.out {
			out[i] = v.ToReflectType()
		}
		return reflect.FuncOf(in, out, t.variadic)
	}
	return nil
}

func (t *typeImpl) AssignableTo(u Type) bool {
	if t == nil {
		return u == nil
	}
	if u == nil {
		return false
	}
	if ut, ok := u.(*typeImpl); ok {
		if t.external != nil && ut.external != nil {
			return t.external.AssignableTo(ut.external)
		}
	}
	rt := t.ToReflectType()
	ru := u.ToReflectType()
	if rt == nil || ru == nil {
		return false
	}
	return rt.AssignableTo(ru)
}

func (t *typeImpl) Zero() any {
	if t == nil {
		return nil
	}
	if t.external != nil {
		return reflect.Zero(t.external).Interface()
	}
	switch t.kind {
	case KindPtr, KindSlice, KindMap, KindFunc, KindInterface, KindChan:
		return nil
	case KindStruct:
		return &Struct{TypeName: t.name, Fields: make(map[string]any)}
	}
	rt := t.ToReflectType()
	if rt == nil {
		return nil
	}
	return reflect.Zero(rt).Interface()
}

func (t *typeImpl) Match(val any) bool {
	if t == nil {
		return val == nil
	}
	if t.external != nil {
		if val == nil {
			k := t.external.Kind()
			return k == reflect.Interface || k == reflect.Ptr || k == reflect.Slice || k == reflect.Map || k == reflect.Func || k == reflect.Chan
		}
		return reflect.TypeOf(val).AssignableTo(t.external)
	}
	if val == nil {
		switch t.kind {
		case KindPtr, KindSlice, KindMap, KindFunc, KindInterface, KindChan:
			return true
		}
		return false
	}
	if t.kind == KindStruct {
		if s, ok := val.(*Struct); ok {
			if t.name != "" && s.TypeName != "" && t.name != s.TypeName {
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

func (t *typeImpl) String() string {
	if t == nil {
		return "nil"
	}
	if t.name != "" {
		return t.name
	}
	if t.external != nil {
		return t.external.String()
	}
	switch t.kind {
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
		return "chan " + t.elem.String()
	case KindPtr:
		return "*" + t.elem.String()
	case KindSlice:
		return "[]" + t.elem.String()
	case KindArray:
		return fmt.Sprintf("[%d]%s", t.len, t.elem.String())
	case KindMap:
		return fmt.Sprintf("map[%s]%s", t.key.String(), t.elem.String())
	case KindFunc:
		return t.funcString()
	case KindInterface:
		return t.interfaceString()
	case KindStruct:
		return "struct{...}"
	}
	return "unknown"
}

func (t *typeImpl) funcString() string {
	var sb strings.Builder
	sb.WriteString("func(")
	for i, v := range t.in {
		if i > 0 {
			sb.WriteString(", ")
		}
		if t.variadic && i == len(t.in)-1 {
			sb.WriteString("...")
			if v != nil && v.Kind() == KindSlice {
				sb.WriteString(v.Elem().String())
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

func (t *typeImpl) appendResults(sb *strings.Builder) {
	if len(t.out) == 1 {
		sb.WriteString(" ")
		sb.WriteString(t.out[0].String())
	} else if len(t.out) > 1 {
		sb.WriteString(" (")
		for i, v := range t.out {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(v.String())
		}
		sb.WriteString(")")
	}
}

func (t *typeImpl) interfaceString() string {
	if len(t.methods) == 0 {
		return "interface{}"
	}
	var names []string
	for n := range t.methods {
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
		sb.WriteString(strings.TrimPrefix(t.methods[n].String(), "func"))
	}
	sb.WriteString(" }")
	return sb.String()
}

// Helper functions for constructing types dynamically

// PtrOf returns a pointer type pointing to elem.
func PtrOf(elem Type) Type {
	if elem == nil {
		return nil
	}
	// For external types, use reflect
	if impl, ok := elem.(*typeImpl); ok && impl.external != nil {
		return FromReflectType(reflect.PointerTo(impl.external))
	}
	// Create a synthetic pointer type
	key := struct {
		kind TypeKind
		elem Type
	}{KindPtr, elem}
	if cached, ok := typeCache.Load(key); ok {
		return cached.(Type)
	}
	res := &typeImpl{
		kind: KindPtr,
		elem: elem,
	}
	typeCache.Store(key, res)
	return res
}