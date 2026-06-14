package kiro

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Fingerprint holds per-account disguise data for Kiro API requests.
// Values match kiro-cli 2.7.0 (Rust-based CLI), captured from real traffic.
type Fingerprint struct {
	RuntimeSDKVersion   string // e.g. 0.1.16551 (aws-sdk-rust version)
	StreamingSDKVersion string // e.g. 0.1.16551
	OSType              string // macos, windows, linux
	KiroVersion         string // e.g. 2.7.0 (appVersion)
}

// FingerprintConfig holds external fingerprint overrides.
type FingerprintConfig struct {
	RuntimeSDKVersion   string
	StreamingSDKVersion string
	OSType              string
	KiroVersion         string
}

// FingerprintManager manages per-account fingerprint generation and caching.
type FingerprintManager struct {
	mu           sync.RWMutex
	fingerprints map[string]*Fingerprint // tokenKey -> fingerprint
	rng          *rand.Rand
	config       *FingerprintConfig // External config (Optional)
}

var (
	// SDK versions for runtime API (getUsageLimits, ListAvailableModels, GetProfile)
	// Captured from kiro-cli 2.7.0: api/codewhispererruntime/0.1.16551
	runtimeSDKVersions = []string{"0.1.16551"}
	// SDK versions for streaming API (generateAssistantResponse)
	// Captured from kiro-cli 2.7.0: api/codewhispererstreaming/0.1.16551
	streamingSDKVersions = []string{"0.1.16551"}
	// OS types — kiro-cli uses "macos" not "darwin"
	osTypes = []string{"macos", "windows", "linux"}
	// Kiro CLI versions (appVersion)
	kiroVersions = []string{"2.7.0"}
	// Global singleton
	globalFingerprintManager     *FingerprintManager
	globalFingerprintManagerOnce sync.Once
)

func GlobalFingerprintManager() *FingerprintManager {
	globalFingerprintManagerOnce.Do(func() {
		globalFingerprintManager = NewFingerprintManager()
	})
	return globalFingerprintManager
}

func SetGlobalFingerprintConfig(cfg *FingerprintConfig) {
	GlobalFingerprintManager().SetConfig(cfg)
}

// SetConfig applies the config and clears the fingerprint cache.
func (fm *FingerprintManager) SetConfig(cfg *FingerprintConfig) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.config = cfg
	// Clear cached fingerprints so they regenerate with the new config
	fm.fingerprints = make(map[string]*Fingerprint)
}

func NewFingerprintManager() *FingerprintManager {
	return &FingerprintManager{
		fingerprints: make(map[string]*Fingerprint),
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GetFingerprint returns the fingerprint for tokenKey, creating one if it doesn't exist.
func (fm *FingerprintManager) GetFingerprint(tokenKey string) *Fingerprint {
	fm.mu.RLock()
	if fp, exists := fm.fingerprints[tokenKey]; exists {
		fm.mu.RUnlock()
		return fp
	}
	fm.mu.RUnlock()

	fm.mu.Lock()
	defer fm.mu.Unlock()

	if fp, exists := fm.fingerprints[tokenKey]; exists {
		return fp
	}

	fp := fm.generateFingerprint(tokenKey)
	fm.fingerprints[tokenKey] = fp
	return fp
}

func (fm *FingerprintManager) generateFingerprint(tokenKey string) *Fingerprint {
	if fm.config != nil {
		return fm.generateFromConfig(tokenKey)
	}
	return fm.generateRandom(tokenKey)
}

// generateFromConfig uses config values, falling back to random for empty fields.
func (fm *FingerprintManager) generateFromConfig(tokenKey string) *Fingerprint {
	cfg := fm.config

	configOrRandom := func(configVal string, choices []string) string {
		if configVal != "" {
			return configVal
		}
		return choices[fm.rng.Intn(len(choices))]
	}

	osType := cfg.OSType
	if osType == "" {
		// Map Go's runtime.GOOS to kiro-cli OS names
		osType = goOSToKiro(runtime.GOOS)
	}

	return &Fingerprint{
		RuntimeSDKVersion:   configOrRandom(cfg.RuntimeSDKVersion, runtimeSDKVersions),
		StreamingSDKVersion: configOrRandom(cfg.StreamingSDKVersion, streamingSDKVersions),
		OSType:              osType,
		KiroVersion:         configOrRandom(cfg.KiroVersion, kiroVersions),
	}
}

// generateRandom generates a deterministic fingerprint seeded by accountKey hash.
func (fm *FingerprintManager) generateRandom(accountKey string) *Fingerprint {
	hash := sha256.Sum256([]byte(accountKey))
	seed := int64(binary.BigEndian.Uint64(hash[:8]))
	rng := rand.New(rand.NewSource(seed))

	osType := goOSToKiro(runtime.GOOS)

	return &Fingerprint{
		RuntimeSDKVersion:   runtimeSDKVersions[rng.Intn(len(runtimeSDKVersions))],
		StreamingSDKVersion: streamingSDKVersions[rng.Intn(len(streamingSDKVersions))],
		OSType:              osType,
		KiroVersion:         kiroVersions[rng.Intn(len(kiroVersions))],
	}
}

// goOSToKiro maps Go's runtime.GOOS to kiro-cli OS type names.
func goOSToKiro(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return osTypes[0] // fallback
	}
}

// GenerateAccountKey returns a 16-char hex key derived from SHA256(seed).
func GenerateAccountKey(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:8])
}

