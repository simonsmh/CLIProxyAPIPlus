package common

import (
	"fmt"
	"strings"

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

	// stubInputSchema is the permissive schema used by
	// SynthesizeToolSpecsFromHistory for fallback tool stubs.
	stubInputSchema = map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": true,
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

// SynthesizeToolSpecsFromHistory walks history and builds stub
// KiroToolWrapper entries for every tool name the assistant has used,
// so a request that replays history but no longer carries explicit
// tool specs still passes Q's "history references unknown tool"
// validator.
func SynthesizeToolSpecsFromHistory(history []KiroHistoryMessage) []KiroToolWrapper {
	if len(history) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var stubs []KiroToolWrapper
	for _, h := range history {
		if h.AssistantResponseMessage == nil {
			continue
		}
		for _, tu := range h.AssistantResponseMessage.ToolUses {
			name := strings.TrimSpace(tu.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			stubs = append(stubs, KiroToolWrapper{
				ToolSpecification: KiroToolSpecification{
					Name:        ShortenToolNameIfNeeded(name),
					Description: fmt.Sprintf("Tool: %s", name),
					InputSchema: KiroInputSchema{JSON: stubInputSchema},
				},
			})
		}
	}
	return stubs
}
