package gotools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reusee/tai/blocks"
)

const LSPReadTagSystemPrompt = `
**LSP Tag (Go sessions, gopls):**

In Go projects a language server (gopls) is attached to this session. Inside a read block, use the <lsp> tag to query it for precise navigation data.

**Tag forms:**
- ` + "`<lsp method=\"definition\" symbol=\"Reader.Read\" />`" + ` — jump to the declaration of a symbol. The symbol may be bare (Read) or qualified (Reader.Read); a qualified form finds the method on the named type.
- ` + "`<lsp method=\"references\" symbol=\"Reader.Read\" />`" + ` — list every use site (the declaration itself is excluded). Also takes a path+line+column target.
- ` + "`<lsp method=\"hover\" path=\"file.go\" line=\"12\" column=\"5\" />`" + ` — show type and signature information at a position.
- ` + "`<lsp method=\"typeDefinition\" symbol=\"...\" />`" + ` and ` + "`<lsp method=\"implementation\" symbol=\"...\" />`" + ` — find a value's type declaration or a type's implementations; accept symbol or path+line+column.
- ` + "`<lsp method=\"documentSymbol\" path=\"file.go\" />`" + ` — outline a file's symbol tree with declaration lines.
- ` + "`<lsp method=\"workspace/symbol\" query=\"Builder\" />`" + ` — project-wide symbol search by free text.

**Rules:**
- path is relative to the project root or absolute; line and column are 1-based (column defaults to 1).
- Symbol-based queries resolve through workspace-wide symbol search; when several symbols share a name, prefer the qualified TypeName.Method form.
- Results are read-only navigation data, delivered in the next round like every other read tag.
- Division of labor with go-src: go-src fetches the declaration source of loaded project symbols (with the references report); the lsp tag serves hover at arbitrary positions, file outlines, project-wide symbol search, and type/implementation navigation across the whole module.
`

// lspPosition is an LSP position. Line and Character are 0-based.
type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspLocation struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type lspSymbolInformation struct {
	Name          string      `json:"name"`
	Kind          int         `json:"kind"`
	Location      lspLocation `json:"location"`
	ContainerName string      `json:"containerName"`
}

type lspHover struct {
	Contents struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	} `json:"contents"`
	Range *lspRange `json:"range"`
}

type lspDocumentSymbol struct {
	Name           string              `json:"name"`
	Detail         string              `json:"detail,omitempty"`
	Kind           int                 `json:"kind"`
	Range          lspRange            `json:"range"`
	SelectionRange lspRange            `json:"selectionRange"`
	Children       []lspDocumentSymbol `json:"children,omitempty"`
}

type lspTextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type lspTextDocumentPositionParams struct {
	TextDocument lspTextDocumentIdentifier `json:"textDocument"`
	Position     lspPosition               `json:"position"`
}

type lspReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type lspReferenceParams struct {
	lspTextDocumentPositionParams
	Context lspReferenceContext `json:"context"`
}

type lspDocumentSymbolParams struct {
	TextDocument lspTextDocumentIdentifier `json:"textDocument"`
}

type lspWorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// lspMethodAliases maps lowercased lsp tag method attributes to full LSP
// method names. The map is the read-only whitelist: editing methods such
// as rename and completion have no entry and are rejected. See
// TheoryOfGopls.
var lspMethodAliases = map[string]string{
	"definition":                  "textDocument/definition",
	"textdocument/definition":     "textDocument/definition",
	"typedefinition":              "textDocument/typeDefinition",
	"textdocument/typedefinition": "textDocument/typeDefinition",
	"references":                  "textDocument/references",
	"textdocument/references":     "textDocument/references",
	"hover":                       "textDocument/hover",
	"textdocument/hover":          "textDocument/hover",
	"implementation":              "textDocument/implementation",
	"textdocument/implementation": "textDocument/implementation",
	"documentsymbol":              "textDocument/documentSymbol",
	"textdocument/documentsymbol": "textDocument/documentSymbol",
	"symbols":                     "textDocument/documentSymbol",
	"symbol":                      "workspace/symbol",
	"workspacesymbol":             "workspace/symbol",
	"workspace/symbol":            "workspace/symbol",
}

// resolveLSPMethod resolves an lsp tag method attribute to its full LSP
// method name, rejecting anything outside the read-only whitelist.
func resolveLSPMethod(method string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(method))
	if resolved, ok := lspMethodAliases[key]; ok {
		return resolved, nil
	}
	return "", fmt.Errorf(
		"unsupported lsp method %q; supported methods: definition, typeDefinition, references, hover, implementation, documentSymbol, workspace/symbol",
		method,
	)
}

