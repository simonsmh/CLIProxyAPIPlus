package common

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

var (
	// emptySchema is the minimal empty-object JSON schema returned
	// by EnsureKiroInputSchema when no parameters are provided.
	// Kiro rejects null schemas.
	emptySchema = map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
)

// NormalizeOrigin maps incoming origin labels onto the two values the Kiro
// upstream actually accepts: "CLI" (Amazon Q quota) and "AI_EDITOR" (Kiro
// IDE quota). Anything that does not need rewriting is returned unchanged.
func NormalizeOrigin(origin string) string {
	switch origin {
	case "KIRO_CLI", "AMAZON_Q":
		return "CLI"
	case "KIRO_AI_EDITOR", "KIRO_IDE":
		return "AI_EDITOR"
	default:
		return origin
	}
}

// ExtractMetadataFromMessages walks messages from newest to oldest and
// returns the value at additional_kwargs.<key> on the first message that
// has it. Used to pull conversationId / agentContinuationId from the
// LangChain-style payload some clients send.
func ExtractMetadataFromMessages(messages gjson.Result, key string) string {
	arr := messages.Array()
	for i := len(arr) - 1; i >= 0; i-- {
		if val := arr[i].Get("additional_kwargs." + key); val.Exists() && val.String() != "" {
			return val.String()
		}
	}
	return ""
}

// ShortenToolNameIfNeeded clips Kiro tool names to the upstream's 64-byte
// limit. MCP tools (prefix "mcp__") get special handling to preserve the
// prefix and the most-distinctive suffix segment.
func ShortenToolNameIfNeeded(name string) string {
	const limit = 64
	if len(name) <= limit {
		return name
	}
	if strings.HasPrefix(name, "mcp__") {
		idx := strings.LastIndex(name, "__")
		if idx > 0 {
			cand := "mcp__" + name[idx+2:]
			if len(cand) > limit {
				return cand[:limit]
			}
			return cand
		}
	}
	return name[:limit]
}

// EnsureKiroInputSchema returns the parameters object verbatim when
// non-nil, or a minimal empty-object schema otherwise (Kiro rejects nil schemas).
func EnsureKiroInputSchema(parameters interface{}) interface{} {
	if parameters != nil {
		return parameters
	}
	return emptySchema
}

// DeduplicateToolResults drops repeated tool_use_id entries from the
// slice, preserving first-seen order.
func DeduplicateToolResults(toolResults []KiroToolResult) []KiroToolResult {
	if len(toolResults) == 0 {
		return toolResults
	}
	seenIDs := make(map[string]struct{})
	unique := make([]KiroToolResult, 0, len(toolResults))
	for _, tr := range toolResults {
		if _, seen := seenIDs[tr.ToolUseID]; !seen {
			seenIDs[tr.ToolUseID] = struct{}{}
			unique = append(unique, tr)
		} else {
			log.Debugf("kiro: skipping duplicate toolResult: %s", tr.ToolUseID)
		}
	}
	return unique
}

// FlattenToolHistory walks history and converts all structured tool_use /
// tool_result content into plain text lines so that Bedrock's "toolConfig
// required" validator never fires. Only call this when the client did NOT
// send tools — when tools are present the structured form is preserved.
//
// Returns a new history slice with all tool references flattened to text.
func FlattenToolHistory(history []KiroHistoryMessage) []KiroHistoryMessage {
	if len(history) == 0 {
		return history
	}
	out := make([]KiroHistoryMessage, 0, len(history))
	for _, h := range history {
		h = flattenOneHistoryMessage(h)
		out = append(out, h)
	}
	return out
}

// FlattenCurrentMessage strips structured tool content from the current
// user message (toolResults → text, removes tools array).
func FlattenCurrentMessage(msg *KiroUserInputMessage) *KiroUserInputMessage {
	if msg == nil || msg.UserInputMessageContext == nil {
		return msg
	}
	ctx := msg.UserInputMessageContext
	if len(ctx.ToolResults) > 0 {
		for _, tr := range ctx.ToolResults {
			text := toolResultContentToText(tr.Content)
			if text != "" {
				if msg.Content != "" {
					msg.Content += "\n\n" + text
				} else {
					msg.Content = text
				}
			}
		}
		ctx.ToolResults = nil
	}
	// Remove tools — client didn't ask for tool calling
	ctx.Tools = nil
	if len(ctx.ToolResults) == 0 && len(ctx.Tools) == 0 {
		msg.UserInputMessageContext = nil
	}
	return msg
}

