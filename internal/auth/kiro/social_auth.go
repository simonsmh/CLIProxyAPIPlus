// Package kiro provides social authentication (Google/GitHub) for Kiro via AuthServiceClient.
package kiro

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	log "github.com/sirupsen/logrus"
	"golang.org/x/term"
)

const (
	// Kiro AuthService endpoint
	kiroAuthServiceEndpoint = "https://prod.us-east-1.auth.desktop.kiro.dev"

	// OAuth timeout
	socialAuthTimeout = 10 * time.Minute

	// Default callback port for social auth HTTP server
	socialAuthCallbackPort = 3128
)

// SocialProvider represents the social login provider.
type SocialProvider string

const (
	// ProviderGoogle is Google OAuth provider
	ProviderGoogle SocialProvider = "Google"
	// ProviderGitHub is GitHub OAuth provider
	ProviderGitHub SocialProvider = "Github"
	// Note: AWS Builder ID is NOT supported by Kiro's auth service.
	// It only supports: Google, Github, Cognito
	// AWS Builder ID must use device code flow via SSO OIDC.
)

// CreateTokenRequest is sent to Kiro's /oauth/token endpoint.
type CreateTokenRequest struct {
	Code           string `json:"code"`
	CodeVerifier   string `json:"code_verifier"`
	RedirectURI    string `json:"redirect_uri"`
	InvitationCode string `json:"invitation_code,omitempty"`
}

// SocialTokenResponse from Kiro's /oauth/token endpoint for social auth.
type SocialTokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ProfileArn   string `json:"profileArn"`
	ExpiresIn    int    `json:"expiresIn"`
}

// RefreshTokenRequest is sent to Kiro's /refreshToken endpoint.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// WebCallbackResult contains the OAuth callback result from HTTP server.
type WebCallbackResult struct {
	Code        string
	State       string
	Error       string
	RedirectURI string
	LoginOption string
	TokenData   *KiroTokenData // Set when IDC/BuilderID flow completes inside callback handler
}

// SocialAuthClient handles social authentication with Kiro.
type SocialAuthClient struct {
	httpClient      *http.Client
	cfg             *config.Config
	protocolHandler *ProtocolHandler
}

// NewSocialAuthClient creates a new social auth client.
func NewSocialAuthClient(cfg *config.Config) *SocialAuthClient {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	return &SocialAuthClient{
		httpClient:      client,
		cfg:             cfg,
		protocolHandler: NewProtocolHandler(),
	}
}

