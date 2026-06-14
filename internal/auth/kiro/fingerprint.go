package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/google/uuid"
)

// Hardcoded constants matching kiro-cli 2.7.0 (Rust) User-Agent captures.
// No per-account variation; kiro-cli sends the same UA for all requests.
const (
	// KiroClientName is the OIDC client registration name.
	KiroClientName = "Kiro CLI"

	// kiroUserAgent is the User-Agent header value for all Kiro requests.
	// Format: aws-sdk-rust/{sdkVer} ua/2.1 api/{api}/{sdkVer} os/{os} lang/rust/{rustVer} md/appVersion-{appVer} app/{app}
	kiroUserAgent = "aws-sdk-rust/1.3.15 ua/2.1 api/codewhispererstreaming/0.1.16551 os/macos lang/rust/1.92.0 md/appVersion-2.7.0 app/AmazonQ-For-CLI"

	// kiroAmzUserAgent is the x-amz-user-agent header value (no md/appVersion segment).
	kiroAmzUserAgent = "aws-sdk-rust/1.3.15 ua/2.1 api/codewhispererstreaming/0.1.16551 os/macos lang/rust/1.92.0 app/AmazonQ-For-CLI"
)

// SetOIDCHeaders sets headers for AWS OIDC requests (login/token refresh).
func SetOIDCHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", kiroUserAgent)
	req.Header.Set("x-amz-user-agent", kiroAmzUserAgent)
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("amz-sdk-request", "attempt=1; max=4")
}

// setRuntimeHeaders sets headers for Kiro management API requests
// (GetProfile, ListAvailableModels, GetUsageLimits).
func setRuntimeHeaders(req *http.Request, accessToken string, _ string) {
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", kiroUserAgent)
	req.Header.Set("x-amz-user-agent", kiroAmzUserAgent)
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("amz-sdk-request", "attempt=1; max=1")
}

// SetStreamingHeaders sets User-Agent headers for Kiro streaming API requests
// (generateAssistantResponse). Exported for use by the executor.
func SetStreamingHeaders(req *http.Request) {
	req.Header.Set("User-Agent", kiroUserAgent)
	req.Header.Set("x-amz-user-agent", kiroAmzUserAgent)
}

// GenerateAccountKey returns a 16-char hex key derived from SHA256(seed).
func GenerateAccountKey(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:8])
}

// GetAccountKey derives an account key from clientID > refreshToken > random UUID.
func GetAccountKey(clientID, refreshToken string) string {
	if clientID != "" {
		return GenerateAccountKey(clientID)
	}
	if refreshToken != "" {
		return GenerateAccountKey(refreshToken)
	}
	return GenerateAccountKey(uuid.New().String())
}
