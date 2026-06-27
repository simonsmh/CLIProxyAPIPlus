package common

import (
	"strings"
	"sync/atomic"
)

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

// UpdateWebSearchToolDescription replaces the description for web_search tools
// with the live MCP description (or a fallback). Non-web-search tools are
// returned unchanged. Only the description is ever modified; the name is
// returned as-is for caller convenience.
func UpdateWebSearchToolDescription(name, description string) string {
	if !IsWebSearchToolName(name) {
		return description
	}
	if cached := GetWebSearchDescription(); cached != "" {
		return cached
	}
	return remoteWebSearchFallbackDescription
}
