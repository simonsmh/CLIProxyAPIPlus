package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// Kiro-cli 2.7.0 UA component constants, captured from real traffic.
const (
	KiroClientName = "Kiro CLI"

	kiroSDKVersion       = "1.3.15" // aws-sdk-rust for runtime/streaming
	kiroOIDCSDKVersion   = "1.3.10" // aws-sdk-rust for OIDC
	kiroAPIVersion       = "0.1.16551"
	kiroOIDCAPIVersion   = "1.92.0"
	kiroRustVersion      = "1.92.0"
	kiroOS               = "macos"
	kiroVersion          = "2.7.0"
	kiroApp              = "AmazonQ-For-CLI"
	kiroDesktopUserAgent = "Kiro-CLI" // for auth.desktop.kiro.dev refresh
)

// KiroSdkApi identifies the AWS SDK service segment in the UA.
type KiroSdkApi string

const (
	ApiStreaming KiroSdkApi = "codewhispererstreaming"
	ApiRuntime   KiroSdkApi = "codewhispererruntime"
	ApiOIDC      KiroSdkApi = "ssooidc"
)

// kiroUserAgent builds the user-agent and x-amz-user-agent header pair
// for a given Kiro/AWS service endpoint, matching official kiro-cli 2.7.0 traffic.
//
// Parameters:
//   - api:     The SDK service segment (streaming, runtime, or ssooidc).
//   - metrics: The m/... metrics segment for x-amz-user-agent (e.g. "F", "F,C", "E").
//
// For OIDC, user-agent is the bare form (no ua/, api/, md/, app/ segments),
// while x-amz-user-agent carries the full form.
// For runtime/streaming, both headers carry the full form (user-agent has
// md/appVersion, x-amz-user-agent has m/<metrics>).
func kiroUserAgent(api KiroSdkApi, metrics string) (userAgent, amzUserAgent string) {
	sdk := kiroSDKVersion
	apiVer := kiroAPIVersion
	if api == ApiOIDC {
		sdk = kiroOIDCSDKVersion
		apiVer = kiroOIDCAPIVersion
	}

	base := fmt.Sprintf("aws-sdk-rust/%s ua/2.1 api/%s/%s os/%s lang/rust/%s",
		sdk, api, apiVer, kiroOS, kiroRustVersion)

	if api == ApiOIDC {
		// OIDC user-agent is bare: no ua/, api/, md/, app/ segments
		userAgent = fmt.Sprintf("aws-sdk-rust/%s os/%s lang/rust/%s",
			sdk, kiroOS, kiroRustVersion)
	} else {
		userAgent = fmt.Sprintf("%s md/appVersion-%s app/%s",
			base, kiroVersion, kiroApp)
	}

	amzUserAgent = fmt.Sprintf("%s m/%s app/%s", base, metrics, kiroApp)
	return
}

// SetOIDCHeaders sets headers for AWS OIDC requests (RegisterClient, device auth, token).
// Uses ssooidc API with metrics "E".
func SetOIDCHeaders(req *http.Request) {
	ua, amzUA := kiroUserAgent(ApiOIDC, "E")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("x-amz-user-agent", amzUA)
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("amz-sdk-request", "attempt=1; max=4")
}

// setRuntimeHeaders sets headers for Kiro management API requests
// (GetProfile, ListAvailableModels, GetUsageLimits).
// Uses codewhispererruntime API with metrics "F,C".
func setRuntimeHeaders(req *http.Request, accessToken string) {
	ua, amzUA := kiroUserAgent(ApiRuntime, "F,C")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("x-amz-user-agent", amzUA)
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("amz-sdk-request", "attempt=1; max=1")
}

// SetStreamingHeaders sets User-Agent headers for Kiro streaming API requests
// (GenerateAssistantResponse). Uses codewhispererstreaming API with metrics "F".
func SetStreamingHeaders(req *http.Request) {
	ua, amzUA := kiroUserAgent(ApiStreaming, "F")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("x-amz-user-agent", amzUA)
}

// SetDesktopRefreshHeaders sets headers for the Kiro desktop auth service
// (auth.desktop.kiro.dev/refreshToken). Uses plain "Kiro-CLI" UA.
func SetDesktopRefreshHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", kiroDesktopUserAgent)
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

// Fingerprint is a compatibility stub for legacy D code (oauth.go, social_auth.go).
// The fingerprint system was rewritten to use hardcoded kiro-cli 2.7.0 constants.
type Fingerprint struct {
	KiroHash            string
	KiroVersion         string
	RuntimeSDKVersion   string
	StreamingSDKVersion string
	OSType              string
	OSVersion           string
}

// FingerprintManager is a compatibility stub.
type FingerprintManager struct{}

// GlobalFingerprintManager returns a stub FingerprintManager for backward compatibility
// with legacy OAuth code. The fingerprint system no longer generates per-account values.
func GlobalFingerprintManager() *FingerprintManager {
	return &FingerprintManager{}
}

// GetFingerprint returns a Fingerprint with hardcoded kiro-cli 2.7.0 values.
func (fm *FingerprintManager) GetFingerprint(_ string) *Fingerprint {
	return &Fingerprint{
		KiroHash:            GenerateAccountKey("kiro-cli-2.7.0"),
		KiroVersion:         kiroVersion,
		RuntimeSDKVersion:   kiroAPIVersion,
		StreamingSDKVersion: kiroSDKVersion,
		OSType:              kiroOS,
		OSVersion:           "2.7.0",
	}
}
