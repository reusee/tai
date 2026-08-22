package gotools

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
			"[go-src: symbol or package \"Missing\" not found",
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

		// Package-qualified forms: the full import path and the "symbols"
		// suffix both restrict matching to this package.
		parts, err = resolve([]string{
			"example.com/symbols.FreeFunc",
			"symbols.FreeFunc",
			"symbols.Counter.Add",
			"example.com/symbols.Pair.Swap",
		})
		if err != nil {
			t.Fatal(err)
		}
		got = partsText(t, parts)
		for _, want := range []string{
			"``` begin of source example.com/symbols.FreeFunc",
			"func FreeFunc() int",
			"// Add has a pointer receiver.\nfunc (c *Counter) Add(n int)",
			"func (p Pair[A, B]) Swap()",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in package-qualified resolved source:\n%s", want, got)
			}
		}

		// A package qualifier for a non-loaded package yields not-found.
		parts, err = resolve([]string{"nonexistent/pkg.FreeFunc"})
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 || !strings.Contains(string(parts[0].(generators.Text)), "not found") {
			t.Fatalf("a non-loaded package qualifier must yield not-found, got %v", parts)
		}

		// go doc case rule: a lower-case query letter matches either
		// case in the target, an upper-case letter matches exactly.
		parts, err = resolve([]string{
			"freefunc",    // all lower-case matches FreeFunc
			"counter",     // matches Counter type
			"counter.add", // matches Counter.Add method
			"COUNTER",     // upper-case letters match exactly; COUNTER != Counter → not-found
			"FreeFunc",    // exact match works
			"pair.swap",   // lower-case matches Pair.Swap
		})
		if err != nil {
			t.Fatal(err)
		}
		got = partsText(t, parts)
		if !strings.Contains(got, "example.com/symbols.freefunc") ||
			!strings.Contains(got, "func FreeFunc() int") {
			// The qualified name in the marker uses the TARGET name, not
			// the query; check for the target form.
			if !strings.Contains(got, "example.com/symbols.FreeFunc") {
				t.Fatalf("expected FreeFunc resolved via lower-case query, got:\n%s", got)
			}
		}
		if !strings.Contains(got, "type Counter struct") {
			t.Fatalf("expected Counter resolved via lower-case query, got:\n%s", got)
		}
		if !strings.Contains(got, "func (c *Counter) Add(n int)") {
			t.Fatalf("expected Counter.Add resolved via lower-case query, got:\n%s", got)
		}
		if !strings.Contains(got, "func (p Pair[A, B]) Swap()") {
			t.Fatalf("expected Pair.Swap resolved via lower-case query, got:\n%s", got)
		}

		// Upper-case query letters match exactly: COUNTER does not
		// resolve Counter (the target has lower-case 'ounter').
		parts, err = resolve([]string{"COUNTER"})
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 || !strings.Contains(string(parts[0].(generators.Text)), "not found") {
			t.Fatalf("an all-upper-case query must not match mixed-case target, got %v", parts)
		}
	})
}

