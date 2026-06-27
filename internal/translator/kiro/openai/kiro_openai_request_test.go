package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestToolResultsFlattenedToText verifies that tool results from "tool" role messages
// are flattened to plain text when no tools array is provided in the request.
// This avoids Bedrock's "toolConfig required" 400 error.
func TestToolResultsFlattenedToText(t *testing.T) {
	input := []byte(`{
		"model": "kiro-claude-opus-4-5-agentic",
		"messages": [
			{"role": "user", "content": "Hello, can you read a file for me?"},
			{
				"role": "assistant",
				"content": "I'll read that file for you.",
				"tool_calls": [
					{
						"id": "call_abc123",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"file_path\": \"/tmp/test.txt\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_abc123",
				"content": "File contents: Hello World!"
			},
			{"role": "user", "content": "What did the file say?"}
		]
	}`)

	result := BuildKiroPayloadFromOpenAI(input, "kiro-model", "", "CLI")

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// No tools provided → tool content flattened to text
	// Assistant message should have tool call as text
	if len(payload.ConversationState.History) < 2 {
		t.Fatalf("Expected at least 2 history entries, got %d", len(payload.ConversationState.History))
	}
	arm := payload.ConversationState.History[1].AssistantResponseMessage
	if arm == nil {
		t.Fatal("Expected assistant message in history[1]")
	}
	if arm.ToolUses != nil && len(arm.ToolUses) > 0 {
		t.Errorf("Expected no structured toolUses after flatten, got %d", len(arm.ToolUses))
	}

	// CurrentMessage should have no UserInputMessageContext (tool results flattened to text)
	ctx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx != nil && len(ctx.ToolResults) > 0 {
		t.Errorf("Expected no structured tool results in currentMessage after flatten")
	}
}

// TestToolResultsInHistoryFlattened verifies that tool results in history are
// flattened to text when no tools are provided.
func TestToolResultsInHistoryFlattened(t *testing.T) {
	input := []byte(`{
		"model": "kiro-claude-opus-4-5-agentic",
		"messages": [
			{"role": "user", "content": "Hello"},
			{
				"role": "assistant",
				"content": "I'll read the file.",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_1",
				"content": "File result"
			},
			{"role": "user", "content": "Thanks for the file"},
			{"role": "assistant", "content": "You're welcome"},
			{"role": "user", "content": "Bye"}
		]
	}`)

	result := BuildKiroPayloadFromOpenAI(input, "kiro-model", "", "CLI")

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// No tools → all tool content flattened to text
	// Verify no structured tool references remain
	allJSON := string(result)
	if strings.Contains(allJSON, `"toolUses"`) {
		t.Error("Expected no structured toolUses in output")
	}
	if strings.Contains(allJSON, `"toolResults"`) {
		t.Error("Expected no structured toolResults in output")
	}
}

// TestToolResultsMultipleFlattened verifies multiple tool calls are flattened
func TestToolResultsMultipleFlattened(t *testing.T) {
	input := []byte(`{
		"model": "kiro-claude-opus-4-5-agentic",
		"messages": [
			{"role": "user", "content": "Read two files for me"},
			{
				"role": "assistant",
				"content": "I'll read both files.",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"file_path\": \"/tmp/file1.txt\"}"
						}
					},
					{
						"id": "call_2",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"file_path\": \"/tmp/file2.txt\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_1",
				"content": "Content of file 1"
			},
			{
				"role": "tool",
				"tool_call_id": "call_2",
				"content": "Content of file 2"
			},
			{"role": "user", "content": "What do they say?"}
		]
	}`)

	result := BuildKiroPayloadFromOpenAI(input, "kiro-model", "", "CLI")

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// No tools → flattened to text. Verify no structured tool content.
	allJSON := string(result)
	if strings.Contains(allJSON, `"toolUses"`) {
		t.Error("Expected no structured toolUses in output")
	}
	if strings.Contains(allJSON, `"toolResults"`) {
		t.Error("Expected no structured toolResults in output")
	}
}

// TestToolResultsAtEndFlattened verifies tool results at end of conversation are flattened
func TestToolResultsAtEndFlattened(t *testing.T) {
	input := []byte(`{
		"model": "kiro-claude-opus-4-5-agentic",
		"messages": [
			{"role": "user", "content": "Read a file"},
			{
				"role": "assistant",
				"content": "Reading the file.",
				"tool_calls": [
					{
						"id": "call_end",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"file_path\": \"/tmp/test.txt\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_end",
				"content": "File contents here"
			}
		]
	}`)

	result := BuildKiroPayloadFromOpenAI(input, "kiro-model", "", "CLI")

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// No tools → flattened. Verify no structured tool content.
	allJSON := string(result)
	if strings.Contains(allJSON, `"toolUses"`) {
		t.Error("Expected no structured toolUses in output")
	}
	if strings.Contains(allJSON, `"toolResults"`) {
		t.Error("Expected no structured toolResults in output")
	}
}

