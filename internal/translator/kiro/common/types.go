// Package common holds the shared Kiro request/response payload types
// used by both the claude- and openai-format request builders.
// These types model the AWS Amazon Q `conversationState` wire format.
package common

// Kiro API request structs.

// KiroPayload is the top-level request structure for Kiro API.
type KiroPayload struct {
	ConversationState            KiroConversationState             `json:"conversationState"`
	ProfileArn                   string                            `json:"profileArn,omitempty"`
	AgentMode                    string                            `json:"agentMode,omitempty"`
	AdditionalModelRequestFields *KiroAdditionalModelRequestFields `json:"additionalModelRequestFields,omitempty"`
}

// KiroAdditionalModelRequestFields holds the thinking and output configuration.
type KiroAdditionalModelRequestFields struct {
	Thinking     *KiroThinkingConfig `json:"thinking,omitempty"`
	OutputConfig *KiroOutputConfig   `json:"output_config,omitempty"`
}

// KiroThinkingConfig holds thinking type.
type KiroThinkingConfig struct {
	Type string `json:"type,omitempty"` // "adaptive" or "disabled"
}

// KiroOutputConfig holds target effort.
type KiroOutputConfig struct {
	Effort string `json:"effort,omitempty"` // "low", "medium", "high", "xhigh", "max"
}

// KiroConversationState holds the conversation context.
type KiroConversationState struct {
	AgentContinuationID string               `json:"agentContinuationId,omitempty"`
	AgentTaskType       string               `json:"agentTaskType,omitempty"`
	ChatTriggerType     string               `json:"chatTriggerType"` // Required: "MANUAL"
	ConversationID      string               `json:"conversationId"`
	CurrentMessage      KiroCurrentMessage   `json:"currentMessage"`
	History             []KiroHistoryMessage `json:"history,omitempty"`
}

// KiroCurrentMessage wraps the current user message.
type KiroCurrentMessage struct {
	UserInputMessage KiroUserInputMessage `json:"userInputMessage"`
}

// KiroHistoryMessage represents a message in the conversation history.
type KiroHistoryMessage struct {
	UserInputMessage         *KiroUserInputMessage         `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *KiroAssistantResponseMessage `json:"assistantResponseMessage,omitempty"`
}

// KiroImage represents an image in Kiro API format.
type KiroImage struct {
	Format string          `json:"format"`
	Source KiroImageSource `json:"source"`
}

// KiroImageSource contains the image data.
type KiroImageSource struct {
	Bytes string `json:"bytes"` // base64 encoded image data
}

// KiroUserInputMessage represents a user message.
type KiroUserInputMessage struct {
	Content                 string                       `json:"content"`
	ModelID                 string                       `json:"modelId"`
	Origin                  string                       `json:"origin"`
	Images                  []KiroImage                  `json:"images,omitempty"`
	UserInputMessageContext *KiroUserInputMessageContext `json:"userInputMessageContext,omitempty"`
}

// KiroUserInputMessageContext contains tool-related context.
type KiroUserInputMessageContext struct {
	ToolResults []KiroToolResult  `json:"toolResults,omitempty"`
	Tools       []KiroToolWrapper `json:"tools,omitempty"`
}

// KiroToolResult represents a tool execution result.
type KiroToolResult struct {
	Content   []KiroTextContent `json:"content"`
	Status    string            `json:"status"`
	ToolUseID string            `json:"toolUseId"`
}

// KiroTextContent represents text content.
type KiroTextContent struct {
	Text string `json:"text"`
}

// KiroToolWrapper wraps a tool specification.
type KiroToolWrapper struct {
	ToolSpecification KiroToolSpecification `json:"toolSpecification"`
}

// KiroToolSpecification defines a tool's schema.
type KiroToolSpecification struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema KiroInputSchema `json:"inputSchema"`
}

// KiroInputSchema wraps the JSON schema for tool input.
type KiroInputSchema struct {
	JSON interface{} `json:"json"`
}

// KiroAssistantResponseMessage represents an assistant message.
type KiroAssistantResponseMessage struct {
	Content  string        `json:"content"`
	ToolUses []KiroToolUse `json:"toolUses,omitempty"`
}

// KiroToolUse represents a tool invocation by the assistant.
// IsTruncated and TruncationInfo are JSON-skipped runtime fields
// populated by the truncation detector.
type KiroToolUse struct {
	ToolUseID      string                 `json:"toolUseId"`
	Name           string                 `json:"name"`
	Input          map[string]interface{} `json:"input"`
	IsTruncated    bool                   `json:"-"`
	TruncationInfo *TruncationInfo        `json:"-"`
}

// TruncationInfo contains details about detected truncation in a tool use event.
// The detection logic lives in internal/translator/kiro/claude/truncation_detector.go.
type TruncationInfo struct {
	IsTruncated    bool              // Whether truncation was detected
	TruncationType string            // Type of truncation detected
	ToolName       string            // Name of the truncated tool
	ToolUseID      string            // ID of the truncated tool use
	RawInput       string            // The raw (possibly truncated) input string
	ParsedFields   map[string]string // Fields that were successfully parsed before truncation
	ErrorMessage   string            // Human-readable error message
}
