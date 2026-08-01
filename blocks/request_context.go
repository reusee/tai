package blocks

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/pathutil"
)

const TheoryOfRequestContext = `
The request-context block allows the model to request additional context during
a generation cycle. When the model needs more information (e.g., file contents or
network resources), it emits a request-context block containing one or more XML
tags describing the desired context. The generate loop detects these blocks via
ParserState, fetches the requested data, appends it as user content, and initiates
another generation request. This block is strictly read-only: it must not produce
any side effects such as writing files or making state-changing API calls. The
order of XML tags within the block determines the order of context parts in the
appended user message. File path handling permits absolute paths as explicit
references while rejecting relative paths that escape the current directory via
parent-directory traversal, balancing flexibility with a basic sanity check.
Absolute file paths are resolved relative to the root directory when within it,
or read directly from the filesystem when outside it, so the model can reference
files in system directories like /tmp.
The fetch tag supports optional HTTP headers (user-agent, referer, cookie) so the
model can access resources that require them, but remains read-only (HTTP GET).
The glob tag lists files matching a pattern without reading their contents,
allowing the model to discover files before requesting their content. It applies
the same path sanity check as the file tag. Glob patterns are resolved relative
to the root directory so that doublestar.FilepathGlob searches within the root's
tree rather than the process's current working directory. The glob tag supports
** (globstar) patterns for recursive directory traversal via doublestar. When **
appears as a complete path segment, it matches zero or more directories.
Only request-context blocks are consumed from ParserState during context processing;
blocks of other kinds are preserved so they remain available after the context is provided.
`

const RequestContextSystemPrompt = `
**Request-Context Block Kind:**

The "request-context" kind enables requesting additional context needed to complete the task. When a file needs to be read or a network resource fetched, emit a request-context block. The system will fetch the requested data and provide it as user input for the next generation turn.

**Request-Context Block Format (complete example):**

<<龘靐 <request-context>
<file path="src/main.go" />
<fetch addr="https://example.com/api" />
<glob pattern="src/**/*.go" />
龘靐

The delimiter 龘靐 in the example is illustrative only: in every block emitted, choose exactly two uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line. The opening marker must start at the beginning of a line, and the closing line is the delimiter alone on its own line. Never write the placeholder text "DELIMITER" or reuse an example delimiter in a real marker.

**Supported XML Tags:**
- ` + "`<file path=\"...\" />`" + `: Read a local file at the given path. The path should be relative to the project root or absolute.
- ` + "`<fetch addr=\"...\" user-agent=\"...\" referer=\"...\" cookie=\"...\" />`" + `: Fetch content from a network address (HTTP GET). The addr should be a valid URL. The user-agent, referer, and cookie attributes are optional and set the corresponding HTTP headers on the request.
- ` + "`<glob pattern=\"...\" />`" + `: List files matching a glob pattern. The pattern should be relative to the project root or absolute. Returns matching file paths without reading their contents.

**Rules:**
- The order of XML tags determines the order of context parts in the response.
- This block is strictly read-only. It must not produce any side effects.
- After emitting a request-context block, stop generating and wait for the system to provide the requested context.
- Do not include request-context blocks alongside change blocks in the same response. If more context is needed, request it first, then emit change blocks in a subsequent response after the context is provided.

**Example:**

Need to see the content of a file to proceed...
<<齉爩 <request-context>
<file path="src/main.go" />
齉爩

Need to fetch a web page that requires a custom user-agent and cookie...
<<黿鼍 <request-context>
<fetch addr="https://example.com/api" user-agent="MyBot/1.0" cookie="session=abc123" />
黿鼍

Need to discover files matching a pattern...
<<龖爨 <request-context>
<glob pattern="src/**/*.go" />
龖爨
`

const RequestContextRestatePrompt = `- If additional context is needed (file contents, network resources, file listings), emit a request-context block:
<<齾麐 <request-context>
<file path="..." />
<fetch addr="..." user-agent="..." referer="..." cookie="..." />
<glob pattern="..." />
齾麐
- The user-agent, referer, and cookie attributes on the fetch tag are optional and set the corresponding HTTP headers.
- The glob tag lists files matching a pattern without reading their contents.
- After emitting a request-context block, stop and wait for the system to provide the context.
- The request-context block is read-only: never use it for writes or side effects.
- Do not emit change blocks in the same response as a request-context block. Request context first, then emit changes after the context is provided.
- The example delimiter 齾麐 is illustrative: choose two uncommon Chinese characters as the delimiter, the SAME delimiter on the closing line. The opening marker starts at the beginning of a line; the closing line is the delimiter alone. Never write the placeholder text "DELIMITER" or reuse an example delimiter literally.`

