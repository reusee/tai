package main

import (
	_ "embed"
	"fmt"
	"github.com/reusee/tai/testdata/dep1"
)

func main() {
	fmt.Printf("hello, world!")
}

var _ = dep1.Foo

//go:embed a.txt
var externalFile string
