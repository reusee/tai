# Tai Lang (`tailang`)

Tai is a lightweight, embeddable, dynamic scripting language for Go. It is designed around the philosophy of "Code as Data" using a prefix notation (Polish notation) style, with deep, seamless integration into Go's type system via reflection.

## Core Features

*   **Prefix Syntax**: Concise function calls without redundant parentheses (e.g., `+ 1 2` instead of `1 + 2`).
*   **Go Interoperability**: Direct mapping to Go types (`int`, `string`, `slice`, `map`, `struct`, `chan`).
*   **Auto-BigMath**: Arithmetic operations automatically promote to `math/big.Int` or `math/big.Float` on overflow, ensuring safety for heavy calculations.
*   **Structured Concurrency**: First-class keywords for `go` routines, `send`/`recv` on channels, and `select` blocks.
*   **Embeddable**: Zero-config instantiation with `NewEnv()`.

## Installation

```bash
go get github.com/reus/reusee/tai/tailang
```

## User Guide

### 1. Variables and Types

Variables are dynamically typed but backed by strong Go types.

```tailang
# Definition
def x 42
def s "Hello World"

# Assignment
set x (+ x 1)

# Lists (Slices) and Maps
def my_list [1 2 3 "four"]
def my_map (make (map_of string int))
```

### 2. Control Flow

Tai supports standard control structures adapted to its prefix style. Blocks are delimited by `{}`.

```tailang
# If / Else
if > x 10 {
    fmt.println "Large"
} else {
    fmt.println "Small"
}

# Loops
while < x 100 {
    set x (+ x 1)
}

# Iteration (Slice, Map, Channel, String)
foreach item ["a" "b" "c"] {
    fmt.println item
}

# Switch
switch x {
    1 { "Option 1" }
    default { "Other" }
}
```

### 3. Functions

Functions support closures, named parameters, and recursion.

```tailang
func add(a b) {
    + a b
}

# Function as first-class citizen
def op &add
op 10 20
```

### 4. Concurrency

Concurrency primitives map directly to Go's runtime.

```tailang
def ch (make (chan_of int))

go {
    send ch 100
}

def val (recv ch)
```

## Developer Guide

### Embedding Tai

To run Tai code within a Go application:

```go
package main

import (
	"fmt"
	"strings"
	"github.com/reus/reusee/tai/tailang"
)

func main() {
	env := tailang.NewEnv()

	// 1. Define custom Go functions
	env.Define("double", func(i int) int {
		return i * 2
	})

	// 2. Execute script
	src := `
        def x 21
        double x
    `
	tokenizer := tailang.NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		panic(err)
	}
	fmt.Println(res) // 42
}
```

### Struct Binding

Tai can write to struct fields using named parameter syntax (`.field value`). Fields must be tagged with `tai`.

```go
type Config struct {
    Host string `tai:"host"`
    Port int    `tai:"port"`
}

// In Script:
// config_obj .host "127.0.0.1" .port 8080
```

## Comprehensive Code Example

This script demonstrates recursion, big-integer math, concurrency (channels and workers), and standard library usage.

```tailang
# Parallel Factorial Calculator
# Demonstrates: func, if, math, go, chan, foreach, fmt

# 1. Define recursive factorial function
# Note: Tai automatically promotes to BigInt for large results
func factorial(n) {
    if <= n 1 {
        1
    } else {
        * n (factorial (- n 1))
    }
}

# 2. Initialize channels
# 'chan_of' is a helper to reflect a channel type
def inputs (make (chan_of int) 10)
def results (make (chan_of string) 10)

# 3. Start a worker goroutine
go {
    # foreach on a channel behaves like Go's 'range'
    foreach n inputs {
        def res (factorial n)
        
        # Use stdlib fmt.sprintf for formatting
        def msg (fmt.sprintf "Factorial of %d is %v" n res)
        
        send results msg
    }
    close results
}

# 4. Send jobs
go {
    # Calculate factorials for 10, 20, ..., 50
    foreach i [10 20 30 40 50] {
        send inputs i
    }
    close inputs
}

# 5. Collect and print results
fmt.println "--- Calculation Results ---"
foreach line results {
    fmt.println line
}
fmt.println "--- Done ---"
```

