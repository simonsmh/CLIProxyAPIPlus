package executor

import (
	"fmt"
	"testing"

	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestBuildKiroEndpointConfigs(t *testing.T) {
	tests := []struct {
		name           string
		region         string
		expectedURL    string
		expectedOrigin string
		expectedName   string
	}{
		{
			name:           "Empty region - defaults to us-east-1",
			region:         "",
			expectedURL:    "https://runtime.us-east-1.kiro.dev/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "KiroRuntime",
		},
		{
			name:           "us-east-1",
			region:         "us-east-1",
			expectedURL:    "https://runtime.us-east-1.kiro.dev/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "KiroRuntime",
		},
		{
			name:           "ap-southeast-1",
			region:         "ap-southeast-1",
			expectedURL:    "https://runtime.ap-southeast-1.kiro.dev/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "KiroRuntime",
		},
		{
			name:           "eu-west-1",
			region:         "eu-west-1",
			expectedURL:    "https://runtime.eu-west-1.kiro.dev/generateAssistantResponse",
			expectedOrigin: "AI_EDITOR",
			expectedName:   "KiroRuntime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := buildKiroEndpointConfigs(tt.region)

			if len(configs) != 3 {
				t.Fatalf("expected 3 endpoint configs, got %d", len(configs))
			}

			// Check primary endpoint (KiroRuntime)
			primary := configs[0]
			if primary.URL != tt.expectedURL {
				t.Errorf("primary URL = %q, want %q", primary.URL, tt.expectedURL)
			}
			if primary.Origin != tt.expectedOrigin {
				t.Errorf("primary Origin = %q, want %q", primary.Origin, tt.expectedOrigin)
			}
			if primary.Name != tt.expectedName {
				t.Errorf("primary Name = %q, want %q", primary.Name, tt.expectedName)
			}
			if primary.AmzTarget == "" {
				t.Errorf("primary AmzTarget should not be empty, got %q", primary.AmzTarget)
			}

			// Check fallback endpoint (AmazonQ)
			fallback1 := configs[1]
			if fallback1.Name != "AmazonQ" {
				t.Errorf("fallback1 Name = %q, want %q", fallback1.Name, "AmazonQ")
			}
			expectedRegion := tt.region
			if expectedRegion == "" {
				expectedRegion = kiroDefaultRegion
			}
			expectedQURL := fmt.Sprintf("https://q.%s.amazonaws.com/generateAssistantResponse", expectedRegion)
			if fallback1.URL != expectedQURL {
				t.Errorf("fallback1 URL = %q, want %q", fallback1.URL, expectedQURL)
			}

			// Check fallback endpoint (CodeWhisperer)
			fallback2 := configs[2]
			if fallback2.Name != "CodeWhisperer" {
				t.Errorf("fallback2 Name = %q, want %q", fallback2.Name, "CodeWhisperer")
			}
			expectedFallbackURL := fmt.Sprintf("https://codewhisperer.%s.amazonaws.com/generateAssistantResponse", expectedRegion)
			if fallback2.URL != expectedFallbackURL {
				t.Errorf("fallback2 URL = %q, want %q", fallback2.URL, expectedFallbackURL)
			}
			if fallback2.AmzTarget == "" {
				t.Error("fallback2 AmzTarget should NOT be empty")
			}
		})
	}
}

func TestGetKiroEndpointConfigs_NilAuth(t *testing.T) {
	configs := getKiroEndpointConfigs(nil)

	if len(configs) != 3 {
		t.Fatalf("expected 3 endpoint configs, got %d", len(configs))
	}

	// Should return default us-east-1 configs
	if configs[0].Name != "KiroRuntime" {
		t.Errorf("first config Name = %q, want %q", configs[0].Name, "KiroRuntime")
	}
	expectedURL := "https://runtime.us-east-1.kiro.dev/generateAssistantResponse"
	if configs[0].URL != expectedURL {
		t.Errorf("first config URL = %q, want %q", configs[0].URL, expectedURL)
	}
}

func TestGetKiroEndpointConfigs_WithRegionFromProfileArn(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata: map[string]any{
			"profile_arn": "arn:aws:codewhisperer:ap-southeast-1:123456789012:profile/ABC",
		},
	}

	configs := getKiroEndpointConfigs(auth)

	if len(configs) != 3 {
		t.Fatalf("expected 3 endpoint configs, got %d", len(configs))
	}

	expectedURL := "https://runtime.ap-southeast-1.kiro.dev/generateAssistantResponse"
	if configs[0].URL != expectedURL {
		t.Errorf("primary URL = %q, want %q", configs[0].URL, expectedURL)
	}
}

