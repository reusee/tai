package blocks

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/pathutil"
)

const TheoryOfIngestBlocks = `
The ingest block lets the model request additional context during a
generation cycle: it emits XML tags describing the desired context; the generate
loop detects the block via ParserState, fetches the requested data, appends it as
user content, and initiates another generation request. The block is strictly
read-only: it must not produce any side effects such as writing files or making
state-changing API calls. The order of XML tags within the block determines the
order of context parts in the appended user message.

The kind is named "ingest" rather than "read" deliberately: "read" collides
with tool names the model has internalized from training, and the collision
produces inertia — the model follows the familiar tool's conventions instead
of this block protocol. An uncommon kind name eliminates the collision.

Symbol-level source fetching belongs to a dedicated kind where one exists:
the codes pipeline teaches go-src — which appends a references report of the
resolved declarations' callers — as the preferred path for Go source, so ingest
keeps whole files, glob discovery, and network resources. The ingest prompt
itself stays language-neutral; see gotools.TheoryOfGoSrcBlocks for the
division of labor.

The lsp tag is ingest's language-server extension point: blocks parses the
tag and defines the LSPHandler contract language-neutrally, while a session
with a language server injects a handler — Go sessions attach gopls, see
gotools.TheoryOfGopls, and append the Go-specific lsp tag documentation to
the ingest prompt only when the handler is attached. A session without a
language server resolves a nil handler: the lsp documentation is omitted
from the prompt, and an emitted lsp tag returns an explicit unavailability
error part rather than being silently ignored.

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
go-src), the ingest prompt carries the summary discipline: the block does not
replace the summary block, the prompt requires a summary block in the same
round after the ingest block, and the stop rule is phrased summary-first — emit
the summary block immediately after the last ingest block's closing line, then
end the response and wait — the same wording as the shell prompt, so no stop
instruction licenses halting at the closing line. The prompt also adds the
sequence rule that the block after the last ingest block's closing line must be
the summary block. At the loop level an ingest block never completes a round on
its own: a round carrying ingest blocks but no summary block is retried with
feedback naming the missing summary, and the ingest blocks are discarded with
the failed attempt and must be re-emitted together with the summary block
(see pipeline.TheoryOfLoops).

Only ingest blocks are consumed from ParserState during context
processing; blocks of other kinds are preserved so they remain available after the
context is provided.
`

const IngestBlockSystemPrompt = `
Ingest Block Kind:

Use the "ingest" kind to request additional context needed to complete the task. When a file needs to be read or a network resource fetched, emit an ingest block. The system will fetch the requested data and provide it as user input for the next generation turn.

**Supported XML Tags:**
- ` + "`<file path=\"...\" />`" + `: Read a local file at the given path. The path should be relative to the project root or absolute.
- ` + "`<fetch addr=\"...\" user-agent=\"...\" referer=\"...\" cookie=\"...\" />`" + `: Fetch content from a network address (HTTP GET). The addr should be a valid URL. The user-agent, referer, and cookie attributes are optional and set the corresponding HTTP headers on the request.
- ` + "`<glob pattern=\"...\" />`" + `: List files matching a glob pattern. The pattern should be relative to the project root or absolute. Returns matching file paths without reading their contents.

**Rules:**
- The order of XML tags determines the order of context parts in the response.
- This block is strictly read-only. It must not produce any side effects.
- After the last ingest block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the system to provide the requested context.
- The ingest block is NOT a completion signal. MUST still emit a summary block in the same round, after the ingest block. Every round must end with a summary block.
- Never end a response on an ingest block, and never stop at its closing line: stopping there omits the mandatory summary block, the response is treated as incomplete, and it is discarded and retried — its blocks are discarded, so the context requests are lost unless re-emitted.
- Do not include ingest blocks alongside change blocks in the same response. If more context is needed, request it first, then emit change blocks in a subsequent response after the context is provided.

**Example use:**
- To read a file: emit an ingest block whose body contains <file path="..." />.
- To fetch a web page with custom headers: emit an ingest block whose body contains <fetch addr="..." user-agent="..." referer="..." cookie="..." />.
- To discover files: emit an ingest block whose body contains <glob pattern="..." />.
`

