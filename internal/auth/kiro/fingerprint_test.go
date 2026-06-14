package kiro

import (
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestNewFingerprintManager(t *testing.T) {
	fm := NewFingerprintManager()
	if fm == nil {
		t.Fatal("expected non-nil FingerprintManager")
	}
	if fm.fingerprints == nil {
		t.Error("expected non-nil fingerprints map")
	}
	if fm.rng == nil {
		t.Error("expected non-nil rng")
	}
}

func TestGetFingerprint_NewToken(t *testing.T) {
	fm := NewFingerprintManager()
	fp := fm.GetFingerprint("token1")

	if fp == nil {
		t.Fatal("expected non-nil Fingerprint")
	}
	if fp.RuntimeSDKVersion == "" {
		t.Error("expected non-empty RuntimeSDKVersion")
	}
	if fp.StreamingSDKVersion == "" {
		t.Error("expected non-empty StreamingSDKVersion")
	}
	if fp.OSType == "" {
		t.Error("expected non-empty OSType")
	}
	if fp.KiroVersion == "" {
		t.Error("expected non-empty KiroVersion")
	}
}

func TestGetFingerprint_SameTokenReturnsSameFingerprint(t *testing.T) {
	fm := NewFingerprintManager()
	fp1 := fm.GetFingerprint("token1")
	fp2 := fm.GetFingerprint("token1")

	if fp1 != fp2 {
		t.Error("expected same fingerprint for same token")
	}
}

func TestGetFingerprint_DifferentTokens(t *testing.T) {
	fm := NewFingerprintManager()
	fp1 := fm.GetFingerprint("token1")
	fp2 := fm.GetFingerprint("token2")

	if fp1 == fp2 {
		t.Error("expected different fingerprints for different tokens")
	}
}

func TestBuildUserAgent(t *testing.T) {
	fm := NewFingerprintManager()
	fp := fm.GetFingerprint("token1")

	ua := fp.BuildUserAgent()
	if ua == "" {
		t.Error("expected non-empty User-Agent")
	}

	amzUA := fp.BuildAmzUserAgent()
	if amzUA == "" {
		t.Error("expected non-empty X-Amz-User-Agent")
	}
}

func TestGetFingerprint_OSTypeIsValid(t *testing.T) {
	fm := NewFingerprintManager()

	validOS := map[string]bool{"macos": true, "windows": true, "linux": true}
	for i := 0; i < 20; i++ {
		fp := fm.GetFingerprint("token" + string(rune('a'+i)))
		if !validOS[fp.OSType] {
			t.Errorf("invalid OS type: %s", fp.OSType)
		}
	}
}

func TestGenerateFromConfig_OSTypeFromRuntimeGOOS(t *testing.T) {
	fm := NewFingerprintManager()

	// Set config with empty OSType to trigger runtime.GOOS fallback
	fm.SetConfig(&FingerprintConfig{
		RuntimeSDKVersion: "0.1.16551",
	})

	fp := fm.GetFingerprint("test-token")

	// Expected OS type based on runtime.GOOS mapping
	var expectedOS string
	switch runtime.GOOS {
	case "darwin":
		expectedOS = "macos"
	case "windows":
		expectedOS = "windows"
	default:
		expectedOS = "linux"
	}

	if fp.OSType != expectedOS {
		t.Errorf("expected OSType '%s' from runtime.GOOS '%s', got '%s'",
			expectedOS, runtime.GOOS, fp.OSType)
	}
}

func TestFingerprintManager_ConcurrentAccess(t *testing.T) {
	fm := NewFingerprintManager()
	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range numOperations {
				tokenKey := "token" + string(rune('a'+id%26))
				switch j % 2 {
				case 0:
					fm.GetFingerprint(tokenKey)
				case 1:
					fp := fm.GetFingerprint(tokenKey)
					_ = fp.BuildUserAgent()
					_ = fp.BuildAmzUserAgent()
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestGlobalFingerprintManager(t *testing.T) {
	fm1 := GlobalFingerprintManager()
	fm2 := GlobalFingerprintManager()

	if fm1 == nil {
		t.Fatal("expected non-nil GlobalFingerprintManager")
	}
	if fm1 != fm2 {
		t.Error("expected GlobalFingerprintManager to return same instance")
	}
}

func TestSetOIDCHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	SetOIDCHeaders(req)

	if req.Header.Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type header to be set")
	}

	amzUA := req.Header.Get("x-amz-user-agent")
	if amzUA == "" {
		t.Error("expected x-amz-user-agent header to be set")
	}
	if !strings.Contains(amzUA, "aws-sdk-rust/") {
		t.Errorf("x-amz-user-agent should contain aws-sdk-rust: %s", amzUA)
	}
	if !strings.Contains(amzUA, "appVersion-") {
		t.Errorf("x-amz-user-agent should contain appVersion: %s", amzUA)
	}

	ua := req.Header.Get("User-Agent")
	if ua == "" {
		t.Error("expected User-Agent header to be set")
	}
	if !strings.Contains(ua, "api/sso-oidc") {
		t.Errorf("User-Agent should contain api name: %s", ua)
	}

	if req.Header.Get("amz-sdk-invocation-id") == "" {
		t.Error("expected amz-sdk-invocation-id header to be set")
	}
	if req.Header.Get("amz-sdk-request") != "attempt=1; max=4" {
		t.Errorf("unexpected amz-sdk-request header: %s", req.Header.Get("amz-sdk-request"))
	}
}

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		path         string
		queryParams  map[string]string
		want         string
		wantContains []string
	}{
		{
			name:        "no query params",
			endpoint:    "https://api.example.com",
			path:        "getUsageLimits",
			queryParams: nil,
			want:        "https://api.example.com/getUsageLimits",
		},
		{
			name:        "empty query params",
			endpoint:    "https://api.example.com",
			path:        "getUsageLimits",
			queryParams: map[string]string{},
			want:        "https://api.example.com/getUsageLimits",
		},
		{
			name:     "single query param",
			endpoint: "https://api.example.com",
			path:     "getUsageLimits",
			queryParams: map[string]string{
				"origin": "AI_EDITOR",
			},
			want: "https://api.example.com/getUsageLimits?origin=AI_EDITOR",
		},
		{
			name:     "multiple query params",
			endpoint: "https://api.example.com",
			path:     "getUsageLimits",
			queryParams: map[string]string{
				"origin":       "AI_EDITOR",
				"resourceType": "AGENTIC_REQUEST",
				"profileArn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/ABCDEF",
			},
			wantContains: []string{
				"https://api.example.com/getUsageLimits?",
				"origin=AI_EDITOR",
				"profileArn=arn%3Aaws%3Acodewhisperer%3Aus-east-1%3A123456789012%3Aprofile%2FABCDEF",
				"resourceType=AGENTIC_REQUEST",
			},
		},
		{
			name:     "omit empty params",
			endpoint: "https://api.example.com",
			path:     "getUsageLimits",
			queryParams: map[string]string{
				"origin":     "AI_EDITOR",
				"profileArn": "",
			},
			want: "https://api.example.com/getUsageLimits?origin=AI_EDITOR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildURL(tt.endpoint, tt.path, tt.queryParams)
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("buildURL() = %v, want %v", got, tt.want)
				}
			}
			if tt.wantContains != nil {
				for _, substr := range tt.wantContains {
					if !strings.Contains(got, substr) {
						t.Errorf("buildURL() = %v, want to contain %v", got, substr)
					}
				}
			}
		})
	}
}

