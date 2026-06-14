package kiro

import (
	"net/http"
	"strings"
	"testing"
)

func TestSetOIDCHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	SetOIDCHeaders(req)

	if req.Header.Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type header to be set")
	}

	ua := req.Header.Get("User-Agent")
	if ua != kiroUserAgent {
		t.Errorf("User-Agent = %q, want %q", ua, kiroUserAgent)
	}
	if !strings.Contains(ua, "aws-sdk-rust/") {
		t.Errorf("User-Agent should contain aws-sdk-rust: %s", ua)
	}
	if !strings.Contains(ua, "app/AmazonQ-For-CLI") {
		t.Errorf("User-Agent should contain app identifier: %s", ua)
	}
	if !strings.Contains(ua, "md/appVersion-2.7.0") {
		t.Errorf("User-Agent should contain appVersion metadata: %s", ua)
	}

	amzUA := req.Header.Get("x-amz-user-agent")
	if amzUA != kiroAmzUserAgent {
		t.Errorf("x-amz-user-agent = %q, want %q", amzUA, kiroAmzUserAgent)
	}
	// x-amz-user-agent should NOT contain md/appVersion
	if strings.Contains(amzUA, "md/appVersion") {
		t.Errorf("x-amz-user-agent should not contain md/appVersion: %s", amzUA)
	}

	if req.Header.Get("amz-sdk-invocation-id") == "" {
		t.Error("expected amz-sdk-invocation-id header to be set")
	}
	if req.Header.Get("amz-sdk-request") != "attempt=1; max=4" {
		t.Errorf("unexpected amz-sdk-request header: %s", req.Header.Get("amz-sdk-request"))
	}
}

func TestSetRuntimeHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	accessToken := "test-access-token-1234567890"

	setRuntimeHeaders(req, accessToken, "ignored-account-key")

	if req.Header.Get("Authorization") != "Bearer "+accessToken {
		t.Errorf("expected Authorization 'Bearer %s', got '%s'", accessToken, req.Header.Get("Authorization"))
	}
	if req.Header.Get("User-Agent") != kiroUserAgent {
		t.Errorf("User-Agent = %q, want %q", req.Header.Get("User-Agent"), kiroUserAgent)
	}
	if req.Header.Get("x-amz-user-agent") != kiroAmzUserAgent {
		t.Errorf("x-amz-user-agent = %q, want %q", req.Header.Get("x-amz-user-agent"), kiroAmzUserAgent)
	}
	if req.Header.Get("amz-sdk-invocation-id") == "" {
		t.Error("expected amz-sdk-invocation-id header to be set")
	}
	if req.Header.Get("amz-sdk-request") != "attempt=1; max=1" {
		t.Errorf("unexpected amz-sdk-request: %s", req.Header.Get("amz-sdk-request"))
	}
}

func TestSetStreamingHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	SetStreamingHeaders(req)

	if req.Header.Get("User-Agent") != kiroUserAgent {
		t.Errorf("User-Agent = %q, want %q", req.Header.Get("User-Agent"), kiroUserAgent)
	}
	if req.Header.Get("x-amz-user-agent") != kiroAmzUserAgent {
		t.Errorf("x-amz-user-agent = %q, want %q", req.Header.Get("x-amz-user-agent"), kiroAmzUserAgent)
	}
}

func TestUserAgentConstants(t *testing.T) {
	// Verify the UA format matches kiro-cli 2.7.0 captures
	requiredParts := []string{
		"aws-sdk-rust/1.3.15",
		"ua/2.1",
		"api/codewhispererstreaming/0.1.16551",
		"os/macos",
		"lang/rust/1.92.0",
		"app/AmazonQ-For-CLI",
	}
	for _, part := range requiredParts {
		if !strings.Contains(kiroUserAgent, part) {
			t.Errorf("User-Agent missing %q: %s", part, kiroUserAgent)
		}
		if !strings.Contains(kiroAmzUserAgent, part) {
			t.Errorf("x-amz-user-agent missing %q: %s", part, kiroAmzUserAgent)
		}
	}

	// User-Agent has md/appVersion, x-amz-user-agent doesn't
	if !strings.Contains(kiroUserAgent, "md/appVersion-2.7.0") {
		t.Error("User-Agent should contain md/appVersion-2.7.0")
	}
	if strings.Contains(kiroAmzUserAgent, "md/appVersion") {
		t.Error("x-amz-user-agent should NOT contain md/appVersion")
	}
}

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		path        string
		queryParams map[string]string
		want        string
	}{
		{
			name:     "no query params",
			endpoint: "https://api.example.com",
			path:     "getUsageLimits",
			want:     "https://api.example.com/getUsageLimits",
		},
		{
			name:     "with query params",
			endpoint: "https://api.example.com",
			path:     "getUsageLimits",
			queryParams: map[string]string{
				"origin": "AI_EDITOR",
			},
			want: "https://api.example.com/getUsageLimits?origin=AI_EDITOR",
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
			if got != tt.want {
				t.Errorf("buildURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateAccountKey(t *testing.T) {
	// Empty seed
	r := GenerateAccountKey("")
	if len(r) != 16 {
		t.Errorf("expected 16 char hex, got %d", len(r))
	}

	// Same seed produces same result
	r1 := GenerateAccountKey("test-seed")
	r2 := GenerateAccountKey("test-seed")
	if r1 != r2 {
		t.Error("same seed should produce same result")
	}

	// Different seeds produce different results
	r3 := GenerateAccountKey("other-seed")
	if r1 == r3 {
		t.Error("different seeds should produce different results")
	}
}

func TestGetAccountKey(t *testing.T) {
	// Priority 1: clientID
	r1 := GetAccountKey("client-123", "refresh-456")
	expected := GenerateAccountKey("client-123")
	if r1 != expected {
		t.Errorf("expected clientID-based key")
	}

	// Priority 2: refreshToken
	r2 := GetAccountKey("", "refresh-789")
	expected2 := GenerateAccountKey("refresh-789")
	if r2 != expected2 {
		t.Errorf("expected refreshToken-based key")
	}

	// Priority 3: random (non-empty)
	r3 := GetAccountKey("", "")
	if len(r3) != 16 {
		t.Errorf("expected 16 char key, got %d", len(r3))
	}
}

func TestKiroClientName(t *testing.T) {
	if KiroClientName != "Kiro CLI" {
		t.Errorf("KiroClientName = %q, want %q", KiroClientName, "Kiro CLI")
	}
}
