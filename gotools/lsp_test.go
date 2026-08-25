package gotools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/modes"
)

// newFakeGoplsClient returns a goplsClient wired to an in-process fake
// server over net.Pipe. The fake answers initialize itself and delegates
// every other request to serve; serve returns a result value or an error,
// both sent as a JSON-RPC response. Notifications are ignored except
// exit, which ends the server goroutine.
func newFakeGoplsClient(t *testing.T, serve func(method string, params json.RawMessage) (any, error)) *goplsClient {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	client := newGoplsClient(clientConn)
	t.Cleanup(client.Close)
	go func() {
		defer serverConn.Close()
		reader := bufio.NewReader(serverConn)
		for {
			msg, err := readJSONRPCMessage(reader)
			if err != nil {
				return
			}
			if msg.ID == nil {
				if msg.Method == "exit" {
					return
				}
				continue
			}
			if msg.Method == "initialize" {
				_ = writeJSONRPCMessage(serverConn, &jsonrpcMessage{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Result:  json.RawMessage(`{"capabilities":{}}`),
				})
				continue
			}
			var result any
			var serveErr error
			if serve != nil {
				result, serveErr = serve(msg.Method, msg.Params)
			}
			resp := &jsonrpcMessage{
				JSONRPC: "2.0",
				ID:      msg.ID,
			}
			if serveErr != nil {
				resp.Error = &jsonrpcError{Message: serveErr.Error()}
			} else {
				resp.Result = mustResult(result)
			}
			if err := writeJSONRPCMessage(serverConn, resp); err != nil {
				return
			}
		}
	}()
	if err := client.initialize(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return client
}

func TestLSPFileURI(t *testing.T) {
	path := "/tmp/a b.go"
	uri := lspFileURI(path)
	if uri != "file:///tmp/a%20b.go" {
		t.Fatalf("unexpected uri %q", uri)
	}
	if got := lspURIPath(uri); got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
	if got := lspURIPath("file:///home/x/y.go"); got != "/home/x/y.go" {
		t.Fatalf("unexpected path %q", got)
	}
	if got := lspURIPath("not-a-uri"); got != "not-a-uri" {
		t.Fatalf("non-file uri must pass through, got %q", got)
	}
}

func TestResolveLSPMethod(t *testing.T) {
	cases := map[string]string{
		"definition":              "textDocument/definition",
		"Definition":              "textDocument/definition",
		"textDocument/definition": "textDocument/definition",
		"typeDefinition":          "textDocument/typeDefinition",
		"references":              "textDocument/references",
		"hover":                   "textDocument/hover",
		"implementation":          "textDocument/implementation",
		"documentSymbol":          "textDocument/documentSymbol",
		"symbols":                 "textDocument/documentSymbol",
		"symbol":                  "workspace/symbol",
		"workspace/symbol":        "workspace/symbol",
	}
	for in, want := range cases {
		got, err := resolveLSPMethod(in)
		if err != nil {
			t.Fatalf("resolveLSPMethod(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("resolveLSPMethod(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"rename", "completion", "textDocument/rename", ""} {
		if _, err := resolveLSPMethod(in); err == nil {
			t.Fatalf("resolveLSPMethod(%q) must be rejected", in)
		}
	}
}

func TestBestLSPSymbolMatch(t *testing.T) {
	syms := []lspSymbolInformation{
		{Name: "Read", ContainerName: "Writer", Location: lspLocation{URI: "file:///w.go"}},
		{Name: "Read", ContainerName: "Reader", Location: lspLocation{URI: "file:///r.go"}},
	}
	got, ok := bestLSPSymbolMatch(syms, "Reader.Read")
	if !ok {
		t.Fatal("expected match")
	}
	if got.Location.URI != "file:///r.go" {
		t.Fatalf("expected container match, got %s", got.Location.URI)
	}

	// Bare name: the first exact name match wins.
	got, ok = bestLSPSymbolMatch(syms, "Read")
	if !ok || got.ContainerName != "Writer" {
		t.Fatalf("expected first exact name match, got %+v ok=%v", got, ok)
	}
}

func TestRenderLSPLocations(t *testing.T) {
	locs := []lspLocation{{
		URI:   "file:///proj/b.go",
		Range: lspRange{Start: lspPosition{Line: 30, Character: 4}},
	}}
	if got := renderLSPLocations(locs); got != "/proj/b.go:31:5" {
		t.Fatalf("unexpected render %q", got)
	}
}

func TestRenderLSPSymbolInformations(t *testing.T) {
	syms := []lspSymbolInformation{{
		Name:          "Builder",
		Kind:          23,
		ContainerName: "strings",
		Location: lspLocation{
			URI:   "file:///x.go",
			Range: lspRange{Start: lspPosition{Line: 10, Character: 0}},
		},
	}}
	got := renderLSPSymbolInformations(syms)
	if got != "strings.Builder (struct) /x.go:11:1" {
		t.Fatalf("unexpected render %q", got)
	}
}

func TestRenderLSPDocumentSymbols(t *testing.T) {
	syms := []lspDocumentSymbol{{
		Name:           "main",
		Kind:           12,
		SelectionRange: lspRange{Start: lspPosition{Line: 0}},
		Children: []lspDocumentSymbol{{
			Name:           "Greeter",
			Kind:           23,
			SelectionRange: lspRange{Start: lspPosition{Line: 2}},
		}},
	}}
	got := renderLSPDocumentSymbols(syms)
	want := "main (function) :1\n  Greeter (struct) :3"
	if got != want {
		t.Fatalf("unexpected render %q, want %q", got, want)
	}
}

func TestGoplsLSPHandlerHoverBySymbol(t *testing.T) {
	var hoverParams lspTextDocumentPositionParams
	client := newFakeGoplsClient(t, func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "workspace/symbol":
			return []lspSymbolInformation{{
				Name: "Foo",
				Kind: 12,
				Location: lspLocation{
					URI:   lspFileURI("/proj/a.go"),
					Range: lspRange{Start: lspPosition{Line: 4, Character: 6}},
				},
			}}, nil
		case "textDocument/hover":
			if err := json.Unmarshal(params, &hoverParams); err != nil {
				return nil, err
			}
			var hover lspHover
			hover.Contents.Kind = "markdown"
			hover.Contents.Value = "func Foo() int"
			return hover, nil
		}
		return nil, fmt.Errorf("unexpected method %s", method)
	})
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/proj")
	text, err := handler(context.Background(), blocks.LSPQuery{Method: "hover", Symbol: "Foo"})
	if err != nil {
		t.Fatal(err)
	}
	if text != "func Foo() int" {
		t.Fatalf("unexpected hover text %q", text)
	}
	if hoverParams.TextDocument.URI != lspFileURI("/proj/a.go") {
		t.Fatalf("unexpected document uri %q", hoverParams.TextDocument.URI)
	}
	if hoverParams.Position != (lspPosition{Line: 4, Character: 6}) {
		t.Fatalf("unexpected position %+v", hoverParams.Position)
	}
}

func TestGoplsLSPHandlerDefinitionByLine(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "a.go")
	var defParams lspTextDocumentPositionParams
	client := newFakeGoplsClient(t, func(method string, params json.RawMessage) (any, error) {
		if method != "textDocument/definition" {
			return nil, fmt.Errorf("unexpected method %s", method)
		}
		if err := json.Unmarshal(params, &defParams); err != nil {
			return nil, err
		}
		return []lspLocation{{
			URI:   lspFileURI(filepath.Join(dir, "b.go")),
			Range: lspRange{Start: lspPosition{Line: 30, Character: 4}},
		}}, nil
	})
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, dir)
	text, err := handler(context.Background(), blocks.LSPQuery{Method: "definition", Path: "a.go", Line: 12})
	if err != nil {
		t.Fatal(err)
	}
	if defParams.TextDocument.URI != lspFileURI(absPath) {
		t.Fatalf("unexpected document uri %q", defParams.TextDocument.URI)
	}
	if defParams.Position != (lspPosition{Line: 11, Character: 0}) {
		t.Fatalf("unexpected position %+v", defParams.Position)
	}
	if !strings.Contains(text, "b.go:31:5") {
		t.Fatalf("expected rendered location, got %q", text)
	}
}

