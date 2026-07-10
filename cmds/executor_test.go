package cmds

import (
	"strings"
	"testing"
)

func TestExecutor(t *testing.T) {
	executor := NewExecutor()

	var a int
	executor.Define("+a", Func(func() {
		a = 42
	}))
	executor.Define("a", Func(func(i int) {
		a = i
	}))

	if err := executor.Execute([]string{
		"+a",
	}); err != nil {
		t.Fatal(err)
	}
	if a != 42 {
		t.Fatal()
	}

	if err := executor.Execute([]string{
		"a", "1",
	}); err != nil {
		t.Fatal(err)
	}
	if a != 1 {
		t.Fatal()
	}

	err := executor.Execute([]string{
		"foo",
	})
	if !strings.Contains(err.Error(), "unknown command: foo") {
		t.Fatalf("got %v", err)
	}

}

func TestInt8Range(t *testing.T) {
	executor := NewExecutor()
	var n int8
	executor.Define("set", Func(func(v int8) {
		n = v
	}))

	// in-range value parses normally
	if err := executor.Execute([]string{"set", "42"}); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatalf("expected 42, got %d", n)
	}

	// out-of-range value must surface an error rather than silently wrap.
	// Before the fix, ParseInt used bitSize 64 and SetInt truncated
	// int8(300) to 44 with no error.
	err := executor.Execute([]string{"set", "300"})
	if err == nil {
		t.Fatal("expected error for int8 overflow, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out of range error, got %v", err)
	}
}

func TestSubCommands(t *testing.T) {
	executor := NewExecutor()
	var bar, baz int
	executor.Define("foo", Sub(map[string]*Command{
		"bar": Func(func() {
			bar = 1
		}),
		"baz": Func(func(i int) {
			baz = i
		}),
	}))

	if err := executor.Execute([]string{
		"foo",
		"bar",
		"baz", "42",
	}); err != nil {
		t.Fatal(err)
	}

	if bar != 1 {
		t.Fatal()
	}
	if baz != 42 {
		t.Fatal()
	}

}

func TestDuplicatedSubCommand(t *testing.T) {
	executor := NewExecutor()
	executor.Define("foo", Sub(map[string]*Command{
		"a": nil,
	}))
	executor.Define("bar", Sub(map[string]*Command{
		"a": nil,
	}))
	err := executor.Execute([]string{"foo", "bar"})
	if !strings.Contains(err.Error(), "duplicated sub command: bar a") {
		t.Fatalf("got %v", err)
	}
}

func TestOptionalArgument(t *testing.T) {
	executor := NewExecutor()
	var n int
	var s string
	executor.Define("foo", Func(func(arg *int, arg2 *string) {
		n = *arg
		s = *arg2
	}))

	err := executor.Execute([]string{"foo", "42", "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatal()
	}
	if s != "foo" {
		t.Fatal()
	}

	err = executor.Execute([]string{"foo", "99"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 99 {
		t.Fatal()
	}
	if s != "" {
		t.Fatal()
	}

	err = executor.Execute([]string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal()
	}
	if s != "" {
		t.Fatal()
	}

}
