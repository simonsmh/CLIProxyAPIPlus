package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBuildAuthURLUsesDefaultClientIDWhenEnvUnset(t *testing.T) {
	t.Setenv(ClientIDEnv, "")

	auth := NewAntigravityAuth(nil, &http.Client{})
	rawURL := auth.BuildAuthURL("state", "http://localhost:51121/oauth-callback")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}

	if got := parsed.Query().Get("client_id"); got != DefaultClientID {
		t.Fatalf("client_id = %q, want default", got)
	}
}

func TestBuildAuthURLUsesEnvClientID(t *testing.T) {
	t.Setenv(ClientIDEnv, "env-client-id")

	auth := NewAntigravityAuth(nil, &http.Client{})
	rawURL := auth.BuildAuthURL("state", "http://localhost:51121/oauth-callback")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}

	if got := parsed.Query().Get("client_id"); got != "env-client-id" {
		t.Fatalf("client_id = %q, want env override", got)
	}
}

func TestExchangeCodeForTokensUsesDefaultOAuthCredentialsWhenEnvUnset(t *testing.T) {
	t.Setenv(ClientIDEnv, "")
	t.Setenv(ClientSecretEnv, "")

	auth := NewAntigravityAuth(nil, &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != TokenEndpoint {
			t.Fatalf("token URL = %s, want %s", req.URL.String(), TokenEndpoint)
		}
		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", got)
		}
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read body: %v", errRead)
		}
		values, errParse := url.ParseQuery(string(body))
		if errParse != nil {
			t.Fatalf("parse form: %v", errParse)
		}
		if got := values.Get("client_id"); got != DefaultClientID {
			t.Fatalf("client_id = %q, want default", got)
		}
		if got := values.Get("client_secret"); got != DefaultClientSecret {
			t.Fatalf("client_secret = %q, want default", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)),
		}, nil
	})})

	token, err := auth.ExchangeCodeForTokens(context.Background(), "code", "http://localhost:51121/oauth-callback")
	if err != nil {
		t.Fatalf("ExchangeCodeForTokens error: %v", err)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" {
		t.Fatalf("unexpected token response: %+v", token)
	}
}

func TestExchangeCodeForTokensUsesEnvOAuthCredentials(t *testing.T) {
	t.Setenv(ClientIDEnv, "env-client-id")
	t.Setenv(ClientSecretEnv, "env-client-secret")

	auth := NewAntigravityAuth(nil, &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read body: %v", errRead)
		}
		values, errParse := url.ParseQuery(string(body))
		if errParse != nil {
			t.Fatalf("parse form: %v", errParse)
		}
		if got := values.Get("client_id"); got != "env-client-id" {
			t.Fatalf("client_id = %q, want env override", got)
		}
		if got := values.Get("client_secret"); got != "env-client-secret" {
			t.Fatalf("client_secret = %q, want env override", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)),
		}, nil
	})})

	if _, err := auth.ExchangeCodeForTokens(context.Background(), "code", "http://localhost:51121/oauth-callback"); err != nil {
		t.Fatalf("ExchangeCodeForTokens error: %v", err)
	}
}

func TestFetchProjectIDFromLoadCodeAssist(t *testing.T) {
	auth := NewAntigravityAuth(nil, &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" {
			t.Fatalf("unexpected request URL: %s", req.URL.String())
		}
		assertLoadCodeAssistHeaders(t, req)
		assertJSONContains(t, req, `"ideType":"ANTIGRAVITY"`)
		return jsonResponse(`{"cloudaicompanionProject":"cogent-snow-4mnnp"}`), nil
	})})

	projectID, err := auth.FetchProjectID(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("FetchProjectID error: %v", err)
	}
	if projectID != "cogent-snow-4mnnp" {
		t.Fatalf("projectID = %q", projectID)
	}
}

func TestFetchProjectIDFallsBackToDailyOnboardUser(t *testing.T) {
	var sawOnboard bool
	auth := NewAntigravityAuth(nil, &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist":
			assertLoadCodeAssistHeaders(t, req)
			return jsonResponse(`{"allowedTiers":[{"id":"free-tier","isDefault":true}]}`), nil
		case "https://daily-cloudcode-pa.googleapis.com/v1internal:onboardUser":
			sawOnboard = true
			assertOnboardUserHeaders(t, req)
			assertJSONContains(t, req, `"tier_id":"free-tier"`)
			assertJSONContains(t, req, `"ide_type":"ANTIGRAVITY"`)
			return jsonResponse(`{
				"done": true,
				"response": {
					"cloudaicompanionProject": {
						"id": "cogent-snow-4mnnp",
						"name": "cogent-snow-4mnnp",
						"projectNumber": "22597072101"
					}
				}
			}`), nil
		default:
			t.Fatalf("unexpected request URL: %s", req.URL.String())
			return nil, nil
		}
	})})

	projectID, err := auth.FetchProjectID(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("FetchProjectID error: %v", err)
	}
	if !sawOnboard {
		t.Fatalf("expected onboardUser fallback")
	}
	if projectID != "cogent-snow-4mnnp" {
		t.Fatalf("projectID = %q", projectID)
	}
}

func assertLoadCodeAssistHeaders(t *testing.T, req *http.Request) {
	t.Helper()
	if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("Accept"); got != "*/*" {
		t.Fatalf("Accept = %q", got)
	}
	if got := req.Header.Get("X-Goog-Api-Client"); got != "" {
		t.Fatalf("X-Goog-Api-Client = %q, want empty", got)
	}
	if got := req.Header.Get("User-Agent"); strings.Contains(got, "google-api-nodejs-client/") {
		t.Fatalf("User-Agent = %q", got)
	}
}

func assertOnboardUserHeaders(t *testing.T, req *http.Request) {
	t.Helper()
	if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("Accept"); got != "*/*" {
		t.Fatalf("Accept = %q", got)
	}
	if got := req.Header.Get("X-Goog-Api-Client"); got != "gl-node/22.21.1" {
		t.Fatalf("X-Goog-Api-Client = %q", got)
	}
	if got := req.Header.Get("User-Agent"); !strings.Contains(got, "google-api-nodejs-client/10.3.0") {
		t.Fatalf("User-Agent = %q", got)
	}
}

func assertJSONContains(t *testing.T, req *http.Request, want string) {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyText := string(body)
	req.Body = io.NopCloser(strings.NewReader(bodyText))
	if !strings.Contains(bodyText, want) {
		t.Fatalf("body missing %s: %s", want, bodyText)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
