package gotools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const TheoryOfGopls = `
gopls is the session's language server for Go workspaces, exposed to the
model through the ingest block's lsp tag (see blocks.TheoryOfIngestBlocks).
The client starts lazily: no gopls process exists until the first lsp
request, so sessions that never query a language server pay nothing. One
gopls process per workspace directory is cached for the process lifetime
and shared by every lsp request, because starting gopls and indexing a
workspace dominates the cost of a single query. The server is started at
the workspace root in workspace mode (or the load directory otherwise) so
its view matches the loader's. Cleanup is implicit: the parent process
exit closes gopls's stdin, and gopls exits on stdin EOF; no explicit
shutdown is needed at process end.

The transport is JSON-RPC over stdio with Content-Length framing. The
client dispatches responses to pending calls by id and must answer every
server-to-client request — gopls blocks on unanswered requests such as
workspace/configuration during startup, so a silent client deadlocks the
server; configuration requests get one empty object per requested item and
unknown requests get null. The lsp tag exposes only read-only methods;
gopls's editing capabilities are never exercised. The model emits 1-based
line and column attributes; the handler converts them to the protocol's
0-based coordinates and renders results with 1-based positions.

Symbol-targeted queries resolve the symbol through workspace/symbol and
prefer an exact name-and-container match, so a qualified form finds the
method on the named type rather than an arbitrary same-named symbol. The
lsp tag's scope is mechanically unrestricted, including hidden packages:
like the ingest block's file tag, hidden-package exclusion is governed by
prompt instruction only (see TheoryOfHiddenPackages).

The gopls process is started with its environment normalized to
GOFLAGS=-mod=readonly (withReadonlyModEnv), so its module loading never
rewrites go.mod or go.sum. The loader injects -mod=mod for go list (see
TheoryOfModModEnv); without the normalization gopls would inherit that
flag and silently write missing checksums. Replacing any existing -mod=
flag with -mod=readonly makes gopls fail instead of modifying module
files, matching the read-only treatment of the go doc tool.
`

const (
	goplsInitializeTimeout = 30 * time.Second
	goplsRequestTimeout    = 60 * time.Second
)

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// goplsClient is a JSON-RPC connection to one gopls process. Calls are
// dispatched by id through the pending map; the read loop runs on its own
// goroutine. See TheoryOfGopls.
type goplsClient struct {
	conn      io.ReadWriteCloser
	stop      func()
	closeOnce sync.Once

	// writeMu serializes frame writes.
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[string]chan *jsonrpcMessage
	closed  bool
	folders []lspWorkspaceFolder
}

func newGoplsClient(conn io.ReadWriteCloser) *goplsClient {
	c := &goplsClient{
		conn:    conn,
		pending: map[string]chan *jsonrpcMessage{},
	}
	go c.readLoop()
	return c
}

// initialize performs the LSP initialize handshake for the workspace at
// rootDir and records the folder for later server-to-client queries.
func (c *goplsClient) initialize(ctx context.Context, rootDir string) error {
	folders := []lspWorkspaceFolder{{
		URI:  lspFileURI(rootDir),
		Name: filepath.Base(rootDir),
	}}
	params := map[string]any{
		"processId":        os.Getpid(),
		"rootUri":          folders[0].URI,
		"workspaceFolders": folders,
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"workspaceFolders": true,
			},
			"textDocument": map[string]any{
				"documentSymbol": map[string]any{
					"hierarchicalDocumentSymbolSupport": true,
				},
			},
		},
	}
	ctx, cancel := context.WithTimeout(ctx, goplsInitializeTimeout)
	defer cancel()
	var result json.RawMessage
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.mu.Lock()
	c.folders = folders
	c.mu.Unlock()
	return nil
}

// call sends one request and waits for its response. The pending entry is
// registered before the frame is written so an immediate reply cannot race
// the registration.
func (c *goplsClient) call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("gopls connection is closed")
	}
	c.nextID++
	id := c.nextID
	key := strconv.FormatInt(id, 10)
	ch := make(chan *jsonrpcMessage, 1)
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()

	idRaw, err := json.Marshal(id)
	if err != nil {
		return err
	}
	paramsRaw, err := marshalParams(params)
	if err != nil {
		return err
	}
	if err := c.write(&jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  paramsRaw,
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case msg := <-ch:
		if msg.Error != nil {
			return fmt.Errorf("gopls %s: %s", method, msg.Error.Message)
		}
		if result != nil && len(msg.Result) > 0 && string(msg.Result) != "null" {
			if err := json.Unmarshal(msg.Result, result); err != nil {
				return fmt.Errorf("gopls %s: decoding result: %w", method, err)
			}
		}
		return nil
	}
}

// notify sends a notification (no id, no response).
func (c *goplsClient) notify(method string, params any) error {
	paramsRaw, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.write(&jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	})
}

func (c *goplsClient) write(msg *jsonrpcMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeJSONRPCMessage(c.conn, msg)
}

