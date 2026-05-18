package kiro

import (
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func resetKiroRuntimeConfigTestState() {
	globalRateLimiter = nil
	globalRateLimiterCfg = nil
	globalRateLimiterOnce = sync.Once{}
}

func TestGlobalRateLimiter_Defaults(t *testing.T) {
	resetKiroRuntimeConfigTestState()
	t.Cleanup(resetKiroRuntimeConfigTestState)

	enabled := true
	SetGlobalRateLimiterConfig(&RateLimiterConfig{Enabled: &enabled})
	rl := GetGlobalRateLimiter()

	if rl.minTokenInterval != DefaultMinTokenInterval {
		t.Fatalf("expected default min interval %v, got %v", DefaultMinTokenInterval, rl.minTokenInterval)
	}
	if rl.maxTokenInterval != DefaultMaxTokenInterval {
		t.Fatalf("expected default max interval %v, got %v", DefaultMaxTokenInterval, rl.maxTokenInterval)
	}

	rl.MarkTokenFailed("test-token")
	if state := rl.GetTokenState("test-token"); state == nil {
		t.Fatal("expected rate limiter to track failure")
	}
}

func TestGlobalRateLimiter_Reconfigure(t *testing.T) {
	resetKiroRuntimeConfigTestState()
	t.Cleanup(resetKiroRuntimeConfigTestState)

	rl := GetGlobalRateLimiter()

	enabled := true
	SetGlobalRateLimiterConfig(&RateLimiterConfig{
		Enabled:          &enabled,
		MinTokenInterval: 5 * time.Second,
		MaxTokenInterval: 7 * time.Second,
	})

	if !rl.enabled {
		t.Fatal("expected rate limiter to be enabled after reconfigure")
	}
	if rl.minTokenInterval != 5*time.Second || rl.maxTokenInterval != 7*time.Second {
		t.Fatalf("unexpected interval config: min=%v max=%v", rl.minTokenInterval, rl.maxTokenInterval)
	}
}

func TestInitFingerprintConfig(t *testing.T) {
	resetKiroRuntimeConfigTestState()
	t.Cleanup(resetKiroRuntimeConfigTestState)

	InitFingerprintConfig(&config.Config{
		KiroFingerprint: &config.KiroFingerprintConfig{
			RuntimeSDKVersion: "3.0.0",
			KiroVersion:       "1.0.0",
			KiroHash:          "abc123",
		},
	})
	// Verify fingerprint manager was initialized
	fm := GlobalFingerprintManager()
	if fm == nil {
		t.Fatal("expected fingerprint manager to be initialized")
	}
}
