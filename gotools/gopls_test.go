package gotools

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestGoplsClientRoundTrip(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	client := newGoplsClient(clientConn)
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer serverConn.Close()
		reader := bufio.NewReader(serverConn)

		readMsg := func() *jsonrpcMessage {
			msg, err := readJSONRPCMessage(reader)
			if err != nil {
				t.Errorf("fake server read: %v", err)
				return nil
			}
			return msg
		}

		initMsg := readMsg()
		if initMsg == nil {
			return
		}
		if initMsg.Method != "initialize" {
			t.Errorf("expected initialize, got %q", initMsg.Method)
		}

		// Before answering initialize, the server sends a client request.
		// The client must always reply; gopls blocks otherwise. See
		// TheoryOfGopls.
		if err := writeJSONRPCMessage(serverConn, &jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`"cfg1"`),
			Method:  "workspace/configuration",
			Params:  json.RawMessage(`{"items":[{},{}]}`),
		}); err != nil {
			t.Errorf("fake server write: %v", err)
			return
		}

		reply := readMsg()
		if reply == nil {
			return
		}
		if string(reply.ID) != `"cfg1"` {
			t.Errorf("expected reply to cfg1, got id %s", reply.ID)
		}
		var values []any
		if err := json.Unmarshal(reply.Result, &values); err != nil {
			t.Errorf("decoding configuration reply: %v", err)
			return
		}
		if len(values) != 2 {
			t.Errorf("expected 2 configuration values, got %d", len(values))
		}

		if err := writeJSONRPCMessage(serverConn, &jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      initMsg.ID,
			Result:  json.RawMessage(`{"capabilities":{}}`),
		}); err != nil {
			t.Errorf("fake server write: %v", err)
			return
		}

		initialized := readMsg()
		if initialized == nil {
			return
		}
		if initialized.Method != "initialized" {
			t.Errorf("expected initialized notification, got %q", initialized.Method)
		}

		sym := readMsg()
		if sym == nil {
			return
		}
		if sym.Method != "workspace/symbol" {
			t.Errorf("expected workspace/symbol, got %q", sym.Method)
			return
		}
		if err := writeJSONRPCMessage(serverConn, &jsonrpcMessage{
			JSONRPC: "2.0",
			ID:      sym.ID,
			Result: json.RawMessage(`[{"name":"Greeter","kind":23,` +
				`"location":{"uri":"file:///proj/main.go",` +
				`"range":{"start":{"line":3,"character":5},"end":{"line":3,"character":12}}},` +
				`"containerName":"main"}]`),
		}); err != nil {
			t.Errorf("fake server write: %v", err)
			return
		}

		// Drain until the client closes the connection, so the client's
		// shutdown/exit notifies on Close do not block.
		for {
			if _, err := readJSONRPCMessage(reader); err != nil {
				return
			}
		}
	}()

	ctx := context.Background()
	if err := client.initialize(ctx, t.TempDir()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	var result json.RawMessage
	if err := client.call(ctx, "workspace/symbol", map[string]any{"query": "Greeter"}, &result); err != nil {
		t.Fatalf("workspace/symbol: %v", err)
	}
	if !strings.Contains(string(result), "Greeter") {
		t.Fatalf("expected Greeter in result, got %s", result)
	}

	client.Close()
	wg.Wait()
}

func TestReadJSONRPCMessage(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	frame := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	reader := bufio.NewReader(strings.NewReader(frame))
	msg, err := readJSONRPCMessage(reader)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "initialize" {
		t.Fatalf("expected method initialize, got %q", msg.Method)
	}
	if string(msg.ID) != "1" {
		t.Fatalf("expected id 1, got %s", msg.ID)
	}
	// A second read hits EOF.
	if _, err := readJSONRPCMessage(reader); err == nil {
		t.Fatal("expected EOF on second read")
	}
}

func TestReadJSONRPCMessageMissingLength(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("X-Header: 1\r\n\r\n{}"))
	_, err := readJSONRPCMessage(reader)
	if err == nil {
		t.Fatal("expected error for missing Content-Length")
	}
	if !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("error must mention Content-Length, got %v", err)
	}
}
