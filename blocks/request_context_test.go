package blocks

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
			name: "fetch with headers",
			body: `<fetch addr="https://example.com/api" user-agent="MyBot/1.0" referer="https://ref.example.com" cookie="session=abc123" />`,
			expected: []RequestContextRequest{
				{Type: "fetch", Addr: "https://example.com/api", UserAgent: "MyBot/1.0", Referer: "https://ref.example.com", Cookie: "session=abc123"},
			},
		},
		{
			name: "fetch with partial headers",
			body: `<fetch addr="https://example.com/api" user-agent="MyBot/1.0" />`,
			expected: []RequestContextRequest{
				{Type: "fetch", Addr: "https://example.com/api", UserAgent: "MyBot/1.0"},
			},
		},
		{
			name: "single glob",
			body: `<glob pattern="src/*.go" />`,
			expected: []RequestContextRequest{
				{Type: "glob", Pattern: "src/*.go"},
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
			name: "multiple mixed with glob",
			body: `<file path="a.go" />` + "\n" + `<glob pattern="*.go" />` + "\n" + `<fetch addr="https://x.com" />`,
			expected: []RequestContextRequest{
				{Type: "file", Path: "a.go"},
				{Type: "glob", Pattern: "*.go"},
				{Type: "fetch", Addr: "https://x.com"},
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
		{
			name:    "glob missing pattern",
			body:    `<glob />`,
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
				if req != tc.expected[i] {
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

func TestGlobFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}

	// Match .go files
	matches, err := globFiles(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}

	// No matches
	matches, err = globFiles(filepath.Join(dir, "*.nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}

	// Path escape
	_, err = globFiles("../../../etc/*")
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

func TestFetchRequestContextGlob(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	requests := []RequestContextRequest{
		{Type: "glob", Pattern: filepath.Join(dir, "*.go")},
	}
	parts := fetchRequestContext(context.Background(), &http.Client{}, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), "a.go") {
		t.Fatalf("expected text to contain a.go: %q", text)
	}
	if !strings.Contains(string(text), "b.go") {
		t.Fatalf("expected text to contain b.go: %q", text)
	}
	if !strings.Contains(string(text), "<context type=\"glob\"") {
		t.Fatalf("expected text to contain glob context tag: %q", text)
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

func TestFetchRequestContextHeaders(t *testing.T) {
	var gotUserAgent, gotReferer, gotCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		gotCookie = r.Header.Get("Cookie")
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	requests := []RequestContextRequest{
		{Type: "fetch", Addr: server.URL, UserAgent: "MyBot/1.0", Referer: "https://ref.example.com", Cookie: "session=abc123"},
	}
	parts := fetchRequestContext(context.Background(), &http.Client{}, requests)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	text, ok := parts[0].(generators.Text)
	if !ok {
		t.Fatalf("expected Text part, got %T", parts[0])
	}
	if !strings.Contains(string(text), "ok") {
		t.Fatalf("expected text to contain response body, got %q", text)
	}
	if gotUserAgent != "MyBot/1.0" {
		t.Fatalf("expected User-Agent %q, got %q", "MyBot/1.0", gotUserAgent)
	}
	if gotReferer != "https://ref.example.com" {
		t.Fatalf("expected Referer %q, got %q", "https://ref.example.com", gotReferer)
	}
	if gotCookie != "session=abc123" {
		t.Fatalf("expected Cookie %q, got %q", "session=abc123", gotCookie)
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