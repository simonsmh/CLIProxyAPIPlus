package helps

import (
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

// IsNonThinkingKiroModel returns true if the model is known to NOT support
// additionalModelRequestFields (thinking/effort). These models return
// 400 "additionalModelRequestFields is not supported" if the field is present.
func IsNonThinkingKiroModel(modelID string) bool {
	normalized := strings.ToLower(modelID)
	// Haiku does not support thinking/effort
	if strings.Contains(normalized, "haiku") {
		return true
	}
	// GLM flash model does not support thinking
	if strings.Contains(normalized, "glm") && strings.Contains(normalized, "flash") {
		return true
	}
	// MiniMax does not support thinking
	if strings.Contains(normalized, "minimax") {
		return true
	}
	return false
}

// StripAdditionalFields removes the "additionalModelRequestFields" key from a JSON payload.
func StripAdditionalFields(payload []byte) []byte {
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return payload
	}
	delete(data, "additionalModelRequestFields")
	stripped, err := json.Marshal(data)
	if err != nil {
		return payload
	}
	return stripped
}

// ApplyKiroTokenUsage maps upstream tokenUsage fields into usage.Detail,
// preserving cache token breakdown (cacheRead vs cacheCreation).
func ApplyKiroTokenUsage(detail *usage.Detail, tokenUsage map[string]interface{}) bool {
	if detail == nil || tokenUsage == nil {
		return false
	}
	updated := false
	if outputTokens, ok := KiroTokenUsageInt64(tokenUsage, "outputTokens"); ok {
		detail.OutputTokens = outputTokens
		updated = true
	}
	if totalTokens, ok := KiroTokenUsageInt64(tokenUsage, "totalTokens"); ok {
		detail.TotalTokens = totalTokens
		updated = true
	}
	if uncachedInputTokens, ok := KiroTokenUsageInt64(tokenUsage, "uncachedInputTokens"); ok {
		detail.InputTokens = uncachedInputTokens
		updated = true
	}
	if cacheReadTokens, ok := KiroTokenUsageInt64(tokenUsage, "cacheReadInputTokens"); ok {
		detail.CacheReadTokens = cacheReadTokens
		detail.CachedTokens = cacheReadTokens
		updated = true
	}
	if cacheCreationTokens, ok := KiroTokenUsageInt64(tokenUsage, "cacheWriteInputTokens"); ok {
		detail.CacheCreationTokens = cacheCreationTokens
		if detail.CachedTokens == 0 {
			detail.CachedTokens = cacheCreationTokens
		}
		updated = true
	}
	return updated
}

// FinalizeKiroUsageTotal computes TotalTokens from components when upstream
// doesn't provide a total.
func FinalizeKiroUsageTotal(detail *usage.Detail) {
	if detail == nil || detail.TotalTokens != 0 {
		return
	}
	detail.TotalTokens = detail.InputTokens +
		detail.OutputTokens +
		detail.ReasoningTokens +
		detail.CacheReadTokens +
		detail.CacheCreationTokens
}

// ApplyKiroContextUsageFallback applies context-percentage-based input token
// estimation only when precise token usage is absent.
func ApplyKiroContextUsageFallback(detail *usage.Detail, contextUsagePercentage float64, hasPreciseTokenUsage bool) (int64, bool) {
	if detail == nil || contextUsagePercentage <= 0 || hasPreciseTokenUsage {
		return 0, false
	}
	calculatedInputTokens := int64(contextUsagePercentage * 200000 / 100)
	if calculatedInputTokens <= 0 {
		return 0, false
	}
	detail.InputTokens = calculatedInputTokens
	FinalizeKiroUsageTotal(detail)
	return calculatedInputTokens, true
}

// KiroTokenUsageInt64 extracts an int64 value from a tokenUsage map,
// handling int, int64, float64, json.Number, and other numeric types.
func KiroTokenUsageInt64(tokenUsage map[string]interface{}, key string) (int64, bool) {
	raw, ok := tokenUsage[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		return int64(value), true
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		return int64(value), true
	case float32:
		return int64(value), true
	case float64:
		return int64(value), true
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return n, true
		}
		if n, err := value.Float64(); err == nil {
			return int64(n), true
		}
	}
	return 0, false
}

// KiroTokenUsageFloat64 extracts a float64 value from a tokenUsage map,
// handling int, int64, float64, json.Number, and other numeric types.
func KiroTokenUsageFloat64(tokenUsage map[string]interface{}, key string) (float64, bool) {
	raw, ok := tokenUsage[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case json.Number:
		n, err := value.Float64()
		return n, err == nil
	}
	return 0, false
}
