package taiui

import (
	"fmt"
	"reflect"
)

func RenderAll(scope Scope, elements ...Element) {
	scope = scope.Fork(func() Scope { return scope })
	for len(elements) > 0 {
		var next []Element
		for _, elem := range elements {
			res := scope.Call(elem.RenderFunc())
			for _, ret := range res.Values {
				switch v := ret.Interface().(type) {
				case Element:
					if v != nil {
						next = append(next, v)
					}
				case []Element:
					for _, e := range v {
						if e != nil {
							next = append(next, e)
						}
					}
				}
			}
		}
		elements = next
	}
}

// uiDesc wraps the flat spec list of an element.
type uiDesc struct {
	initFuncs []any
}

// newUIDesc wraps each spec: functions are resolved as injectable initializers
// via the scope; any other value is returned as-is.
func newUIDesc(specs []any) uiDesc {
	var initFuncs []any
	for _, spec := range specs {
		if t := reflect.TypeOf(spec); t != nil && t.Kind() == reflect.Func {
			initFuncs = append(initFuncs, spec)
		} else {
			s := spec
			initFuncs = append(initFuncs, func() any { return s })
		}
	}
	return uiDesc{initFuncs: initFuncs}
}

func (u uiDesc) iterSpecs(scope Scope, cb func(any)) {
	for _, fn := range u.initFuncs {
		res := scope.Call(fn)
		for _, ret := range res.Values {
			cb(ret.Interface())
		}
	}
}

type _Margin []int

func Margin(spec ...int) _Margin { return _Margin(spec) }

type _Padding []int

func Padding(spec ...int) _Padding { return _Padding(spec) }

func applyBoxModel(v []int) (top, right, bottom, left int) {
	switch len(v) {
	case 0:
	case 1:
		top, bottom, left, right = v[0], v[0], v[0], v[0]
	case 2:
		top, bottom, left, right = v[0], v[0], v[1], v[1]
	case 3:
		top, left, right, bottom = v[0], v[1], v[1], v[2]
	case 4:
		top, right, bottom, left = v[0], v[1], v[2], v[3]
	default:
		panic(fmt.Errorf("bad box model spec: %v", v))
	}
	return
}

func applyMargin(v _Margin) (top, right, bottom, left int) {
	return applyBoxModel(v)
}

func applyPadding(v _Padding) (top, right, bottom, left int) {
	return applyBoxModel(v)
}

// ElementFrom

var _ Element = _ElementFrom{}

type _ElementFrom struct{ uiDesc }

func ElementFrom(specs ...any) _ElementFrom {
	return _ElementFrom{uiDesc: newUIDesc(specs)}
}

func (e _ElementFrom) RenderFunc() any {
	return func(scope Scope) {
		var children []Element
		e.iterSpecs(scope, func(v any) {
			switch v := v.(type) {
			case Element:
				if v != nil {
					children = append(children, v)
				}
			case []Element:
				for _, elem := range v {
					if elem != nil {
						children = append(children, elem)
					}
				}
			default:
				panic(fmt.Errorf("unknown spec %#v", v))
			}
		})
		RenderAll(scope, children...)
	}
}

// ElementFunc

// ElementFunc builds an element from an injectable function and extra
// definitions: the function runs in a scope forked with the definitions,
// and any Elements it returns are rendered.
func ElementFunc(fn any, provides ...any) Element {
	return ElementWith(ElementFrom(fn), provides...)
}

// ElementWith

var _ Element = _ElementWith{}

type _ElementWith struct {
	elem     Element
	provides []any
}

func ElementWith(elem Element, provides ...any) _ElementWith {
	return _ElementWith{elem: elem, provides: provides}
}

func (s _ElementWith) RenderFunc() any {
	return func(scope Scope) {
		RenderAll(scope.Fork(s.provides...), s.elem)
	}
}
