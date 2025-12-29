package taivm

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
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
}

type StructField struct {
	Name      string
	Type      *Type
	Tag       string
	Anonymous bool
}

func FromReflectType(t reflect.Type) *Type {
	if t == nil {
		return nil
	}
	res := &Type{
		Kind: TypeKind(t.Kind()),
		Name: t.Name(),
	}
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
		res.Elem = FromReflectType(t.Elem())
		if t.Kind() == reflect.Array {
			res.Len = t.Len()
		}
	case reflect.Map:
		res.Key = FromReflectType(t.Key())
		res.Elem = FromReflectType(t.Elem())
	case reflect.Func:
		res.Variadic = t.IsVariadic()
		for i := 0; i < t.NumIn(); i++ {
			res.In = append(res.In, FromReflectType(t.In(i)))
		}
		for i := 0; i < t.NumOut(); i++ {
			res.Out = append(res.Out, FromReflectType(t.Out(i)))
		}
	case reflect.Interface:
		res.Methods = make(map[string]*Type)
		for i := 0; i < t.NumMethod(); i++ {
			m := t.Method(i)
			res.Methods[m.Name] = FromReflectType(m.Type)
		}
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			res.Fields = append(res.Fields, StructField{
				Name:      f.Name,
				Type:      FromReflectType(f.Type),
				Tag:       string(f.Tag),
				Anonymous: f.Anonymous,
			})
		}
	}
	return res
}

func (t *Type) ToReflectType() reflect.Type {
	if t == nil {
		return nil
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
		return reflect.TypeFor[uintptr]() // Fallback to uintptr if unsafe is not imported
	case KindPtr:
		et := t.Elem.ToReflectType()
		if et == nil {
			return nil
		}
		return reflect.PointerTo(et)
	case KindSlice:
		et := t.Elem.ToReflectType()
		if et == nil {
			return nil
		}
		return reflect.SliceOf(et)
	case KindArray:
		et := t.Elem.ToReflectType()
		if et == nil {
			return nil
		}
		return reflect.ArrayOf(t.Len, et)
	case KindMap:
		kt := t.Key.ToReflectType()
		et := t.Elem.ToReflectType()
		if kt == nil || et == nil {
			return nil
		}
		return reflect.MapOf(kt, et)
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
	case KindInterface:
		return reflect.TypeFor[any]()
	case KindStruct:
		if len(t.Fields) == 0 {
			return reflect.TypeFor[struct{}]()
		}
		var fields []reflect.StructField
		for _, f := range t.Fields {
			ft := f.Type.ToReflectType()
			if ft == nil {
				return nil
			}
			sf := reflect.StructField{
				Name:      f.Name,
				Type:      ft,
				Tag:       reflect.StructTag(f.Tag),
				Anonymous: f.Anonymous,
			}
			if sf.Anonymous && ft.Kind() == reflect.Interface {
				sf.Anonymous = false
			}
			if sf.Name != "" && sf.Name[0] >= 'a' && sf.Name[0] <= 'z' {
				sf.PkgPath = "main"
			}
			fields = append(fields, sf)
		}
		return reflect.StructOf(fields)
	}
	return nil
}

func (t *Type) AssignableTo(u *Type) bool {
	if t == nil || u == nil {
		return t == u
	}
	if t.Kind != u.Kind {
		return false
	}
	if t.Name != "" && u.Name != "" {
		return t.Name == u.Name
	}
	switch t.Kind {
	case KindPtr, KindSlice, KindArray:
		if t.Elem == nil || u.Elem == nil {
			return t.Elem == u.Elem
		}
		return t.Elem.AssignableTo(u.Elem)
	case KindMap:
		if t.Key == nil || u.Key == nil || t.Elem == nil || u.Elem == nil {
			return t.Key == u.Key && t.Elem == u.Elem
		}
		return t.Key.AssignableTo(u.Key) && t.Elem.AssignableTo(u.Elem)
	case KindFunc:
		if len(t.In) != len(u.In) || len(t.Out) != len(u.Out) || t.Variadic != u.Variadic {
			return false
		}
		for i := range t.In {
			if !t.In[i].AssignableTo(u.In[i]) {
				return false
			}
		}
		for i := range t.Out {
			if !t.Out[i].AssignableTo(u.Out[i]) {
				return false
			}
		}
		return true
	}
	return true
}

