# tailang

**tailang** is a lightweight, embedded scripting language written in Go. It is designed for seamless interoperability with the Go type system, utilizing Go's reflection capabilities to provide a dynamic yet robust scripting environment.

Its syntax is inspired by Lisp (Polish notation/prefix notation) but tailored for Go's concurrency model and standard library.

## Core Philosophy

1.  **Go-Native**: `tailang` types *are* Go types. `int` is `int`, `map` is `reflect.Map`, and functions are `reflect.Func`. There is no heavy wrapping layer.
2.  **Concurrency First**: First-class support for `go`, `chan`, `select`, `send`, and `recv`, mimicking Go's concurrency primitives.
3.  **Reflection-Based**: Calls to Go functions, methods, and field access are resolved dynamically at runtime using `reflect`.
4.  **Prefix Notation**: Everything is an expression in the form `op arg1 arg2 ...`.

## Installation

```bash
go get github.com/reus/reusee/tai/tailang
```

## Quick Start (For Developers)

To embed `tailang` in your Go application:

```go
package main

import (
    "fmt"
    "strings"
    "github.com/reus/reusee/tai/tailang"
)

func main() {
    // 1. Create an Environment
    env := tailang.NewEnv()

    // 2. Define custom Go values or functions
    env.Define("greet", func(name string) {
        fmt.Printf("Hello, %s!\n", name)
    })

    // 3. Evaluate Script
    src := `
        def name "World"
        greet name
        + 1 2
    `
    tokenizer := tailang.NewTokenizer(strings.NewReader(src))
    result, err := env.Evaluate(tokenizer)
    if err != nil {
        panic(err)
    }
    
    fmt.Println("Result:", result) // Result: 3
}
```

## Language Tour

### Basic Syntax
tailang uses prefix notation (Polish notation). The operator or function name always comes first.

```tailang
# Arithmetic
+ 1 2           # 1 + 2
* 3 (+ 1 1)     # 3 * (1 + 1)

# Method Chaining (Go methods)
# equivalent to: time.Now().Format("2006-01-02")
time.now:Format "2006-01-02"
```

### Variables
Variables are lexically scoped.

```tailang
def x 10        # Define x
set x 20        # Update x
```

### Data Types
tailang supports standard Go types and literals.

*   **Numbers**: `1`, `3.14`, `1e9`. Auto-promotes to `big.Int` or `big.Float` on overflow.
*   **Strings**: `"hello"`, `'world'`, `` `multiline` ``.
*   **Lists (Slices)**: `[1 2 3]` or `[.elem int 1 2 3]` for typed slices.
*   **Blocks**: `{ print "hi" }`.

### Control Flow

**If / Else**
```tailang
if > x 10 {
    print "big"
} else {
    print "small"
}
```

**Loops**
```tailang
# While
while < i 10 {
    set i (+ i 1)
}

# Foreach (iterates slices, maps, strings, channels)
foreach item ["a" "b"] {
    print item
}
```

**Switch**
```tailang
switch val {
    1 { "one" }
    2 { "two" }
    default { "other" }
}
```

### Functions
Functions support closures and recursion.

```tailang
func add(a b) {
    + a b
}

# Anonymous function
def sub (func(a b) { - a b })
```

### Concurrency
tailang exposes Go's concurrency primitives directly.

```tailang
def c (make (chan_of int) 0)

go {
    time.sleep (time.parse_duration "100ms")
    send c 42
}

def result (recv c)
```

### Stdlib Integration
Most of Go's standard library is pre-registered (e.g., `fmt`, `strings`, `math`, `os`, `json`).

```tailang
fmt.println (strings.to_upper "hello")
json.marshal (map_of string int)
```

## Comprehensive Example

The following script demonstrates the majority of `tailang` features: concurrency, flow control, scoping, recursion, struct usage, and standard library integration.

```tailang
# 1. Define a recursive function (Fibonacci)
func fib(n) {
    if <= n 1 {
        n
    } else {
        + (fib (- n 1)) (fib (- n 2))
    }
}

# 2. Concurrency: Worker pool pattern
def jobs (make (chan_of int) 5)
def results (make (chan_of int) 5)

# Function to process jobs
func worker(id) {
    # 'defer' runs when function exits
    defer { fmt.printf "Worker %d finished\n" [id] }
    
    # Range over channel using foreach
    foreach n jobs {
        if == n 0 {
            # Demonstrate control flow
            continue
        }
        
        # Calculate result
        def res (fib n)
        
        # Artificial delay using Go stdlib
        time.sleep (time.parse_duration "10ms")
        
        send results res
    }
}

# 3. Start workers
fmt.println "Starting workers..."
go { worker 1 }
go { worker 2 }

# 4. Send jobs
foreach i [5 8 10 0 12] {
    send jobs i
}
close jobs

# 5. Collect results with Select (with timeout)
def collected 0
def timeout (time.after (time.parse_duration "1s"))

# Loop 4 times (we sent 5 items, but 0 is skipped)
def i 0
while < i 4 {
    select {
        case recv results r {
            fmt.printf "Got result: %v\n" [r]
            set collected (+ collected 1)
        }
        case recv timeout t {
            fmt.println "Timed out!"
            break
        }
    }
    set i (+ i 1)
}

# 6. Big Integer Math (Auto-promotion)
# 2^64 is too big for int64, automatically becomes *big.Int
def big_val (math.pow 2 64) 
fmt.printf "Big calculation: %v (Type: %s)\n" [big_val (type big_val)]

# 7. List manipulation and Strings
def parts ["tailang" "is" "fun"]
def message (strings.join parts " ")
strings.to_upper message
```
