package taigo

import (
	"strings"
	"testing"
)

func BenchmarkCompile(b *testing.B) {
	src := `package main; var a = 1 + 2`
	for b.Loop() {
		_, err := NewVM("main", strings.NewReader(src), nil)
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
	for b.Loop() {
		vm, err := NewVM("main", strings.NewReader(src), nil)
		if err != nil {
			b.Fatal(err)
		}
		for _, err := range vm.Run {
			if err != nil {
				b.Fatal(err)
			}
		}
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
	for b.Loop() {
		vm, err := NewVM("main", strings.NewReader(src), nil)
		if err != nil {
			b.Fatal(err)
		}
		for _, err := range vm.Run {
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
