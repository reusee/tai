package codes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/tai/generators"
)

func TestParseRequestContextBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected []RequestContextRequest
		wantErr  bool
	}{
		{
			name: "single file",
			body: `<file path="src/main.go" />`,
			expected: []RequestContextRequest{
				{Type: "file", Path: "src/main.go"},
			},
		},
		{
			name: "single fetch",
			body: `<fetch addr="https://example.com/api" />`,
			expected: []RequestContextRequest{
				{Type: "fetch", Addr: "https://example.com/api"},
			},
		},
		{
			name: "multiple mixed",
			body: `<file path="a.go" />` + "\n" + `<fetch addr="https://x.com" />` + "\n" + `<file path="b.go" />`,
			expected: []RequestContextRequest{
				{Type: "file", Path: "a.go"},
				{Type: "fetch", Addr: "https://x.com"},
				{Type: "file", Path: "b.go"},
			},
		},
		{
			name:     "empty body",
			body:     "",
			expected: nil,
		},
		{
			name:    "file missing path",
			body:    `<file />`,
			wantErr: true,
		},
		{
			name:    "fetch missing addr",
			body:    `<fetch />`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRequestContextBody(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d requests, got %d", len(tc.expected), len(got))
			}
			for i, req := range got {
				if req.Type != tc.expected[i].Type || req.Path != tc.expected[i].Path || req.Addr != tc.expected[i].Addr {
					t.Fatalf("request %d: expected %+v, got %+v", i, tc.expected[i], req)
				}
			}
		})
	}
}

func TestReadContextFile(t *testing.T) {
	dir := t.TempDir()
	content := "hello world"
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Read existing file
	got, err := readContextFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Fatalf("expected %q, got %q", content, got)
	}

	// Read non-existent file
	_, err = readContextFile(filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}

	// Path escape
	_, err = readContextFile("../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestFetchRequestContextFile(t *testing.T) {
	dir := t.TempDir()
	content := "file content here"
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	requests := []RequestContextRequest{
		{Type: "file", Path: path},
	}
	parts := fetchRequestContext(context.Background(), &http.Client{}, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), content) {
		t.Fatalf("expected text to contain %q, got %q", content, text)
	}
}

func TestFetchRequestContextFetch(t *testing.T) {
	responseBody := "fetch response body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	requests := []RequestContextRequest{
		{Type: "fetch", Addr: server.URL},
	}
	parts := fetchRequestContext(context.Background(), &http.Client{}, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), responseBody) {
		t.Fatalf("expected text to contain %q, got %q", responseBody, text)
	}
}

func TestFetchRequestContextError(t *testing.T) {
	// File not found
	requests := []RequestContextRequest{
		{Type: "file", Path: "/nonexistent/path/file.txt"},
	}
	parts := fetchRequestContext(context.Background(), &http.Client{}, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), "error") {
		t.Fatalf("expected error text, got %q", text)
	}
}