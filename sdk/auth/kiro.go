package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// KiroAuthenticator implements OAuth authentication for Kiro with Google login.
type KiroAuthenticator struct{}

// NewKiroAuthenticator constructs a Kiro authenticator.
func NewKiroAuthenticator() *KiroAuthenticator {
	return &KiroAuthenticator{}
}

// Provider returns the provider key for the authenticator.
func (a *KiroAuthenticator) Provider() string {
	return "kiro"
}

// RefreshLead indicates how soon before expiry a refresh should be attempted.
// Set to 20 minutes for proactive refresh before token expiry.
func (a *KiroAuthenticator) RefreshLead() *time.Duration {
	d := 20 * time.Minute
	return &d
}

// CreateAuthRecord creates an auth record from token data.
// This is the canonical way to build a coreauth.Auth from KiroTokenData.
func CreateAuthRecord(tokenData *kiroauth.KiroTokenData, source string) (*coreauth.Auth, error) {
	return createAuthRecord(tokenData, source)
}

// createAuthRecord creates an auth record from token data.
func createAuthRecord(tokenData *kiroauth.KiroTokenData, source string) (*coreauth.Auth, error) {
	// Parse expires_at
	expiresAt, err := time.Parse(time.RFC3339, tokenData.ExpiresAt)
	if err != nil {
		expiresAt = time.Now().Add(1 * time.Hour)
	}

	// Determine label based on auth method
	var label string
	switch tokenData.AuthMethod {
	case "idc":
		label = "kiro-idc"
	case "builder-id":
		label = "kiro-aws"
	default:
		label = fmt.Sprintf("kiro-%s", source)
	}

	// Use canonical filename generation
	fileName := kiroauth.GenerateTokenFileName(tokenData)

	now := time.Now()

	metadata := map[string]any{
		"type":          "kiro",
		"access_token":  tokenData.AccessToken,
		"refresh_token": tokenData.RefreshToken,
		"profile_arn":   tokenData.ProfileArn,
		"expires_at":    tokenData.ExpiresAt,
		"auth_method":   tokenData.AuthMethod,
		"provider":      tokenData.Provider,
		"client_id":     tokenData.ClientID,
		"client_secret": tokenData.ClientSecret,
		"email":         tokenData.Email,
		"last_refresh":  now.Format(time.RFC3339),
	}

	// Add IDC-specific fields if present
	if tokenData.StartURL != "" {
		metadata["start_url"] = tokenData.StartURL
	}
	if tokenData.Region != "" {
		metadata["region"] = tokenData.Region
	}

	attributes := map[string]string{
		"profile_arn": tokenData.ProfileArn,
		"source":      source,
		"email":       tokenData.Email,
	}

	// Add IDC-specific attributes if present
	if tokenData.AuthMethod == "idc" {
		attributes["source"] = "aws-idc"
		if tokenData.StartURL != "" {
			attributes["start_url"] = tokenData.StartURL
		}
		if tokenData.Region != "" {
			attributes["region"] = tokenData.Region
		}
	}

	record := &coreauth.Auth{
		ID:               fileName,
		Provider:         "kiro",
		FileName:         fileName,
		Label:            label,
		Status:           coreauth.StatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
		Metadata:         metadata,
		Attributes:       attributes,
		NextRefreshAfter: expiresAt.Add(-20 * time.Minute),
	}

	if tokenData.Email != "" {
		fmt.Printf("\n✓ Kiro authentication completed successfully! (Account: %s)\n", tokenData.Email)
	} else {
		fmt.Println("\n✓ Kiro authentication completed successfully!")
	}

	return record, nil
}

// Login performs OAuth login for Kiro with AWS (Builder ID or IDC).
// This shows a method selection prompt and handles both flows.
func (a *KiroAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kiro auth: configuration is required")
	}

	// Extract IDC options from metadata if present
	var idcOpts *kiroauth.IDCLoginOptions
	if opts != nil && opts.Metadata != nil {
		if startURL := opts.Metadata["start-url"]; startURL != "" {
			idcOpts = &kiroauth.IDCLoginOptions{
				StartURL:      startURL,
				Region:        opts.Metadata["region"],
				UseDeviceCode: opts.Metadata["flow"] == "device",
			}
		}
	}

	// Use the unified method selection flow (Builder ID or IDC)
	ssoClient := kiroauth.NewSSOOIDCClient(cfg)
	tokenData, err := ssoClient.LoginWithMethodSelection(ctx, idcOpts)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	return createAuthRecord(tokenData, "aws")
}

// LoginWithAuthCode performs OAuth login for Kiro with AWS Builder ID using authorization code flow.
// This provides a better UX than device code flow as it uses automatic browser callback.
func (a *KiroAuthenticator) LoginWithAuthCode(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kiro auth: configuration is required")
	}

	oauth := kiroauth.NewKiroOAuth(cfg)

	// Use AWS Builder ID authorization code flow
	tokenData, err := oauth.LoginWithBuilderIDAuthCode(ctx)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	record, errRecord := createAuthRecord(tokenData, "aws")
	if errRecord != nil {
		return nil, errRecord
	}
	// Override source attribute for auth code flow
	record.Attributes["source"] = "aws-builder-id-authcode"
	return record, nil
}

