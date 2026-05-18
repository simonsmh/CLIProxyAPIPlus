package common

import (
	"strings"
	"sync/atomic"
)

// Q's `web_search` upstream tool is reachable only via Kiro IDE's
// `/algo/api/v2/.../mcp` endpoint; the chat endpoint sees it under a
// different name. Reverse-engineered by Kiro IDE fork maintainers as
// the placeholder name the chat endpoint accepts so its validator lets
// the request through.
//
// In practice: clients that ship a `web_search` tool spec get rewritten
// to `remote_web_search` before being sent to Q's chat endpoint, and
// any history-side `tool_use` referencing `web_search` is rewritten the
// same way so the spec/use names match.
const RemoteWebSearchToolName = "remote_web_search"

// remoteWebSearchFallbackDescription is the minimal description we ship
// when no live description has been fetched from the MCP tools/list
// endpoint yet. The executor populates the live description via
// SetWebSearchDescription on first MCP fetch.
const remoteWebSearchFallbackDescription = "WebSearch looks up information outside the model's training data. Supports multiple queries to gather comprehensive information."

// cachedRemoteWebSearchDescription stores the live web_search tool
// description fetched from Kiro's MCP tools/list. Atomic + lock-free so
// the executor's background fetch and per-request reads don't contend.
var cachedRemoteWebSearchDescription atomic.Value // string

// GetWebSearchDescription returns the cached web_search tool description,
// or empty string if no fetch has populated it yet.
func GetWebSearchDescription() string {
	if v := cachedRemoteWebSearchDescription.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// SetWebSearchDescription stores the dynamically-fetched web_search tool
// description. The executor calls this after a successful MCP
// tools/list fetch.
func SetWebSearchDescription(desc string) {
	cachedRemoteWebSearchDescription.Store(desc)
}

// IsWebSearchToolName returns true when the supplied name matches any
// shape that should be treated as the Kiro `web_search` tool.
func IsWebSearchToolName(name string) bool {
	return name == "web_search"
}

// RenameWebSearchTool rewrites a tool spec's name and description to
// the form Q's chat endpoint accepts. If the input name isn't
// `web_search`, the inputs are returned unchanged.
//
// The new description prefers the live MCP description (set by
// SetWebSearchDescription) and falls back to remoteWebSearchFallbackDescription.
func RenameWebSearchTool(name, description string) (string, string) {
	if !IsWebSearchToolName(name) {
		return name, description
	}
	if cached := GetWebSearchDescription(); cached != "" {
		description = cached
	} else {
		description = remoteWebSearchFallbackDescription
	}
	return RemoteWebSearchToolName, description
}

// RenameWebSearchToolUse rewrites a tool_use's tool name to match the
// renamed spec, so the assistant history references resolve against
// the right entry. Returns the input unchanged if it isn't `web_search`.
func RenameWebSearchToolUse(name string) string {
	if strings.TrimSpace(name) == "web_search" {
		return RemoteWebSearchToolName
	}
	return name
}
