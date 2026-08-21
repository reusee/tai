package gocodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

const resolveSymbolsTestSource = `package symbols

// FreeFunc is a free function.
func FreeFunc() int { return 1 }

// Counter counts things.
type Counter struct {
	N int
}

// Add has a pointer receiver.
func (c *Counter) Add(n int) { c.N += n }

// Value has a value receiver.
func (c Counter) Value() int { return c.N }

// Pair is a generic type.
type Pair[A, B any] struct {
	First  A
	Second B
}

// Swap is a method on a generic type.
func (p Pair[A, B]) Swap() Pair[B, A] {
	return Pair[B, A]{First: p.Second, Second: p.First}
}

const ConstOne = 1

const (
	// GroupedA is grouped const A.
	GroupedA = 10
	GroupedB = 20
)

var VarOne = "one"

var (
	VarTwo, VarThree = 2, 3
)
`

// partsText concatenates the Text parts of a resolution result.
func partsText(t *testing.T, parts []generators.Part) string {
	t.Helper()
	var b strings.Builder
	for _, part := range parts {
		if text, ok := part.(generators.Text); ok {
			b.WriteString(string(text))
		}
	}
	return b.String()
}

func TestResolveGoSymbols(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOWORK", "")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/symbols\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "symbols.go"), []byte(resolveSymbolsTestSource), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(dir) },
	).Call(func(resolve ResolveGoSymbols) {

		parts, err := resolve([]string{
			"FreeFunc", "Counter", "Counter.Add", "Counter.Value",
			"Pair", "Pair.Swap", "*Pair.Swap", "Pair[B,A].Swap",
			"ConstOne", "GroupedA", "VarOne", "VarTwo",
			"Missing",
		})
		if err != nil {
			t.Fatal(err)
		}
		got := partsText(t, parts)
		for _, want := range []string{
			"``` begin of source example.com/symbols.FreeFunc",
			"// FreeFunc is a free function.\nfunc FreeFunc() int { return 1 }",
			"type Counter struct",
			"// Add has a pointer receiver.\nfunc (c *Counter) Add(n int)",
			"func (c Counter) Value() int",
			"type Pair[A, B any] struct",
			"func (p Pair[A, B]) Swap()",
			"const ConstOne = 1",
			"// GroupedA is grouped const A.\n\tGroupedA = 10",
			"var VarOne = \"one\"",
			"VarTwo, VarThree = 2, 3",
			"[go-src: symbol \"Missing\" not found",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in resolved source:\n%s", want, got)
			}
		}

		// The three method forms all resolve to the same declaration:
		// begin markers count matches (each match emits begin and end
		// markers carrying the qualified name).
		parts, err = resolve([]string{"Pair.Swap", "*Pair.Swap", "Pair[B,A].Swap"})
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(partsText(t, parts), "``` begin of source example.com/symbols.Pair.Swap"); n != 3 {
			t.Fatalf("expected 3 Pair.Swap matches, got %d", n)
		}

		// A plain name must not match a method; the method requires the
		// TypeName.MethodName form. See TheoryOfGoSrcResolution.
		parts, err = resolve([]string{"Add"})
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 || !strings.Contains(string(parts[0].(generators.Text)), "not found") {
			t.Fatalf("a plain name must not match methods, got %v", parts)
		}

		// A duplicated symbol is resolved once.
		parts, err = resolve([]string{"FreeFunc", "FreeFunc"})
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(partsText(t, parts), "``` begin of source"); n != 1 {
			t.Fatalf("expected 1 match for a duplicated symbol, got %d", n)
		}

		// Empty input resolves nothing.
		parts, err = resolve(nil)
		if err != nil || parts != nil {
			t.Fatalf("expected nil parts and nil error for empty input, got %v, %v", parts, err)
		}
	})
}