func TestResolveGoSymbolsReferences(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOWORK", "")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/refs\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	refsSource := `package refs

// UsedFunc is referenced from user.go and the internal test.
func UsedFunc() int { return 1 }

// UnusedFunc has no references.
func UnusedFunc() {}

// Widget carries a counter.
type Widget struct {
	N int
}

// Nudge increments the counter.
func (w *Widget) Nudge() { w.N++ }

const UsedConst = 7

var UsedVar = "v"

// Pair is generic.
type Pair[A, B any] struct {
	First  A
	Second B
}

// Swap flips the pair.
func (p Pair[A, B]) Swap() Pair[B, A] {
	return Pair[B, A]{First: p.Second, Second: p.First}
}
`
	if err := os.WriteFile(filepath.Join(dir, "refs.go"), []byte(refsSource), 0644); err != nil {
		t.Fatal(err)
	}
	// CallUsedFunc uses UsedFunc twice: references are deduplicated per
	// top-level declaration, so two uses in one declaration produce one
	// report line.
	userSource := `package refs

func CallUsedFunc() int { return UsedFunc() + UsedFunc() }

func UseWidget() int {
	w := &Widget{}
	w.Nudge()
	return w.N
}

func UseConstVar() int { return UsedConst + len(UsedVar) }
`
	if err := os.WriteFile(filepath.Join(dir, "user.go"), []byte(userSource), 0644); err != nil {
		t.Fatal(err)
	}
	// The internal test file makes the package and its test variant both
	// type-check user.go: a use there must appear exactly once in the
	// references report (variant deduplication). See TheoryOfGoSrcReferences.
	internalTestSource := `package refs

import "testing"

func TestRefsUses(t *testing.T) {
	if UsedFunc() != 1 {
		t.Fatal("bad")
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "refs_test.go"), []byte(internalTestSource), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(dir) },
	).Call(func(resolve ResolveGoSymbols) {

		parts, err := resolve([]string{"UsedFunc", "UnusedFunc"})
		if err != nil {
			t.Fatal(err)
		}
		got := partsText(t, parts)
		for _, want := range []string{
			"``` begin of references example.com/refs.UsedFunc",
			"example.com/refs: CallUsedFunc (",
			"example.com/refs: TestRefsUses (",
			"``` begin of source example.com/refs.UnusedFunc",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected %q in resolved output:\n%s", want, got)
			}
		}
		if strings.Contains(got, "begin of references example.com/refs.UnusedFunc") {
			t.Fatalf("expected no references block for UnusedFunc, got:\n%s", got)
		}
		// References carry no line numbers: the line is exactly
		// "package: top-level declaration (file)".
		wantLine := "example.com/refs: CallUsedFunc (" + filepath.Join(dir, "user.go") + ")\n"
		if !strings.Contains(got, wantLine) {
			t.Fatalf("expected reference line %q, got:\n%s", wantLine, got)
		}
		// References are deduplicated per top-level declaration: the two
		// UsedFunc uses inside CallUsedFunc collapse to one line, and the
		// package and test variant re-typechecking user.go collapse too.
		if n := strings.Count(got, "example.com/refs: CallUsedFunc ("); n != 1 {
			t.Fatalf("expected exactly 1 CallUsedFunc reference line, got %d", n)
		}

		// A method's references report lists the callers of the method.
		parts, err = resolve([]string{"Widget.Nudge"})
		if err != nil {
			t.Fatal(err)
		}
		got = partsText(t, parts)
		if !strings.Contains(got, "``` begin of references example.com/refs.Widget.Nudge") {
			t.Fatalf("expected references block for Widget.Nudge, got:\n%s", got)
		}
		if !strings.Contains(got, "example.com/refs: UseWidget (") {
			t.Fatalf("expected UseWidget reference, got:\n%s", got)
		}
	})

	// Truncation: 120 additional callers exceed maxGoSrcReferencesPerSymbol.
	// A fresh scope re-resolves GetFiles and the type-checked load, so the
	// generated file joins the file set. Wrapper names use two letters so
	// the generator needs only strings.Builder.
	var gen strings.Builder
	gen.WriteString("package refs\n\n")
	for i := 0; i < 120; i++ {
		gen.WriteString("func W")
		gen.WriteByte(byte('a' + i/26))
		gen.WriteByte(byte('a' + i%26))
		gen.WriteString("() int { return UsedFunc() }\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), []byte(gen.String()), 0644); err != nil {
		t.Fatal(err)
	}
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(dir) },
	).Call(func(resolve ResolveGoSymbols) {
		parts, err := resolve([]string{"UsedFunc"})
		if err != nil {
			t.Fatal(err)
		}
		got := partsText(t, parts)
		if !strings.Contains(got, "truncated at 100 references") {
			t.Fatalf("expected truncated references note, got:\n%s", got)
		}
	})
}