func TestBuildUserAgentFormat(t *testing.T) {
	fm := NewFingerprintManager()
	fp := fm.GetFingerprint("token1")

	ua := fp.BuildUserAgent()
	requiredParts := []string{
		"aws-sdk-rust/",
		"api/codewhispererstreaming/",
		"os/",
		"appVersion-",
	}
	for _, part := range requiredParts {
		if !strings.Contains(ua, part) {
			t.Errorf("User-Agent missing required part %q: %s", part, ua)
		}
	}
}

func TestBuildAmzUserAgentFormat(t *testing.T) {
	fm := NewFingerprintManager()
	fp := fm.GetFingerprint("token1")

	amzUA := fp.BuildAmzUserAgent()
	requiredParts := []string{
		"aws-sdk-rust/",
		"appVersion-",
	}
	for _, part := range requiredParts {
		if !strings.Contains(amzUA, part) {
			t.Errorf("X-Amz-User-Agent missing required part %q: %s", part, amzUA)
		}
	}

	// Amz-User-Agent should be shorter than User-Agent
	ua := fp.BuildUserAgent()
	if len(amzUA) >= len(ua) {
		t.Error("X-Amz-User-Agent should be shorter than User-Agent")
	}
}

func TestSetRuntimeHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	accessToken := "test-access-token-1234567890"
	clientID := "test-client-id-12345"
	accountKey := GenerateAccountKey(clientID)

	setRuntimeHeaders(req, accessToken, accountKey)

	// Check Authorization header
	if req.Header.Get("Authorization") != "Bearer "+accessToken {
		t.Errorf("expected Authorization header 'Bearer %s', got '%s'", accessToken, req.Header.Get("Authorization"))
	}

	// Check x-amz-user-agent header
	amzUA := req.Header.Get("x-amz-user-agent")
	if amzUA == "" {
		t.Error("expected x-amz-user-agent header to be set")
	}
	if !strings.Contains(amzUA, "aws-sdk-rust/") {
		t.Errorf("x-amz-user-agent should contain aws-sdk-rust: %s", amzUA)
	}
	if !strings.Contains(amzUA, "appVersion-") {
		t.Errorf("x-amz-user-agent should contain appVersion: %s", amzUA)
	}

	// Check User-Agent header
	ua := req.Header.Get("User-Agent")
	if ua == "" {
		t.Error("expected User-Agent header to be set")
	}
	if !strings.Contains(ua, "api/codewhispererruntime/") {
		t.Errorf("User-Agent should contain api/codewhispererruntime: %s", ua)
	}

	// Check amz-sdk-invocation-id (should be a UUID)
	invocationID := req.Header.Get("amz-sdk-invocation-id")
	if invocationID == "" {
		t.Error("expected amz-sdk-invocation-id header to be set")
	}
	if len(invocationID) != 36 {
		t.Errorf("expected amz-sdk-invocation-id to be UUID (36 chars), got %d", len(invocationID))
	}

	// Check amz-sdk-request
	if req.Header.Get("amz-sdk-request") != "attempt=1; max=1" {
		t.Errorf("unexpected amz-sdk-request header: %s", req.Header.Get("amz-sdk-request"))
	}
}

