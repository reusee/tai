package generators

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestOpenAIParserEmptyDelta(t *testing.T) {
	parser := new(OpenAIParser)

	contents, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Content: "foo",
		Role:    string(RoleAssistant),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatal()
	}

	contents, err = parser.Input(ChatCompletionStreamChoiceDelta{})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) > 0 {
		t.Fatal()
	}

	contents, err = parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatal(err)
	}
	if contents[0].Role != RoleAssistant {
		t.Fatalf("got %+v", contents)
	}
}

func TestOpenAIParserEmptyRole(t *testing.T) {
	parser := new(OpenAIParser)
	contents, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) > 0 {
		t.Fatal()
	}
	contents, err = parser.Input(ChatCompletionStreamChoiceDelta{
		Content: "foo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) > 0 {
		t.Fatal()
	}
	contents, err = parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatal()
	}
	if contents[0].Role != RoleAssistant {
		t.Fatalf("got %+v", contents)
	}
}

func TestOpenAIParserReasoningContent(t *testing.T) {
	parser := new(OpenAIParser)

	contents, err := parser.Input(ChatCompletionStreamChoiceDelta{
		ReasoningContent: "think",
		Role:             string(RoleAssistant),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatal()
	}

	contents, err = parser.Input(ChatCompletionStreamChoiceDelta{
		Content: "content",
		Role:    string(RoleAssistant),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatal()
	}

	contents, err = parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatal(contents)
	}
	if len(contents[0].Parts) != 2 {
		t.Fatal(contents)
	}
	thought, ok := contents[0].Parts[0].(Thought)
	if !ok {
		t.Fatalf("got %#v", contents[0].Parts[0])
	}
	if thought != "think" {
		t.Fatalf("got %v", thought)
	}
	content, ok := contents[0].Parts[1].(Text)
	if !ok {
		t.Fatalf("got %#v", contents[0].Parts[1])
	}
	if content != "content" {
		t.Fatalf("got %v", content)
	}
}

func TestOpenAIParserToolCallStreamedArgs(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and tool call start
	contents, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name: "test_func",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatal()
	}

	// First part of args
	contents, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ToolCalls: []ToolCall{
			{
				Function: FunctionCall{
					Arguments: `{"arg1": "val`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatal()
	}

	// Second part of args
	contents, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ToolCalls: []ToolCall{
			{
				Function: FunctionCall{
					Arguments: `ue"}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatal()
	}

	// End of stream
	contents, err = parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if content.Role != RoleAssistant {
		t.Fatalf("wrong role: %s", content.Role)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	funcCall, ok := content.Parts[0].(FuncCall)
	if !ok {
		t.Fatalf("part is not FuncCall: %+v", content.Parts)
	}
	if funcCall.ID != "call_123" {
		t.Errorf("wrong ID: %s", funcCall.ID)
	}
	if funcCall.Name != "test_func" {
		t.Errorf("wrong name: %s", funcCall.Name)
	}
	expectedArgs := map[string]any{"arg1": "value"}
	if !reflect.DeepEqual(funcCall.Args, expectedArgs) {
		t.Errorf("wrong args: got %+v, want %+v", funcCall.Args, expectedArgs)
	}
}

func TestOpenAIParserMultipleToolCalls(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and first tool call
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
		ToolCalls: []ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "func1",
					Arguments: `{"a": 1}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second tool call
	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ToolCalls: []ToolCall{
			{
				ID:   "call_2",
				Type: "function",
				Function: FunctionCall{
					Name:      "func2",
					Arguments: `{"b": 2}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content.Parts))
	}

	// Check first call
	funcCall1, ok := content.Parts[0].(FuncCall)
	if !ok {
		t.Fatalf("part 0 is not FuncCall: %+v", content.Parts)
	}
	if funcCall1.ID != "call_1" || funcCall1.Name != "func1" {
		t.Errorf("unexpected funcCall1: %+v", funcCall1)
	}
	if !reflect.DeepEqual(funcCall1.Args, map[string]any{"a": float64(1)}) {
		t.Errorf("unexpected args for funcCall1: %+v", funcCall1.Args)
	}

	// Check second call
	funcCall2, ok := content.Parts[1].(FuncCall)
	if !ok {
		t.Fatalf("part 1 is not FuncCall: %+v", content.Parts)
	}
	if funcCall2.ID != "call_2" || funcCall2.Name != "func2" {
		t.Errorf("unexpected funcCall2: %+v", funcCall2)
	}
	if !reflect.DeepEqual(funcCall2.Args, map[string]any{"b": float64(2)}) {
		t.Errorf("unexpected args for funcCall2: %+v", funcCall2.Args)
	}
}

func TestOpenAIParserTextAndToolCall(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and text
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleAssistant),
		Content: "Here is the tool call: ",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Tool call
	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "test_func",
					Arguments: `{}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content.Parts))
	}

	text, ok := content.Parts[0].(Text)
	if !ok || text != "Here is the tool call: " {
		t.Errorf("unexpected part 0: %+v", content.Parts)
	}

	funcCall, ok := content.Parts[1].(FuncCall)
	if !ok {
		t.Fatalf("part 1 is not FuncCall: %+v", content.Parts)
	}
	if funcCall.Name != "test_func" {
		t.Errorf("unexpected funcCall: %+v", funcCall)
	}
}

func TestOpenAIParserToolCallAndText(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and tool call
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "test_func",
					Arguments: `{}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Text
	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		Content: "Tool call finished.",
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d, parts: %+v", len(content.Parts), content.Parts)
	}

	funcCall, ok := content.Parts[0].(FuncCall)
	if !ok {
		t.Fatalf("part 0 is not FuncCall: %+v", content.Parts)
	}
	if funcCall.Name != "test_func" {
		t.Errorf("unexpected funcCall: %+v", funcCall)
	}

	text, ok := content.Parts[1].(Text)
	if !ok || text != "Tool call finished." {
		t.Errorf("unexpected part 1: %+v", content.Parts)
	}
}

func TestOpenAIParserToolCallNoArgs(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and tool call
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "test_func",
					Arguments: "", // Empty string for arguments
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}

	funcCall, ok := content.Parts[0].(FuncCall)
	if !ok {
		t.Fatalf("part 0 is not FuncCall: %+v", content.Parts)
	}
	if funcCall.Name != "test_func" {
		t.Errorf("unexpected funcCall: %+v", funcCall)
	}
	if len(funcCall.Args) != 0 {
		t.Errorf("expected empty args, got: %+v", funcCall.Args)
	}
}

func TestOpenAIParserToolCallInvalidJSON(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and tool call with invalid JSON
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "test_func",
					Arguments: `{"arg1": "val`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.End()
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("expected json.SyntaxError, got %T: %v", err, err)
	}
}

func TestOpenAIParserRoleChange(t *testing.T) {
	parser := new(OpenAIParser)

	// Assistant starts speaking
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleAssistant),
		Content: "Hello. ",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Role changes
	contents, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleTool),
		Content: "Tool output.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatal()
	}

	content1 := contents[0]
	if content1.Role != RoleAssistant {
		t.Errorf("content1 has wrong role: %s", content1.Role)
	}
	if len(content1.Parts) != 1 || content1.Parts[0].(Text) != "Hello. " {
		t.Errorf("unexpected content1 parts: %+v", content1.Parts)
	}

	contents, err = parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 contents, got %d", len(contents))
	}

	content2 := contents[0]
	if content2.Role != RoleTool {
		t.Errorf("content2 has wrong role: %s", content2.Role)
	}
	if len(content2.Parts) != 1 || content2.Parts[0].(Text) != "Tool output." {
		t.Errorf("unexpected content2 parts: %+v", content2.Parts)
	}
}

func TestOpenAIParserFlushOnBufferFull(t *testing.T) {
	parser := new(OpenAIParser)

	longText := Text("This is a very long text that is definitely longer than 64 characters to test the flushing mechanism.")

	// Role and first part of long text
	contents, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleAssistant),
		Content: string(longText[:70]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content due to flush, got %d", len(contents))
	}
	if content := contents[0]; len(content.Parts) != 1 || content.Parts[0].(Text) != longText[:70] {
		t.Errorf("unexpected flushed content: %+v", content)
	}

	// Second part of long text
	contents, err = parser.Input(ChatCompletionStreamChoiceDelta{
		Content: string(longText[70:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatalf("expected 0 contents, got %d", len(contents))
	}

	// End of stream
	contents, err = parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content at end, got %d", len(contents))
	}
	if content := contents[0]; len(content.Parts) != 1 || content.Parts[0].(Text) != longText[70:] {
		t.Errorf("unexpected final content: %+v", content)
	}
}

func TestOpenAIParserSingleDeltaMultipleToolCalls(t *testing.T) {
	parser := new(OpenAIParser)

	// Role and two tool calls in one delta
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role: string(RoleAssistant),
		ToolCalls: []ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "func1",
					Arguments: `{"a": 1}`,
				},
			},
			{
				ID:   "call_2",
				Type: "function",
				Function: FunctionCall{
					Name:      "func2",
					Arguments: `{"b": 2}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content.Parts))
	}

	// Check first call
	funcCall1, ok := content.Parts[0].(FuncCall)
	if !ok {
		t.Fatalf("part 0 is not FuncCall: %+v", content.Parts)
	}
	if funcCall1.ID != "call_1" || funcCall1.Name != "func1" {
		t.Errorf("unexpected funcCall1: %+v", funcCall1)
	}
	if !reflect.DeepEqual(funcCall1.Args, map[string]any{"a": float64(1)}) {
		t.Errorf("unexpected args for funcCall1: %+v", funcCall1.Args)
	}

	// Check second call
	funcCall2, ok := content.Parts[1].(FuncCall)
	if !ok {
		t.Fatalf("part 1 is not FuncCall: %+v", content.Parts)
	}
	if funcCall2.ID != "call_2" || funcCall2.Name != "func2" {
		t.Errorf("unexpected funcCall2: %+v", funcCall2)
	}
	if !reflect.DeepEqual(funcCall2.Args, map[string]any{"b": float64(2)}) {
		t.Errorf("unexpected args for funcCall2: %+v", funcCall2.Args)
	}
}

func TestOpenAIParserDeltaWithContentAndToolCall(t *testing.T) {
	parser := new(OpenAIParser)

	// Delta with both content and a tool call
	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleAssistant),
		Content: "Here is the tool call: ",
		ToolCalls: []ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: FunctionCall{
					Name:      "test_func",
					Arguments: `{}`,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content.Parts))
	}

	text, ok := content.Parts[0].(Text)
	if !ok || text != "Here is the tool call: " {
		t.Errorf("unexpected part 0: %+v", content.Parts)
	}

	funcCall, ok := content.Parts[1].(FuncCall)
	if !ok {
		t.Fatalf("part 1 is not FuncCall: %+v", content.Parts)
	}
	if funcCall.Name != "test_func" {
		t.Errorf("unexpected funcCall: %+v", funcCall)
	}
}

func TestOpenAIParserInterleavedTextAndReasoning(t *testing.T) {
	parser := new(OpenAIParser)

	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleAssistant),
		Content: "Some text. ",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ReasoningContent: "Some thought. ",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		Content: "More text.",
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %+v", len(content.Parts), content.Parts)
	}

	if _, ok := content.Parts[0].(Text); !ok {
		t.Errorf("part 0 is not Text")
	}
	if _, ok := content.Parts[1].(Thought); !ok {
		t.Errorf("part 1 is not Thought")
	}
	if _, ok := content.Parts[2].(Text); !ok {
		t.Errorf("part 2 is not Text")
	}
}

func TestOpenAIParserPartMerging(t *testing.T) {
	parser := new(OpenAIParser)

	_, err := parser.Input(ChatCompletionStreamChoiceDelta{
		Role:    string(RoleAssistant),
		Content: "Part 1. ",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		Content: "Part 2.",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ReasoningContent: "Thought 1. ",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = parser.Input(ChatCompletionStreamChoiceDelta{
		ReasoningContent: "Thought 2.",
	})
	if err != nil {
		t.Fatal(err)
	}

	contents, err := parser.End()
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	content := contents[0]
	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %+v", len(content.Parts), content.Parts)
	}

	text, ok := content.Parts[0].(Text)
	if !ok || text != "Part 1. Part 2." {
		t.Errorf("unexpected text part: %+v", content.Parts)
	}

	thought, ok := content.Parts[1].(Thought)
	if !ok || thought != "Thought 1. Thought 2." {
		t.Errorf("unexpected thought part: %+v", content.Parts)
	}
}