// RequestContextRequest represents a single context request parsed from the block body.
type RequestContextRequest struct {
	Type      string
	Path      string
	Addr      string
	UserAgent string
	Referer   string
	Cookie    string
	Pattern   string
}

// parseRequestContextBody parses the XML tags in a request-context block body.
func parseRequestContextBody(body string) ([]RequestContextRequest, error) {
	decoder := xml.NewDecoder(strings.NewReader(body))
	var requests []RequestContextRequest
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "file":
			var path string
			for _, attr := range start.Attr {
				if attr.Name.Local == "path" {
					path = attr.Value
				}
			}
			if path == "" {
				return nil, fmt.Errorf("file tag missing path attribute")
			}
			requests = append(requests, RequestContextRequest{Type: "file", Path: path})
		case "fetch":
			var addr, userAgent, referer, cookie string
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "addr":
					addr = attr.Value
				case "user-agent":
					userAgent = attr.Value
				case "referer":
					referer = attr.Value
				case "cookie":
					cookie = attr.Value
				}
			}
			if addr == "" {
				return nil, fmt.Errorf("fetch tag missing addr attribute")
			}
			requests = append(requests, RequestContextRequest{
				Type:      "fetch",
				Addr:      addr,
				UserAgent: userAgent,
				Referer:   referer,
				Cookie:    cookie,
			})
		case "glob":
			var pattern string
			for _, attr := range start.Attr {
				if attr.Name.Local == "pattern" {
					pattern = attr.Value
				}
			}
			if pattern == "" {
				return nil, fmt.Errorf("glob tag missing pattern attribute")
			}
			requests = append(requests, RequestContextRequest{Type: "glob", Pattern: pattern})
		}
	}
	return requests, nil
}

// fetchRequestContext fetches the requested context and returns parts.
// File read errors and fetch errors are returned as error text parts rather
// than aborting the entire generation, so the model can adapt.
func fetchRequestContext(ctx context.Context, root *os.Root, httpClient nets.HTTPClient, requests []RequestContextRequest) []generators.Part {
	var parts []generators.Part
	for _, req := range requests {
		switch req.Type {
		case "file":
			content, err := readContextFile(root, req.Path)
			if err != nil {
				parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"file\" path=%q>\n[error: %v]\n</context>\n\n", req.Path, err)))
				continue
			}
			parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"file\" path=%q>\n%s\n</context>\n\n", req.Path, content)))
		case "fetch":
			content, err := fetchURL(ctx, httpClient, req)
			if err != nil {
				parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"fetch\" addr=%q>\n[error: %v]\n</context>\n\n", req.Addr, err)))
				continue
			}
			parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"fetch\" addr=%q>\n%s\n</context>\n\n", req.Addr, content)))
		case "glob":
			matches, err := globFiles(root, req.Pattern)
			if err != nil {
				parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"glob\" pattern=%q>\n[error: %v]\n</context>\n\n", req.Pattern, err)))
				continue
			}
			parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"glob\" pattern=%q>\n%s\n</context>\n\n", req.Pattern, strings.Join(matches, "\n"))))
		}
	}
	return parts
}

// ProcessRequestContextBlocks checks request-context blocks, fetches the
// requested content, and appends it as user content to the state. Only
// request-context blocks are processed. The hasRequestContext flag indicates
// whether any request-context blocks were found, so callers can trigger a
// new round. See TheoryOfRequestContext.
func ProcessRequestContextBlocks(
	blocks []Block,
	ctx context.Context,
	root *os.Root,
	httpClient nets.HTTPClient,
	state generators.State,
) (generators.State, bool, error) {
	hasRequestContext := false
	for _, block := range blocks {
		hasRequestContext = true
		requests, parseErr := parseRequestContextBody(block.Body)
		if parseErr != nil {
			var appendErr error
			state, appendErr = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(fmt.Sprintf("[request-context parse error: %v]\n\n", parseErr)),
				},
			})
			if appendErr != nil {
				return state, hasRequestContext, appendErr
			}
			continue
		}
		parts := fetchRequestContext(ctx, root, httpClient, requests)
		if len(parts) > 0 {
			var appendErr error
			state, appendErr = state.AppendContent(&generators.Content{
				Role:  "user",
				Parts: parts,
			})
			if appendErr != nil {
				return state, hasRequestContext, appendErr
			}
		}
	}
	return state, hasRequestContext, nil
}

