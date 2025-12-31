package taigo

import (
	"testing"
)

func BenchmarkCompile(b *testing.B) {
	src := `package main; var a = 1 + 2`
	for b.Loop() {
		env := &Env{
			Source: src,
		}
		_, err := env.NewVM()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFib15(b *testing.B) {
	src := `
		package main
		func fib(n any) {
			if n <= 1 { return n }
			return fib(n-1) + fib(n-2)
		}
		func main() {
			_ = fib(15)
		}
	`
	env := &Env{
		Source: src,
	}
	vm, err := env.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		for _, err := range vm.Run {
			if err != nil {
				b.Fatal(err)
			}
		}
		vm.Reset()
	}
}

func BenchmarkLoop10000(b *testing.B) {
	src := `
		package main
		func main() {
			var sum = 0
			for i := 0; i < 10000; i++ {
				sum += i
			}
		}
	`
	env := &Env{
		Source: src,
	}
	vm, err := env.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		for _, err := range vm.Run {
			if err != nil {
				b.Fatal(err)
			}
		}
		vm.Reset()
	}
}
