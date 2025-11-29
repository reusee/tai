# Tailang

Tailang is a dynamic, embeddable scripting language written in Go. It is designed for seamless interoperability with Go applications, offering a syntax that emphasizes data flow (pipelines) and simplicity.

## Core Concepts

*   **Go Interoperability**: Tailang wraps Go's reflection capabilities, allowing scripts to call Go functions, instantiate structs, and manipulate Go types directly.
*   **Pipeline First**: Inspired by functional programming and shell pipes, Tailang supports `|` (pipe to first argument) and `|>` (pipe to last argument) to create clean data transformation chains.
*   **Robust Math**: Built-in support for arbitrary-precision integers and floats. Mathematical operations automatically promote operands to `big.Int` or `big.Float` when necessary to prevent overflow or precision loss.
*   **Embeddable**: Designed to be easily integrated into Go programs with a minimal API surface.

## Language Guide

### Hello World

```tailang
fmt.println "Hello, World!"
```

### Variables and Types

Variables are defined using `def` and modified using `set`. Variables are dynamically typed by default, but types can be enforced.

```tailang
def name "Tailang"
def count 42
def ratio 3.14

# Typed definition
def .type int x 100
```

Supported types include:
*   Integers & Floats (with automatic big number promotion)
*   Strings (quoted `"..."`, `'...'`, `` `...` ``)
*   Lists `[ 1 2 3 ]`
*   Blocks `{ ... }`

### Functions

Functions are first-class citizens and can be defined using `func`.

```tailang
func greet(name) {
    fmt.sprintf "Hello, %v" name
}

greet "User"
# Output: "Hello, User"
```

### Control Flow

Tailang supports standard control structures including `if`, `while`, `foreach`, `switch`, and `repeat`.

```tailang
# If/Else
if > x 10 {
    fmt.println "Greater"
} else {
    fmt.println "Smaller"
}

# Foreach
foreach item ["a" "b" "c"] {
    fmt.println item
}

# While
def i 0
while < i 5 {
    set i (+ i 1)
}
```

### Pipelines

Pipelines allow chaining function calls, reducing the need for nested parentheses.

*   `|`: Passes the result of the previous expression as the **first** argument of the next function.
*   `|>`: Passes the result of the previous expression as the **last** argument of the next function.

```tailang
# Standard call
strings.to_upper "hello"

# Pipe to first
"hello" | strings.to_upper

# Chaining operations
"  tailang  " | strings.trim_space | strings.to_upper | fmt.println
# Output: TAILANG

# Pipe to last (useful for functions where the data comes last)
def list [1 2 3]
"," |> strings.join list
# Output: 1,2,3
```

### Named Parameters

Tailang supports named parameters mapping to struct fields, useful for configuration objects or complex function arguments.

```tailang
# Sets the field 'Val' on the 'cmd' object/function
cmd .val 42
```

## Developer Guide (Embedding)

To use Tailang in your Go project:

```go
package main

import (
    "fmt"
    "strings"
    "github.com/reusee/tai/tailang"
)

func main() {
    // 1. Create an Environment
    env := tailang.NewEnv()

    // 2. Define custom Go functions
    env.Define("my_func", tailang.GoFunc{
        Name: "my_func",
        Func: func(s string) string {
            return "Custom: " + s
        },
    })

    // 3. Evaluate code
    src := `
        def res ("world" | my_func)
        fmt.sprintf "Result: %v" res
    `
    tokenizer := tailang.NewTokenizer(strings.NewReader(src))
    res, err := env.Evaluate(tokenizer)
    if err != nil {
        panic(err)
    }
    fmt.Println(res)
}
```

## Standard Library

Tailang exposes a comprehensive set of Go's standard library packages by default, making it a powerful tool for scripting system tasks.

Supported packages include:
*   `fmt`: Formatting and printing.
*   `strings`, `bytes`: Text manipulation.
*   `math`: Mathematical constants and functions.
*   `os`, `path`, `filepath`: File system and environment interaction.
*   `time`: Time measurement and parsing.
*   `json`: JSON encoding/decoding.
*   `regexp`: Regular expressions.
*   `sort`: Sorting primitives.
*   `base64`, `hex`, `md5`, `sha256`: Encoding and hashing.