// readContextFile reads a local file at the given path. Absolute paths are
// permitted because they represent explicit, intentional references by the
// model. Relative paths containing parent-directory traversal are rejected as
// a sanity check against accidental escapes. The check distinguishes ".."
// (parent directory) and "../"-prefixed paths from names that merely start with
// two dots (e.g., "..hidden", "..."). Absolute paths are resolved relative to
// the root directory when within it, or read directly from the filesystem when
// outside it, so the model can reference files in system directories like /tmp.
// See TheoryOfRequestContext.
func readContextFile(root *os.Root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		cleaned := filepath.Clean(path)
		if pathutil.EscapesDir(cleaned) {
			return "", fmt.Errorf("path escapes current directory: %s", path)
		}
	}
	// Absolute paths are permitted as explicit references. os.Root methods
	// reject absolute paths, so convert to a root-relative path when the
	// absolute path is within the root, or fall back to os.ReadFile for
	// paths outside the root. See TheoryOfRequestContext.
	if filepath.IsAbs(path) {
		rootDir, err := filepath.Abs(root.Name())
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err == nil && !pathutil.EscapesDir(filepath.Clean(rel)) {
			content, err := root.ReadFile(rel)
			if err == nil {
				return string(content), nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(content), nil
	}
	content, err := root.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// globFiles lists files matching a glob pattern. It applies the same path
// sanity check as readContextFile: absolute patterns are permitted, while
// relative patterns containing parent-directory traversal are rejected.
// Patterns are resolved relative to the root directory via
// doublestar.FilepathGlob, which natively handles ** (globstar) patterns for
// recursive directory traversal. When ** appears as a complete path segment, it
// matches zero or more directories. This unifies all glob-based file matching
// across the system on the doublestar library, replacing the prior mix of
// filepath.Glob and a custom ** walker. See TheoryOfPatternMatching in
// anytexts/code_provider.go.
func globFiles(root *os.Root, pattern string) ([]string, error) {
	if !filepath.IsAbs(pattern) {
		cleaned := filepath.Clean(pattern)
		if pathutil.EscapesDir(cleaned) {
			return nil, fmt.Errorf("pattern escapes current directory: %s", pattern)
		}
	}
	// Resolve the pattern relative to the root directory so that
	// doublestar.FilepathGlob searches within the root's tree,
	// not the process's current working directory. See TheoryOfRequestContext.
	rootDir, err := filepath.Abs(root.Name())
	if err != nil {
		return nil, err
	}
	searchPattern := pattern
	if !filepath.IsAbs(pattern) {
		searchPattern = filepath.Join(rootDir, pattern)
	}
	// doublestar.FilepathGlob unifies glob expansion with native ** support.
	// WithFilesOnly excludes directories, matching the prior globWithDoubleStar
	// behavior where filepath.Walk skipped directories.
	// See TheoryOfPatternMatching in anytexts/code_provider.go.
	matches, err := doublestar.FilepathGlob(searchPattern, doublestar.WithFilesOnly())
	if err != nil {
		return nil, err
	}
	// Filter matches to those within the root. Convert absolute paths
	// to root-relative paths for the stat check, since os.Root methods
	// do not accept absolute paths. See TheoryOfRequestContext.
	var filtered []string
	for _, m := range matches {
		relPath := m
		if filepath.IsAbs(m) {
			rel, relErr := filepath.Rel(rootDir, m)
			if relErr != nil || pathutil.EscapesDir(filepath.Clean(rel)) {
				continue
			}
			relPath = rel
		}
		if _, statErr := root.Stat(relPath); statErr == nil {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

func fetchURL(ctx context.Context, httpClient nets.HTTPClient, req RequestContextRequest) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", req.Addr, nil)
	if err != nil {
		return "", err
	}
	if req.UserAgent != "" {
		httpReq.Header.Set("User-Agent", req.UserAgent)
	}
	if req.Referer != "" {
		httpReq.Header.Set("Referer", req.Referer)
	}
	if req.Cookie != "" {
		httpReq.Header.Set("Cookie", req.Cookie)
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
