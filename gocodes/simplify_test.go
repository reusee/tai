package gocodes

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

func TestSimplify(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		dscope.Provide(configs.NewLoader(nil, "")),
	)

	dir := filepath.Join(testdataDir, "main")
	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		getFiles GetFiles,
		getGenerator generators.GetGenerator,
		simplifyFiles SimplifyFiles,
		count generators.GeminiTokenCounter,
	) {

		files, err := getFiles()
		if err != nil {
			t.Fatal(err)
		}

		files, err = simplifyFiles(files, 256, count("gemini-1.5-pro"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) < 2 {
			t.Fatalf("got %v", len(files))
		}
		t.Logf("num files: %v", len(files))
		if files[len(files)-1].Path != filepath.Join(dir, "main.go") {
			t.Fatalf("got %v", files[0].Path)
		}
		if files[len(files)-2].Path != filepath.Join(dir, "a.txt") {
			t.Fatalf("got %v", files[0].Path)
		}
		if files[len(files)-3].Path != filepath.Join(dir, "..", "dep1", "dep1.go") {
			t.Fatalf("got %v", files[1].Path)
		}

	})
}

func TestSimplifySingleFile(t *testing.T) {
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
		dscope.Provide(configs.NewLoader(nil, "")),
	)

	dir := t.TempDir()
	err := os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte(`
	package main

	func main() {}
		`),
		0644,
	)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`
module test
	`),
		0644,
	)
	if err != nil {
		t.Fatal(err)
	}

	scope.Fork(
		func() LoadDir {
			return LoadDir(dir)
		},
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(8192, countTokens)
		if err != nil {
			t.Fatal(err)
		}
		_ = parts
	})

}

func TestDeleteFunctionBodyPreservesDoc(t *testing.T) {
	fset := token.NewFileSet()
	const src = `package main

// This is the doc for main.
func main() {
       println("hello")
}
`
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	simplified := deleteFunctionBody(file)

	buf := new(bytes.Buffer)
	err = format.Node(buf, fset, simplified)
	if err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "// This is the doc for main.") {
		t.Errorf("doc comment was removed:\n%s", got)
	}
	if strings.Contains(got, `println("hello")`) {
		t.Errorf("function body was not removed:\n%s", got)
	}
	if !strings.Contains(got, `panic("function body omitted")`) {
		t.Errorf("function body not correctly replaced:\n%s", got)
	}
}

func TestDeleteFunctionBody(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test", `
package main

func main() {
       println("hello")
}

func foo() (int, error) {
       return 0, nil
}

func bar() {
       println("hello")
}
       `, 0)
	if err != nil {
		t.Fatal(err)
	}

	simplified := deleteFunctionBody(file)

	buf := new(bytes.Buffer)
	err = format.Node(buf, fset, simplified)
	if err != nil {
		t.Fatal(err)
	}

	text := buf.String()
	if strings.Count(text, `panic("function body omitted")`) != 3 {
		t.Fatalf("expected 3 panics, got %s", text)
	}
}