// TestToolResultsFollowedByAssistantFlattened verifies tool results followed by assistant
// are flattened when no tools provided.
func TestToolResultsFollowedByAssistantFlattened(t *testing.T) {
	input := []byte(`{
		"model": "kiro-claude-opus-4-5-agentic",
		"messages": [
			{"role": "user", "content": "Read two files for me"},
			{
				"role": "assistant",
				"content": "I'll read both files.",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"file_path\": \"/tmp/a.txt\"}"
						}
					},
					{
						"id": "call_2",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"file_path\": \"/tmp/b.txt\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_1",
				"content": "Contents of file A"
			},
			{
				"role": "tool",
				"tool_call_id": "call_2",
				"content": "Contents of file B"
			},
			{
				"role": "assistant",
				"content": "I've read both files."
			},
			{"role": "user", "content": "What did they say?"}
		]
	}`)

	result := BuildKiroPayloadFromOpenAI(input, "kiro-model", "", "CLI")

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// No tools → flattened. Verify no structured tool content.
	allJSON := string(result)
	if strings.Contains(allJSON, `"toolUses"`) {
		t.Error("Expected no structured toolUses in output")
	}
	if strings.Contains(allJSON, `"toolResults"`) {
		t.Error("Expected no structured toolResults in output")
	}
}

// TestAssistantEndsConversation verifies handling when assistant is the last message
func TestAssistantEndsConversation(t *testing.T) {
	input := []byte(`{
		"model": "kiro-claude-opus-4-5-agentic",
		"messages": [
			{"role": "user", "content": "Hello"},
			{
				"role": "assistant",
				"content": "Hi there!"
			}
		]
	}`)

	result := BuildKiroPayloadFromOpenAI(input, "kiro-model", "", "CLI")

	var payload KiroPayload
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// When assistant is last, a continuation user message should be created with empty content
	if payload.ConversationState.CurrentMessage.UserInputMessage.Content != "" {
		t.Errorf("Expected a continuation message with empty content when assistant is last, got %q", payload.ConversationState.CurrentMessage.UserInputMessage.Content)
	}
}

func TestFilterOrphanedToolResults_RemovesHistoryAndCurrentOrphans(t *testing.T) {
	history := []KiroHistoryMessage{
		{
			AssistantResponseMessage: &KiroAssistantResponseMessage{
				Content: "assistant",
				ToolUses: []KiroToolUse{
					{ToolUseID: "keep-1", Name: "Read", Input: map[string]interface{}{}},
				},
			},
		},
		{
			UserInputMessage: &KiroUserInputMessage{
				Content: "user-with-mixed-results",
				UserInputMessageContext: &KiroUserInputMessageContext{
					ToolResults: []KiroToolResult{
						{ToolUseID: "keep-1", Status: "success", Content: []KiroTextContent{{Text: "ok"}}},
						{ToolUseID: "orphan-1", Status: "success", Content: []KiroTextContent{{Text: "bad"}}},
					},
				},
			},
		},
		{
			UserInputMessage: &KiroUserInputMessage{
				Content: "user-only-orphans",
				UserInputMessageContext: &KiroUserInputMessageContext{
					ToolResults: []KiroToolResult{
						{ToolUseID: "orphan-2", Status: "success", Content: []KiroTextContent{{Text: "bad"}}},
					},
				},
			},
		},
	}

	currentToolResults := []KiroToolResult{
		{ToolUseID: "keep-1", Status: "success", Content: []KiroTextContent{{Text: "ok"}}},
		{ToolUseID: "orphan-3", Status: "success", Content: []KiroTextContent{{Text: "bad"}}},
	}

	filteredHistory, filteredCurrent := filterOrphanedToolResults(history, currentToolResults)

	ctx1 := filteredHistory[1].UserInputMessage.UserInputMessageContext
	if ctx1 == nil || len(ctx1.ToolResults) != 1 || ctx1.ToolResults[0].ToolUseID != "keep-1" {
		t.Fatalf("expected mixed history message to keep only keep-1, got: %+v", ctx1)
	}

	if filteredHistory[2].UserInputMessage.UserInputMessageContext != nil {
		t.Fatalf("expected orphan-only history context to be removed")
	}

	if len(filteredCurrent) != 1 || filteredCurrent[0].ToolUseID != "keep-1" {
		t.Fatalf("expected current tool results to keep only keep-1, got: %+v", filteredCurrent)
	}
}