const IngestBlockRestatePrompt = `- If additional context is needed (file contents, network resources, file listings), emit an ingest block whose body contains the corresponding XML tags: <file path="..." />, <fetch addr="..." user-agent="..." referer="..." cookie="..." />, and <glob pattern="..." />.
- The user-agent, referer, and cookie attributes on the fetch tag are optional and set the corresponding HTTP headers.
- The glob tag lists files matching a pattern without reading their contents.
- After the last ingest block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the system to provide the context — never stop at the closing line itself.
- An ingest block does NOT replace the summary block. MUST still emit a summary block in the same round, after the ingest block.
- Never end a response on an ingest block: after the ingest block's closing line, the next block MUST be the summary block.
- The ingest block is read-only: never use it for writes or side effects.
- Do not emit change blocks in the same response as an ingest block. Request the context first, then emit changes after the context is provided.`

// IngestRequest represents a single context request parsed from the block body.
type IngestRequest struct {
	Type      string
	Path      string
	Addr      string
	UserAgent string
	Referer   string
	Cookie    string
	Pattern   string

	// LSP tag fields. Method is the required language-server method; the
	// remaining fields select the query target. Line and Column are
	// 1-based; 0 means absent.
	Method string
	Symbol string
	Query  string
	Line   int
	Column int
}

// LSPQuery carries one language-server query parsed from an lsp tag. The
// query shape is defined by blocks so tag parsing stays language-neutral,
// while the handler implementation is injected by the session (Go sessions
// attach gopls; see gotools.TheoryOfGopls).
type LSPQuery struct {
	// Method is the requested language-server method (e.g. "definition",
	// "references", "hover", "workspace/symbol").
	Method string
	// Path is the file the query targets, relative to the project root or
	// absolute. May be empty for symbol-only queries.
	Path string
	// Symbol names a symbol for symbol-based queries, bare ("Read") or
	// qualified ("Reader.Read").
	Symbol string
	// Query is the free-text search string for workspace/symbol.
	Query string
	// Line and Column locate a position within Path. Both are 1-based;
	// 0 means absent. Handlers convert them to the protocol's 0-based
	// coordinates.
	Line   int
	Column int
}

// LSPHandler answers one language-server query, returning the rendered
// result text. A nil handler means no language server is available in the
// session; fetchIngestRequests then returns an explicit unavailability error
// part for every lsp tag instead of silently ignoring it.
// See TheoryOfIngestBlocks and gotools.TheoryOfGopls.
type LSPHandler func(ctx context.Context, q LSPQuery) (string, error)

// parseIngestBody parses the XML tags in an ingest block body.
func parseIngestBody(body string) ([]IngestRequest, error) {
	decoder := xml.NewDecoder(strings.NewReader(body))
	var requests []IngestRequest
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
			requests = append(requests, IngestRequest{Type: "file", Path: path})
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
			requests = append(requests, IngestRequest{
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
			requests = append(requests, IngestRequest{Type: "glob", Pattern: pattern})
		case "lsp":
			var method, path, symbol, query, lineStr, colStr string
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "method":
					method = attr.Value
				case "path":
					path = attr.Value
				case "symbol":
					symbol = attr.Value
				case "query":
					query = attr.Value
				case "line":
					lineStr = attr.Value
				case "column":
					colStr = attr.Value
				}
			}
			if method == "" {
				return nil, fmt.Errorf("lsp tag missing method attribute")
			}
			line := 0
			if lineStr != "" {
				var err error
				line, err = strconv.Atoi(lineStr)
				if err != nil {
					return nil, fmt.Errorf("lsp tag line attribute must be a number: %q", lineStr)
				}
			}
			column := 0
			if colStr != "" {
				var err error
				column, err = strconv.Atoi(colStr)
				if err != nil {
					return nil, fmt.Errorf("lsp tag column attribute must be a number: %q", colStr)
				}
			}
			requests = append(requests, IngestRequest{
				Type:   "lsp",
				Method: method,
				Path:   path,
				Symbol: symbol,
				Query:  query,
				Line:   line,
				Column: column,
			})
		}
	}
	return requests, nil
}

