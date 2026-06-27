// Package claude provides request translation functionality for Claude API to Kiro format.
// It handles parsing and transforming Claude API requests into the Kiro/Amazon Q API format,
// extracting model information, system instructions, message contents, and tool declarations.
package claude

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	kirocommon "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/common"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type (
	KiroPayload                      = kirocommon.KiroPayload
	KiroConversationState            = kirocommon.KiroConversationState
	KiroCurrentMessage               = kirocommon.KiroCurrentMessage
	KiroHistoryMessage               = kirocommon.KiroHistoryMessage
	KiroImage                        = kirocommon.KiroImage
	KiroImageSource                  = kirocommon.KiroImageSource
	KiroUserInputMessage             = kirocommon.KiroUserInputMessage
	KiroUserInputMessageContext      = kirocommon.KiroUserInputMessageContext
	KiroToolResult                   = kirocommon.KiroToolResult
	KiroTextContent                  = kirocommon.KiroTextContent
	KiroToolWrapper                  = kirocommon.KiroToolWrapper
	KiroToolSpecification            = kirocommon.KiroToolSpecification
	KiroInputSchema                  = kirocommon.KiroInputSchema
	KiroAssistantResponseMessage     = kirocommon.KiroAssistantResponseMessage
	KiroToolUse                      = kirocommon.KiroToolUse
	KiroAdditionalModelRequestFields = kirocommon.KiroAdditionalModelRequestFields
	KiroThinkingConfig               = kirocommon.KiroThinkingConfig
	KiroOutputConfig                 = kirocommon.KiroOutputConfig
)

// ConvertClaudeRequestToKiro converts a Claude API request to Kiro format.
// This is the main entry point for request translation.
func ConvertClaudeRequestToKiro(modelName string, inputRawJSON []byte, stream bool) []byte {
	// For Kiro, we pass through the Claude format since buildKiroPayload
	// expects Claude format and does the conversion internally.
	// The actual conversion happens in the executor when building the HTTP request.
	return inputRawJSON
}

// BuildKiroPayload constructs the Kiro API request payload from Claude format.
// Supports tool calling - tools are passed via userInputMessageContext.
// origin parameter determines which quota to use: "CLI" for Amazon Q, "AI_EDITOR" for Kiro IDE.
// Returns the serialized Kiro API request payload.
func BuildKiroPayload(claudeBody []byte, modelID, profileArn, origin string) []byte {
	log.Debugf("kiro: BuildKiroPayload called, modelID=%s, origin=%s", modelID, origin)

	// Normalize origin value for Kiro API compatibility
	origin = kirocommon.NormalizeOrigin(origin)
	log.Debugf("kiro: normalized origin value: %s", origin)

	messages := gjson.GetBytes(claudeBody, "messages")

	tools := gjson.GetBytes(claudeBody, "tools")

	// Extract system prompt
	systemPrompt := extractSystemPrompt(claudeBody)

	// Convert Claude tools to Kiro format
	kiroTools := convertClaudeToolsToKiro(tools)
	log.Infof("kiro: tools conversion: input_exist=%v, output_count=%d", tools.IsArray(), len(kiroTools))
	for i, t := range kiroTools {
		log.Debugf("kiro: tool[%d]: name=%s", i, t.ToolSpecification.Name)
	}

	// Process messages and build history
	history, currentUserMsg, currentToolResults := processMessages(messages, modelID, origin)

	// ALWAYS flatten tool_use/tool_result in history to plain text.
	// Bedrock requires toolConfig for structured tool content, but the Kiro
	// API forwarding layer doesn't support passing it through. Flattening
	// avoids the 400 while preserving tool call/result content as readable text.
	history = kirocommon.FlattenToolHistory(history)
	if currentUserMsg != nil && len(currentToolResults) > 0 {
		for _, tr := range currentToolResults {
			text := toolResultToText(tr.Content)
			if text != "" {
				if currentUserMsg.Content != "" {
					currentUserMsg.Content += "\n\n" + text
				} else {
					currentUserMsg.Content = text
				}
			}
		}
		currentToolResults = nil
	}

	// Build content with system prompt.
	if currentUserMsg != nil {
		currentUserMsg.Content = buildFinalContent(currentUserMsg.Content, systemPrompt, currentToolResults)

		// Keep tools in userInputMessageContext so Kiro knows what's available,
		// but no structured tool_use/tool_result remains in history.
		if len(kiroTools) > 0 {
			currentUserMsg.UserInputMessageContext = &KiroUserInputMessageContext{
				Tools: kiroTools,
			}
		}
	}

	// Build payload
	var currentMessage KiroCurrentMessage
	if currentUserMsg != nil {
		currentMessage = KiroCurrentMessage{UserInputMessage: *currentUserMsg}
	} else {
		fallbackContent := ""
		if systemPrompt != "" {
			fallbackContent = systemPrompt
		} else {
			log.Debugf("kiro: no system prompt present in fallback user message")
		}
		currentMessage = KiroCurrentMessage{UserInputMessage: KiroUserInputMessage{
			Content: fallbackContent,
			ModelID: modelID,
			Origin:  origin,
		}}
	}

	// Session IDs: extract from messages[].additional_kwargs (LangChain format) or random
	conversationID := kirocommon.ExtractMetadataFromMessages(messages, "conversationId")
	continuationID := kirocommon.ExtractMetadataFromMessages(messages, "continuationId")
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	// Extract thinking config only from the request body
	config := thinking.ExtractThinkingConfigPublic(claudeBody, "claude")

	// Build additionalModelRequestFields from thinking config
	additionalFields := kirocommon.BuildKiroThinkingConfig(config, modelID)

	payload := KiroPayload{
		ConversationState: KiroConversationState{
			AgentTaskType:   "vibe",
			ChatTriggerType: "MANUAL",
			ConversationID:  conversationID,
			CurrentMessage:  currentMessage,
			History:         history,
		},
		ProfileArn:                   profileArn,
		AgentMode:                    "vibe",
		AdditionalModelRequestFields: additionalFields,
	}

	// Only set AgentContinuationID if client provided
	if continuationID != "" {
		payload.ConversationState.AgentContinuationID = continuationID
	}

	result, err := json.Marshal(payload)
	if err != nil {
		log.Debugf("kiro: failed to marshal payload: %v", err)
		return nil
	}

	return result
}