// startWebCallbackServer starts a local HTTP server to receive the OAuth callback.
// This is used instead of the kiro:// protocol handler to avoid redirect_mismatch errors.
// Returns the redirect URI, a result channel, a shutdown function, and any error.
func (c *SocialAuthClient) startWebCallbackServer(ctx context.Context, expectedState string) (string, <-chan WebCallbackResult, func(), error) {
	// Try to find an available port - use localhost like Kiro does
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", socialAuthCallbackPort))
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to start callback server on port %d: %w", socialAuthCallbackPort, err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	// Use http scheme for local callback server.
	// Kiro's registered Cognito redirect URI is http://localhost:3128
	redirectURI := fmt.Sprintf("http://localhost:%d", port)
	resultChan := make(chan WebCallbackResult, 1)

	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdownServer := func() {
		_ = server.Shutdown(context.Background())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only handle root path, oauth/callback, or signin/callback
		if r.URL.Path != "/" && r.URL.Path != "/oauth/callback" && r.URL.Path != "/signin/callback" {
			http.NotFound(w, r)
			return
		}

		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Login Failed</title></head>
<body><h1>Login Failed</h1><p>%s</p><p>You can close this window.</p></body></html>`, html.EscapeString(errParam))
			resultChan <- WebCallbackResult{Error: errParam}
			return
		}

		if state != expectedState {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Login Failed</title></head>
<body><h1>Login Failed</h1><p>Invalid state parameter</p><p>You can close this window.</p></body></html>`)
			resultChan <- WebCallbackResult{Error: "state mismatch"}
			return
		}

		loginOption := r.URL.Query().Get("login_option")

		// Check for IDC/BuilderID callback (issuer_url present, no code)
		// Must be checked BEFORE writing response body, otherwise redirect headers cannot be set.
		issuerURL := r.URL.Query().Get("issuer_url")
		if issuerURL != "" {
			idcRegion := r.URL.Query().Get("idc_region")
			c.handleIDCCallback(ctx, w, issuerURL, idcRegion, loginOption, resultChan)
			return
		}

		// Social login (Google/GitHub): write success response
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Login Successful</title></head>
<body><h1>Login Successful!</h1><p>You can close this window and return to the terminal.</p>
<script>window.close();</script></body></html>`)

		// The actual redirect URI registered on AWS Cognito includes query parameters
		path := r.URL.Path
		if path == "/" {
			path = ""
		}
		actualRedirectURI := fmt.Sprintf("http://localhost:3128%s", path)
		if loginOption != "" {
			actualRedirectURI = fmt.Sprintf("%s?login_option=%s", actualRedirectURI, loginOption)
		}

		resultChan <- WebCallbackResult{Code: code, State: state, RedirectURI: actualRedirectURI, LoginOption: loginOption}
	})

	server.Handler = mux

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Debugf("kiro social auth callback server error: %v", err)
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(socialAuthTimeout):
		}
		_ = server.Shutdown(context.Background())
	}()

	return redirectURI, resultChan, shutdownServer, nil
}

// handleIDCCallback handles IDC/BuilderID callback by starting the SSO OIDC device code flow.
// This is triggered when the Kiro signin page redirects back with issuer_url parameter.
// The browser is redirected to the device verification URL, and the device code is polled in background.
func (c *SocialAuthClient) handleIDCCallback(ctx context.Context, w http.ResponseWriter, issuerURL, idcRegion, loginOption string, resultChan chan<- WebCallbackResult) {
	if idcRegion == "" {
		idcRegion = "us-east-1"
	}

	log.Debugf("kiro social auth: IDC/BuilderID callback received, issuer_url=%s, login_option=%s, region=%s", issuerURL, loginOption, idcRegion)

	ssoClient := NewSSOOIDCClient(c.cfg)

	// Step 1: Register OIDC client
	regResp, err := ssoClient.RegisterClientWithRegion(ctx, idcRegion)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Login Failed</title></head><body><h1>Login Failed</h1><p>Failed to register OIDC client.</p></body></html>`)
		resultChan <- WebCallbackResult{Error: fmt.Sprintf("IDC client registration failed: %v", err)}
		return
	}

	// Step 2: Start device authorization
	authResp, err := ssoClient.StartDeviceAuthorizationWithIDC(ctx, regResp.ClientID, regResp.ClientSecret, issuerURL, idcRegion)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Login Failed</title></head><body><h1>Login Failed</h1><p>Failed to start device authorization.</p></body></html>`)
		resultChan <- WebCallbackResult{Error: fmt.Sprintf("IDC device authorization failed: %v", err)}
		return
	}

	// Step 3: Redirect browser to device verification URL
	w.Header().Set("Location", authResp.VerificationURIComplete)
	w.WriteHeader(http.StatusFound)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Redirecting</title></head><body><p>Redirecting to device verification...</p><p>If not redirected, <a href="%s">click here</a>.</p></body></html>`, html.EscapeString(authResp.VerificationURIComplete))

	fmt.Printf("\n  AWS device code: %s\n", authResp.UserCode)
	fmt.Println("  Confirm the code in the browser...")

	// Step 4: Poll for device code in background
	go func() {
		interval := pollInterval
		if authResp.Interval > 0 {
			interval = time.Duration(authResp.Interval) * time.Second
		}
		deadline := time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)

		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				resultChan <- WebCallbackResult{Error: "cancelled"}
				return
			case <-time.After(interval):
				tokenResp, err := ssoClient.CreateTokenWithRegion(ctx, regResp.ClientID, regResp.ClientSecret, authResp.DeviceCode, idcRegion)
				if err != nil {
					if errors.Is(err, ErrAuthorizationPending) {
						fmt.Print(".")
						continue
					}
					if errors.Is(err, ErrSlowDown) {
						interval += 5 * time.Second
						continue
					}
					resultChan <- WebCallbackResult{Error: fmt.Sprintf("IDC token creation failed: %v", err)}
					return
				}

				fmt.Println("\n\n✓ AWS authorization successful!")

				// Fetch profile and email
				profileArn := ssoClient.FetchProfileArn(ctx, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken, idcRegion)
				email := FetchUserEmailWithFallback(ctx, c.cfg, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken)

				// Fallback to default Builder ID profile ARN if FetchProfileArn returns empty
				if profileArn == "" {
					profileArn = DefaultBuilderIDProfileArn
					log.Debug("kiro social auth: FetchProfileArn returned empty, using default Builder ID profile ARN")
				}

				expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

				authMethod := "idc"
				if loginOption == "builderid" {
					authMethod = "builder-id"
				}

				resultChan <- WebCallbackResult{
					TokenData: &KiroTokenData{
						AccessToken:  tokenResp.AccessToken,
						RefreshToken: tokenResp.RefreshToken,
						ProfileArn:   profileArn,
						ExpiresAt:    expiresAt.Format(time.RFC3339),
						AuthMethod:   authMethod,
						Provider:     "AWS",
						ClientID:     regResp.ClientID,
						ClientSecret: regResp.ClientSecret,
						Email:        email,
						StartURL:     issuerURL,
						Region:       idcRegion,
					},
					LoginOption: loginOption,
				}
				return
			}
		}
		resultChan <- WebCallbackResult{Error: "AWS authorization timed out"}
	}()
}