func TestGoplsLSPHandlerWorkspaceSymbol(t *testing.T) {
	client := newFakeGoplsClient(t, func(method string, params json.RawMessage) (any, error) {
		if method != "workspace/symbol" {
			return nil, fmt.Errorf("unexpected method %s", method)
		}
		return []lspSymbolInformation{{
			Name:          "Builder",
			Kind:          23,
			ContainerName: "strings",
			Location: lspLocation{
				URI:   "file:///x.go",
				Range: lspRange{Start: lspPosition{Line: 10, Character: 0}},
			},
		}}, nil
	})
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/proj")
	text, err := handler(context.Background(), blocks.LSPQuery{Method: "workspace/symbol", Query: "Builder"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "strings.Builder (") {
		t.Fatalf("expected rendered symbol, got %q", text)
	}
}

func TestGoplsLSPHandlerDocumentSymbol(t *testing.T) {
	client := newFakeGoplsClient(t, func(method string, params json.RawMessage) (any, error) {
		if method != "textDocument/documentSymbol" {
			return nil, fmt.Errorf("unexpected method %s", method)
		}
		return []lspDocumentSymbol{{
			Name:           "main",
			Kind:           12,
			SelectionRange: lspRange{Start: lspPosition{Line: 0}},
			Children: []lspDocumentSymbol{{
				Name:           "Greeter",
				Kind:           23,
				SelectionRange: lspRange{Start: lspPosition{Line: 2}},
			}},
		}}, nil
	})
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/proj")
	text, err := handler(context.Background(), blocks.LSPQuery{Method: "documentSymbol", Path: "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "main (function) :1") || !strings.Contains(text, "  Greeter (struct) :3") {
		t.Fatalf("expected rendered tree, got %q", text)
	}
}

func TestGoplsLSPHandlerReferencesExcludesDeclaration(t *testing.T) {
	var refParams lspReferenceParams
	client := newFakeGoplsClient(t, func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "workspace/symbol":
			return []lspSymbolInformation{{
				Name: "Foo",
				Kind: 12,
				Location: lspLocation{
					URI:   lspFileURI("/p/a.go"),
					Range: lspRange{Start: lspPosition{Line: 0, Character: 0}},
				},
			}}, nil
		case "textDocument/references":
			if err := json.Unmarshal(params, &refParams); err != nil {
				return nil, err
			}
			return []lspLocation{{
				URI:   lspFileURI("/p/b.go"),
				Range: lspRange{Start: lspPosition{Line: 2, Character: 0}},
			}}, nil
		}
		return nil, fmt.Errorf("unexpected method %s", method)
	})
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/p")
	text, err := handler(context.Background(), blocks.LSPQuery{Method: "references", Symbol: "Foo"})
	if err != nil {
		t.Fatal(err)
	}
	if refParams.Context.IncludeDeclaration {
		t.Fatal("references must exclude the declaration")
	}
	if !strings.Contains(text, "/p/b.go:3:1") {
		t.Fatalf("expected rendered reference, got %q", text)
	}
}

