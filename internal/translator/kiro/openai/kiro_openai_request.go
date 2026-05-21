// Package openai provides request translation from OpenAI Chat Completions to Kiro format.
// It handles parsing and transforming OpenAI API requests into the Kiro/Amazon Q API format,
// extracting model information, system instructions, message contents, and tool declarations.
package openai

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
	KiroPayload                  = kirocommon.KiroPayload
	KiroConversationState        = kirocommon.KiroConversationState
	KiroCurrentMessage           = kirocommon.KiroCurrentMessage
	KiroHistoryMessage           = kirocommon.KiroHistoryMessage
	KiroImage                    = kirocommon.KiroImage
	KiroImageSource              = kirocommon.KiroImageSource
	KiroUserInputMessage         = kirocommon.KiroUserInputMessage
	KiroUserInputMessageContext  = kirocommon.KiroUserInputMessageContext
	KiroToolResult               = kirocommon.KiroToolResult
	KiroTextContent              = kirocommon.KiroTextContent
	KiroToolWrapper              = kirocommon.KiroToolWrapper
	KiroToolSpecification        = kirocommon.KiroToolSpecification
	KiroInputSchema              = kirocommon.KiroInputSchema
	KiroAssistantResponseMessage = kirocommon.KiroAssistantResponseMessage
	KiroToolUse                  = kirocommon.KiroToolUse
)

// ConvertOpenAIRequestToKiro converts an OpenAI Chat Completions request to Kiro format.
// This is the main entry point for request translation.
// Note: The actual payload building happens in the executor, this just passes through
// the OpenAI format which will be converted by BuildKiroPayloadFromOpenAI.
func ConvertOpenAIRequestToKiro(modelName string, inputRawJSON []byte, stream bool) []byte {
	// Pass through the OpenAI format - actual conversion happens in BuildKiroPayloadFromOpenAI
	return inputRawJSON
}

