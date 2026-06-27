package claude

import (
	"strings"
	"testing"

	kirocommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/common"
	"github.com/tidwall/gjson"
)

// TestBuildKiroPayload_HistoryWithToolUseButNoTools verifies that when the
// client sends no tools but history contains tool_use/tool_result, the tool
// content is flattened to plain text. This avoids Bedrock's "toolConfig
// required" 400.
func TestBuildKiroPayload_HistoryWithToolUseButNoTools(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "list files"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "ls"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "file1\nfile2"}
			]},
			{"role": "user", "content": "now what?"}
		]
	}`

	out := BuildKiroPayload([]byte(claudeReq), "claude-sonnet-4-5", "arn:test", "test")
	if len(out) == 0 {
		t.Fatal("expected non-empty payload")
	}

	// No tools → flattened to text. No structured tools array.
	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		t.Fatalf("expected no synthesized tools after flatten, got: %s", tools.Raw)
	}

	// Verify no structured toolUses/toolResults remain
	payloadStr := string(out)
	if strings.Contains(payloadStr, `"toolUses"`) {
		t.Error("expected no structured toolUses after flatten")
	}
	if strings.Contains(payloadStr, `"toolResults"`) {
		t.Error("expected no structured toolResults after flatten")
	}
}

// TestBuildKiroPayload_HistoryWithToolUseAndExplicitTools confirms that when
// the client DOES attach tools, we don't double-add stubs.
func TestBuildKiroPayload_HistoryWithToolUseAndExplicitTools(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 1024,
		"tools": [
			{"name": "Bash", "description": "real desc", "input_schema": {"type": "object", "properties": {"command": {"type": "string"}}}}
		],
		"messages": [
			{"role": "user", "content": "list files"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "ls"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "ok"}
			]},
			{"role": "user", "content": "next"}
		]
	}`

	out := BuildKiroPayload([]byte(claudeReq), "claude-sonnet-4-5", "arn:test", "test")
	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if !tools.IsArray() || len(tools.Array()) != 1 {
		t.Fatalf("expected exactly 1 tool, got: %s", tools.Raw)
	}
	if got := tools.Array()[0].Get("toolSpecification.description").String(); got != "real desc" {
		t.Fatalf("expected real description preserved, got %q (likely overwritten by stub)", got)
	}
}

// TestBuildKiroPayload_NoToolsNoHistoryToolUse is the baseline: a plain text
// turn with no tool use anywhere should not introduce any tools.
func TestBuildKiroPayload_NoToolsNoHistoryToolUse(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"messages": [
			{"role": "user", "content": "hello"}
		]
	}`
	out := BuildKiroPayload([]byte(claudeReq), "claude-sonnet-4-5", "arn:test", "test")
	tools := gjson.GetBytes(out, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools")
	if tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		t.Fatalf("did not expect tools to be synthesized for plain chat turn: %s", tools.Raw)
	}
}

// TestFlattenToolHistory ensures tool_use and tool_result in history are
// converted to plain text when the client sends no tools.
func TestFlattenToolHistory(t *testing.T) {
	hist := []KiroHistoryMessage{
		{UserInputMessage: &KiroUserInputMessage{Content: "Read the file"}},
		{AssistantResponseMessage: &KiroAssistantResponseMessage{
			Content:  "I'll read it",
			ToolUses: []KiroToolUse{{Name: "Read", Input: map[string]interface{}{"path": "a.txt"}}},
		}},
		{UserInputMessage: &KiroUserInputMessage{
			Content: "Here's the result",
			UserInputMessageContext: &KiroUserInputMessageContext{
				ToolResults: []KiroToolResult{{
					ToolUseID: "tu_1",
					Content:   []KiroTextContent{{Text: "file contents"}},
				}},
			},
		}},
	}
	flattened := kirocommon.FlattenToolHistory(hist)

	// Assistant message: toolUse should be converted to text
	arm := flattened[1].AssistantResponseMessage
	if len(arm.ToolUses) != 0 {
		t.Fatalf("expected 0 toolUses after flatten, got %d", len(arm.ToolUses))
	}
	if !strings.Contains(arm.Content, "[Tool call: Read(") {
		t.Fatalf("expected tool call text in assistant content, got: %s", arm.Content)
	}

	// User message: toolResult should be converted to text
	uim := flattened[2].UserInputMessage
	if uim.UserInputMessageContext != nil {
		t.Fatalf("expected nil UserInputMessageContext after flatten")
	}
	if !strings.Contains(uim.Content, "[Tool result: file contents]") {
		t.Fatalf("expected tool result text in user content, got: %s", uim.Content)
	}
}