func TestGoplsLSPHandlerEmptyResults(t *testing.T) {
	client := newFakeGoplsClient(t, func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "workspace/symbol":
			return []lspSymbolInformation{}, nil
		case "textDocument/hover":
			return nil, nil
		case "textDocument/definition":
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected method %s", method)
	})
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/p")

	text, err := handler(context.Background(), blocks.LSPQuery{Method: "workspace/symbol", Query: "Nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "no symbols matched") {
		t.Fatalf("expected empty-result message, got %q", text)
	}

	text, err = handler(context.Background(), blocks.LSPQuery{Method: "hover", Path: "a.go", Line: 3})
	if err != nil {
		t.Fatal(err)
	}
	if text != "no hover information at position" {
		t.Fatalf("expected empty hover message, got %q", text)
	}

	text, err = handler(context.Background(), blocks.LSPQuery{Method: "definition", Path: "a.go", Line: 3})
	if err != nil {
		t.Fatal(err)
	}
	if text != "no locations found" {
		t.Fatalf("expected empty definition message, got %q", text)
	}
}

func TestGoplsLSPHandlerUnsupportedMethod(t *testing.T) {
	client := newFakeGoplsClient(t, nil)
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/p")
	_, err := handler(context.Background(), blocks.LSPQuery{Method: "rename", Symbol: "Foo"})
	if err == nil {
		t.Fatal("expected error for unsupported method")
	}
	if !strings.Contains(err.Error(), "unsupported lsp method") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoplsLSPHandlerMissingTarget(t *testing.T) {
	client := newFakeGoplsClient(t, nil)
	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, "/p")
	_, err := handler(context.Background(), blocks.LSPQuery{Method: "hover"})
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "requires a symbol or a line") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLSPHandlerProvider(t *testing.T) {
	// modes.ForTest supplies *testing.T, which the module graph needs
	// (logs.Module's Writer provider); without it dscope.New panics.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Call(func(handler blocks.LSPHandler) {
		if handler == nil {
			t.Fatal("expected non-nil LSPHandler")
		}
	})
}

func TestGoplsIntegration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module lspdemo\n\ngo 1.22\n")
	write("main.go", "package main\n\ntype Greeter struct{}\n\nfunc (Greeter) Greet() string { return \"hi\" }\n\nfunc main() {\n\tprintln(Greeter{}.Greet())\n}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := startGopls(ctx, dir, nil)
	if err != nil {
		t.Skipf("gopls failed to start: %v", err)
	}
	t.Cleanup(client.Close)

	handler := goplsLSPHandler(func(context.Context) (*goplsClient, error) {
		return client, nil
	}, dir)

	text, err := handler(ctx, blocks.LSPQuery{Method: "workspace/symbol", Query: "Greeter"})
	if err != nil {
		t.Fatalf("workspace/symbol: %v", err)
	}
	if !strings.Contains(text, "Greeter") {
		t.Fatalf("expected Greeter in workspace/symbol result: %q", text)
	}

	text, err = handler(ctx, blocks.LSPQuery{Method: "definition", Symbol: "Greeter.Greet"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if !strings.Contains(text, "main.go:") {
		t.Fatalf("expected main.go location in definition result: %q", text)
	}
}