// BuildKiroPayloadFromOpenAI constructs the Kiro API request payload from OpenAI format.
// Supports tool calling - tools are passed via userInputMessageContext.
// origin parameter determines which quota to use: "CLI" for Amazon Q, "AI_EDITOR" for Kiro IDE.
// Returns the serialized Kiro API request payload.
func BuildKiroPayloadFromOpenAI(openaiBody []byte, modelID, profileArn, origin string) []byte {
	log.Debugf("kiro-openai: BuildKiroPayloadFromOpenAI called, modelID=%s, origin=%s", modelID, origin)

	// Normalize origin value for Kiro API compatibility
	origin = kirocommon.NormalizeOrigin(origin)
	log.Debugf("kiro-openai: normalized origin value: %s", origin)

	messages := gjson.GetBytes(openaiBody, "messages")

	tools := gjson.GetBytes(openaiBody, "tools")

	// Extract system prompt from messages
	systemPrompt := extractSystemPromptFromOpenAI(messages)

	// Convert OpenAI tools to Kiro format
	kiroTools := convertOpenAIToolsToKiro(tools)
	log.Infof("kiro-openai: tools conversion: input_exist=%v, output_count=%d", tools.IsArray(), len(kiroTools))
	for i, t := range kiroTools {
		log.Debugf("kiro-openai: tool[%d]: name=%s", i, t.ToolSpecification.Name)
	}

	// Process messages and build history
	history, currentUserMsg, currentToolResults := processOpenAIMessages(messages, modelID, origin)

	// Build content with system prompt
	if currentUserMsg != nil {
		currentUserMsg.Content = buildFinalContent(currentUserMsg.Content, systemPrompt, currentToolResults)

		// Deduplicate currentToolResults
		currentToolResults = kirocommon.DeduplicateToolResults(currentToolResults)

		// Build userInputMessageContext with tools and tool results.
		// See claude translator for the rationale — Kiro rejects requests when
		// history contains tool turns but currentMessage.tools is empty. Fall
		// back to stub specs derived from history if the client omitted tools.
		if len(kiroTools) == 0 {
			kiroTools = kirocommon.SynthesizeToolSpecsFromHistory(history)
			if len(kiroTools) > 0 {
				log.Infof("kiro-openai: synthesized %d stub tool spec(s) from history (client did not send tools)", len(kiroTools))
			}
		}
		if len(kiroTools) > 0 || len(currentToolResults) > 0 {
			currentUserMsg.UserInputMessageContext = &KiroUserInputMessageContext{
				Tools:       kiroTools,
				ToolResults: currentToolResults,
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
			log.Debugf("kiro-openai: no system prompt present in fallback user message")
		}
		// CRITICAL: Kiro API requires non-empty content for currentMessage.
		// Use DefaultUserContent to avoid "Improperly formed request" 400 error.
		if strings.TrimSpace(fallbackContent) == "" {
			fallbackContent = kirocommon.DefaultUserContent
			log.Debugf("kiro-openai: fallback user message content was empty, using default: %s", fallbackContent)
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

	payload := KiroPayload{
		ConversationState: KiroConversationState{
			AgentTaskType:   "vibe",
			ChatTriggerType: "MANUAL",
			ConversationID:  conversationID,
			CurrentMessage:  currentMessage,
			History:         history,
		},
		ProfileArn: profileArn,
	}

	// Only set AgentContinuationID if client provided
	if continuationID != "" {
		payload.ConversationState.AgentContinuationID = continuationID
	}

	result, err := json.Marshal(payload)
	if err != nil {
		log.Debugf("kiro-openai: failed to marshal payload: %v", err)
		return nil
	}

	return result
}

// extractSystemPromptFromOpenAI extracts system prompt from OpenAI messages
func extractSystemPromptFromOpenAI(messages gjson.Result) string {
	if !messages.IsArray() {
		return ""
	}

	var systemParts []string
	for _, msg := range messages.Array() {
		if msg.Get("role").String() == "system" {
			content := msg.Get("content")
			if content.Type == gjson.String {
				systemParts = append(systemParts, content.String())
			} else if content.IsArray() {
				// Handle array content format
				for _, part := range content.Array() {
					if part.Get("type").String() == "text" {
						systemParts = append(systemParts, part.Get("text").String())
					}
				}
			}
		}
	}

	return strings.Join(systemParts, "\n")
}


// convertOpenAIToolsToKiro converts OpenAI tools to Kiro format
func convertOpenAIToolsToKiro(tools gjson.Result) []KiroToolWrapper {
	var kiroTools []KiroToolWrapper
	if !tools.IsArray() {
		return kiroTools
	}

	for _, tool := range tools.Array() {
		// Support two tool formats:
		// 1. Standard OpenAI: {"type":"function","function":{"name":"...","description":"...","parameters":{...}}}
		// 2. Flat format (Claude Code/Kiro IDE): {"name":"...","description":"...","parameters":{...}} or with "input_schema"
		toolType := tool.Get("type").String()
		var name, description string
		var parametersResult gjson.Result

		if toolType == "function" {
			fn := tool.Get("function")
			if !fn.Exists() {
				log.Debugf("kiro-openai: skipping function tool with no function field")
				continue
			}
			name = fn.Get("name").String()
			description = fn.Get("description").String()
			parametersResult = fn.Get("parameters")
		} else if tool.Get("name").Exists() {
			// Flat format: tool definition at top level (Claude Code / Kiro IDE)
			name = tool.Get("name").String()
			description = tool.Get("description").String()
			parametersResult = tool.Get("parameters")
			if !parametersResult.Exists() {
				parametersResult = tool.Get("input_schema")
			}
			log.Debugf("kiro-openai: using flat tool format for tool: %s", name)
		} else {
			rawSnippet := tool.Raw
			if len(rawSnippet) > 200 {
				rawSnippet = rawSnippet[:200]
			}
			log.Infof("kiro-openai: skipping unrecognized tool format, raw=%s", rawSnippet)
			continue
		}

		var parameters interface{}
		if parametersResult.Exists() && parametersResult.Type != gjson.Null {
			parameters = parametersResult.Value()
		}
		parameters = kirocommon.EnsureKiroInputSchema(parameters)

		// Shorten tool name if it exceeds 64 characters (common with MCP tools)
		originalName := name
		name = kirocommon.ShortenToolNameIfNeeded(name)
		if name != originalName {
			log.Debugf("kiro-openai: shortened tool name from '%s' to '%s'", originalName, name)
		}

		// CRITICAL FIX: Kiro API requires non-empty description
		if strings.TrimSpace(description) == "" {
			description = fmt.Sprintf("Tool: %s", name)
			log.Debugf("kiro-openai: tool '%s' has empty description, using default: %s", name, description)
		}

		// Rewrite web_search to the name Q's chat endpoint accepts
		// (and use the live MCP description if we have one).
		if newName, newDesc := kirocommon.RenameWebSearchTool(name, description); newName != name {
			name, description = newName, newDesc
			log.Debugf("kiro-openai: renamed tool web_search → %s", name)
		}

		// Truncate long descriptions
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
				InputSchema: KiroInputSchema{JSON: parameters},
			},
		})
	}

	return kiroTools
}