// extractSystemPrompt extracts system prompt from Claude request
func extractSystemPrompt(claudeBody []byte) string {
	systemField := gjson.GetBytes(claudeBody, "system")
	if systemField.IsArray() {
		var sb strings.Builder
		for _, block := range systemField.Array() {
			if block.Get("type").String() == "text" {
				sb.WriteString(block.Get("text").String())
			} else if block.Type == gjson.String {
				sb.WriteString(block.String())
			}
		}
		return sb.String()
	}
	return systemField.String()
}

// convertClaudeToolsToKiro converts Claude tools to Kiro format
func convertClaudeToolsToKiro(tools gjson.Result) []KiroToolWrapper {
	var kiroTools []KiroToolWrapper
	if !tools.IsArray() {
		return kiroTools
	}

	for _, tool := range tools.Array() {
		name := tool.Get("name").String()
		description := tool.Get("description").String()
		inputSchemaResult := tool.Get("input_schema")
		var inputSchema interface{}
		if inputSchemaResult.Exists() && inputSchemaResult.Type != gjson.Null {
			inputSchema = inputSchemaResult.Value()
		}
		inputSchema = kirocommon.EnsureKiroInputSchema(inputSchema)

		// Shorten tool name if it exceeds 64 characters (common with MCP tools)
		originalName := name
		name = kirocommon.ShortenToolNameIfNeeded(name)
		if name != originalName {
			log.Debugf("kiro: shortened tool name from '%s' to '%s'", originalName, name)
		}

		// CRITICAL FIX: Kiro API requires non-empty description
		if strings.TrimSpace(description) == "" {
			description = fmt.Sprintf("Tool: %s", name)
			log.Debugf("kiro: tool '%s' has empty description, using default: %s", name, description)
		}

		// Update web_search tool description with the live MCP description
		description = kirocommon.UpdateWebSearchToolDescription(name, description)

		// Truncate long descriptions (individual tool limit)
		if len(description) > kirocommon.KiroMaxToolDescLen {
			truncLen := kirocommon.KiroMaxToolDescLen - 30
			for truncLen > 0 && !utf8.RuneStart(description[truncLen]) {
				truncLen--
			}
			description = description[:truncLen] + "... (description truncated)"
		}

		kiroTools = append(kiroTools, KiroToolWrapper{
			ToolSpecification: KiroToolSpecification{
				Name:        name,
				Description: description,
				InputSchema: KiroInputSchema{JSON: inputSchema},
			},
		})
	}

	return kiroTools
}