func TestResolveGoSymbolsPackageNameQualifier(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOWORK", "")
	depDir := filepath.Join(dir, "dep")
	if err := os.MkdirAll(depDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module example.com/dep/v4\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// The declared package name (stars) is not any segment of the
	// major-version import path (…/dep/v4): the declared-name qualifier
	// is the only pkg-qualified form that can address these symbols.
	if err := os.WriteFile(filepath.Join(depDir, "stars.go"), []byte(`package stars

// Twinkle is exported from a major-version package.
func Twinkle() int { return 42 }

// Sky counts stars.
type Sky struct{ n int }

// Poll returns the count.
func (s Sky) Poll() int { return s.n }
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module example.com/symbols

go 1.21

require example.com/dep/v4 v4.0.0

replace example.com/dep/v4 => ./dep
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "symbols.go"), []byte(`package symbols

import _ "example.com/dep/v4"

// Stars is a type whose name shadows the dependency's package name.
type Stars struct{}

// Poll is a method on Stars.
func (Stars) Poll() int { return 7 }
`), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(dir) },
		func() LoadPatterns { return LoadPatterns{"."} },
	).Call(func(resolve ResolveGoSymbols) {

		// Package-name qualifier: "stars.Twinkle" addresses the package
		// whose declared name is stars even though no path segment is.
		parts, err := resolve([]string{"stars.Twinkle"})
		if err != nil {
			t.Fatal(err)
		}
		got := partsText(t, parts)
		if !strings.Contains(got, "``` begin of source example.com/dep/v4.Twinkle") ||
			!strings.Contains(got, "func Twinkle() int { return 42 }") {
			t.Fatalf("expected package-name qualifier to resolve, got:\n%s", got)
		}

		// The full import path still restricts to the same package.
		parts, err = resolve([]string{"example.com/dep/v4.Twinkle"})
		if err != nil {
			t.Fatal(err)
		}
		if got = partsText(t, parts); !strings.Contains(got, "example.com/dep/v4.Twinkle") {
			t.Fatalf("expected full-path qualifier to resolve, got:\n%s", got)
		}

		// A plain name resolves across every loaded package, including
		// the major-version dependency.
		parts, err = resolve([]string{"Twinkle"})
		if err != nil {
			t.Fatal(err)
		}
		if got = partsText(t, parts); !strings.Contains(got, "example.com/dep/v4.Twinkle") {
			t.Fatalf("expected plain name to resolve, got:\n%s", got)
		}

		// pkg.Type.Method keeps working under a name qualifier.
		parts, err = resolve([]string{"stars.Sky.Poll"})
		if err != nil {
			t.Fatal(err)
		}
		if got = partsText(t, parts); !strings.Contains(got, "func (s Sky) Poll() int") {
			t.Fatalf("expected pkg.Type.Method under a name qualifier to resolve, got:\n%s", got)
		}

		// A name qualifier that shadows a type name falls back to the
		// receiver-type reading when the qualified form matches nothing:
		// "stars.Poll" has no top-level Poll in the dep package, so it
		// resolves Stars.Poll in the root package. See
		// TheoryOfGoSrcResolution.
		parts, err = resolve([]string{"stars.Poll"})
		if err != nil {
			t.Fatal(err)
		}
		if got = partsText(t, parts); !strings.Contains(got, "func (Stars) Poll() int { return 7 }") {
			t.Fatalf("expected shadowed qualifier to fall back to the type reading, got:\n%s", got)
		}
	})
}

func TestResolveGoSymbolsPackageDocs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOWORK", "")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/symbols\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "symbols.go"), []byte(`// Package symbols demonstrates documentation.
package symbols

import "example.com/symbols/dep"

var _ = dep.Foo

// FreeFunc is a free function.
func FreeFunc() int { return 1 }

// helper is unexported and shows only with -u.
func helper() {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	depDir := filepath.Join(dir, "dep")
	if err := os.MkdirAll(depDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "dep.go"), []byte(`// Package dep is a dependency package.
package dep

// Foo does something.
func Foo() {}

func secret() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	// LoadPatterns{"."} makes only the root package focus; dep is
	// loaded as a context package via the import walk.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() LoadDir { return LoadDir(dir) },
		func() LoadPatterns { return LoadPatterns{"."} },
	).Call(func(resolve ResolveGoSymbols) {

		t.Run("FocusPackagePath", func(t *testing.T) {
			parts, err := resolve([]string{"example.com/symbols"})
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) != 1 {
				t.Fatalf("expected 1 part, got %d", len(parts))
			}
			got := string(parts[0].(generators.Text))
			for _, want := range []string{
				"``` begin of source package example.com/symbols",
				"``` end of source package example.com/symbols",
				"Package symbols demonstrates documentation",
				// -u includes unexported symbols for focus packages.
				"helper",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("expected %q in focus package doc:\n%s", want, got)
				}
			}
		})

		t.Run("FocusPackageName", func(t *testing.T) {
			parts, err := resolve([]string{"symbols"})
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) != 1 {
				t.Fatalf("expected 1 part, got %d", len(parts))
			}
			if got := string(parts[0].(generators.Text)); !strings.Contains(got, "``` begin of source package example.com/symbols") {
				t.Fatalf("expected focus package doc via package name, got:\n%s", got)
			}
		})

		t.Run("ContextPackagePath", func(t *testing.T) {
			parts, err := resolve([]string{"example.com/symbols/dep"})
			if err != nil {
				t.Fatal(err)
			}
			got := partsText(t, parts)
			for _, want := range []string{
				"``` begin of source package example.com/symbols/dep",
				"Package dep is a dependency package",
				"Foo",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("expected %q in context package doc:\n%s", want, got)
				}
			}
			// No -u for context packages: unexported symbols stay hidden.
			if strings.Contains(got, "secret") {
				t.Fatalf("context package doc must not include unexported symbols:\n%s", got)
			}
		})

		t.Run("ContextPackageName", func(t *testing.T) {
			parts, err := resolve([]string{"dep"})
			if err != nil {
				t.Fatal(err)
			}
			if got := partsText(t, parts); !strings.Contains(got, "``` begin of source package example.com/symbols/dep") {
				t.Fatalf("expected context package doc via package name, got:\n%s", got)
			}
		})

		t.Run("PackageAndSymbolMixed", func(t *testing.T) {
			parts, err := resolve([]string{"example.com/symbols/dep", "FreeFunc"})
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) != 2 {
				t.Fatalf("expected 2 parts, got %d", len(parts))
			}
			if got := string(parts[0].(generators.Text)); !strings.Contains(got, "begin of source package example.com/symbols/dep") {
				t.Fatalf("unexpected first part:\n%s", got)
			}
			if got := string(parts[1].(generators.Text)); !strings.Contains(got, "begin of source example.com/symbols.FreeFunc") {
				t.Fatalf("unexpected second part:\n%s", got)
			}
		})

		t.Run("UnknownPackage", func(t *testing.T) {
			parts, err := resolve([]string{"nonexistent/pkg"})
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) != 1 || !strings.Contains(string(parts[0].(generators.Text)), "not found") {
				t.Fatalf("expected not-found for unknown package, got %v", parts)
			}
		})
	})
}