// fetchIngestRequests fetches the requested context and returns parts.
// File read errors and fetch errors are returned as error text parts rather
// than aborting the entire generation, so the model can adapt.
func fetchIngestRequests(ctx context.Context, root *os.Root, httpClient nets.HTTPClient, lsp LSPHandler, requests []IngestRequest) []generators.Part {
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
		case "lsp":
			label := lspLabel(req)
			if lsp == nil {
				parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"lsp\" method=%q>\n[error: no language server is available in this session; do not emit lsp tags]\n</context>\n\n", label)))
				continue
			}
			text, err := lsp(ctx, LSPQuery{
				Method: req.Method,
				Path:   req.Path,
				Symbol: req.Symbol,
				Query:  req.Query,
				Line:   req.Line,
				Column: req.Column,
			})
			if err != nil {
				parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"lsp\" method=%q>\n[error: %v]\n</context>\n\n", label, err)))
				continue
			}
			parts = append(parts, generators.Text(fmt.Sprintf("<context type=\"lsp\" method=%q>\n%s\n</context>\n\n", label, text)))
		}
	}
	return parts
}

// lspLabel builds the display label of an lsp request for its context part:
// the method plus its primary target (symbol, query, or path position).
func lspLabel(req IngestRequest) string {
	label := req.Method
	switch {
	case req.Symbol != "":
		label += " " + req.Symbol
	case req.Query != "":
		label += " " + req.Query
	case req.Path != "":
		label += " " + req.Path
		if req.Line > 0 {
			label += fmt.Sprintf(":%d", req.Line)
			if req.Column > 0 {
				label += fmt.Sprintf(":%d", req.Column)
			}
		}
	}
	return label
}

// ProcessIngestBlocks checks ingest blocks, fetches the requested content, and
// appends it as user content to the state. Only blocks with Kind "ingest" are
// processed. The hasIngest flag indicates whether any ingest blocks were found,
// so callers can trigger a new round. See TheoryOfIngestBlocks.
func ProcessIngestBlocks(
	blocks []Block,
	ctx context.Context,
	root *os.Root,
	httpClient nets.HTTPClient,
	lsp LSPHandler,
	state generators.State,
) (generators.State, bool, error) {
	hasIngest := false
	for _, block := range blocks {
		if block.Kind != "ingest" {
			continue
		}
		hasIngest = true
		requests, parseErr := parseIngestBody(block.Body)
		if parseErr != nil {
			var appendErr error
			state, appendErr = state.AppendContent(&generators.Content{
				Role: "user",
				Parts: []generators.Part{
					generators.Text(fmt.Sprintf("[ingest block parse error: %v]\n\n", parseErr)),
				},
			})
			if appendErr != nil {
				return state, hasIngest, appendErr
			}
			continue
		}
		parts := fetchIngestRequests(ctx, root, httpClient, lsp, requests)
		if len(parts) > 0 {
			var appendErr error
			state, appendErr = state.AppendContent(&generators.Content{
				Role:  "user",
				Parts: parts,
			})
			if appendErr != nil {
				return state, hasIngest, appendErr
			}
		}
	}
	return state, hasIngest, nil
}

// readContextFile reads a local file at the given path. Absolute paths are
// permitted because they represent explicit, intentional references by the
// model. Relative paths containing parent-directory traversal are rejected as
// a sanity check against accidental escapes. The check distinguishes ".."
// (parent directory) and "../"-prefixed paths from names that merely start with
// two dots (e.g., "..hidden", "..."). Absolute paths are resolved relative to
// the root directory when within it, or read directly from the filesystem when
// outside it, so the model can reference files in system directories like /tmp.
// See TheoryOfIngestBlocks.
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
	// paths outside the root. See TheoryOfIngestBlocks.
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

func fetchURL(ctx context.Context, httpClient nets.HTTPClient, req IngestRequest) (string, error) {
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
