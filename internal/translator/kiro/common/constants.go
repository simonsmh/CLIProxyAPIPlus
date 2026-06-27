// Package common provides shared constants and utilities for Kiro translator.
package common

const (
	// KiroMaxToolDescLen is the maximum description length for Kiro API tools.
	// Kiro API limit is 10240 bytes, leave room for "..."
	KiroMaxToolDescLen = 10237

	// ThinkingStartTag is the start tag for thinking blocks in responses.
	ThinkingStartTag = "<thinking>"

	// ThinkingEndTag is the end tag for thinking blocks in responses.
	ThinkingEndTag = "</thinking>"

	// CodeFenceMarker is the markdown code fence marker.
	CodeFenceMarker = "```"

	// AltCodeFenceMarker is the alternative markdown code fence marker.
	AltCodeFenceMarker = "~~~"

	// InlineCodeMarker is the markdown inline code marker (backtick).
	InlineCodeMarker = "`"

	// DefaultAssistantContentWithTools is the fallback content for assistant messages
	// that have tool_use but no text content. Kiro API requires non-empty content.
	// IMPORTANT: Use a bracketed marker so the model recognizes it as a structural
	// placeholder rather than conversational content to parrot back.
	// History: "." caused the model to echo "." in subsequent turns; "I'll help
	// you with that." caused parroting of that exact phrase.
	DefaultAssistantContentWithTools = "[tool_call]"

	// DefaultAssistantContent is the fallback content for assistant messages
	// that have no content at all. Kiro API requires non-empty content.
	// IMPORTANT: Use a bracketed marker so the model recognizes it as a structural
	// placeholder rather than conversational content to parrot back.
	DefaultAssistantContent = "[empty]"
)