// generatePKCE generates PKCE code verifier and challenge.
func generatePKCE() (verifier, challenge string, err error) {
	// Generate 32 bytes of random data for verifier
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)

	// Generate SHA256 hash of verifier for challenge
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}

// generateState generates a random state parameter.
func generateStateParam() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// buildLoginURL constructs the Kiro OAuth login URL.
// The login endpoint expects a GET request with query parameters.
// If provider is empty, login_option is omitted so the user can choose in the browser.
func (c *SocialAuthClient) buildLoginURL(provider, redirectURI, codeChallenge, state string) string {
	baseURL := fmt.Sprintf("https://app.kiro.dev/signin?state=%s&code_challenge=%s&code_challenge_method=S256&redirect_uri=%s&redirect_from=kirocli",
		state,
		codeChallenge,
		url.QueryEscape(redirectURI),
	)
	if provider != "" {
		loginOption := strings.ToLower(provider)
		baseURL += fmt.Sprintf("&login_option=%s", loginOption)
	}
	return baseURL
}

// CreateToken exchanges the authorization code for tokens.
func (c *SocialAuthClient) CreateToken(ctx context.Context, req *CreateTokenRequest) (*SocialTokenResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token request: %w", err)
	}

	tokenURL := kiroAuthServiceEndpoint + "/oauth/token"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	SetDesktopRefreshHeaders(httpReq)
	httpReq.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("token exchange failed (status %d)", resp.StatusCode)
		return nil, fmt.Errorf("token exchange failed (status %d)", resp.StatusCode)
	}

	var tokenResp SocialTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshSocialToken refreshes an expired social auth token.
