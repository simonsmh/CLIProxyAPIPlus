package common

import (
	"strings"
	"sync/atomic"
)

// Q's chat endpoint rejects tool specs named `web_search` — only the
// separate MCP endpoint accepts that name. The chat endpoint expects
// `remote_web_search` instead, so all tool specs and history tool_use
// references must be rewritten before sending.
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
	return name == "web_search" || strings.HasPrefix(name, "web_search") || strings.HasPrefix(name, "web_fetch")
}

// RenameWebSearchTool rewrites a tool spec's description to the live MCP description.
// It preserves the original name instead of renaming to remote_web_search.
func RenameWebSearchTool(name, description string) (string, string) {
	if !IsWebSearchToolName(name) {
		return name, description
	}
	if cached := GetWebSearchDescription(); cached != "" {
		description = cached
	} else {
		description = remoteWebSearchFallbackDescription
	}
	return name, description
}

// RenameWebSearchToolUse is a no-op that preserves the original tool_use name.
func RenameWebSearchToolUse(name string) string {
	return name
}
