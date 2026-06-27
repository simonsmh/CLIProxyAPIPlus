package helps

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestIsNonThinkingKiroModel(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		expected bool
	}{
		// Haiku models
		{"claude-haiku", "claude-haiku", true},
		{"claude-3-haiku", "claude-3-haiku", true},
		{"Claude-Haiku uppercase", "Claude-Haiku-3.5", true},

		// GLM flash models
		{"glm-4-flash", "glm-4-flash", true},
		{"GLM-Flash", "GLM-Flash", true},
		{"glm flash without dash", "glm flash", true},

		// MiniMax models
		{"minimax-m2", "minimax-m2", true},
		{"MiniMax", "MiniMax-M1", true},

		// Models that should NOT match
		{"claude-sonnet", "claude-sonnet-4.5", false},
		{"claude-opus", "claude-opus-4.7", false},
		{"glm-5 (no flash)", "glm-5", false},
		{"qwen3-coder", "qwen3-coder-next", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNonThinkingKiroModel(tt.modelID); got != tt.expected {
				t.Errorf("IsNonThinkingKiroModel(%q) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestStripAdditionalFields(t *testing.T) {
	t.Run("removes additionalModelRequestFields", func(t *testing.T) {
		payload := []byte(`{"model":"test","additionalModelRequestFields":{"thinking":{"type":"adaptive"}},"other":"value"}`)
		result := StripAdditionalFields(payload)

		var data map[string]interface{}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if _, exists := data["additionalModelRequestFields"]; exists {
			t.Error("additionalModelRequestFields should have been removed")
		}
		if data["model"] != "test" {
			t.Errorf("model = %v, want test", data["model"])
		}
		if data["other"] != "value" {
			t.Errorf("other = %v, want value", data["other"])
		}
	})

	t.Run("no additionalModelRequestFields to remove", func(t *testing.T) {
		payload := []byte(`{"model":"test","other":"value"}`)
		result := StripAdditionalFields(payload)

		var data map[string]interface{}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if data["model"] != "test" {
			t.Errorf("model = %v, want test", data["model"])
		}
	})

	t.Run("invalid JSON returns original", func(t *testing.T) {
		payload := []byte(`{invalid json`)
		result := StripAdditionalFields(payload)
		if string(result) != string(payload) {
			t.Errorf("expected original payload back for invalid JSON")
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		payload := []byte(`{}`)
		result := StripAdditionalFields(payload)

		var data map[string]interface{}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if len(data) != 0 {
			t.Errorf("expected empty map, got %v", data)
		}
	})
}

func TestApplyKiroTokenUsage(t *testing.T) {
	t.Run("all fields populated", func(t *testing.T) {
		detail := &usage.Detail{}
		ok := ApplyKiroTokenUsage(detail, map[string]interface{}{
			"outputTokens":          float64(100),
			"totalTokens":           float64(500),
			"uncachedInputTokens":   float64(200),
			"cacheReadInputTokens":  float64(150),
			"cacheWriteInputTokens": float64(50),
		})
		if !ok {
			t.Fatal("ApplyKiroTokenUsage() = false, want true")
		}
		if detail.OutputTokens != 100 {
			t.Errorf("OutputTokens = %d, want 100", detail.OutputTokens)
		}
		if detail.TotalTokens != 500 {
			t.Errorf("TotalTokens = %d, want 500", detail.TotalTokens)
		}
		if detail.InputTokens != 400 {
			t.Errorf("InputTokens = %d, want 400 (uncached 200 + cacheRead 150 + cacheWrite 50)", detail.InputTokens)
		}
		if detail.CacheReadTokens != 150 {
			t.Errorf("CacheReadTokens = %d, want 150", detail.CacheReadTokens)
		}
		if detail.CacheCreationTokens != 50 {
			t.Errorf("CacheCreationTokens = %d, want 50", detail.CacheCreationTokens)
		}
		if detail.CachedTokens != 150 {
			t.Errorf("CachedTokens = %d, want 150 (first seen is cacheRead)", detail.CachedTokens)
		}
	})

	t.Run("nil detail returns false", func(t *testing.T) {
		if ApplyKiroTokenUsage(nil, map[string]interface{}{}) {
			t.Error("expected false for nil detail")
		}
	})

	t.Run("nil tokenUsage returns false", func(t *testing.T) {
		if ApplyKiroTokenUsage(&usage.Detail{}, nil) {
			t.Error("expected false for nil tokenUsage")
		}
	})

	t.Run("int types handled", func(t *testing.T) {
		detail := &usage.Detail{}
		ok := ApplyKiroTokenUsage(detail, map[string]interface{}{
			"outputTokens": int(42),
			"totalTokens":  int64(100),
		})
		if !ok {
			t.Fatal("ApplyKiroTokenUsage() = false, want true")
		}
		if detail.OutputTokens != 42 {
			t.Errorf("OutputTokens = %d, want 42", detail.OutputTokens)
		}
		if detail.TotalTokens != 100 {
			t.Errorf("TotalTokens = %d, want 100", detail.TotalTokens)
		}
	})
}

func TestFinalizeKiroUsageTotal(t *testing.T) {
	t.Run("computes total from components", func(t *testing.T) {
		detail := &usage.Detail{
			InputTokens:         100,
			OutputTokens:        50,
			ReasoningTokens:     25,
			CacheReadTokens:     200,
			CacheCreationTokens: 10,
		}
		FinalizeKiroUsageTotal(detail)
		expected := int64(385)
		if detail.TotalTokens != expected {
			t.Errorf("TotalTokens = %d, want %d", detail.TotalTokens, expected)
		}
	})

	t.Run("does not overwrite existing total", func(t *testing.T) {
		detail := &usage.Detail{
			TotalTokens:  999,
			InputTokens:  100,
			OutputTokens: 50,
		}
		FinalizeKiroUsageTotal(detail)
		if detail.TotalTokens != 999 {
			t.Errorf("TotalTokens = %d, want 999 (should not overwrite)", detail.TotalTokens)
		}
	})

	t.Run("nil detail is safe", func(t *testing.T) {
		FinalizeKiroUsageTotal(nil) // should not panic
	})
}

func TestKiroTokenUsageInt64(t *testing.T) {
	tests := []struct {
		name       string
		tokenUsage map[string]interface{}
		key        string
		wantVal    int64
		wantOk     bool
	}{
		{"float64", map[string]interface{}{"k": float64(42)}, "k", 42, true},
		{"int", map[string]interface{}{"k": int(10)}, "k", 10, true},
		{"int64", map[string]interface{}{"k": int64(99)}, "k", 99, true},
		{"json.Number", map[string]interface{}{"k": json.Number("77")}, "k", 77, true},
		{"missing key", map[string]interface{}{"other": float64(1)}, "k", 0, false},
		{"nil value", map[string]interface{}{"k": nil}, "k", 0, false},
		{"string value", map[string]interface{}{"k": "not a number"}, "k", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := KiroTokenUsageInt64(tt.tokenUsage, tt.key)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if val != tt.wantVal {
				t.Errorf("val = %d, want %d", val, tt.wantVal)
			}
		})
	}
}
