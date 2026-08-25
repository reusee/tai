package blocks

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
)

func TestParseReadBodyLSP(t *testing.T) {
	got, err := parseReadBody(`<lsp method="references" symbol="Reader.Read" />`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	req := got[0]
	if req.Type != "lsp" || req.Method != "references" || req.Symbol != "Reader.Read" {
		t.Fatalf("unexpected request: %+v", req)
	}
}

func TestFetchReadRequestsLSPNilHandler(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	requests := []ReadRequest{
		{Type: "lsp", Method: "hover", Symbol: "Foo"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, nil, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), `<context type="lsp" method="hover Foo">`) {
		t.Fatalf("expected lsp context tag with method label, got %q", text)
	}
	if !strings.Contains(string(text), "no language server is available") {
		t.Fatalf("expected unavailability error, got %q", text)
	}
}

func TestFetchReadRequestsLSPHandler(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	var gotQuery LSPQuery
	handler := LSPHandler(func(ctx context.Context, q LSPQuery) (string, error) {
		gotQuery = q
		return "hover text", nil
	})
	requests := []ReadRequest{
		{Type: "lsp", Method: "hover", Path: "a.go", Line: 12},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, handler, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), "<context type=\"lsp\"") {
		t.Fatalf("expected lsp context tag, got %q", text)
	}
	if !strings.Contains(string(text), "hover text") {
		t.Fatalf("expected handler result in part, got %q", text)
	}
	if gotQuery.Method != "hover" || gotQuery.Path != "a.go" || gotQuery.Line != 12 {
		t.Fatalf("unexpected query passed to handler: %+v", gotQuery)
	}
}

func TestFetchReadRequestsLSPHandlerError(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	handler := LSPHandler(func(ctx context.Context, q LSPQuery) (string, error) {
		return "", context.DeadlineExceeded
	})
	requests := []ReadRequest{
		{Type: "lsp", Method: "definition", Symbol: "Foo"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, handler, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), "[error: ") {
		t.Fatalf("expected error text in part, got %q", text)
	}
}
