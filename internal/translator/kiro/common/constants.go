// Package common provides shared constants and utilities for Kiro translator.
package common

import "sync/atomic"

const (
	// KiroMaxToolDescLen is the maximum description length for Kiro API tools.
	// Kiro API limit is 10240 bytes, leave room for "..."
	KiroMaxToolDescLen = 10237
)

// extractThinkingTagEnabled controls whether inline <thinking>...</thinking>
// tags inside assistantResponseEvent content are parsed into Claude thinking
// blocks. This is an unofficial path — Kiro's official reasoning signal is
// reasoningContentEvent. The tag parser can false-positive when content
// literally mentions the tag string (code samples, discussion, XML fixtures),
// which silently truncates responses. Default: 0 (disabled).
var extractThinkingTagEnabled atomic.Int32

func init() {
	extractThinkingTagEnabled.Store(0)
}

// SetExtractThinkingTagEnabled toggles inline <thinking> tag extraction.
func SetExtractThinkingTagEnabled(enabled bool) {
	if enabled {
		extractThinkingTagEnabled.Store(1)
	} else {
		extractThinkingTagEnabled.Store(0)
	}
}

// IsExtractThinkingTagEnabled reports whether inline <thinking> tag extraction is active.
func IsExtractThinkingTagEnabled() bool {
	return extractThinkingTagEnabled.Load() == 1
}
