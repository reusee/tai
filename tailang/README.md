# tailang

`tailang` is a dynamic, embeddable scripting language implemented in Go. It is designed for seamless integration with Go applications, allowing direct mapping of Go functions and types to the scripting environment.

## Features

- **Go Integration**: Directly wrap and call Go functions.
- **Dynamic Typing**: Supports standard types (`int`, `float64`, `string`, `bool`, `list`, `function`).
- **Standard Library**: Built-in bindings for many Go standard library packages (`fmt`, `strings`, `math`, `os`, etc.).
- **Control Flow**: Includes `if`, `while`, `repeat`, `foreach`, and `switch`.
- **Function References**: First-class support for function passing.

## Usage

### Quick Start

```go
package main

import (
	"fmt"
	"strings"
	"github.com/reusee/tai/tailang"
)

func main() {
	// 1. Create an environment
	env := tailang.NewEnv()

	// 2. Define custom values or functions
	env.Define("greet", func(name string) {
		fmt.Printf("Hello, %s!\n", name)
	})

	// 3. Script source
	src := `
		def name "World"
		greet name
		
		def result (+ 40 2)
		fmt.printf "Result: %v\n" result
	`

	// 4. Parse and Evaluate
	tokenizer := tailang.NewTokenizer(strings.NewReader(src))
	_, err := env.Evaluate(tokenizer)
	if err != nil {
		panic(err)
	}
}
```

## Language Reference

### Comments
Lines starting with `#` are comments.

```tailang
# This is a comment
def x 1
```

### Variables
Use `def` to define new variables and `set` to update them.

```tailang
def x 10
set x 20
```

### Types
`tailang` supports:
- Integers and Floats
- Strings (single `'`, double `"`, or backtick `` ` `` quotes)
- Booleans
- Lists
- Functions

### Lists
Lists are created using brackets `[]`. They can be untyped (holding `any`) or typed.

```tailang
# Untyped list
def l1 [ 1 "two" 3.0 ]

# Typed list (using named parameter .elem)
def l2 [ .elem int 1 2 3 ]
```

### Operators
- Arithmetic: `+`, `-`, `*`, `/`, `%`
- Comparison: `==`, `!=`, `<`, `<=`, `>`, `>=`

### Control Flow

#### If / Else
```tailang
if > x 10 {
    fmt.println "Greater"
} else {
    fmt.println "Not greater"
}
```

#### While
The condition must be wrapped in a block `{ ... }`.
```tailang
def i 0
while { < i 5 } {
    fmt.println i
    set i (+ i 1)
}
```

#### Repeat
Loops a specific number of times. The loop variable (1-based) is defined in the block scope.
```tailang
repeat i 5 {
    fmt.printf "Iteration %d\n" i
}
```

#### Foreach
Iterates over a list.
```tailang
foreach item [ "a" "b" "c" ] {
    fmt.println item
}
```

#### Switch
```tailang
switch val {
    1 { fmt.println "one" }
    2 { fmt.println "two" }
}
```

### Functions

#### Definition
Define functions using `func`.
```tailang
func add(a b) {
    + a b
}
```

#### Calling
Call functions by name followed by arguments.
```tailang
add 1 2
```

#### Function References
Use `&` to reference a function object (e.g., for passing as an argument).
```tailang
func handler(v) {
    fmt.println v
}
# Pass handler to another function
some_func &handler
```

#### Variadic Functions
When calling Go variadic functions (other than list construction), use `end` to mark the end of arguments.
```tailang
fmt.printf "%d %d %d" 1 2 3 end
```

#### Named Parameters
Struct-based functions (like `[`) allow setting fields via named parameters syntax (`.Field val`).
```tailang
# Sets the Elem field of the List struct before calling
[ .elem int 1 2 3 ]
```

## Standard Library
The environment comes pre-loaded with many Go standard library functions converted to `snake_case`.

**Examples:**
- `fmt.print`, `fmt.printf`, `fmt.errorf`
- `strings.split`, `strings.join`, `strings.to_upper`
- `math.max`, `math.pow`, `math.abs`
- `time.now`, `time.sleep`, `time.parse`
- `os.get_env`, `os.read_file`
- `json.marshal`, `json.unmarshal`

Refer to `stdlib.go` for the complete list of registered functions.