// flattenOneHistoryMessage converts tool_use / tool_result in a single
// history message to plain text.
func flattenOneHistoryMessage(h KiroHistoryMessage) KiroHistoryMessage {
	// Flatten assistant message: convert toolUses to text lines
	if h.AssistantResponseMessage != nil && len(h.AssistantResponseMessage.ToolUses) > 0 {
		arm := h.AssistantResponseMessage
		for _, tu := range arm.ToolUses {
			argStr := "{}"
			if len(tu.Input) > 0 {
				if b, err := json.Marshal(tu.Input); err == nil {
					argStr = string(b)
				}
			}
			line := fmt.Sprintf("[Tool call: %s(%s)]", tu.Name, argStr)
			if arm.Content != "" {
				arm.Content += "\n" + line
			} else {
				arm.Content = line
			}
		}
		arm.ToolUses = nil
	}

	// Flatten user message: convert toolResults to text lines
	if h.UserInputMessage != nil && h.UserInputMessage.UserInputMessageContext != nil {
		ctx := h.UserInputMessage.UserInputMessageContext
		if len(ctx.ToolResults) > 0 {
			for _, tr := range ctx.ToolResults {
				text := toolResultContentToText(tr.Content)
				if text != "" {
					if h.UserInputMessage.Content != "" {
						h.UserInputMessage.Content += "\n\n" + text
					} else {
						h.UserInputMessage.Content = text
					}
				}
			}
			ctx.ToolResults = nil
		}
		ctx.Tools = nil
		if len(ctx.ToolResults) == 0 && len(ctx.Tools) == 0 {
			h.UserInputMessage.UserInputMessageContext = nil
		}
	}

	return h
}

// toolResultContentToText converts KiroTextContent array to a single string.
func toolResultContentToText(content []KiroTextContent) string {
	if len(content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, c := range content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" {
		return ""
	}
	return "[Tool result: " + text + "]"
}

// BuildKiroThinkingConfig converts a canonical thinking.ThinkingConfig into
// the Kiro-specific additionalModelRequestFields payload. Returns nil when no
// thinking configuration was requested or when modelID is "auto".
//
// Shared by both the Claude and OpenAI request translators to avoid
// duplicating the mapping logic.
func BuildKiroThinkingConfig(config thinking.ThinkingConfig, modelID string) *KiroAdditionalModelRequestFields {
	hasThinkingConfig := config.Mode != thinking.ModeBudget || config.Budget != 0 || config.Level != ""
	if modelID == "auto" || !hasThinkingConfig {
		return nil
	}

	switch {
	case config.Mode == thinking.ModeLevel && config.Level != "":
		return buildFromLevel(strings.ToLower(strings.TrimSpace(string(config.Level))))

	case config.Mode == thinking.ModeBudget:
		levelStr, ok := thinking.ConvertBudgetToLevel(config.Budget)
		if !ok {
			return nil
		}
		return buildFromLevel(strings.ToLower(strings.TrimSpace(levelStr)))

	case config.Mode == thinking.ModeNone:
		return &KiroAdditionalModelRequestFields{
			Thinking: &KiroThinkingConfig{Type: "disabled"},
		}

	case config.Mode == thinking.ModeAuto:
		return &KiroAdditionalModelRequestFields{
			Thinking: &KiroThinkingConfig{Type: "adaptive"},
		}

	default:
		return nil
	}
}

// buildFromLevel maps a normalised effort level string to Kiro fields.
func buildFromLevel(levelStr string) *KiroAdditionalModelRequestFields {
	if levelStr == "minimal" {
		levelStr = "low"
	}
	switch levelStr {
	case "auto":
		return &KiroAdditionalModelRequestFields{
			Thinking: &KiroThinkingConfig{Type: "adaptive"},
		}
	case "none":
		return &KiroAdditionalModelRequestFields{
			Thinking: &KiroThinkingConfig{Type: "disabled"},
		}
	case "low", "medium", "high", "xhigh", "max":
		return &KiroAdditionalModelRequestFields{
			Thinking:     &KiroThinkingConfig{Type: "adaptive"},
			OutputConfig: &KiroOutputConfig{Effort: levelStr},
		}
	default:
		return nil
	}
}