// GetAccountKey derives an account key from clientID > refreshToken > random UUID.
func GetAccountKey(clientID, refreshToken string) string {
	// 1. Prefer ClientID
	if clientID != "" {
		return GenerateAccountKey(clientID)
	}

	// 2. Fallback to RefreshToken
	if refreshToken != "" {
		return GenerateAccountKey(refreshToken)
	}

	// 3. Random fallback
	return GenerateAccountKey(uuid.New().String())
}

// BuildUserAgent builds the User-Agent for streaming API requests.
// Format: aws-sdk-rust/{SDKVersion} api/codewhispererstreaming/{SDKVersion} os/{OSType} appVersion-{KiroVersion}
func (fp *Fingerprint) BuildUserAgent() string {
	return fmt.Sprintf(
		"aws-sdk-rust/%s api/codewhispererstreaming/%s os/%s appVersion-%s",
		fp.StreamingSDKVersion, fp.StreamingSDKVersion, fp.OSType, fp.KiroVersion,
	)
}

// BuildAmzUserAgent builds the x-amz-user-agent for streaming API requests.
// Format: aws-sdk-rust/{SDKVersion} appVersion-{KiroVersion}
func (fp *Fingerprint) BuildAmzUserAgent() string {
	return fmt.Sprintf("aws-sdk-rust/%s appVersion-%s", fp.StreamingSDKVersion, fp.KiroVersion)
}

// SetOIDCHeaders sets headers for AWS OIDC requests (login/token refresh).
// kiro-cli uses the same Rust SDK format for OIDC endpoints.
func SetOIDCHeaders(req *http.Request) {
	fp := GlobalFingerprintManager().GetFingerprint("oidc-session")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-amz-user-agent", fmt.Sprintf("aws-sdk-rust/%s appVersion-%s",
		fp.RuntimeSDKVersion, fp.KiroVersion))
	req.Header.Set("User-Agent", fmt.Sprintf(
		"aws-sdk-rust/%s api/sso-oidc/%s os/%s appVersion-%s",
		fp.RuntimeSDKVersion, fp.RuntimeSDKVersion, fp.OSType, fp.KiroVersion))
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("amz-sdk-request", "attempt=1; max=4")
}

// setRuntimeHeaders sets headers for Kiro management API requests
// (GetProfile, ListAvailableModels, GetUsageLimits).
func setRuntimeHeaders(req *http.Request, accessToken string, accountKey string) {
	fp := GlobalFingerprintManager().GetFingerprint(accountKey)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("x-amz-user-agent", fmt.Sprintf("aws-sdk-rust/%s appVersion-%s",
		fp.RuntimeSDKVersion, fp.KiroVersion))
	req.Header.Set("User-Agent", fmt.Sprintf(
		"aws-sdk-rust/%s api/codewhispererruntime/%s os/%s appVersion-%s",
		fp.RuntimeSDKVersion, fp.RuntimeSDKVersion, fp.OSType, fp.KiroVersion))
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("amz-sdk-request", "attempt=1; max=1")
}