func TestSDKVersionsAreValid(t *testing.T) {
	for _, v := range runtimeSDKVersions {
		parts := strings.Split(v, ".")
		if len(parts) != 3 {
			t.Errorf("Runtime SDK version should have 3 parts: %s", v)
		}
	}

	for _, v := range streamingSDKVersions {
		parts := strings.Split(v, ".")
		if len(parts) != 3 {
			t.Errorf("Streaming SDK version should have 3 parts: %s", v)
		}
	}
}

func TestKiroVersionsAreValid(t *testing.T) {
	for _, v := range kiroVersions {
		parts := strings.Split(v, ".")
		if len(parts) != 3 {
			t.Errorf("Kiro version should have 3 parts: %s", v)
		}
	}
}

func TestFingerprintManager_SetConfig(t *testing.T) {
	fm := NewFingerprintManager()

	// Without config, should generate random fingerprint
	fp1 := fm.GetFingerprint("token1")
	if fp1 == nil {
		t.Fatal("expected non-nil fingerprint")
	}

	// Set config with all fields
	cfg := &FingerprintConfig{
		RuntimeSDKVersion:   "9.9.9",
		StreamingSDKVersion: "8.8.8",
		OSType:              "macos",
		KiroVersion:         "9.9.999",
	}
	fm.SetConfig(cfg)

	// After setting config, should use config values
	fp2 := fm.GetFingerprint("token2")
	if fp2.RuntimeSDKVersion != "9.9.9" {
		t.Errorf("expected RuntimeSDKVersion '9.9.9', got '%s'", fp2.RuntimeSDKVersion)
	}
	if fp2.StreamingSDKVersion != "8.8.8" {
		t.Errorf("expected StreamingSDKVersion '8.8.8', got '%s'", fp2.StreamingSDKVersion)
	}
	if fp2.OSType != "macos" {
		t.Errorf("expected OSType 'macos', got '%s'", fp2.OSType)
	}
	if fp2.KiroVersion != "9.9.999" {
		t.Errorf("expected KiroVersion '9.9.999', got '%s'", fp2.KiroVersion)
	}
}

func TestFingerprintManager_SetConfig_PartialFields(t *testing.T) {
	fm := NewFingerprintManager()

	// Set config with only some fields
	cfg := &FingerprintConfig{
		KiroVersion: "1.2.345",
	}
	fm.SetConfig(cfg)

	fp := fm.GetFingerprint("token1")

	// Configured fields should use config values
	if fp.KiroVersion != "1.2.345" {
		t.Errorf("expected KiroVersion '1.2.345', got '%s'", fp.KiroVersion)
	}

	// Empty fields should be randomly selected (non-empty)
	if fp.OSType == "" {
		t.Error("expected non-empty OSType")
	}
	if fp.RuntimeSDKVersion == "" {
		t.Error("expected non-empty RuntimeSDKVersion")
	}
}

