package tailang

import (
	"fmt"
	"reflect"
)

func Len(v any) int {
	if v == nil {
		return 0
	}
	return reflect.ValueOf(v).Len()
}

func Cap(v any) int {
	if v == nil {
		return 0
	}
	return reflect.ValueOf(v).Cap()
}

func Make(t reflect.Type, args ...int) (any, error) {
	switch t.Kind() {
	case reflect.Slice:
		length := 0
		cap := 0
		if len(args) > 0 {
			length = args[0]
		}
		if len(args) > 1 {
			cap = args[1]
		}
		cap = max(cap, length)
		return reflect.MakeSlice(t, length, cap).Interface(), nil
	case reflect.Map:
		sz := 0
		if len(args) > 0 {
			sz = args[0]
		}
		return reflect.MakeMapWithSize(t, sz).Interface(), nil
	case reflect.Chan:
		sz := 0
		if len(args) > 0 {
			sz = args[0]
		}
		return reflect.MakeChan(t, sz).Interface(), nil
	default:
		return nil, fmt.Errorf("cannot make type %v", t)
	}
}

func New(t reflect.Type) any {
	return reflect.New(t).Interface()
}

func Append(s any, elems ...any) (any, error) {
	slice := reflect.ValueOf(s)
	if slice.Kind() != reflect.Slice {
		return nil, fmt.Errorf("first argument to append must be slice")
	}

	vals := make([]reflect.Value, len(elems))
	for i, e := range elems {
		v, err := PrepareAssign(e, slice.Type().Elem())
		if err != nil {
			return nil, fmt.Errorf("append elem %d: %w", i, err)
		}
		vals[i] = v
	}
	return reflect.Append(slice, vals...).Interface(), nil
}

func Copy(dst, src any) int {
	return reflect.Copy(reflect.ValueOf(dst), reflect.ValueOf(src))
}

func Delete(m any, key any) {
	reflect.ValueOf(m).SetMapIndex(reflect.ValueOf(key), reflect.Value{})
}

func Close(c any) {
	reflect.ValueOf(c).Close()
}

func Panic(v any) {
	panic(v)
}

func Recover() any {
	return recover()
}

func Complex(r, i float64) complex128 {
	return complex(r, i)
}

func Real(c complex128) float64 {
	return real(c)
}

func Imag(c complex128) float64 {
	return imag(c)
}

func Index(container any, key any) (any, error) {
	v := reflect.ValueOf(container)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.String:
		idx, ok := AsInt(key)
		if !ok {
			return nil, fmt.Errorf("index must be integer")
		}
		if idx < 0 || idx >= v.Len() {
			return nil, fmt.Errorf("index out of range")
		}
		if v.Kind() == reflect.String {
			return v.String()[idx], nil
		}
		return v.Index(idx).Interface(), nil
	case reflect.Map:
		val := v.MapIndex(reflect.ValueOf(key))
		if !val.IsValid() {
			return nil, nil
		}
		return val.Interface(), nil
	}
	return nil, fmt.Errorf("type %T does not support indexing", container)
}

func Slice(container any, args ...any) (any, error) {
	// slice container low high [max]
	v := reflect.ValueOf(container)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.String:
		if len(args) < 2 {
			return nil, fmt.Errorf("slice expects low and high indices")
		}
		low, ok1 := AsInt(args[0])
		high, ok2 := AsInt(args[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("slice indices must be integers")
		}
		if len(args) > 2 {
			max, ok3 := AsInt(args[2])
			if !ok3 {
				return nil, fmt.Errorf("slice max index must be integer")
			}
			return v.Slice3(low, high, max).Interface(), nil
		}
		return v.Slice(low, high).Interface(), nil
	}
	return nil, fmt.Errorf("type %T does not support slicing", container)
}

func SetIndex(container any, key any, val any) error {
	v := reflect.ValueOf(container)
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		idx, ok := AsInt(key)
		if !ok {
			return fmt.Errorf("index must be integer")
		}
		if idx < 0 || idx >= v.Len() {
			return fmt.Errorf("index out of range")
		}
		elemType := v.Type().Elem()
		valV, err := PrepareAssign(val, elemType)
		if err != nil {
			return err
		}
		v.Index(idx).Set(valV)
		return nil
	case reflect.Map:
		keyV, err := PrepareAssign(key, v.Type().Key())
		if err != nil {
			return fmt.Errorf("invalid map key: %w", err)
		}
		valV, err := PrepareAssign(val, v.Type().Elem())
		if err != nil {
			return fmt.Errorf("invalid map value: %w", err)
		}
		v.SetMapIndex(keyV, valV)
		return nil
	}
	return fmt.Errorf("type %T does not support setting index", container)
}

func Send(ch any, val any) error {
	v := reflect.ValueOf(ch)
	if v.Kind() != reflect.Chan {
		return fmt.Errorf("send expects channel, got %T", ch)
	}
	valV, err := PrepareAssign(val, v.Type().Elem())
	if err != nil {
		return err
	}
	v.Send(valV)
	return nil
}

func Recv(ch any) (any, error) {
	v := reflect.ValueOf(ch)
	if v.Kind() != reflect.Chan {
		return nil, fmt.Errorf("recv expects channel, got %T", ch)
	}
	val, ok := v.Recv()
	if !ok {
		return nil, nil
	}
	return val.Interface(), nil
}

type Break struct{}

var _ Function = Break{}

func (b Break) FunctionName() string {
	return "break"
}

func (b Break) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	return nil, BreakSignal{}
}

type Continue struct{}

var _ Function = Continue{}

func (c Continue) FunctionName() string {
	return "continue"
}

func (c Continue) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	return nil, ContinueSignal{}
}

type Return struct{}

var _ Function = Return{}

func (r Return) FunctionName() string {
	return "return"
}

func (r Return) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	val, err := env.evalExpr(stream, nil)
	if err != nil {
		return nil, err
	}
	return nil, ReturnSignal{Val: val}
}

type Go struct{}

var _ Function = Go{}

func (g Go) FunctionName() string {
	return "go"
}

func (g Go) Call(env *Env, stream TokenStream, expectedType reflect.Type) (any, error) {
	block, err := ParseBlock(stream)
	if err != nil {
		return nil, err
	}
	go func() {
		env.NewScope().Evaluate(NewSliceTokenStream(block.Body))
	}()
	return nil, nil
}

func Clear(c any) {
	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			v.SetMapIndex(iter.Key(), reflect.Value{})
		}
	case reflect.Slice:
		zero := reflect.Zero(v.Type().Elem())
		for i := 0; i < v.Len(); i++ {
			v.Index(i).Set(zero)
		}
	}
}

func Deref(v any) (any, error) {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("expected pointer, got %T", v)
	}
	return val.Elem().Interface(), nil
}

func Zero(t reflect.Type) any {
	return reflect.Zero(t).Interface()
}