// processMessages processes Claude messages and builds Kiro history
func processMessages(messages gjson.Result, modelID, origin string) ([]KiroHistoryMessage, *KiroUserInputMessage, []KiroToolResult) {
	var history []KiroHistoryMessage
	var currentUserMsg *KiroUserInputMessage
	var currentToolResults []KiroToolResult

	// Merge adjacent messages with the same role
	messagesArray := kirocommon.MergeAdjacentMessages(messages.Array())

	// FIX: Kiro API requires history to start with a user message.
	// Some clients (e.g., OpenClaw) send conversations starting with an assistant message,
	// which is valid for the Claude API but causes "Improperly formed request" on Kiro.
	// Prepend a placeholder user message so the history alternation is correct.
	if len(messagesArray) > 0 && messagesArray[0].Get("role").String() == "assistant" {
		placeholder := `{"role":"user","content":"[start]"}`
		messagesArray = append([]gjson.Result{gjson.Parse(placeholder)}, messagesArray...)
		log.Infof("kiro: messages started with assistant role, prepended placeholder user message for Kiro API compatibility")
	}

	for i, msg := range messagesArray {
		role := msg.Get("role").String()
		isLastMessage := i == len(messagesArray)-1

		if role == "user" {
			userMsg, toolResults := BuildUserMessageStruct(msg, modelID, origin)
			if isLastMessage {
				currentUserMsg = &userMsg
				currentToolResults = toolResults
			} else {
				// For history messages, embed tool results in context
				if len(toolResults) > 0 {
					userMsg.UserInputMessageContext = &KiroUserInputMessageContext{
						ToolResults: toolResults,
					}
				}
				history = append(history, KiroHistoryMessage{
					UserInputMessage: &userMsg,
				})
			}
		} else if role == "assistant" {
			assistantMsg := BuildAssistantMessageStruct(msg)
			if isLastMessage {
				history = append(history, KiroHistoryMessage{
					AssistantResponseMessage: &assistantMsg,
				})
				// Create a continuation user message as currentMessage
				currentUserMsg = &KiroUserInputMessage{
					Content: "",
					ModelID: modelID,
					Origin:  origin,
				}
			} else {
				history = append(history, KiroHistoryMessage{
					AssistantResponseMessage: &assistantMsg,
				})
			}
		}
	}

	// POST-PROCESSING step 1: Remove orphaned tool_results that have no matching tool_use
	// in any assistant message. This happens when Claude Code compaction truncates
	// the conversation and removes the assistant message containing the tool_use,
	// but keeps the user message with the corresponding tool_result.
	// Without this fix, Kiro API returns "Improperly formed request".
	validToolUseIDs := make(map[string]bool)
	for _, h := range history {
		if h.AssistantResponseMessage != nil {
			for _, tu := range h.AssistantResponseMessage.ToolUses {
				validToolUseIDs[tu.ToolUseID] = true
			}
		}
	}

	// Filter orphaned tool results from history user messages
	for i := range history {
		if history[i].UserInputMessage != nil && history[i].UserInputMessage.UserInputMessageContext != nil {
			ctx := history[i].UserInputMessage.UserInputMessageContext
			if len(ctx.ToolResults) > 0 {
				filtered := make([]KiroToolResult, 0, len(ctx.ToolResults))
				for _, tr := range ctx.ToolResults {
					if validToolUseIDs[tr.ToolUseID] {
						filtered = append(filtered, tr)
					} else {
						log.Debugf("kiro: dropping orphaned tool_result in history[%d]: toolUseId=%s (no matching tool_use)", i, tr.ToolUseID)
					}
				}
				ctx.ToolResults = filtered
				if len(ctx.ToolResults) == 0 && len(ctx.Tools) == 0 {
					// Use index to modify the actual slice element, not a range copy
					history[i].UserInputMessage.UserInputMessageContext = nil
				}
			}
		}
	}

	// Filter orphaned tool results from current message
	if len(currentToolResults) > 0 {
		filtered := make([]KiroToolResult, 0, len(currentToolResults))
		for _, tr := range currentToolResults {
			if validToolUseIDs[tr.ToolUseID] {
				filtered = append(filtered, tr)
			} else {
				log.Debugf("kiro: dropping orphaned tool_result in currentMessage: toolUseId=%s (no matching tool_use)", tr.ToolUseID)
			}
		}
		if len(filtered) != len(currentToolResults) {
			log.Infof("kiro: dropped %d orphaned tool_result(s) from currentMessage (compaction artifact)", len(currentToolResults)-len(filtered))
		}
		currentToolResults = filtered
	}

	return history, currentUserMsg, currentToolResults
}