// lspSymbolKindNames maps LSP SymbolKind values to display names.
var lspSymbolKindNames = map[int]string{
	1:  "file",
	2:  "module",
	3:  "namespace",
	4:  "package",
	5:  "class",
	6:  "method",
	7:  "property",
	8:  "field",
	9:  "constructor",
	10: "enum",
	11: "interface",
	12: "function",
	13: "variable",
	14: "constant",
	15: "string",
	16: "number",
	17: "boolean",
	18: "array",
	19: "object",
	20: "key",
	21: "null",
	22: "enum member",
	23: "struct",
	24: "event",
	25: "operator",
	26: "type parameter",
}

func lspSymbolKindName(kind int) string {
	if name, ok := lspSymbolKindNames[kind]; ok {
		return name
	}
	return fmt.Sprintf("kind %d", kind)
}

// formatLSPLine renders a location as "path:line:col" with 1-based
// positions, matching compiler diagnostics conventions.
func formatLSPLine(loc lspLocation) string {
	return fmt.Sprintf("%s:%d:%d",
		lspURIPath(loc.URI),
		loc.Range.Start.Line+1,
		loc.Range.Start.Character+1,
	)
}

func renderLSPLocations(locs []lspLocation) string {
	lines := make([]string, len(locs))
	for i, loc := range locs {
		lines[i] = formatLSPLine(loc)
	}
	return strings.Join(lines, "\n")
}

func renderLSPSymbolInformations(syms []lspSymbolInformation) string {
	lines := make([]string, len(syms))
	for i, sym := range syms {
		var b strings.Builder
		if sym.ContainerName != "" {
			b.WriteString(sym.ContainerName)
			b.WriteString(".")
		}
		b.WriteString(sym.Name)
		b.WriteString(" (")
		b.WriteString(lspSymbolKindName(sym.Kind))
		b.WriteString(") ")
		b.WriteString(formatLSPLine(sym.Location))
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

func renderLSPDocumentSymbols(syms []lspDocumentSymbol) string {
	var lines []string
	var render func(syms []lspDocumentSymbol, depth int)
	render = func(syms []lspDocumentSymbol, depth int) {
		for _, sym := range syms {
			lines = append(lines, fmt.Sprintf("%s%s (%s) :%d",
				strings.Repeat("  ", depth),
				sym.Name,
				lspSymbolKindName(sym.Kind),
				sym.SelectionRange.Start.Line+1,
			))
			render(sym.Children, depth+1)
		}
	}
	render(syms, 0)
	return strings.Join(lines, "\n")
}

// decodeLSPLocations decodes a definition-family result, which may be a
// location array, a single location object, or null (nothing found).
func decodeLSPLocations(raw json.RawMessage) ([]lspLocation, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var locs []lspLocation
		if err := json.Unmarshal(raw, &locs); err != nil {
			return nil, err
		}
		return locs, nil
	}
	var loc lspLocation
	if err := json.Unmarshal(raw, &loc); err != nil {
		return nil, err
	}
	return []lspLocation{loc}, nil
}

// bestLSPSymbolMatch selects the best workspace/symbol candidate for the
// requested symbol: an exact name match on the named container type wins,
// then the first exact name match, then the first candidate.
func bestLSPSymbolMatch(syms []lspSymbolInformation, symbol string) (lspSymbolInformation, bool) {
	if len(syms) == 0 {
		return lspSymbolInformation{}, false
	}
	name := symbol
	container := ""
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		container = symbol[:i]
		name = symbol[i+1:]
	}
	var nameOnly *lspSymbolInformation
	for i := range syms {
		if syms[i].Name != name {
			continue
		}
		if syms[i].ContainerName == container {
			return syms[i], true
		}
		if nameOnly == nil {
			sym := syms[i]
			nameOnly = &sym
		}
	}
	if nameOnly != nil {
		return *nameOnly, true
	}
	return syms[0], true
}

// lspTagFileURI builds the document URI for an lsp tag path attribute.
// Relative paths are resolved against the load directory.
func lspTagFileURI(path, loadDir string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("method requires a path attribute when no symbol is given")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(loadDir, path)
	}
	return lspFileURI(path), nil
}

func goplsWorkspaceSymbols(ctx context.Context, client *goplsClient, query string) ([]lspSymbolInformation, error) {
	var syms []lspSymbolInformation
	if err := client.call(ctx, "workspace/symbol", lspWorkspaceSymbolParams{Query: query}, &syms); err != nil {
		return nil, err
	}
	return syms, nil
}

