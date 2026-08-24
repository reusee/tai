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

func TestParseReadBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected []ReadRequest
		wantErr  bool
	}{
		{
			name: "single file",
			body: `<file path="src/main.go" />`,
			expected: []ReadRequest{
				{Type: "file", Path: "src/main.go"},
			},
		},
		{
			name: "single fetch",
			body: `<fetch addr="https://example.com/api" />`,
			expected: []ReadRequest{
				{Type: "fetch", Addr: "https://example.com/api"},
			},
		},
		{
			name: "fetch with headers",
			body: `<fetch addr="https://example.com/api" user-agent="MyBot/1.0" referer="https://ref.example.com" cookie="session=abc123" />`,
			expected: []ReadRequest{
				{Type: "fetch", Addr: "https://example.com/api", UserAgent: "MyBot/1.0", Referer: "https://ref.example.com", Cookie: "session=abc123"},
			},
		},
		{
			name: "fetch with partial headers",
			body: `<fetch addr="https://example.com/api" user-agent="MyBot/1.0" />`,
			expected: []ReadRequest{
				{Type: "fetch", Addr: "https://example.com/api", UserAgent: "MyBot/1.0"},
			},
		},
		{
			name: "single glob",
			body: `<glob pattern="src/*.go" />`,
			expected: []ReadRequest{
				{Type: "glob", Pattern: "src/*.go"},
			},
		},
		{
			name: "multiple mixed",
			body: `<file path="a.go" />` + "\n" + `<fetch addr="https://x.com" />` + "\n" + `<file path="b.go" />`,
			expected: []ReadRequest{
				{Type: "file", Path: "a.go"},
				{Type: "fetch", Addr: "https://x.com"},
				{Type: "file", Path: "b.go"},
			},
		},
		{
			name: "multiple mixed with glob",
			body: `<file path="a.go" />` + "\n" + `<glob pattern="*.go" />` + "\n" + `<fetch addr="https://x.com" />`,
			expected: []ReadRequest{
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
			got, err := parseReadBody(tc.body)
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

func TestFetchReadRequestsFile(t *testing.T) {
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

	requests := []ReadRequest{
		{Type: "file", Path: "test.txt"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestFetchReadRequestsGlob(t *testing.T) {
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

	requests := []ReadRequest{
		{Type: "glob", Pattern: "*.go"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestFetchReadRequestsGlobDoubleStar(t *testing.T) {
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

	requests := []ReadRequest{
		{Type: "glob", Pattern: "**/*.go"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestFetchReadRequestsFetch(t *testing.T) {
	responseBody := "fetch response body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(responseBody))
	}))
	defer server.Close()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	requests := []ReadRequest{
		{Type: "fetch", Addr: server.URL},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestFetchReadRequestsHeaders(t *testing.T) {
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

	requests := []ReadRequest{
		{Type: "fetch", Addr: server.URL, UserAgent: "MyBot/1.0", Referer: "https://ref.example.com", Cookie: "session=abc123"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestFetchReadRequestsError(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	// File not found
	requests := []ReadRequest{
		{Type: "file", Path: "nonexistent.txt"},
	}
	parts := fetchReadRequests(context.Background(), root, nets.HTTPClient{&http.Client{}}, requests)
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

func TestReadBlockPromptsRequireSummary(t *testing.T) {
	// The read prompts must not license omitting the summary
	// block: the stop rule is phrased summary-first — emit the summary
	// block IMMEDIATELY after the last read block's closing line, then
	// end the response — and the block is declared not to replace the
	// summary block. A bare "stop generating ..." instruction placed
	// before the summary requirement makes the model halt at the closing
	// line and is the most likely cause of missing summary blocks after
	// read blocks. See TheoryOfReadBlocks and TheoryOfSummaryBlocks.
	if !strings.Contains(ReadBlockSystemPrompt, "read block is NOT a completion signal") {
		t.Fatal("system prompt must state that the read block is not a completion signal and a summary block is still required")
	}
	if !strings.Contains(ReadBlockSystemPrompt, "emit the summary block IMMEDIATELY") {
		t.Fatal("system prompt must phrase the stop rule summary-first: emit the summary block immediately after the last read block")
	}
	if strings.Contains(ReadBlockSystemPrompt, "stop generating") {
		t.Fatal("system prompt must not carry a bare stop instruction before the summary requirement")
	}
	if !strings.Contains(ReadBlockSystemPrompt, "never stop at") {
		t.Fatal("system prompt must forbid stopping at a read block's closing line")
	}
	if !strings.Contains(ReadBlockSystemPrompt, "Never end a response on a read block") {
		t.Fatal("system prompt must state the sequence rule: the block after a read block must be the summary block")
	}
	if !strings.Contains(ReadBlockRestatePrompt, "does NOT replace the summary block") {
		t.Fatal("restate prompt must state that a read block does not replace the summary block")
	}
	if !strings.Contains(ReadBlockRestatePrompt, "Never end a response on a read block") {
		t.Fatal("restate prompt must state the sequence rule: the block after a read block must be the summary block")
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

func TestProcessReadBlocksAppendsFetchedContent(t *testing.T) {
	// ProcessReadBlocks only processes read blocks. Non-read blocks
	// are not passed to it (filtered by ProcessComponents), so this test
	// verifies that read blocks are processed correctly.
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := "test content"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	state := generators.NewPrompts("", nil)
	readBlocks := []Block{
		{Kind: "read", Body: `<file path="test.txt" />`},
	}

	newState, hasRead, err := ProcessReadBlocks(readBlocks, context.Background(), root, nets.HTTPClient{&http.Client{}}, state)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRead {
		t.Fatal("expected hasRead=true")
	}

	// Verify content was appended to state.
	found := false
	for c := range newState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok {
				if strings.Contains(string(text), content) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected fetched content in state")
	}
}

func TestProcessReadBlocksFiltersByKind(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	content := "test content"
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	state := generators.NewPrompts("", nil)

	// Non-read blocks must not set hasRead or append content.
	// Before kind filtering, hasRead was set unconditionally
	// for every block, causing false positives and parse attempts on
	// non-read bodies.
	blocks := []Block{
		{Kind: "change", Body: "some change"},
		{Kind: "summary", Body: "- done"},
	}
	_, hasRead, err := ProcessReadBlocks(blocks, context.Background(), root, nets.HTTPClient{&http.Client{}}, state)
	if err != nil {
		t.Fatal(err)
	}
	if hasRead {
		t.Fatal("expected hasRead=false for non-read blocks")
	}

	// Mixed blocks: only read blocks should be processed.
	mixed := []Block{
		{Kind: "change", Body: "some change"},
		{Kind: "read", Body: `<file path="test.txt" />`},
		{Kind: "summary", Body: "- done"},
	}
	newState, hasRead, err := ProcessReadBlocks(mixed, context.Background(), root, nets.HTTPClient{&http.Client{}}, state)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRead {
		t.Fatal("expected hasRead=true for mixed blocks with read")
	}
	found := false
	for c := range newState.Contents() {
		for _, p := range c.Parts {
			if text, ok := p.(generators.Text); ok {
				if strings.Contains(string(text), content) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected read block to be processed in mixed blocks")
	}
}