func (t *Type) Zero() any {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case KindBool:
		return false
	case KindInt:
		return 0
	case KindInt8:
		return int8(0)
	case KindInt16:
		return int16(0)
	case KindInt32:
		return int32(0)
	case KindInt64:
		return int64(0)
	case KindUint:
		return uint(0)
	case KindUint8:
		return uint8(0)
	case KindUint16:
		return uint16(0)
	case KindUint32:
		return uint32(0)
	case KindUint64:
		return uint64(0)
	case KindUintptr:
		return uintptr(0)
	case KindFloat32:
		return float32(0)
	case KindFloat64:
		return float64(0)
	case KindComplex64:
		return complex64(0)
	case KindComplex128:
		return complex128(0)
	case KindString:
		return ""
	case KindSlice, KindMap, KindPtr, KindFunc, KindChan, KindInterface, KindUnsafePointer:
		return nil
	case KindStruct:
		return &Struct{TypeName: t.Name, Fields: make(map[string]any)}
	}
	if rt := t.ToReflectType(); rt != nil {
		return reflect.Zero(rt).Interface()
	}
	return nil
}

func (t *Type) Match(val any) bool {
	if val == nil {
		return t == nil || t.Kind == KindInterface || t.Kind == KindPtr || t.Kind == KindSlice || t.Kind == KindMap || t.Kind == KindFunc
	}
	if t == nil {
		return false
	}
	if t.Kind == KindInterface && len(t.Methods) == 0 {
		return true
	}
	if s, ok := val.(*Struct); ok {
		return t.Name != "" && s.TypeName == t.Name
	}
	// Direct matches for all basic kinds to avoid reflect.TypeOf
	switch t.Kind {
	case KindBool:
		_, ok := val.(bool)
		return ok
	case KindInt:
		_, ok := val.(int)
		return ok
	case KindInt8:
		_, ok := val.(int8)
		return ok
	case KindInt16:
		_, ok := val.(int16)
		return ok
	case KindInt32:
		_, ok := val.(int32)
		return ok
	case KindInt64:
		_, ok := val.(int64)
		return ok
	case KindUint:
		_, ok := val.(uint)
		return ok
	case KindUint8:
		_, ok := val.(uint8)
		return ok
	case KindUint16:
		_, ok := val.(uint16)
		return ok
	case KindUint32:
		_, ok := val.(uint32)
		return ok
	case KindUint64:
		_, ok := val.(uint64)
		return ok
	case KindUintptr:
		_, ok := val.(uintptr)
		return ok
	case KindFloat32:
		_, ok := val.(float32)
		return ok
	case KindFloat64:
		_, ok := val.(float64)
		return ok
	case KindComplex64:
		_, ok := val.(complex64)
		return ok
	case KindComplex128:
		_, ok := val.(complex128)
		return ok
	case KindString:
		_, ok := val.(string)
		return ok
	case KindSlice:
		if _, ok := val.(*List); ok {
			return true
		}
		if _, ok := val.([]any); ok {
			return true
		}
	case KindMap:
		if _, ok := val.(map[any]any); ok {
			return true
		}
		if _, ok := val.(map[string]any); ok {
			return true
		}
	}
	rt := reflect.TypeOf(val)
	if targetRT := t.ToReflectType(); targetRT != nil {
		return rt.AssignableTo(targetRT)
	}
	return false
}

func (t *Type) String() string {
	if t == nil {
		return "<nil>"
	}
	if t.Name != "" {
		return t.Name
	}
	switch t.Kind {
	case KindPtr:
		return "*" + t.Elem.String()
	case KindSlice:
		return "[]" + t.Elem.String()
	case KindArray:
		return fmt.Sprintf("[%d]%s", t.Len, t.Elem.String())
	case KindMap:
		return "map[" + t.Key.String() + "]" + t.Elem.String()
	case KindChan:
		return "chan " + t.Elem.String()
	case KindFunc:
		return t.funcString()
	case KindInterface:
		return t.interfaceString()
	default:
		return reflect.Kind(t.Kind).String()
	}
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