func TestGetKiroEndpointConfigs_WithApiRegionOverride(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata: map[string]any{
			"api_region":  "eu-central-1",
			"profile_arn": "arn:aws:codewhisperer:us-east-1:123456789012:profile/ABC",
		},
	}

	configs := getKiroEndpointConfigs(auth)

	// api_region should take precedence over profile_arn
	expectedURL := "https://runtime.eu-central-1.kiro.dev/generateAssistantResponse"
	if configs[0].URL != expectedURL {
		t.Errorf("primary URL = %q, want %q", configs[0].URL, expectedURL)
	}
}

func TestGetKiroEndpointConfigs_PreferredEndpoint(t *testing.T) {
	tests := []struct {
		name              string
		preference        string
		expectedFirstName string
	}{
		{
			name:              "Prefer codewhisperer",
			preference:        "codewhisperer",
			expectedFirstName: "CodeWhisperer",
		},
		{
			name:              "Prefer ide (alias for codewhisperer)",
			preference:        "ide",
			expectedFirstName: "CodeWhisperer",
		},
		{
			name:              "Prefer amazonq",
			preference:        "amazonq",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Prefer q (alias for amazonq)",
			preference:        "q",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Prefer cli (alias for amazonq)",
			preference:        "cli",
			expectedFirstName: "AmazonQ",
		},
		{
			name:              "Prefer kiroruntime",
			preference:        "kiroruntime",
			expectedFirstName: "KiroRuntime",
		},
		{
			name:              "Prefer runtime (alias for kiroruntime)",
			preference:        "runtime",
			expectedFirstName: "KiroRuntime",
		},
		{
			name:              "Prefer kiro (alias for kiroruntime)",
			preference:        "kiro",
			expectedFirstName: "KiroRuntime",
		},
		{
			name:              "Unknown preference - no reordering",
			preference:        "unknown",
			expectedFirstName: "KiroRuntime",
		},
		{
			name:              "Empty preference - no reordering",
			preference:        "",
			expectedFirstName: "KiroRuntime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &cliproxyauth.Auth{
				Metadata: map[string]any{
					"preferred_endpoint": tt.preference,
				},
			}

			configs := getKiroEndpointConfigs(auth)

			if configs[0].Name != tt.expectedFirstName {
				t.Errorf("first endpoint Name = %q, want %q", configs[0].Name, tt.expectedFirstName)
			}
		})
	}
}

func TestGetKiroEndpointConfigs_PreferredEndpointFromAttributes(t *testing.T) {
	// Test that preferred_endpoint can also come from Attributes
	auth := &cliproxyauth.Auth{
		Metadata:   map[string]any{},
		Attributes: map[string]string{"preferred_endpoint": "codewhisperer"},
	}

	configs := getKiroEndpointConfigs(auth)

	if configs[0].Name != "CodeWhisperer" {
		t.Errorf("first endpoint Name = %q, want %q", configs[0].Name, "CodeWhisperer")
	}
}

func TestGetKiroEndpointConfigs_MetadataTakesPrecedenceOverAttributes(t *testing.T) {
	auth := &cliproxyauth.Auth{
		Metadata:   map[string]any{"preferred_endpoint": "amazonq"},
		Attributes: map[string]string{"preferred_endpoint": "codewhisperer"},
	}

	configs := getKiroEndpointConfigs(auth)

	// Metadata should take precedence
	if configs[0].Name != "AmazonQ" {
		t.Errorf("first endpoint Name = %q, want %q", configs[0].Name, "AmazonQ")
	}
}

func TestGetAuthValue(t *testing.T) {
	tests := []struct {
		name     string
		auth     *cliproxyauth.Auth
		key      string
		expected string
	}{
		{
			name: "From metadata",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"test_key": "metadata_value"},
			},
			key:      "test_key",
			expected: "metadata_value",
		},
		{
			name: "From attributes (fallback)",
			auth: &cliproxyauth.Auth{
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
		{
			name: "Metadata takes precedence",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"test_key": "metadata_value"},
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "metadata_value",
		},
		{
			name: "Key not found",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"other_key": "value"},
				Attributes: map[string]string{"another_key": "value"},
			},
			key:      "test_key",
			expected: "",
		},
		{
			name: "Nil metadata",
			auth: &cliproxyauth.Auth{
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
		{
			name:     "Both nil",
			auth:     &cliproxyauth.Auth{},
			key:      "test_key",
			expected: "",
		},
		{
			name: "Value is trimmed and lowercased",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"test_key": "  UPPER_VALUE  "},
			},
			key:      "test_key",
			expected: "upper_value",
		},
		{
			name: "Empty string value in metadata - falls back to attributes",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"test_key": ""},
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
		{
			name: "Non-string value in metadata - falls back to attributes",
			auth: &cliproxyauth.Auth{
				Metadata:   map[string]any{"test_key": 123},
				Attributes: map[string]string{"test_key": "attribute_value"},
			},
			key:      "test_key",
			expected: "attribute_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAuthValue(tt.auth, tt.key)
			if result != tt.expected {
				t.Errorf("getAuthValue() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetAccountKey(t *testing.T) {
	tests := []struct {
		name    string
		auth    *cliproxyauth.Auth
		checkFn func(t *testing.T, result string)
	}{
		{
			name: "From client_id",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{
					"client_id":     "test-client-id-123",
					"refresh_token": "test-refresh-token-456",
				},
			},
			checkFn: func(t *testing.T, result string) {
				expected := kiroauth.GetAccountKey("test-client-id-123", "test-refresh-token-456")
				if result != expected {
					t.Errorf("expected %s, got %s", expected, result)
				}
			},
		},
		{
			name: "From refresh_token only",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{
					"refresh_token": "test-refresh-token-789",
				},
			},
			checkFn: func(t *testing.T, result string) {
				expected := kiroauth.GetAccountKey("", "test-refresh-token-789")
				if result != expected {
					t.Errorf("expected %s, got %s", expected, result)
				}
			},
		},
		{
			name: "Nil auth",
			auth: nil,
			checkFn: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
		{
			name: "Nil metadata",
			auth: &cliproxyauth.Auth{},
			checkFn: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
		{
			name: "Empty metadata",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{},
			},
			checkFn: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAccountKey(tt.auth)
			tt.checkFn(t, result)
		})
	}
}