// resolveLSPQueryPosition builds textDocument/position params from an lsp
// tag query: an explicit line positions the query in the named file; a
// symbol is resolved through workspace/symbol to its declaration position;
// anything else is an error.
func resolveLSPQueryPosition(ctx context.Context, client *goplsClient, q blocks.LSPQuery, loadDir string) (lspTextDocumentPositionParams, error) {
	var params lspTextDocumentPositionParams
	if q.Line > 0 {
		uri, err := lspTagFileURI(q.Path, loadDir)
		if err != nil {
			return params, err
		}
		params.TextDocument.URI = uri
		params.Position = lspPosition{
			Line:      q.Line - 1,
			Character: max(q.Column, 1) - 1,
		}
		return params, nil
	}
	if q.Symbol != "" {
		syms, err := goplsWorkspaceSymbols(ctx, client, q.Symbol)
		if err != nil {
			return params, err
		}
		candidate, ok := bestLSPSymbolMatch(syms, q.Symbol)
		if !ok {
			return params, fmt.Errorf("no symbols matched %q", q.Symbol)
		}
		params.TextDocument.URI = candidate.Location.URI
		params.Position = candidate.Location.Range.Start
		return params, nil
	}
	return params, fmt.Errorf("method requires a symbol or a line attribute")
}

// goplsLSPHandler answers lsp tag queries through one gopls client.
// The query method is whitelisted by resolveLSPMethod; every result is
// rendered as plain navigation text, and an empty result is an explicit
// message rather than an error.
func goplsLSPHandler(
	getClient func(context.Context) (*goplsClient, error),
	loadDir string,
) blocks.LSPHandler {
	return func(ctx context.Context, q blocks.LSPQuery) (string, error) {
		method, err := resolveLSPMethod(q.Method)
		if err != nil {
			return "", err
		}
		client, err := getClient(ctx)
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, goplsRequestTimeout)
		defer cancel()

		switch method {
		case "workspace/symbol":
			query := q.Query
			if query == "" {
				query = q.Symbol
			}
			if query == "" {
				return "", fmt.Errorf("workspace/symbol requires a query or symbol attribute")
			}
			syms, err := goplsWorkspaceSymbols(ctx, client, query)
			if err != nil {
				return "", err
			}
			if len(syms) == 0 {
				return fmt.Sprintf("no symbols matched %q", query), nil
			}
			return renderLSPSymbolInformations(syms), nil

		case "textDocument/documentSymbol":
			if q.Path == "" {
				return "", fmt.Errorf("documentSymbol requires a path attribute")
			}
			uri, err := lspTagFileURI(q.Path, loadDir)
			if err != nil {
				return "", err
			}
			var syms []lspDocumentSymbol
			if err := client.call(ctx, "textDocument/documentSymbol",
				lspDocumentSymbolParams{TextDocument: lspTextDocumentIdentifier{URI: uri}}, &syms); err != nil {
				return "", err
			}
			if len(syms) == 0 {
				return fmt.Sprintf("no symbols found in %s", q.Path), nil
			}
			return renderLSPDocumentSymbols(syms), nil

		case "textDocument/references":
			params, err := resolveLSPQueryPosition(ctx, client, q, loadDir)
			if err != nil {
				return "", err
			}
			refParams := lspReferenceParams{
				lspTextDocumentPositionParams: params,
				Context:                       lspReferenceContext{IncludeDeclaration: false},
			}
			var locs []lspLocation
			if err := client.call(ctx, "textDocument/references", refParams, &locs); err != nil {
				return "", err
			}
			if len(locs) == 0 {
				return "no references found", nil
			}
			return renderLSPLocations(locs), nil

		case "textDocument/hover":
			params, err := resolveLSPQueryPosition(ctx, client, q, loadDir)
			if err != nil {
				return "", err
			}
			var hover lspHover
			if err := client.call(ctx, "textDocument/hover", params, &hover); err != nil {
				return "", err
			}
			if hover.Contents.Value == "" {
				return "no hover information at position", nil
			}
			return hover.Contents.Value, nil

		default:
			// definition, typeDefinition, implementation.
			params, err := resolveLSPQueryPosition(ctx, client, q, loadDir)
			if err != nil {
				return "", err
			}
			var raw json.RawMessage
			if err := client.call(ctx, method, params, &raw); err != nil {
				return "", err
			}
			locs, err := decodeLSPLocations(raw)
			if err != nil {
				return "", err
			}
			if len(locs) == 0 {
				return "no locations found", nil
			}
			return renderLSPLocations(locs), nil
		}
	}
}

// LSPHandler provides the gopls-backed language-server handler for the
// read block's lsp tag. The gopls process runs at the workspace root in
// workspace mode (or the load directory, or the working directory when no
// directory is configured), matching the loader's view of the module
// graph. See TheoryOfGopls and blocks.TheoryOfReadBlocks.
func (Module) LSPHandler(
	loadDir LoadDir,
	workspace Workspace,
	envs Envs,
) blocks.LSPHandler {
	dir := string(workspace)
	if dir == "" {
		dir = string(loadDir)
	}
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	if dir == "" {
		return nil
	}
	return goplsLSPHandler(func(ctx context.Context) (*goplsClient, error) {
		return getGoplsClient(ctx, dir, envs)
	}, string(loadDir))
}