func (c *SocialAuthClient) RefreshSocialToken(ctx context.Context, refreshToken string) (*KiroTokenData, error) {
	body, err := json.Marshal(&RefreshTokenRequest{RefreshToken: refreshToken})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	refreshURL := kiroAuthServiceEndpoint + "/refreshToken"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	SetDesktopRefreshHeaders(httpReq)
	httpReq.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Debugf("token refresh failed (status %d)", resp.StatusCode)
		return nil, fmt.Errorf("token refresh failed (status %d)", resp.StatusCode)
	}

	var tokenResp SocialTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	// Validate ExpiresIn - use default 1 hour if invalid
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // Default 1 hour
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	return &KiroTokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ProfileArn:   tokenResp.ProfileArn,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		AuthMethod:   "social",
		Provider:     "", // Caller should preserve original provider
		Region:       "us-east-1",
	}, nil
}

// LoginWithSocial performs OAuth login with Google or GitHub.
// Uses local HTTP callback server instead of custom protocol handler to avoid redirect_mismatch errors.
func (c *SocialAuthClient) LoginWithSocial(ctx context.Context, provider SocialProvider) (*KiroTokenData, error) {
	providerName := string(provider)

	fmt.Println("\n╔══════════════════════════════════════════════════════════╗")
	if providerName != "" {
		fmt.Printf("║         Kiro Authentication (%s)                    ║\n", providerName)
	} else {
		fmt.Println("║              Kiro Authentication                       ║")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	// Step 1: Start local HTTP callback server (instead of kiro:// protocol handler)
	// This avoids redirect_mismatch errors with AWS Cognito
	fmt.Println("\nSetting up authentication...")

	// Step 2: Generate PKCE codes
	codeVerifier, codeChallenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	// Step 3: Generate state
	state, err := generateStateParam()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Step 4: Start local HTTP callback server
	redirectURI, resultChan, shutdownServer, err := c.startWebCallbackServer(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("failed to start callback server: %w", err)
	}
	defer shutdownServer()
	log.Debugf("kiro social auth: callback server started at %s", redirectURI)

	// Step 5: Build the login URL using HTTP redirect URI
	authURL := c.buildLoginURL(providerName, redirectURI, codeChallenge, state)

	// Set incognito mode based on config (defaults to true for Kiro, can be overridden with --no-incognito)
	// Incognito mode enables multi-account support by bypassing cached sessions
	if c.cfg != nil {
		browser.SetIncognitoMode(c.cfg.IncognitoBrowser)
		if !c.cfg.IncognitoBrowser {
			log.Info("kiro: using normal browser mode (--no-incognito). Note: You may not be able to select a different account.")
		} else {
			log.Debug("kiro: using incognito mode for multi-account support")
		}
	} else {
		browser.SetIncognitoMode(true) // Default to incognito if no config
		log.Debug("kiro: using incognito mode for multi-account support (default)")
	}

	// Step 6: Open browser for user authentication
	fmt.Println("\n════════════════════════════════════════════════════════════")
	if providerName != "" {
		fmt.Printf("  Opening browser for %s authentication...\n", providerName)
	} else {
		fmt.Println("  Opening browser for authentication (choose Google or GitHub)...")
	}
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Printf("\n  URL: %s\n\n", authURL)

	if err := browser.OpenURL(authURL); err != nil {
		log.Warnf("Could not open browser automatically: %v", err)
		fmt.Println("  ⚠ Could not open browser automatically.")
		fmt.Println("  Please open the URL above in your browser manually.")
	} else {
		fmt.Println("  (Browser opened automatically)")
	}

	fmt.Println("\n  Waiting for authentication callback...")

	// Step 7: Wait for callback from HTTP server
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(socialAuthTimeout):
		return nil, fmt.Errorf("authentication timed out")
	case callback := <-resultChan:
		if callback.Error != "" {
			return nil, fmt.Errorf("authentication error: %s", callback.Error)
		}

		// If IDC/BuilderID flow completed inside callback handler, return directly
		if callback.TokenData != nil {
			fmt.Println("\n✓ AWS authentication successful!")
			if callback.TokenData.Email != "" {
				fmt.Printf("  Logged in as: %s\n", callback.TokenData.Email)
			}
			return callback.TokenData, nil
		}

		// State is already validated by the callback server
		if callback.Code == "" {
			return nil, fmt.Errorf("no authorization code received")
		}

		fmt.Println("\n✓ Authorization received!")

		// Step 8: Exchange code for tokens
		fmt.Println("Exchanging code for tokens...")

		tokenReq := &CreateTokenRequest{
			Code:         callback.Code,
			CodeVerifier: codeVerifier,
			RedirectURI:  callback.RedirectURI,
		}

		tokenResp, err := c.CreateToken(ctx, tokenReq)
		if err != nil {
			return nil, fmt.Errorf("failed to exchange code for tokens: %w", err)
		}

		fmt.Println("\n✓ Authentication successful!")

		// Close the browser window
		if err := browser.CloseBrowser(); err != nil {
			log.Debugf("Failed to close browser: %v", err)
		}

		// Validate ExpiresIn - use default 1 hour if invalid
		expiresIn := tokenResp.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 3600
		}
		expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

		// Try multiple methods to extract email, then fall back to manual prompt
		email := cwFetchEmail(ctx, c.cfg, tokenResp.AccessToken, "", tokenResp.RefreshToken, tokenResp.ProfileArn)
		if email == "" {
			email = promptAccountLabel()
		}

		// Resolve provider: prefer CLI selection, fall back to callback's login_option
		resolvedProvider := providerName
		if resolvedProvider == "" {
			resolvedProvider = callback.LoginOption
		}

		return &KiroTokenData{
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ProfileArn:   tokenResp.ProfileArn,
			ExpiresAt:    expiresAt.Format(time.RFC3339),
			AuthMethod:   "social",
			Provider:     resolvedProvider,
			Email:        email,
			Region:       "us-east-1",
		}, nil
	}
}

