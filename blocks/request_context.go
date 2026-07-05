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

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
)

const TheoryOfRequestContext = `
The request-context block allows the model to request additional context during
a generation cycle. When the model needs more information (e.g., file contents or
network resources), it emits a request-context block containing one or more XML
tags describing the desired context. The generate loop detects these blocks via
BlockState, fetches the requested data, appends it as user content, and initiates
another generation request. This block is strictly read-only: it must not produce
any side effects such as writing files or making state-changing API calls. The
order of XML tags within the block determines the order of context parts in the
appended user message. File path handling permits absolute paths as explicit
references while rejecting relative paths that escape the current directory via
parent-directory traversal, balancing flexibility with a basic sanity check.
The fetch tag supports optional HTTP headers (user-agent, referer, cookie) so the
model can access resources that require them, but remains read-only (HTTP GET).
The glob tag lists files matching a pattern without reading their contents,
allowing the model to discover files before requesting their content. It applies
the same path sanity check as the file tag.
`

const RequestContextSystemPrompt = `**Request-Context Block Kind:**

The "request-context" kind allows you to request additional context needed to complete the task. When you need to read a file or fetch a network resource, emit a request-context block. The system will fetch the requested data and provide it as user input for your next generation turn.

**Request-Context Block Format:**

:::request-context <boundary>
<one or more XML tags describing context requests>
:::end <boundary>

**Supported XML Tags:**
- ` + "`<file path=\"...\" />`" + `: Read a local file at the given path. The path should be relative to the project root or absolute.
- ` + "`<fetch addr=\"...\" user-agent=\"...\" referer=\"...\" cookie=\"...\" />`" + `: Fetch content from a network address (HTTP GET). The addr should be a valid URL. The user-agent, referer, and cookie attributes are optional and set the corresponding HTTP headers on the request.
- ` + "`<glob pattern=\"...\" />`" + `: List files matching a glob pattern. The pattern should be relative to the project root or absolute. Returns matching file paths without reading their contents.

**Rules:**
- The order of XML tags determines the order of context parts in the response.
- This block is strictly read-only. It must not produce any side effects.
- Use a distinct, freshly generated random boundary for each block, following the same boundary uniqueness rules as change blocks.
- After emitting a request-context block, stop generating and wait for the system to provide the requested context.
- Do not include request-context blocks alongside change blocks in the same response. If you need more context, request it first, then emit change blocks in a subsequent response after the context is provided.

**Example:**

I need to see the content of a file to proceed...
:::request-context 徕珑
<file path="src/main.go" />
:::end 徕珑

I need to fetch a web page that requires a custom user-agent and cookie...
:::request-context 栢彣
<fetch addr="https://example.com/api" user-agent="MyBot/1.0" cookie="session=abc123" />
:::end 栢彣

I need to discover files matching a pattern...
:::request-context 骐骎
<glob pattern="src/**/*.go" />
:::end 骐骎

Note: The boundaries above are illustrative only. **Never reuse these boundary strings.** Generate a fresh random pair of two uncommon, meaningless Chinese characters for every block.
`

const RequestContextRestatePrompt = `- If you need additional context (file contents, network resources, file listings), emit a request-context block:
:::request-context <random_boundary>
<file path="..." />
<fetch addr="..." user-agent="..." referer="..." cookie="..." />
<glob pattern="..." />
:::end <random_boundary>
- Use a distinct, freshly generated random boundary for each request-context block.
- The user-agent, referer, and cookie attributes on the fetch tag are optional and set the corresponding HTTP headers.
- The glob tag lists files matching a pattern without reading their contents.
- After emitting a request-context block, stop and wait for the system to provide the context.
- The request-context block is read-only: never use it for writes or side effects.
- Do not emit change blocks in the same response as a request-context block. Request context first, then emit changes after the context is provided.
`

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
func fetchRequestContext(ctx context.Context, httpClient nets.HTTPClient, requests []RequestContextRequest) []generators.Part {
	var parts []generators.Part
	for _, req := range requests {
		switch req.Type {
		case "file":
			content, err := readContextFile(req.Path)
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
			matches, err := globFiles(req.Pattern)
			if err != nil {
				parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"glob\" pattern=%q>\n[error: %v]\n</context>\n\n", req.Pattern, err)))
				continue
			}
			parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"glob\" pattern=%q>\n%s\n</context>\n\n", req.Pattern, strings.Join(matches, "\n"))))
		}
	}
	return parts
}

// ProcessRequestContextBlocks checks BlockState for request-context blocks,
// fetches the requested content, and appends it as user content to the state.
// Returns the updated state, whether any request-context blocks were found,
// and any error from appending content.
func ProcessRequestContextBlocks(
	blockState *BlockState,
	ctx context.Context,
	httpClient nets.HTTPClient,
	state generators.State,
) (generators.State, bool, error) {
	blocks := blockState.PopBlocks()
	hasRequestContext := false
	for _, block := range blocks {
		if block.Kind != "request-context" {
			continue
		}
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
		parts := fetchRequestContext(ctx, httpClient, requests)
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

func readContextFile(path string) (string, error) {
	// Absolute paths are permitted because they represent explicit,
	// intentional references by the model. Relative paths containing
	// parent-directory traversal are rejected as a sanity check against
	// accidental escapes. See TheoryOfRequestContext.
	if !filepath.IsAbs(path) {
		cleaned := filepath.Clean(path)
		if strings.HasPrefix(cleaned, "..") {
			return "", fmt.Errorf("path escapes current directory: %s", path)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// globFiles lists files matching a glob pattern. It applies the same path
// sanity check as readContextFile: absolute patterns are permitted, while
// relative patterns containing parent-directory traversal are rejected.
// See TheoryOfRequestContext.
func globFiles(pattern string) ([]string, error) {
	if !filepath.IsAbs(pattern) {
		cleaned := filepath.Clean(pattern)
		if strings.HasPrefix(cleaned, "..") {
			return nil, fmt.Errorf("pattern escapes current directory: %s", pattern)
		}
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	return matches, nil
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