func TestFingerprintManager_SetConfig_ClearsCache(t *testing.T) {
	fm := NewFingerprintManager()

	// Get fingerprint before config
	fp1 := fm.GetFingerprint("token1")
	originalVersion := fp1.KiroVersion

	// Set config
	cfg := &FingerprintConfig{
		KiroVersion: "99.99.99",
	}
	fm.SetConfig(cfg)

	// Same token should now return different fingerprint (cache cleared)
	fp2 := fm.GetFingerprint("token1")
	if fp2.KiroVersion == originalVersion {
		t.Error("expected cache to be cleared after SetConfig")
	}
	if fp2.KiroVersion != "99.99.99" {
		t.Errorf("expected KiroVersion '99.99.99', got '%s'", fp2.KiroVersion)
	}
}

func TestGenerateAccountKey(t *testing.T) {
	tests := []struct {
		name  string
		seed  string
		check func(t *testing.T, result string)
	}{
		{
			name: "Empty seed",
			seed: "",
			check: func(t *testing.T, result string) {
				if result == "" {
					t.Error("expected non-empty result for empty seed")
				}
				if len(result) != 16 {
					t.Errorf("expected 16 char hex string, got %d chars", len(result))
				}
			},
		},
		{
			name: "Simple seed",
			seed: "test-client-id",
			check: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char hex string, got %d chars", len(result))
				}
				for _, c := range result {
					if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
						t.Errorf("invalid hex character: %c", c)
					}
				}
			},
		},
		{
			name: "Same seed produces same result",
			seed: "deterministic-seed",
			check: func(t *testing.T, result string) {
				result2 := GenerateAccountKey("deterministic-seed")
				if result != result2 {
					t.Errorf("same seed should produce same result: %s vs %s", result, result2)
				}
			},
		},
		{
			name: "Different seeds produce different results",
			seed: "seed-one",
			check: func(t *testing.T, result string) {
				result2 := GenerateAccountKey("seed-two")
				if result == result2 {
					t.Errorf("different seeds should produce different results: %s vs %s", result, result2)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateAccountKey(tt.seed)
			tt.check(t, result)
		})
	}
}

func TestGetAccountKey(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		refreshToken string
		check        func(t *testing.T, result string)
	}{
		{
			name:         "Priority 1: clientID when both provided",
			clientID:     "client-id-123",
			refreshToken: "refresh-token-456",
			check: func(t *testing.T, result string) {
				expected := GenerateAccountKey("client-id-123")
				if result != expected {
					t.Errorf("expected clientID-based key %s, got %s", expected, result)
				}
			},
		},
		{
			name:         "Priority 2: refreshToken when clientID is empty",
			clientID:     "",
			refreshToken: "refresh-token-789",
			check: func(t *testing.T, result string) {
				expected := GenerateAccountKey("refresh-token-789")
				if result != expected {
					t.Errorf("expected refreshToken-based key %s, got %s", expected, result)
				}
			},
		},
		{
			name:         "Priority 3: random when both empty",
			clientID:     "",
			refreshToken: "",
			check: func(t *testing.T, result string) {
				if len(result) != 16 {
					t.Errorf("expected 16 char key, got %d chars", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAccountKey(tt.clientID, tt.refreshToken)
			tt.check(t, result)
		})
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	fm := NewFingerprintManager()
	accountKey := GenerateAccountKey("test-client-id")

	fp1 := fm.GetFingerprint(accountKey)
	fp2 := fm.GetFingerprint(accountKey)

	if fp1 != fp2 {
		t.Error("expected same fingerprint pointer for same key")
	}

	// Create new manager and verify same values
	fm2 := NewFingerprintManager()
	fp3 := fm2.GetFingerprint(accountKey)

	if fp1.OSType != fp3.OSType {
		t.Errorf("OSType should be deterministic: %s vs %s", fp1.OSType, fp3.OSType)
	}
	if fp1.KiroVersion != fp3.KiroVersion {
		t.Errorf("KiroVersion should be deterministic: %s vs %s", fp1.KiroVersion, fp3.KiroVersion)
	}
}
