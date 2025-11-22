package debugs

import (
	"fmt"
	"reflect"

	"github.com/reusee/starlarkutil"
	"go.starlark.net/starlark"
)

func toStarlarkValue(v any) starlark.Value {
	switch v := v.(type) {

	case nil:
		return starlark.None

	case bool:
		return starlark.Bool(v)

	case []byte:
		return starlark.Bytes(v)
	case string:
		return starlark.String(v)

	case int:
		return starlark.MakeInt(v)
	case int8:
		return starlark.MakeInt(int(v))
	case int16:
		return starlark.MakeInt(int(v))
	case int32:
		return starlark.MakeInt(int(v))
	case int64:
		return starlark.MakeInt64(v)

	case uint:
		return starlark.MakeUint(v)
	case uint8:
		return starlark.MakeUint(uint(v))
	case uint16:
		return starlark.MakeUint(uint(v))
	case uint32:
		return starlark.MakeUint(uint(v))
	case uint64:
		return starlark.MakeUint64(v)

	case float32:
		return starlark.Float(v)
	case float64:
		return starlark.Float(v)

	case []any:
		elems := make([]starlark.Value, len(v))
		for i, e := range v {
			elems[i] = toStarlarkValue(e)
		}
		return starlark.NewList(elems)

	case map[string]any:
		d := starlark.NewDict(len(v))
		for k, val := range v {
			d.SetKey(starlark.String(k), toStarlarkValue(val))
		}
		return d

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
