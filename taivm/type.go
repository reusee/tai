package taivm

import "reflect"

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
		return reflect.TypeFor[uintptr]()
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