func TestEndpointAliases(t *testing.T) {
	// Verify all expected aliases are defined
	expectedAliases := map[string]string{
		"codewhisperer": "codewhisperer",
		"ide":           "codewhisperer",
		"amazonq":       "amazonq",
		"q":             "amazonq",
		"cli":           "amazonq",
		"kiroruntime":   "kiroruntime",
		"runtime":       "kiroruntime",
		"kiro":          "kiroruntime",
	}

	for alias, target := range expectedAliases {
		if actual, ok := endpointAliases[alias]; !ok {
			t.Errorf("missing alias %q", alias)
		} else if actual != target {
			t.Errorf("alias %q = %q, want %q", alias, actual, target)
		}
	}

	// Verify no unexpected aliases
	if len(endpointAliases) != len(expectedAliases) {
		t.Errorf("unexpected number of aliases: got %d, want %d", len(endpointAliases), len(expectedAliases))
	}
}

func TestMapModelToKiro_MapsClaudeOpus47Variants(t *testing.T) {
	executor := &KiroExecutor{}
	tests := []struct {
		name     string
		model    string
		expected string
	}{
		{
			name:     "kiro alias",
			model:    "kiro-claude-opus-4-7",
			expected: "claude-opus-4.7",
		},
		{
			name:     "native hyphen alias",
			model:    "claude-opus-4-7",
			expected: "claude-opus-4.7",
		},
		{
			name:     "native dotted alias",
			model:    "claude-opus-4.7",
			expected: "claude-opus-4.7",
		},
		{
			name:     "dated alias collapses to canonical version",
			model:    "claude-sonnet-4-5-20250929",
			expected: "claude-sonnet-4.5",
		},
		{
			name:     "amazonq prefix",
			model:    "amazonq-claude-sonnet-4-5",
			expected: "claude-sonnet-4.5",
		},
		{
			name:     "non-Claude model passes through",
			model:    "kiro-glm-5",
			expected: "glm-5",
		},
		{
			name:     "non-Claude minimax versioned",
			model:    "kiro-minimax-m2-5",
			expected: "minimax-m2.5",
		},
		{
			name:     "identifier without trailing version unchanged",
			model:    "kiro-qwen3-coder-next",
			expected: "qwen3-coder-next",
		},
		{
			name:     "unknown model passes through unchanged",
			model:    "kiro-future-model-9",
			expected: "future-model-9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := executor.mapModelToKiro(tt.model); got != tt.expected {
				t.Fatalf("mapModelToKiro(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

func TestApplyKiroTokenUsagePreservesCacheTokenBreakdown(t *testing.T) {
	detail := usage.Detail{}
	ok := helps.ApplyKiroTokenUsage(&detail, map[string]interface{}{
		"uncachedInputTokens":   float64(290),
		"outputTokens":          float64(1),
		"totalTokens":           float64(4130),
		"cacheReadInputTokens":  float64(3822),
		"cacheWriteInputTokens": float64(17),
	})
	if !ok {
		t.Fatal("helps.ApplyKiroTokenUsage() = false, want true")
	}
	if detail.InputTokens != 4129 {
		t.Fatalf("InputTokens = %d, want 4129 (uncached 290 + cacheRead 3822 + cacheWrite 17)", detail.InputTokens)
	}
	if detail.OutputTokens != 1 {
		t.Fatalf("OutputTokens = %d, want 1", detail.OutputTokens)
	}
	if detail.TotalTokens != 4130 {
		t.Fatalf("TotalTokens = %d, want 4130", detail.TotalTokens)
	}
	if detail.CacheReadTokens != 3822 {
		t.Fatalf("CacheReadTokens = %d, want 3822", detail.CacheReadTokens)
	}
	if detail.CacheCreationTokens != 17 {
		t.Fatalf("CacheCreationTokens = %d, want 17", detail.CacheCreationTokens)
	}
	if detail.CachedTokens != 3822 {
		t.Fatalf("CachedTokens = %d, want 3822", detail.CachedTokens)
	}
}

func TestFinalizeKiroUsageTotalIncludesCacheTokensWhenUpstreamTotalMissing(t *testing.T) {
	detail := usage.Detail{
		InputTokens:         290,
		OutputTokens:        1,
		CacheReadTokens:     3822,
		CacheCreationTokens: 17,
	}
	helps.FinalizeKiroUsageTotal(&detail)
	if detail.TotalTokens != 4130 {
		t.Fatalf("TotalTokens = %d, want 4130", detail.TotalTokens)
	}
}

func TestKiroContextUsageFallbackDoesNotOverwritePreciseTokenUsage(t *testing.T) {
	detail := usage.Detail{}
	hasPreciseTokenUsage := helps.ApplyKiroTokenUsage(&detail, map[string]interface{}{
		"uncachedInputTokens":    float64(290),
		"outputTokens":           float64(1),
		"totalTokens":            float64(4130),
		"cacheReadInputTokens":   float64(3822),
		"cacheWriteInputTokens":  float64(17),
		"contextUsagePercentage": float64(50),
	})
	if !hasPreciseTokenUsage {
		t.Fatal("helps.ApplyKiroTokenUsage() = false, want true")
	}
	if _, applied := helps.ApplyKiroContextUsageFallback(&detail, 50, hasPreciseTokenUsage); applied {
		t.Fatal("helps.ApplyKiroContextUsageFallback() applied despite precise token usage")
	}
	if detail.InputTokens != 4129 {
		t.Fatalf("InputTokens = %d, want 4129 (uncached 290 + cacheRead 3822 + cacheWrite 17)", detail.InputTokens)
	}
	if detail.TotalTokens != 4130 {
		t.Fatalf("TotalTokens = %d, want upstream total 4130", detail.TotalTokens)
	}
}

func TestKiroContextUsageFallbackAppliesWhenPreciseTokenUsageMissing(t *testing.T) {
	detail := usage.Detail{OutputTokens: 3}
	calculated, applied := helps.ApplyKiroContextUsageFallback(&detail, 50, false)
	if !applied {
		t.Fatal("helps.ApplyKiroContextUsageFallback() applied = false, want true")
	}
	if calculated != 100000 {
		t.Fatalf("calculated input tokens = %d, want 100000", calculated)
	}
	if detail.InputTokens != 100000 {
		t.Fatalf("InputTokens = %d, want 100000", detail.InputTokens)
	}
	if detail.TotalTokens != 100003 {
		t.Fatalf("TotalTokens = %d, want 100003", detail.TotalTokens)
	}
}

func TestGetEffectiveProfileArnWithWarning(t *testing.T) {
	tests := []struct {
		name       string
		auth       *cliproxyauth.Auth
		profileArn string
		expected   string
	}{
		{
			name:       "existing profileArn returned as-is",
			auth:       nil,
			profileArn: "arn:aws:codewhisperer:us-east-1:123:profile/ABC",
			expected:   "arn:aws:codewhisperer:us-east-1:123:profile/ABC",
		},
		{
			name: "builder-id fallback uses DefaultBuilderIDProfileArn",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"auth_method": "builder-id"},
			},
			profileArn: "",
			expected:   kiroauth.DefaultBuilderIDProfileArn,
		},
		{
			name: "idc fallback uses DefaultBuilderIDProfileArn",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"auth_method": "idc"},
			},
			profileArn: "",
			expected:   kiroauth.DefaultBuilderIDProfileArn,
		},
		{
			name: "aws_sso_oidc auth_type fallback uses DefaultBuilderIDProfileArn",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"auth_type": "aws_sso_oidc"},
			},
			profileArn: "",
			expected:   kiroauth.DefaultBuilderIDProfileArn,
		},
		{
			name:       "no auth and no profileArn returns empty",
			auth:       nil,
			profileArn: "",
			expected:   "",
		},
		{
			name: "unknown auth method with no profileArn returns empty",
			auth: &cliproxyauth.Auth{
				Metadata: map[string]any{"auth_method": "social"},
			},
			profileArn: "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getEffectiveProfileArnWithWarning(tt.auth, tt.profileArn)
			if result != tt.expected {
				t.Errorf("getEffectiveProfileArnWithWarning() = %q, want %q", result, tt.expected)
			}
		})
	}
}