// LoginWithSocialSelection prompts the user to choose between Google and GitHub social login.
func (a *KiroAuthenticator) LoginWithSocialSelection(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kiro auth: configuration is required")
	}

	client := kiroauth.NewSocialAuthClient(cfg)
	tokenData, err := client.LoginWithSocialSelection(ctx)
	if err != nil {
		return nil, err
	}
	return createAuthRecord(tokenData, "social")
}

// ImportFromKiroIDE imports token from Kiro IDE's token file.
func (a *KiroAuthenticator) ImportFromKiroIDE(ctx context.Context, cfg *config.Config) (*coreauth.Auth, error) {
	tokenData, err := kiroauth.LoadKiroIDEToken()
	if err != nil {
		return nil, fmt.Errorf("failed to load Kiro IDE token: %w", err)
	}

	// Extract email from JWT if not already set (for imported tokens)
	if tokenData.Email == "" {
		tokenData.Email = kiroauth.ExtractEmailFromJWT(tokenData.AccessToken)
	}

	record, errRecord := createAuthRecord(tokenData, "imported")
	if errRecord != nil {
		return nil, errRecord
	}

	// Override source attribute for IDE import
	record.Attributes["source"] = "kiro-ide-import"
	record.Attributes["region"] = tokenData.Region

	// Store client_id_hash and region/start_url in metadata for imported tokens
	record.Metadata["client_id_hash"] = tokenData.ClientIDHash
	record.Metadata["region"] = tokenData.Region
	record.Metadata["start_url"] = tokenData.StartURL

	// Display the email if extracted
	if tokenData.Email != "" {
		fmt.Printf("\n✓ Imported Kiro token from IDE (Provider: %s, Account: %s)\n", tokenData.Provider, tokenData.Email)
	} else {
		fmt.Printf("\n✓ Imported Kiro token from IDE (Provider: %s)\n", tokenData.Provider)
	}

	return record, nil
}

// Refresh refreshes an expired Kiro token using AWS SSO OIDC.
func (a *KiroAuthenticator) Refresh(ctx context.Context, cfg *config.Config, auth *coreauth.Auth) (*coreauth.Auth, error) {
	if auth == nil || auth.Metadata == nil {
		return nil, fmt.Errorf("invalid auth record")
	}

	refreshToken, ok := auth.Metadata["refresh_token"].(string)
	if !ok || refreshToken == "" {
		return nil, fmt.Errorf("refresh token not found")
	}

	clientID, _ := auth.Metadata["client_id"].(string)
	clientSecret, _ := auth.Metadata["client_secret"].(string)
	clientIDHash, _ := auth.Metadata["client_id_hash"].(string)
	authMethod, _ := auth.Metadata["auth_method"].(string)
	startURL, _ := auth.Metadata["start_url"].(string)
	region, _ := auth.Metadata["region"].(string)

	// For Enterprise Kiro IDE (IDC auth), try to load clientId/clientSecret from device registration
	// if they are missing from metadata. This handles the case where token was imported without
	// clientId/clientSecret but has clientIdHash.
	if (clientID == "" || clientSecret == "") && clientIDHash != "" {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			if loadedClientID, loadedClientSecret, errLoad := kiroauth.LoadDeviceRegistration(homeDir, clientIDHash); errLoad == nil {
				clientID = loadedClientID
				clientSecret = loadedClientSecret
			}
		}
	}

	var tokenData *kiroauth.KiroTokenData
	var err error

	ssoClient := kiroauth.NewSSOOIDCClient(cfg)

	// Use SSO OIDC refresh for AWS Builder ID or IDC, otherwise use Kiro's OAuth refresh endpoint
	switch {
	case clientID != "" && clientSecret != "" && authMethod == "idc" && region != "":
		// IDC refresh with region-specific endpoint
		tokenData, err = ssoClient.RefreshTokenWithRegion(ctx, clientID, clientSecret, refreshToken, region, startURL)
	case clientID != "" && clientSecret != "" && (authMethod == "builder-id" || authMethod == "idc"):
		// Builder ID or IDC refresh with default endpoint (us-east-1)
		tokenData, err = ssoClient.RefreshToken(ctx, clientID, clientSecret, refreshToken)
	default:
		// Fallback to Kiro's refresh endpoint (for social auth: Google/GitHub)
		oauth := kiroauth.NewKiroOAuth(cfg)
		tokenData, err = oauth.RefreshToken(ctx, refreshToken)
	}

	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	// Parse expires_at
	expiresAt, err := time.Parse(time.RFC3339, tokenData.ExpiresAt)
	if err != nil {
		expiresAt = time.Now().Add(1 * time.Hour)
	}

	// Clone auth to avoid mutating the input parameter
	updated := auth.Clone()
	now := time.Now()
	updated.UpdatedAt = now
	updated.LastRefreshedAt = now
	updated.Metadata["access_token"] = tokenData.AccessToken
	updated.Metadata["refresh_token"] = tokenData.RefreshToken
	updated.Metadata["expires_at"] = tokenData.ExpiresAt
	updated.Metadata["last_refresh"] = now.Format(time.RFC3339) // For double-check optimization
	// Store clientId/clientSecret if they were loaded from device registration
	if clientID != "" && updated.Metadata["client_id"] == nil {
		updated.Metadata["client_id"] = clientID
	}
	if clientSecret != "" && updated.Metadata["client_secret"] == nil {
		updated.Metadata["client_secret"] = clientSecret
	}
	// NextRefreshAfter: 20 minutes before expiry
	updated.NextRefreshAfter = expiresAt.Add(-20 * time.Minute)

	return updated, nil
}