// writeJSONRPCMessage writes one JSON-RPC frame with a Content-Length
// header. It is package-level so tests drive a fake gopls server with the
// same framing.
func writeJSONRPCMessage(w io.Writer, msg *jsonrpcMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (c *goplsClient) readLoop() {
	reader := bufio.NewReader(c.conn)
	for {
		msg, err := readJSONRPCMessage(reader)
		if err != nil {
			c.failPending(fmt.Errorf("gopls connection closed: %w", err))
			return
		}
		switch {
		case msg.ID != nil && msg.Method != "":
			c.answerServerRequest(msg)
		case msg.ID != nil:
			key := string(msg.ID)
			c.mu.Lock()
			ch := c.pending[key]
			delete(c.pending, key)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
		default:
			// A notification; nothing to dispatch.
		}
	}
}

// answerServerRequest replies to a server-to-client request. gopls blocks
// on unanswered client requests — e.g. workspace/configuration during
// startup — so every request must receive a reply, even one the client
// does not understand. See TheoryOfGopls.
func (c *goplsClient) answerServerRequest(msg *jsonrpcMessage) {
	var result any
	switch msg.Method {
	case "workspace/workspaceFolders":
		c.mu.Lock()
		folders := c.folders
		c.mu.Unlock()
		if folders == nil {
			folders = []lspWorkspaceFolder{}
		}
		result = folders
	case "workspace/configuration":
		var params struct {
			Items []struct{} `json:"items"`
		}
		if len(msg.Params) > 0 {
			_ = json.Unmarshal(msg.Params, &params)
		}
		// One value per requested item; the lengths must match.
		values := make([]map[string]any, len(params.Items))
		for i := range values {
			values[i] = map[string]any{}
		}
		result = values
	default:
		result = nil
	}
	_ = c.write(&jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  mustResult(result),
	})
}

func mustResult(result any) json.RawMessage {
	if result == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(result)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// failPending fails every in-flight call, marking the connection closed.
// Safe to call more than once: the pending map is swapped out on the first
// call, so later calls find nothing to fail.
func (c *goplsClient) failPending(err error) {
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = map[string]chan *jsonrpcMessage{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- &jsonrpcMessage{Error: &jsonrpcError{Message: err.Error()}}
	}
}

func (c *goplsClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *goplsClient) Close() {
	c.closeOnce.Do(func() {
		_ = c.notify("shutdown", nil)
		_ = c.notify("exit", nil)
		_ = c.conn.Close()
		if c.stop != nil {
			c.stop()
		}
		c.failPending(errors.New("gopls client closed"))
	})
}

// readJSONRPCMessage reads one Content-Length framed JSON-RPC message.
func readJSONRPCMessage(reader *bufio.Reader) (*jsonrpcMessage, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(name, "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length %q: %w", value, err)
			}
			contentLength = n
		}
	}
	if contentLength <= 0 {
		return nil, errors.New("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	var msg jsonrpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("decoding JSON-RPC message: %w", err)
	}
	return &msg, nil
}

// lspWorkspaceFolder is one workspace root reported to the language server
// during initialization.
type lspWorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// lspFileURI encodes an absolute file path as a file:// URI.
func lspFileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// lspURIPath decodes a file:// URI to its path; anything else is returned
// unchanged.
func lspURIPath(uri string) string {
	u, err := url.ParseRequestURI(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return u.Path
}

// goplsClients caches one gopls client per workspace directory for the
// process lifetime: starting gopls and indexing a workspace dominates the
// cost of a single query, so repeated lsp requests reuse the same server.
// See TheoryOfGopls.
var goplsClients sync.Map // dir string -> *goplsClient

// getGoplsClient returns the cached gopls client for dir, starting one when
// none is running. A cached client whose connection has died is evicted and
// restarted.
func getGoplsClient(ctx context.Context, dir string, envs Envs) (*goplsClient, error) {
	if v, ok := goplsClients.Load(dir); ok {
		client := v.(*goplsClient)
		if !client.isClosed() {
			return client, nil
		}
		goplsClients.CompareAndDelete(dir, v)
	}
	client, err := startGopls(ctx, dir, envs)
	if err != nil {
		return nil, err
	}
	actual, loaded := goplsClients.LoadOrStore(dir, client)
	if loaded {
		// Another goroutine won the race; use its client and drop ours.
		client.Close()
		client = actual.(*goplsClient)
	}
	return client, nil
}

// startGopls launches gopls -mode=stdio with dir as its working directory
// and performs the LSP initialize handshake.
func startGopls(ctx context.Context, dir string, envs Envs) (*goplsClient, error) {
	cmd := exec.Command("gopls", "-mode=stdio")
	cmd.Dir = dir
	// gopls must never rewrite module files; normalize its environment to
	// GOFLAGS=-mod=readonly, overriding the loader's -mod=mod injection.
	// See TheoryOfGopls.
	cmd.Env = withReadonlyModEnv(append(os.Environ(), envs...))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting gopls: %w (is gopls installed?)", err)
	}
	go io.Copy(io.Discard, stderr)
	client := newGoplsClient(&goplsConn{ReadCloser: stdout, WriteCloser: stdin})
	client.stop = func() {
		_ = cmd.Process.Kill()
		go func() { _ = cmd.Wait() }()
	}
	if err := client.initialize(ctx, dir); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// goplsConn pairs the process's stdout (reader) and stdin (writer) as one
// ReadWriteCloser. Close is defined explicitly because the promoted Close
// from the two embedded interfaces is ambiguous at compile time.
type goplsConn struct {
	io.ReadCloser
	io.WriteCloser
}

func (c *goplsConn) Close() error {
	readErr := c.ReadCloser.Close()
	writeErr := c.WriteCloser.Close()
	if readErr != nil {
		return readErr
	}
	return writeErr
}
