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

const TheoryOfReadBlocks = `
The read block lets the model request additional context during a
generation cycle: it emits XML tags describing the desired context; the generate
loop detects the block via ParserState, fetches the requested data, appends it as
user content, and initiates another generation request. The block is strictly
read-only: it must not produce any side effects such as writing files or making
state-changing API calls. The order of XML tags within the block determines the
order of context parts in the appended user message.

The file tag permits absolute paths as explicit references while rejecting relative
paths that escape the current directory via parent-directory traversal, balancing
flexibility with a basic sanity check. Absolute paths are resolved relative to the
root directory when within it, or read directly from the filesystem when outside
it, so the model can reference files in system directories like /tmp. The fetch tag
supports optional HTTP headers (user-agent, referer, cookie) so the model can access
resources that require them, but remains read-only (HTTP GET). The glob tag lists
files matching a pattern without reading their contents, allowing the model to
discover files before requesting their content; it applies the same path sanity
check as the file tag. Glob patterns are resolved relative to the root directory so
that doublestar.FilepathGlob searches within the root's tree rather than the
process's current working directory. The glob tag supports ** (globstar) patterns
for recursive directory traversal via doublestar; when ** appears as a complete
path segment, it matches zero or more directories.

Like every kind whose prompt stops and waits for the next round (shell, go-test,
go-src), the read prompt carries the summary discipline: the block does not
replace the summary block, the prompt requires a summary block in the same
round after the read block, and the stop rule is phrased as "stop
generating, end the response with a summary block, and wait" — the same wording as
the shell prompt — so the stop instruction never licenses omitting the summary
block. At the loop level the block still completes the round (see
pipeline.TheoryOfLoops), but the summary block remains required so the round
statistics and the summary display carry the round's narrative.

Only read blocks are consumed from ParserState during context
processing; blocks of other kinds are preserved so they remain available after the
context is provided.
`

const ReadBlockSystemPrompt = `
Read Block Kind:

Use the "read" kind to request additional context needed to complete the task. When a file needs to be read or a network resource fetched, emit a read block. The system will fetch the requested data and provide it as user input for the next generation turn.

**Supported XML Tags:**
- ` + "`<file path=\"...\" />`" + `: Read a local file at the given path. The path should be relative to the project root or absolute.
- ` + "`<fetch addr=\"...\" user-agent=\"...\" referer=\"...\" cookie=\"...\" />`" + `: Fetch content from a network address (HTTP GET). The addr should be a valid URL. The user-agent, referer, and cookie attributes are optional and set the corresponding HTTP headers on the request.
- ` + "`<glob pattern=\"...\" />`" + `: List files matching a glob pattern. The pattern should be relative to the project root or absolute. Returns matching file paths without reading their contents.

**Rules:**
- The order of XML tags determines the order of context parts in the response.
- This block is strictly read-only. It must not produce any side effects.
- After emitting a read block, stop generating, end the response with a summary block, and wait for the system to provide the requested context.
- The read block is NOT a completion signal. MUST still emit a summary block in the same round, after the read block. Every round must end with a summary block.
- Do not include read blocks alongside change blocks in the same response. If more context is needed, request it first, then emit change blocks in a subsequent response after the context is provided.

**Example use:**
- To read a file: emit a read block whose body contains <file path="..." />.
- To fetch a web page with custom headers: emit a read block whose body contains <fetch addr="..." user-agent="..." referer="..." cookie="..." />.
- To discover files: emit a read block whose body contains <glob pattern="..." />.
`

const ReadBlockRestatePrompt = `- If additional context is needed (file contents, network resources, file listings), emit a read block whose body contains the corresponding XML tags: <file path="..." />, <fetch addr="..." user-agent="..." referer="..." cookie="..." />, and <glob pattern="..." />.
- The user-agent, referer, and cookie attributes on the fetch tag are optional and set the corresponding HTTP headers.
- The glob tag lists files matching a pattern without reading their contents.
- After emitting a read block, stop and end the response with a summary block, then wait for the system to provide the context.
- A read block does NOT replace the summary block. MUST still emit a summary block in the same round, after the read block.
- The read block is read-only: never use it for writes or side effects.
- Do not emit change blocks in the same response as a read block. Request the context first, then emit changes after the context is provided.`