// LoginWithSocialSelection opens the Kiro signin page without pre-selecting a provider.
// The user chooses between Google and GitHub in the browser.
func (c *SocialAuthClient) LoginWithSocialSelection(ctx context.Context) (*KiroTokenData, error) {
	return c.LoginWithSocial(ctx, "")
}

// LoginWithGoogle performs OAuth login with Google.
func (c *SocialAuthClient) LoginWithGoogle(ctx context.Context) (*KiroTokenData, error) {
	return c.LoginWithSocial(ctx, ProviderGoogle)
}

// LoginWithGitHub performs OAuth login with GitHub.
func (c *SocialAuthClient) LoginWithGitHub(ctx context.Context) (*KiroTokenData, error) {
	return c.LoginWithSocial(ctx, ProviderGitHub)
}

// forceDefaultProtocolHandler sets our protocol handler as the default for kiro:// URLs.
// This prevents the "Open with" dialog from appearing on Linux.
// On non-Linux platforms, this is a no-op as they use different mechanisms.
func forceDefaultProtocolHandler() {
	if runtime.GOOS != "linux" {
		return // Non-Linux platforms use different handler mechanisms
	}

	// Set our handler as default using xdg-mime
	cmd := exec.Command("xdg-mime", "default", "kiro-oauth-handler.desktop", "x-scheme-handler/kiro")
	if err := cmd.Run(); err != nil {
		log.Warnf("Failed to set default protocol handler: %v. You may see a handler selection dialog.", err)
	}
}

// isInteractiveTerminal checks if stdin is connected to an interactive terminal.
// Returns false in CI/automated environments or when stdin is piped.
func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptAccountLabel prompts the user for an account label when email cannot be auto-detected.
// Only prompts in interactive terminal mode. Returns empty string if skipped or non-interactive.
func promptAccountLabel() string {
	if !isInteractiveTerminal() {
		return ""
	}
	fmt.Print("\n  Enter account label for file naming (optional, press Enter to skip): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		log.Debugf("Failed to read account label: %v", err)
		return ""
	}
	return strings.TrimSpace(input)
}
