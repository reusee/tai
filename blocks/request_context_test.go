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
	"github.com/reusee/tai/nets"
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := "hello world"
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Read existing file using root with relative path
	got, err := readContextFile(root, "test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Fatalf("expected %q, got %q", content, got)
	}

	// Read non-existent file
	_, err = readContextFile(root, "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}

	// Path escape
	_, err = readContextFile(root, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestReadContextFileNotPathEscapeForDoubleDotPrefix(t *testing.T) {
	// A filename starting with ".." but not representing parent-directory
	// traversal (e.g., "..notescape.txt") must not be rejected by the path
	// escape sanity check. Before the fix, strings.HasPrefix(cleaned, "..")
	// incorrectly matched any name starting with two dots.
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	filename := "..notescape.txt"
	content := "test content"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readContextFile(root, filename)
	if err != nil {
		t.Fatalf("unexpected error reading file with .. prefix: %v", err)
	}
	if got != content {
		t.Fatalf("expected %q, got %q", content, got)
	}
}

func TestGlobFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

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
	matches, err := globFiles(root, "*.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}

	// No matches
	matches, err = globFiles(root, "*.nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}

	// Path escape
	_, err = globFiles(root, "../../../etc/*")
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestGlobFilesNotPathEscapeForDoubleDotPrefix(t *testing.T) {
	// A directory name starting with ".." but not representing parent-
	// directory traversal (e.g., "..notescape") must not be rejected by the
	// path escape sanity check. Before the fix, strings.HasPrefix(cleaned, "..")
	// incorrectly matched any name starting with two dots.
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	dotDir := filepath.Join(dir, "..notescape")
	if err := os.MkdirAll(dotDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotDir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotDir, "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	matches, err := globFiles(root, "..notescape/*.go")
	if err != nil {
		t.Fatalf("unexpected error globbing ..notescape/*.go: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}

func TestGlobFilesDoubleStar(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep", "c.go"), []byte("c"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep", "d.txt"), []byte("d"), 0644); err != nil {
		t.Fatal(err)
	}

	// **/*.go matches all .go files recursively
	matches, err := globFiles(root, "**/*.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 .go matches, got %d: %v", len(matches), matches)
	}

	// **/*.txt matches all .txt files recursively
	matches, err = globFiles(root, "**/*.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 .txt match, got %d: %v", len(matches), matches)
	}

	// sub/**/*.go matches .go files under sub/ recursively
	matches, err = globFiles(root, "sub/**/*.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches under sub/, got %d: %v", len(matches), matches)
	}

	// Bare ** matches all files recursively
	matches, err = globFiles(root, "**")
	if err != nil {
		t.Fatalf("unexpected error for bare **: %v", err)
	}
	if len(matches) != 4 {
		t.Fatalf("expected 4 matches for bare **, got %d: %v", len(matches), matches)
	}
}

func TestFetchRequestContextFile(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := "file content here"
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	requests := []RequestContextRequest{
		{Type: "file", Path: "test.txt"},
	}
	parts := fetchRequestContext(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	requests := []RequestContextRequest{
		{Type: "glob", Pattern: "*.go"},
	}
	parts := fetchRequestContext(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestFetchRequestContextGlobDoubleStar(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	requests := []RequestContextRequest{
		{Type: "glob", Pattern: "**/*.go"},
	}
	parts := fetchRequestContext(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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
}

func TestFetchRequestContextFetch(t *testing.T) {
	responseBody := "fetch response body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	// root is not used for fetch, but required by fetchRequestContext.
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	requests := []RequestContextRequest{
		{Type: "fetch", Addr: server.URL},
	}
	parts := fetchRequestContext(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	requests := []RequestContextRequest{
		{Type: "fetch", Addr: server.URL, UserAgent: "MyBot/1.0", Referer: "https://ref.example.com", Cookie: "session=abc123"},
	}
	parts := fetchRequestContext(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// File not found
	requests := []RequestContextRequest{
		{Type: "file", Path: "nonexistent.txt"},
	}
	parts := fetchRequestContext(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestReadContextFileAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := "absolute path content"
	path := filepath.Join(dir, "abs.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Read using absolute path — should resolve within the root.
	got, err := readContextFile(root, path)
	if err != nil {
		t.Fatalf("unexpected error reading absolute path: %v", err)
	}
	if got != content {
		t.Fatalf("expected %q, got %q", content, got)
	}
}

func TestGlobFilesAbsolutePattern(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	// Match .go files using an absolute pattern.
	pattern := filepath.Join(dir, "*.go")
	matches, err := globFiles(root, pattern)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}

func TestProcessRequestContextBlocksPreservesChangeBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewParserState(upstream)

	// Append a change block with no request-context blocks.
	text := ":::徕珑 <change op=\"MODIFY\" target=\"Foo\" file-path=\"/test.go\">\nfunc Foo() {}\n:::徕珑 </change>\n"
	if _, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	}); err != nil {
		t.Fatal(err)
	}

	// ProcessRequestContextBlocks must not discard non-request-context blocks.
	_, hasRC, err := ProcessRequestContextBlocks(state, context.Background(), nil, nets.HTTPClient{}, state)
	if err != nil {
		t.Fatal(err)
	}
	if hasRC {
		t.Fatal("expected no request-context blocks")
	}

	// The change block must still be available after processing.
	blocks := state.PopBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 change block to be preserved, got %d", len(blocks))
	}
	if blocks[0].Kind != "change" {
		t.Fatalf("expected change block, got %s", blocks[0].Kind)
	}
}