// buildFinalContent builds the final content with system prompt
func buildFinalContent(content, systemPrompt string, toolResults []KiroToolResult) string {
	var contentBuilder strings.Builder

	if systemPrompt != "" {
		contentBuilder.WriteString(systemPrompt)
		contentBuilder.WriteString("\n\n")
	}

	contentBuilder.WriteString(content)
	finalContent := contentBuilder.String()

	return finalContent
}

// BuildUserMessageStruct builds a user message and extracts tool results
func BuildUserMessageStruct(msg gjson.Result, modelID, origin string) (KiroUserInputMessage, []KiroToolResult) {
	content := msg.Get("content")
	var contentBuilder strings.Builder
	var toolResults []KiroToolResult
	var images []KiroImage

	// Track seen toolUseIds to deduplicate
	seenToolUseIDs := make(map[string]bool)

	if content.IsArray() {
		for _, part := range content.Array() {
			partType := part.Get("type").String()
			switch partType {
			case "text":
				contentBuilder.WriteString(part.Get("text").String())
			case "image":
				mediaType := part.Get("source.media_type").String()
				data := part.Get("source.data").String()

				format := ""
				if idx := strings.LastIndex(mediaType, "/"); idx != -1 {
					format = mediaType[idx+1:]
				}

				if format != "" && data != "" {
					images = append(images, KiroImage{
						Format: format,
						Source: KiroImageSource{
							Bytes: data,
						},
					})
				}
			case "tool_result":
				toolUseID := part.Get("tool_use_id").String()

				// Skip duplicate toolUseIds
				if seenToolUseIDs[toolUseID] {
					log.Debugf("kiro: skipping duplicate tool_result with toolUseId: %s", toolUseID)
					continue
				}
				seenToolUseIDs[toolUseID] = true

				isError := part.Get("is_error").Bool()
				resultContent := part.Get("content")

				var textContents []KiroTextContent

				if resultContent.IsArray() {
					for _, item := range resultContent.Array() {
						if item.Get("type").String() == "text" {
							textContents = append(textContents, KiroTextContent{Text: item.Get("text").String()})
						} else if item.Type == gjson.String {
							textContents = append(textContents, KiroTextContent{Text: item.String()})
						}
					}
				} else if resultContent.Type == gjson.String {
					textContents = append(textContents, KiroTextContent{Text: resultContent.String()})
				}

				if len(textContents) == 0 {
					textContents = append(textContents, KiroTextContent{Text: "Tool use was cancelled by the user"})
				}

				status := "success"
				if isError {
					status = "error"
				}

				toolResults = append(toolResults, KiroToolResult{
					ToolUseID: toolUseID,
					Content:   textContents,
					Status:    status,
				})
			}
		}
	} else {
		contentBuilder.WriteString(content.String())
	}

	userMsg := KiroUserInputMessage{
		Content: contentBuilder.String(),
		ModelID: modelID,
		Origin:  origin,
	}

	if len(images) > 0 {
		userMsg.Images = images
	}

	return userMsg, toolResults
}

// BuildAssistantMessageStruct builds an assistant message with tool uses
func BuildAssistantMessageStruct(msg gjson.Result) KiroAssistantResponseMessage {
	content := msg.Get("content")
	var contentBuilder strings.Builder
	var toolUses []KiroToolUse

	if content.IsArray() {
		for _, part := range content.Array() {
			partType := part.Get("type").String()
			switch partType {
			case "text":
				contentBuilder.WriteString(part.Get("text").String())
			case "tool_use":
				toolUseID := part.Get("id").String()
				toolName := part.Get("name").String()
				toolInput := part.Get("input")

				var inputMap map[string]interface{}
				if toolInput.IsObject() {
					inputMap = make(map[string]interface{})
					toolInput.ForEach(func(key, value gjson.Result) bool {
						inputMap[key.String()] = value.Value()
						return true
					})
				}

				toolUses = append(toolUses, KiroToolUse{
					ToolUseID: toolUseID,
					Name:      toolName,
					Input:     inputMap,
				})
			default:
				log.Debugf("kiro: dropping unsupported assistant content block type: %s", partType)
			}
		}
	} else {
		contentBuilder.WriteString(content.String())
	}

	finalContent := contentBuilder.String()

	return KiroAssistantResponseMessage{
		Content:  finalContent,
		ToolUses: toolUses,
	}
}

// toolResultToText converts KiroTextContent array to a readable text line.
func toolResultToText(content []KiroTextContent) string {
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