// processOpenAIMessages processes OpenAI messages and builds Kiro history
func processOpenAIMessages(messages gjson.Result, modelID, origin string) ([]KiroHistoryMessage, *KiroUserInputMessage, []KiroToolResult) {
	var history []KiroHistoryMessage
	var currentUserMsg *KiroUserInputMessage
	var currentToolResults []KiroToolResult

	if !messages.IsArray() {
		return history, currentUserMsg, currentToolResults
	}

	// Merge adjacent messages with the same role
	messagesArray := kirocommon.MergeAdjacentMessages(messages.Array())

	// Track pending tool results that should be attached to the next user message
	// This is critical for LiteLLM-translated requests where tool results appear
	// as separate "tool" role messages between assistant and user messages
	var pendingToolResults []KiroToolResult

	for i, msg := range messagesArray {
		role := msg.Get("role").String()
		isLastMessage := i == len(messagesArray)-1

		switch role {
		case "system":
			// System messages are handled separately via extractSystemPromptFromOpenAI
			continue

		case "user":
			userMsg, toolResults := buildUserMessageFromOpenAI(msg, modelID, origin)
			// Merge any pending tool results from preceding "tool" role messages
			toolResults = append(pendingToolResults, toolResults...)
			pendingToolResults = nil // Reset pending tool results

			if isLastMessage {
				currentUserMsg = &userMsg
				currentToolResults = toolResults
			} else {
				// CRITICAL: Kiro API requires content to be non-empty for history messages
				if strings.TrimSpace(userMsg.Content) == "" {
					if len(toolResults) > 0 {
						userMsg.Content = kirocommon.DefaultUserContentWithToolResults
					} else {
						userMsg.Content = kirocommon.DefaultUserContent
					}
				}
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

		case "assistant":
			assistantMsg := buildAssistantMessageFromOpenAI(msg)

			// If there are pending tool results, we need to insert a synthetic user message
			// before this assistant message to maintain proper conversation structure
			if len(pendingToolResults) > 0 {
				syntheticUserMsg := KiroUserInputMessage{
					Content: kirocommon.DefaultUserContentWithToolResults,
					ModelID: modelID,
					Origin:  origin,
					UserInputMessageContext: &KiroUserInputMessageContext{
						ToolResults: pendingToolResults,
					},
				}
				history = append(history, KiroHistoryMessage{
					UserInputMessage: &syntheticUserMsg,
				})
				pendingToolResults = nil
			}

			if isLastMessage {
				history = append(history, KiroHistoryMessage{
					AssistantResponseMessage: &assistantMsg,
				})
				// Create a continuation user message as currentMessage
				currentUserMsg = &KiroUserInputMessage{
					Content: kirocommon.DefaultUserContent,
					ModelID: modelID,
					Origin:  origin,
				}
			} else {
				history = append(history, KiroHistoryMessage{
					AssistantResponseMessage: &assistantMsg,
				})
			}

		case "tool":
			// Tool messages in OpenAI format provide results for tool_calls
			// These are typically followed by user or assistant messages
			// Collect them as pending and attach to the next user message
			toolCallID := msg.Get("tool_call_id").String()
			content := msg.Get("content").String()

			if toolCallID != "" {
				toolResult := KiroToolResult{
					ToolUseID: toolCallID,
					Content:   []KiroTextContent{{Text: content}},
					Status:    "success",
				}
				// Collect pending tool results to attach to the next user message
				pendingToolResults = append(pendingToolResults, toolResult)
			}
		}
	}

	// Handle case where tool results are at the end with no following user message
	if len(pendingToolResults) > 0 {
		currentToolResults = append(currentToolResults, pendingToolResults...)
		// If there's no current user message, create a synthetic one for the tool results
		if currentUserMsg == nil {
			currentUserMsg = &KiroUserInputMessage{
				Content: kirocommon.DefaultUserContentWithToolResults,
				ModelID: modelID,
				Origin:  origin,
			}
		}
	}

	// Truncate history if too long to prevent Kiro API errors
	history = truncateHistoryIfNeeded(history)
	history, currentToolResults = filterOrphanedToolResults(history, currentToolResults)

	return history, currentUserMsg, currentToolResults
}

const kiroMaxHistoryMessages = 50

func truncateHistoryIfNeeded(history []KiroHistoryMessage) []KiroHistoryMessage {
	if len(history) <= kiroMaxHistoryMessages {
		return history
	}

	log.Debugf("kiro-openai: truncating history from %d to %d messages", len(history), kiroMaxHistoryMessages)
	return history[len(history)-kiroMaxHistoryMessages:]
}

func filterOrphanedToolResults(history []KiroHistoryMessage, currentToolResults []KiroToolResult) ([]KiroHistoryMessage, []KiroToolResult) {
	// Remove tool results with no matching tool_use in retained history.
	// This happens after truncation when the assistant turn that produced tool_use
	// is dropped but a later user/tool_result survives.
	validToolUseIDs := make(map[string]bool)
	for _, h := range history {
		if h.AssistantResponseMessage == nil {
			continue
		}
		for _, tu := range h.AssistantResponseMessage.ToolUses {
			validToolUseIDs[tu.ToolUseID] = true
		}
	}

	for i := range history {
		if history[i].UserInputMessage == nil || history[i].UserInputMessage.UserInputMessageContext == nil {
			continue
		}
		ctx := history[i].UserInputMessage.UserInputMessageContext
		if len(ctx.ToolResults) == 0 {
			continue
		}

		filtered := make([]KiroToolResult, 0, len(ctx.ToolResults))
		for _, tr := range ctx.ToolResults {
			if validToolUseIDs[tr.ToolUseID] {
				filtered = append(filtered, tr)
				continue
			}
			log.Debugf("kiro-openai: dropping orphaned tool_result in history[%d]: toolUseId=%s (no matching tool_use)", i, tr.ToolUseID)
		}
		ctx.ToolResults = filtered
		if len(ctx.ToolResults) == 0 && len(ctx.Tools) == 0 {
			// Use index to modify the actual slice element, not a range copy
			history[i].UserInputMessage.UserInputMessageContext = nil
		}
	}

	if len(currentToolResults) > 0 {
		filtered := make([]KiroToolResult, 0, len(currentToolResults))
		for _, tr := range currentToolResults {
			if validToolUseIDs[tr.ToolUseID] {
				filtered = append(filtered, tr)
				continue
			}
			log.Debugf("kiro-openai: dropping orphaned tool_result in currentMessage: toolUseId=%s (no matching tool_use)", tr.ToolUseID)
		}
		if len(filtered) != len(currentToolResults) {
			log.Infof("kiro-openai: dropped %d orphaned tool_result(s) from currentMessage", len(currentToolResults)-len(filtered))
		}
		currentToolResults = filtered
	}

	return history, currentToolResults
}

// buildUserMessageFromOpenAI builds a user message from OpenAI format and extracts tool results
func buildUserMessageFromOpenAI(msg gjson.Result, modelID, origin string) (KiroUserInputMessage, []KiroToolResult) {
	content := msg.Get("content")
	var contentBuilder strings.Builder
	var toolResults []KiroToolResult
	var images []KiroImage

	if content.IsArray() {
		for _, part := range content.Array() {
			partType := part.Get("type").String()
			switch partType {
			case "text":
				contentBuilder.WriteString(part.Get("text").String())
			case "tool_result":
				// Claude format: tool_result embedded in user message content array
				toolUseID := part.Get("tool_use_id").String()
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

				if toolUseID != "" {
					toolResults = append(toolResults, KiroToolResult{
						ToolUseID: toolUseID,
						Content:   textContents,
						Status:    status,
					})
					log.Debugf("kiro-openai: extracted tool_result from user content array: toolUseId=%s", toolUseID)
				}
			case "image":
				// Claude format: {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}
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
			case "image_url":
				imageURL := part.Get("image_url.url").String()
				if strings.HasPrefix(imageURL, "data:") {
					// Parse data URL: data:image/png;base64,xxxxx
					if idx := strings.Index(imageURL, ";base64,"); idx != -1 {
						mediaType := imageURL[5:idx] // Skip "data:"
						data := imageURL[idx+8:]     // Skip ";base64,"

						format := ""
						if lastSlash := strings.LastIndex(mediaType, "/"); lastSlash != -1 {
							format = mediaType[lastSlash+1:]
						}

						if format != "" && data != "" {
							images = append(images, KiroImage{
								Format: format,
								Source: KiroImageSource{
									Bytes: data,
								},
							})
						}
					}
				}
			}
		}
	} else if content.Type == gjson.String {
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

// buildAssistantMessageFromOpenAI builds an assistant message from OpenAI format
func buildAssistantMessageFromOpenAI(msg gjson.Result) KiroAssistantResponseMessage {
	content := msg.Get("content")
	var contentBuilder strings.Builder
	var toolUses []KiroToolUse

	// Handle content
	if content.Type == gjson.String {
		contentBuilder.WriteString(content.String())
	} else if content.IsArray() {
		for _, part := range content.Array() {
			partType := part.Get("type").String()
			switch partType {
			case "text":
				contentBuilder.WriteString(part.Get("text").String())
			case "thinking", "reasoning":
				// Replayed reasoning from a prior turn (Anthropic-style
				// thinking block, or OpenAI o1-style reasoning content).
				// Q has no separate reasoning channel for *history*, so
				// we fold the text into assistant content rather than
				// drop it on the floor.
				contentBuilder.WriteString(thinking.GetThinkingText(part))
			case "tool_use":
				// Handle tool_use in content array (Anthropic/OpenCode format)
				// This is different from OpenAI's tool_calls format
				toolUseID := part.Get("id").String()
				toolName := part.Get("name").String()
				inputData := part.Get("input")

				inputMap := make(map[string]interface{})
				if inputData.Exists() && inputData.IsObject() {
					inputData.ForEach(func(key, value gjson.Result) bool {
						inputMap[key.String()] = value.Value()
						return true
					})
				}

				toolUses = append(toolUses, KiroToolUse{
					ToolUseID: toolUseID,
					Name:      kirocommon.RenameWebSearchToolUse(toolName),
					Input:     inputMap,
				})
				log.Debugf("kiro-openai: extracted tool_use from content array: %s", toolName)
			default:
				log.Debugf("kiro-openai: dropping unsupported assistant content block type: %s", partType)
			}
		}
	}

	// Handle tool_calls (OpenAI format)
	toolCalls := msg.Get("tool_calls")
	if toolCalls.IsArray() {
		for _, tc := range toolCalls.Array() {
			if tc.Get("type").String() != "function" {
				continue
			}

			toolUseID := tc.Get("id").String()
			toolName := tc.Get("function.name").String()
			toolArgs := tc.Get("function.arguments").String()

			var inputMap map[string]interface{}
			if err := json.Unmarshal([]byte(toolArgs), &inputMap); err != nil {
				log.Debugf("kiro-openai: failed to parse tool arguments: %v", err)
				inputMap = make(map[string]interface{})
			}

			toolUses = append(toolUses, KiroToolUse{
				ToolUseID: toolUseID,
				Name:      kirocommon.RenameWebSearchToolUse(toolName),
				Input:     inputMap,
			})
		}
	}

	// CRITICAL FIX: Kiro API requires non-empty content for assistant messages
	// This can happen with compaction requests or error recovery scenarios
	finalContent := contentBuilder.String()
	if strings.TrimSpace(finalContent) == "" {
		if len(toolUses) > 0 {
			finalContent = kirocommon.DefaultAssistantContentWithTools
		} else {
			finalContent = kirocommon.DefaultAssistantContent
		}
		log.Debugf("kiro-openai: assistant content was empty, using default: %s", finalContent)
	}

	return KiroAssistantResponseMessage{
		Content:  finalContent,
		ToolUses: toolUses,
	}
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

	// CRITICAL: Kiro API requires content to be non-empty
	if strings.TrimSpace(finalContent) == "" {
		if len(toolResults) > 0 {
			finalContent = kirocommon.DefaultUserContentWithToolResults
		} else {
			finalContent = kirocommon.DefaultUserContent
		}
		log.Debugf("kiro-openai: content was empty, using default: %s", finalContent)
	}

	return finalContent
}



