package debugs

import (
	"testing"

	"go.starlark.net/starlark"
)

func TestToStarlarkValue(t *testing.T) {
	type testStruct struct {
		Exported   string
		unexported int
	}

	ptrStruct := &testStruct{
		Exported:   "hello",
		unexported: 42,
	}

	testCases := []struct {
		name     string
		input    any
		expected starlark.Value
	}{
		{"nil", nil, starlark.None},
		{"bool true", true, starlark.True},
		{"bool false", false, starlark.False},
		{"bytes", []byte("abc"), starlark.Bytes("abc")},
		{"string", "hello", starlark.String("hello")},
		{"int", int(42), starlark.MakeInt(42)},
		{"int8", int8(42), starlark.MakeInt(42)},
		{"int16", int16(42), starlark.MakeInt(42)},
		{"int32", int32(42), starlark.MakeInt(42)},
		{"int64", int64(42), starlark.MakeInt64(42)},
		{"uint", uint(42), starlark.MakeUint(42)},
		{"uint8", uint8(42), starlark.MakeUint(42)},
		{"uint16", uint16(42), starlark.MakeUint(42)},
		{"uint32", uint32(42), starlark.MakeUint(42)},
		{"uint64", uint64(42), starlark.MakeUint64(42)},
		{"float32", float32(3.14), starlark.Float(float64(float32(3.14)))},
		{"float64", float64(3.14), starlark.Float(3.14)},
		{"[]any", []any{1, "a", true}, starlark.NewList([]starlark.Value{starlark.MakeInt(1), starlark.String("a"), starlark.True})},
		{"map[string]any", map[string]any{"a": 1, "b": "c"}, func() starlark.Value {
			d := starlark.NewDict(2)
			d.SetKey(starlark.String("a"), starlark.MakeInt(1))
			d.SetKey(starlark.String("b"), starlark.String("c"))
			return d
		}()},
		{"[]int", []int{1, 2, 3}, starlark.NewList([]starlark.Value{starlark.MakeInt(1), starlark.MakeInt(2), starlark.MakeInt(3)})},
		{"string", []string{"a", "b"}, starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})},
		{"map[int]bool", map[int]bool{1: true, 2: false}, func() starlark.Value {
			d := starlark.NewDict(2)
			d.SetKey(starlark.MakeInt(1), starlark.True)
			d.SetKey(starlark.MakeInt(2), starlark.False)
			return d
		}()},
		{"struct", testStruct{Exported: "hello", unexported: 42}, func() starlark.Value {
			d := starlark.NewDict(1)
			d.SetKey(starlark.String("Exported"), starlark.String("hello"))
			return d
		}()},
		{"pointer to struct", ptrStruct, func() starlark.Value {
			d := starlark.NewDict(1)
			d.SetKey(starlark.String("Exported"), starlark.String("hello"))
			return d
		}()},
		{"pointer to pointer to struct", &ptrStruct, func() starlark.Value {
			d := starlark.NewDict(1)
			d.SetKey(starlark.String("Exported"), starlark.String("hello"))
			return d
		}()},
		{"nested structure", map[string]any{
			"list": []any{
				testStruct{Exported: "foo"},
				&testStruct{Exported: "bar"},
			},
		}, func() starlark.Value {
			d := starlark.NewDict(1)
			struct1 := starlark.NewDict(1)
			struct1.SetKey(starlark.String("Exported"), starlark.String("foo"))
			struct2 := starlark.NewDict(1)
			struct2.SetKey(starlark.String("Exported"), starlark.String("bar"))
			list := starlark.NewList([]starlark.Value{struct1, struct2})
			d.SetKey(starlark.String("list"), list)
			return d
		}()},
		{"nil pointer", (*testStruct)(nil), starlark.None},
		{"nil interface", (any)(nil), starlark.None},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := toStarlarkValue(tc.input)
			equal, err := starlark.Equal(actual, tc.expected)
			if err != nil {
				t.Fatalf("comparison failed: %v", err)
			}
			if !equal {
				t.Errorf("toStarlarkValue(%#v) = %v, want %v", tc.input, actual, tc.expected)
			}
		})
	}

	t.Run("panic on unsupported type", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("toStarlarkValue did not panic on unsupported type")
			}
		}()
		toStarlarkValue(make(chan bool))
	})
}