// ReadRequest represents a single context request parsed from the block body.
type ReadRequest struct {
	Type      string
	Path      string
	Addr      string
	UserAgent string
	Referer   string
	Cookie    string
	Pattern   string
}

// parseReadBody parses the XML tags in a read block body.
func parseReadBody(body string) ([]ReadRequest, error) {
	decoder := xml.NewDecoder(strings.NewReader(body))
	var requests []ReadRequest
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
			requests = append(requests, ReadRequest{Type: "file", Path: path})
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
			requests = append(requests, ReadRequest{
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
			requests = append(requests, ReadRequest{Type: "glob", Pattern: pattern})
		}
	}
	return requests, nil
}

// fetchReadRequests fetches the requested context and returns parts.
// File read errors and fetch errors are returned as error text parts rather
// than aborting the entire generation, so the model can adapt.
func fetchReadRequests(ctx context.Context, root *os.Root, httpClient nets.HTTPClient, requests []ReadRequest) []generators.Part {
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

// ProcessReadBlocks checks read blocks, fetches the requested content, and
// appends it as user content to the state. Only blocks with Kind "read" are
// processed. The hasRead flag indicates whether any read blocks were found, so
// callers can trigger a new round. See TheoryOfReadBlocks.
func ProcessReadBlocks(
	blocks []Block,
	ctx context.Context,
	root *os.Root,
	httpClient nets.HTTPClient,
	state generators.State,
) (generators.State, bool, error) {
	hasRead := false
	for _, block := range blocks {
		if block.Kind != "read" {
			continue
		}
		hasRead = true
		requests, parseErr := parseReadBody(block.Body)
		if parseErr != nil {
			var appendErr error
			state, appendErr = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(fmt.Sprintf("[read block parse error: %v]\n\n", parseErr)),
				},
			})
			if appendErr != nil {
				return state, hasRead, appendErr
			}
			continue
		}
		parts := fetchReadRequests(ctx, root, httpClient, requests)
		if len(parts) > 0 {
			var appendErr error
			state, appendErr = state.AppendContent(&generators.Content{
				Role:  "user",
				Parts: parts,
			})
			if appendErr != nil {
				return state, hasRead, appendErr
			}
		}
	}
	return state, hasRead, nil
}

// readContextFile reads a local file at the given path. Absolute paths are
// permitted because they represent explicit, intentional references by the
// model. Relative paths containing parent-directory traversal are rejected as
// a sanity check against accidental escapes. The check distinguishes ".."
// (parent directory) and "../"-prefixed paths from names that merely start with
// two dots (e.g., "..hidden", "..."). Absolute paths are resolved relative to
// the root directory when within it, or read directly from the filesystem when
// outside it, so the model can reference files in system directories like /tmp.
// See TheoryOfReadBlocks.
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
	// paths outside the root. See TheoryOfReadBlocks.
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

func globFiles(root *os.Root, pattern string) ([]string, error) {
	if !filepath.IsAbs(pattern) {
		cleaned := filepath.Clean(pattern)
		if pathutil.EscapesDir(cleaned) {
			return nil, fmt.Errorf("pattern escapes current directory: %s", pattern)
		}
	}
	// Resolve the pattern relative to the root directory so that
	// doublestar.FilepathGlob searches within the root's tree,
	// not the process's current working directory. See TheoryOfReadBlocks.
	rootDir, err := filepath.Abs(root.Name())
	if err != nil {
		return nil, err
	}
	searchPattern := pattern
	if !filepath.IsAbs(pattern) {
		searchPattern = filepath.Join(rootDir, pattern)
	}
	// doublestar.FilepathGlob unifies glob expansion with native ** support.
	// WithFilesOnly excludes directories.
	// See TheoryOfPatternMatching in anytexts/code_provider.go.
	matches, err := doublestar.FilepathGlob(searchPattern, doublestar.WithFilesOnly())
	if err != nil {
		return nil, err
	}
	// Filter matches to those within the root. Convert absolute paths
	// to root-relative paths for the stat check, since os.Root methods
	// do not accept absolute paths. See TheoryOfReadBlocks.
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

func fetchURL(ctx context.Context, httpClient nets.HTTPClient, req ReadRequest) (string, error) {
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
