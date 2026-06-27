package kiro

import (
	"net/http"
	"strings"
	"testing"
)

func TestKiroUserAgent_Streaming(t *testing.T) {
	ua, amzUA := kiroUserAgent(ApiStreaming, "F")

	// user-agent should have full form with md/appVersion
	requiredUA := []string{
		"aws-sdk-rust/1.3.15", "ua/2.1",
		"api/codewhispererstreaming/0.1.16551",
		"os/macos", "lang/rust/1.92.0",
		"md/appVersion-2.7.0", "app/AmazonQ-For-CLI",
	}
	for _, part := range requiredUA {
		if !strings.Contains(ua, part) {
			t.Errorf("streaming user-agent missing %q: %s", part, ua)
		}
	}

	// x-amz-user-agent should have m/F, not md/appVersion
	requiredAmz := []string{
		"aws-sdk-rust/1.3.15", "ua/2.1",
		"api/codewhispererstreaming/0.1.16551",
		"os/macos", "lang/rust/1.92.0",
		"m/F", "app/AmazonQ-For-CLI",
	}
	for _, part := range requiredAmz {
		if !strings.Contains(amzUA, part) {
			t.Errorf("streaming x-amz-user-agent missing %q: %s", part, amzUA)
		}
	}
	if strings.Contains(amzUA, "md/appVersion") {
		t.Errorf("x-amz-user-agent should not contain md/appVersion: %s", amzUA)
	}
}

func TestKiroUserAgent_Runtime(t *testing.T) {
	ua, amzUA := kiroUserAgent(ApiRuntime, "F,C")

	// user-agent: full form with codewhispererruntime
	if !strings.Contains(ua, "api/codewhispererruntime/0.1.16551") {
		t.Errorf("runtime user-agent should use codewhispererruntime: %s", ua)
	}
	if !strings.Contains(ua, "md/appVersion-2.7.0") {
		t.Errorf("runtime user-agent should have md/appVersion: %s", ua)
	}

	// x-amz-user-agent: m/F,C
	if !strings.Contains(amzUA, "m/F,C") {
		t.Errorf("runtime x-amz-user-agent should have m/F,C: %s", amzUA)
	}
	if !strings.Contains(amzUA, "api/codewhispererruntime/0.1.16551") {
		t.Errorf("runtime x-amz-user-agent should use codewhispererruntime: %s", amzUA)
	}
}

func TestKiroUserAgent_OIDC(t *testing.T) {
	ua, amzUA := kiroUserAgent(ApiOIDC, "E")

	// OIDC user-agent is bare: no ua/, api/, md/, app/
	if strings.Contains(ua, "ua/2.1") {
		t.Errorf("OIDC user-agent should not contain ua/2.1: %s", ua)
	}
	if strings.Contains(ua, "api/") {
		t.Errorf("OIDC user-agent should not contain api/: %s", ua)
	}
	if strings.Contains(ua, "md/appVersion") {
		t.Errorf("OIDC user-agent should not contain md/appVersion: %s", ua)
	}
	if strings.Contains(ua, "app/AmazonQ") {
		t.Errorf("OIDC user-agent should not contain app/: %s", ua)
	}

	// OIDC user-agent should be bare
	expectedBare := "aws-sdk-rust/1.3.10 os/macos lang/rust/1.92.0"
	if ua != expectedBare {
		t.Errorf("OIDC user-agent = %q, want %q", ua, expectedBare)
	}

	// OIDC x-amz-user-agent: full form with ssooidc and m/E
	requiredAmz := []string{
		"aws-sdk-rust/1.3.10", "ua/2.1",
		"api/ssooidc/1.92.0",
		"os/macos", "lang/rust/1.92.0",
		"m/E", "app/AmazonQ-For-CLI",
	}
	for _, part := range requiredAmz {
		if !strings.Contains(amzUA, part) {
			t.Errorf("OIDC x-amz-user-agent missing %q: %s", part, amzUA)
		}
	}
}

func TestSetOIDCHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	SetOIDCHeaders(req)

	if req.Header.Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type: application/json")
	}

	ua := req.Header.Get("User-Agent")
	expectedUA := "aws-sdk-rust/1.3.10 os/macos lang/rust/1.92.0"
	if ua != expectedUA {
		t.Errorf("User-Agent = %q, want %q", ua, expectedUA)
	}

	amzUA := req.Header.Get("x-amz-user-agent")
	if !strings.Contains(amzUA, "api/ssooidc/1.92.0") {
		t.Errorf("x-amz-user-agent should contain ssooidc: %s", amzUA)
	}
	if !strings.Contains(amzUA, "m/E") {
		t.Errorf("x-amz-user-agent should contain m/E: %s", amzUA)
	}

	if req.Header.Get("amz-sdk-invocation-id") == "" {
		t.Error("expected amz-sdk-invocation-id")
	}
	if req.Header.Get("amz-sdk-request") != "attempt=1; max=4" {
		t.Errorf("unexpected amz-sdk-request: %s", req.Header.Get("amz-sdk-request"))
	}
}

func TestSetRuntimeHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	setRuntimeHeaders(req, "test-token")

	if req.Header.Get("Authorization") != "Bearer test-token" {
		t.Error("expected Authorization header")
	}

	ua := req.Header.Get("User-Agent")
	if !strings.Contains(ua, "api/codewhispererruntime/0.1.16551") {
		t.Errorf("User-Agent should use codewhispererruntime: %s", ua)
	}
	if !strings.Contains(ua, "md/appVersion-2.7.0") {
		t.Errorf("User-Agent should have md/appVersion: %s", ua)
	}

	amzUA := req.Header.Get("x-amz-user-agent")
	if !strings.Contains(amzUA, "m/F,C") {
		t.Errorf("x-amz-user-agent should have m/F,C: %s", amzUA)
	}

	if req.Header.Get("amz-sdk-request") != "attempt=1; max=1" {
		t.Errorf("unexpected amz-sdk-request: %s", req.Header.Get("amz-sdk-request"))
	}
}

func TestSetStreamingHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	SetStreamingHeaders(req)

	ua := req.Header.Get("User-Agent")
	if !strings.Contains(ua, "api/codewhispererstreaming/0.1.16551") {
		t.Errorf("User-Agent should use codewhispererstreaming: %s", ua)
	}
	if !strings.Contains(ua, "md/appVersion-2.7.0") {
		t.Errorf("User-Agent should have md/appVersion: %s", ua)
	}

	amzUA := req.Header.Get("x-amz-user-agent")
	if !strings.Contains(amzUA, "m/F") {
		t.Errorf("x-amz-user-agent should have m/F: %s", amzUA)
	}
	// Ensure it's exactly m/F, not m/F,C or similar
	if strings.Contains(amzUA, "m/F,") {
		t.Errorf("x-amz-user-agent should have m/F only, not m/F,...: %s", amzUA)
	}
}

func TestSetDesktopRefreshHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://example.com", nil)
	SetDesktopRefreshHeaders(req)

	if req.Header.Get("Content-Type") != "application/json" {
		t.Error("expected Content-Type: application/json")
	}
	if req.Header.Get("User-Agent") != "Kiro-CLI" {
		t.Errorf("User-Agent = %q, want %q", req.Header.Get("User-Agent"), "Kiro-CLI")
	}
}

func TestBuildURL(t *testing.T) {
	got := buildURL("https://api.example.com", "getUsageLimits", map[string]string{
		"origin":     "AI_EDITOR",
		"profileArn": "",
	})
	want := "https://api.example.com/getUsageLimits?origin=AI_EDITOR"
	if got != want {
		t.Errorf("buildURL() = %v, want %v", got, want)
	}
}

func TestGenerateAccountKey(t *testing.T) {
	r1 := GenerateAccountKey("test-seed")
	r2 := GenerateAccountKey("test-seed")
	if r1 != r2 {
		t.Error("same seed should produce same result")
	}
	if len(r1) != 16 {
		t.Errorf("expected 16 char hex, got %d", len(r1))
	}
}

func TestGetAccountKey(t *testing.T) {
	// Priority 1: clientID
	r1 := GetAccountKey("client-123", "refresh-456")
	if r1 != GenerateAccountKey("client-123") {
		t.Error("expected clientID-based key")
	}
	// Priority 2: refreshToken
	r2 := GetAccountKey("", "refresh-789")
	if r2 != GenerateAccountKey("refresh-789") {
		t.Error("expected refreshToken-based key")
	}
	// Priority 3: random
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
