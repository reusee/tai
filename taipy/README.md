# Taipy

**Taipy** is a lightweight, embedded scripting language for Go. It implements a Python-like syntax (using the Starlark parser) that compiles down to bytecode for the **TaiVM**, a custom stack-based virtual machine.

Taipy is designed to provide a familiar, Pythonic scripting environment that is easy to integrate into Go applications, supporting advanced features like closures, comprehensions, and rich argument handling.

## Features

Taipy supports a significant subset of Python 3 syntax:

*   **Data Types:** Integers, Floats, Strings, Lists, Dictionaries (Maps), Tuples, and dynamic Structs.
*   **Control Flow:** `if`, `elif`, `else`, `while`, `for` loops, `break`, `continue`.
*   **Functions:**
    *   First-class functions and closures.
    *   Lambda expressions (`lambda x: x + 1`).
    *   Complex argument handling: default values, keyword arguments, `*args`, and `**kwargs`.
*   **Comprehensions:** List comprehensions (`[x*2 for x in l]`) and Dictionary comprehensions (`{k:v for k,v in d.items()}`).
*   **Slicing:** Full Python slicing support `sequence[start:stop:step]` for lists and strings.
*   **Operators:** Arithmetic, bitwise, comparison, logical (`and`, `or`), and conditional expressions (`a if b else c`).
*   **Assignments:** Tuple/List unpacking (`a, b = 1, 2`) and augmented assignment (`+=`, `*=`, etc.).

## Installation

```bash
go get github.com/reusee/tai/taipy
```

## Quick Start

Here is how to embed Taipy in your Go application:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/reusee/tai/taipy"
)

func main() {
	// 1. Define your script
	src := `
def fib(n):
    if n <= 1:
        return n
    return fib(n-1) + fib(n-2)

print("Fibonacci of 10 is:", fib(10))

# List comprehension
squares = [x**2 for x in range(5)]
print("Squares:", squares)
`

	// 2. Create the VM instance
	// This compiles the source code into bytecode.
	vm, err := taipy.NewVM("example.py", strings.NewReader(src))
	if err != nil {
		panic(fmt.Errorf("compilation failed: %v", err))
	}

	// 3. Run the VM
	// vm.Run returns an iterator that yields on interrupts or completion.
	// For simple scripts, just iterate until finished.
	for _, runErr := range vm.Run {
		if runErr != nil {
			panic(fmt.Errorf("runtime error: %v", runErr))
		}
	}
	
	// 4. Access variables from Go
	if val, ok := vm.Get("squares"); ok {
		fmt.Printf("Result from VM: %v\n", val)
	}
}
```

## Language Reference

### Built-in Functions

Taipy comes with a standard set of native functions:

*   **`print(*args)`**: Prints arguments to stdout.
*   **`len(obj)`**: Returns the length of a string, list, map, or range.
*   **`range(stop)`** or **`range(start, stop[, step])`**: Generates a sequence of integers.
*   **`pow(x, y)`**: Returns `x` to the power of `y`.
*   **`struct(dict)`**: Creates a dynamic object with dot-notation access from a dictionary.

### Structs

The `struct` builtin allows for Javascript-object-like behavior:

```python
s = struct({"name": "Taipy", "version": 1})
print(s.name)
s.version += 1
```

### Argument Handling

Taipy supports Python's robust function argument syntax:

```python
def func(a, b=2, *args, **kwargs):
    print(a, b)
    print(args)   # Tuple of extra positional args
    print(kwargs) # Map of extra keyword args

func(1, c=3, d=4)
# Output:
# 1 2
# []
# {"c": 3, "d": 4}
```

## Architecture

1.  **Parser**: Taipy uses `go.starlark.net/syntax` to parse source code into an Abstract Syntax Tree (AST).
2.  **Compiler**: The `taipy` package walks the AST and compiles it into opcodes defined in `taivm`. It handles scope management, variable binding, and control flow patching.
3.  **VM**: The `taivm` package executes the bytecode. It is a stack-based VM that supports:
    *   Frame-based execution for function calls.
    *   Closures and upvalue capturing.
    *   Native function bridging.
    *   Cooperative multitasking (via `yield` in the Run loop).

## Development

To run the test suite, including coverage tests for the compiler and VM:

```bash
go test ./...
```

The core VM logic resides in the `github.com/reusee/tai/taivm` module, while the compiler and language binding reside in `taipy`.

