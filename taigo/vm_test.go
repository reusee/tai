package taigo

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
)

func TestVM(t *testing.T) {
	run := func(src string) *taivm.VM {
		vm, err := NewVM("main", strings.NewReader(src))
		if err != nil {
			t.Helper()
			t.Fatal(err)
		}
		vm.Run(func(i *taivm.Interrupt, err error) bool {
			if err != nil {
				t.Helper()
				t.Fatalf("vm error: %v", err)
			}
			return true
		})
		return vm
	}

	checkInt := func(vm *taivm.VM, name string, expected int64) {
		val, ok := vm.Get(name)
		if !ok {
			t.Helper()
			t.Fatalf("variable %s not found", name)
		}
		if i, ok := taivm.ToInt64(val); !ok || i != expected {
			t.Helper()
			t.Fatalf("variable %s: expected %d, got %v", name, expected, val)
		}
	}

	checkString := func(vm *taivm.VM, name string, expected string) {
		val, ok := vm.Get(name)
		if !ok {
			t.Helper()
			t.Fatalf("variable %s not found", name)
		}
		if s, ok := val.(string); !ok || s != expected {
			t.Helper()
			t.Fatalf("variable %s: expected %q, got %v", name, expected, val)
		}
	}

	t.Run("basic literals", func(t *testing.T) {
		vm := run(`
			package main
			var a = 1
			var b = -5
			var c = "hello"
			var d = true
		`)
		checkInt(vm, "a", 1)
		checkInt(vm, "b", -5)
		checkString(vm, "c", "hello")
		// Check boolean manually
		if v, _ := vm.Get("d"); v != true {
			t.Fatalf("expected d=true, got %v", v)
		}
	})

	t.Run("arithmetic", func(t *testing.T) {
		vm := run(`
			package main
			var a = 1 + 2
			var b = 10 - 4
			var c = 2 * 3
			var d = 15 / 3
			var e = 10 % 3
			var f = 0
			func init() {
				f += 5
				f *= 2
			}
		`)
		checkInt(vm, "a", 3)
		checkInt(vm, "b", 6)
		checkInt(vm, "c", 6)
		checkInt(vm, "d", 5)
		checkInt(vm, "e", 1)
		checkInt(vm, "f", 10)
	})

	t.Run("comparison and logic", func(t *testing.T) {
		vm := run(`
			package main

			var res = 0
			func init() {
				if 1 < 2 { res = 1 }
				if 2 > 1 { res += 1 }
				if 1 == 1 { res += 1 }
				if 1 != 2 { res += 1 }
			}
			
			var neg = 0
			func init() {
				if !false { neg = 1 }
			}

			var bit = 0
			func init() {
				bit = 1 | 2 // 3
				bit = bit & 2 // 2
				bit = bit ^ 1 // 3
			}
		`)
		checkInt(vm, "res", 4)
		checkInt(vm, "neg", 1)
		checkInt(vm, "bit", 3)
	})

	t.Run("loops", func(t *testing.T) {
		vm := run(`
			package main

			var sum = 0
			func init() {
				for i := 0; i < 5; i++ {
					sum += i
				}
			}
			
			var j = 0
			var sum2 = 0
			func init() {
				for j < 5 {
					sum2 += j
					j++
				}
			}
			
			var sum3 = 0
			func init() {
				for k := 0; k < 10; k++ {
					if k < 5 { continue }
					if k > 7 { break }
					sum3 += k
				}
			}
		`)
		checkInt(vm, "sum", 10)
		checkInt(vm, "sum2", 10)
		checkInt(vm, "sum3", 5+6+7) // 18
	})

	t.Run("switch", func(t *testing.T) {
		vm := run(`
			package main

			var a = 1
			var res = 0
			func init() {
				switch a {
				case 1:
					res = 10
				case 2:
					res = 20
				default:
					res = 30
				}
			}

			func init() {
			var res2 = 0
				switch {
				case a == 1:
					res2 = 100
				default:
					res2 = 200
				}
			}
		`)
		checkInt(vm, "res", 10)
		checkInt(vm, "res2", 100)
	})

	t.Run("functions", func(t *testing.T) {
		vm := run(`
			package main

			func add(a, b) {
				return a + b
			}
			var res = add(3, 4)

			func fact(n) {
				if n <= 1 { return 1 }
				return n * fact(n-1)
			}
			var f = fact(5)

			func multi() {
				return 1, 2
			}
			var x, y = multi()
		`)
		checkInt(vm, "res", 7)
		checkInt(vm, "f", 120)
		checkInt(vm, "x", 1)
		checkInt(vm, "y", 2)
	})

	t.Run("closures", func(t *testing.T) {
		vm := run(`
			package main
			func makeAdder(base) {
				return func(v) {
					return base + v
				}
			}
			var add5 = makeAdder(5)
			var res = add5(10)
		`)
		checkInt(vm, "res", 15)
	})

	t.Run("multi assignment", func(t *testing.T) {
		vm := run(`
			package main
			var a, b = 1, 2
			func init() {
				a, b = b, a
			}
		`)
		checkInt(vm, "a", 2)
		checkInt(vm, "b", 1)
	})

	t.Run("slices", func(t *testing.T) {
		vm := run(`
			package main

			var s = make("[]int", 3)
			func init() {
				s[0] = 10
				s[1] = 20
				s[2] = 30
			}

			var l = len(s)
			var c = cap(s)
			func init() {
				s = append(s, 40)
			}
			var last = s[3]
			
			var part = s[1:3]
		`)
		checkInt(vm, "l", 3)
		checkInt(vm, "last", 40)

		val, _ := vm.Get("part")
		if _, ok := val.(*taivm.List); !ok {
			t.Errorf("expected *List for slice result, got %T", val)
		}
	})

	t.Run("maps", func(t *testing.T) {
		vm := run(`
			package main

			var m = make("map[string]int")
			func init() {
				m["one"] = 1
				var v = m["one"]
				delete(m, "one")
				// v2 should be nil/undefined behavior depending on impl, let's just check delete happened
				// m["one"] returns nil in vm implementation if missing
			}
		`)
		checkInt(vm, "v", 1)
		mArg, _ := vm.Get("m")
		m := mArg.(map[any]any)
		if len(m) != 0 {
			t.Fatal("delete failed")
		}
	})

	t.Run("map literals", func(t *testing.T) {
		vm := run(`
			package main
			var m = map[string]int{
				"a": 1,
				"b": 2,
			}
			var sum = m["a"] + m["b"]
		`)
		checkInt(vm, "sum", 3)
	})

	t.Run("range map", func(t *testing.T) {
		vm := run(`
			package main
			var m = map[string]int{"a": 1, "b": 2}
			var sum = 0
			func init() {
				for k := range m {
					sum += m[k]
				}
			}
		`)
		checkInt(vm, "sum", 3)
	})

	t.Run("string ops", func(t *testing.T) {
		vm := run(`
			package main
			var s = "hello" + " " + "world"
			var l = len(s)
			var sub = s[0:5]
		`)
		checkString(vm, "s", "hello world")
		checkInt(vm, "l", 11)
		checkString(vm, "sub", "hello")
	})

	t.Run("variadic function", func(t *testing.T) {
		vm := run(`
			package main
			func sum(nums...) {
				var s = 0
				for n := range nums {
					s += n
				}
				return s
			}
			var res = sum(1, 2, 3, 4)
		`)
		checkInt(vm, "res", 10)
	})
}
