package debugs

import (
	"fmt"
	"reflect"

	"github.com/reusee/starlarkutil"
	"go.starlark.net/starlark"
)

// toStarlarkValue converts a Go value to its Starlark counterpart.
// Only nil and []byte need a dedicated case: every other supported type
// is dispatched by kind in the reflect fallback below, which produces
// the same value the concrete cases would.
func toStarlarkValue(v any) starlark.Value {
	switch v := v.(type) {

	case nil:
		return starlark.None

	case []byte:
		return starlark.Bytes(v)

	}

	value := reflect.ValueOf(v)
	switch value.Kind() {

	case reflect.Bool:
		return starlark.Bool(value.Bool())

	case reflect.String:
		return starlark.String(value.String())

	case reflect.Int:
		return starlark.MakeInt(int(value.Int()))
	case reflect.Int8:
		return starlark.MakeInt(int(value.Int()))
	case reflect.Int16:
		return starlark.MakeInt(int(value.Int()))
	case reflect.Int32:
		return starlark.MakeInt(int(value.Int()))
	case reflect.Int64:
		return starlark.MakeInt64(value.Int())

	case reflect.Uint:
		return starlark.MakeUint(uint(value.Uint()))
	case reflect.Uint8:
		return starlark.MakeUint(uint(value.Uint()))
	case reflect.Uint16:
		return starlark.MakeUint(uint(value.Uint()))
	case reflect.Uint32:
		return starlark.MakeUint(uint(value.Uint()))
	case reflect.Uint64:
		return starlark.MakeUint64(value.Uint())

	case reflect.Float32:
		return starlark.Float(value.Float())
	case reflect.Float64:
		return starlark.Float(value.Float())

	case reflect.Slice, reflect.Array:
		l := value.Len()
		elems := make([]starlark.Value, l)
		for i := range l {
			elem := value.Index(i)
			elems[i] = toStarlarkValue(elem.Interface())
		}
		return starlark.NewList(elems)

	case reflect.Map:
		d := starlark.NewDict(value.Len())
		iter := value.MapRange()
		for iter.Next() {
			d.SetKey(
				toStarlarkValue(iter.Key().Interface()),
				toStarlarkValue(iter.Value().Interface()),
			)
		}
		return d

	case reflect.Struct:
		n := value.NumField()
		d := starlark.NewDict(n)
		typ := value.Type()
		for i := range n {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			d.SetKey(
				starlark.String(field.Name),
				toStarlarkValue(value.Field(i).Interface()),
			)
		}
		return d

	case reflect.Pointer, reflect.Interface:
		elem := value.Elem()
		if !elem.IsValid() {
			return starlark.None
		}
		return toStarlarkValue(elem.Interface())

	case reflect.Func:
		return starlarkutil.MakeFunc("", value.Interface())

	}

	panic(fmt.Errorf("unsupported type for starlark: %T", v))
}
