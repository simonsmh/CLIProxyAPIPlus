---
name: add-auth-provider
description: Guide for adding a new Auth Provider to CLIProxyAPI Plus. Covers the full development flow including auth handler, SDK authenticator, login command, CLI flag registration, and access provider registration. Use when developing new OAuth/auth providers, adding login commands, or registering new authenticators in the project.
---

# Add Auth Provider Development Guide

## Overview

Adding a new Auth Provider to CLIProxyAPI Plus involves changes across 6 key locations. This guide provides the complete checklist based on the proven pattern (e.g., Kiro, Codex, Claude providers).

## Architecture Layers

```
cmd/server/main.go          → CLI flags + command dispatch
internal/cmd/<provider>_login.go → Login command entry points
internal/cmd/auth_manager.go     → Authenticator registration
sdk/auth/<provider>.go           → SDK Authenticator (implements Authenticator interface)
internal/auth/<provider>/        → Core auth logic (OAuth, token storage, refresh)
internal/access/config_access/   → Access provider registration (if needed)
```

## Implementation Checklist

### Task 1: Core Auth Logic (`internal/auth/<provider>/`)

Create the provider directory with:

- **`token.go`** — Token data structures (e.g., `KiroTokenData`)
- **`oauth.go`** — OAuth flow implementation (device code, auth code, social login)
- **`token_storage.go`** — Implement `auth.TokenStorage` interface:
  ```go
  type TokenStorage interface {
      SaveTokenToFile(authFilePath string) error
  }
  ```
- Additional files as needed: refresh logic, rate limiting, fingerprinting, etc.

### Task 2: SDK Authenticator (`sdk/auth/<provider>.go`)

Implement the `Authenticator` interface from `sdk/auth/interfaces.go`:

```go
type Authenticator interface {
    Provider() string                                          // Provider key, e.g. "kiro"
    Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error)
    RefreshLead() *time.Duration                              // How soon before expiry to refresh
}
```

Key responsibilities:
- Create `*coreauth.Auth` record with proper `ID`, `FileName`, `Label`, `Metadata`, `Attributes`
- Implement `Refresh()` method if token refresh is supported
- File naming convention: `<provider>-<identifier>.json`

### Task 3: Login Command (`internal/cmd/<provider>_login.go`)

Create login entry point functions following the pattern:

```go
func Do<Provider>Login(cfg *config.Config, options *LoginOptions) {
    manager := newAuthManager()
    authenticator := sdkAuth.New<Provider>Authenticator()
    record, err := authenticator.Login(context.Background(), cfg, &sdkAuth.LoginOptions{
        NoBrowser: options.NoBrowser,
        Metadata:  map[string]string{},
        Prompt:    options.Prompt,
    })
    if err != nil {
        log.Errorf("<Provider> authentication failed: %v", err)
        // Print troubleshooting steps
        return
    }
    savedPath, err := manager.SaveAuth(record, cfg)
    // Handle save result...
}
```

If multiple login methods exist (e.g., social, AWS, import), create separate functions for each.

### Task 4: Register Authenticator (`internal/cmd/auth_manager.go`)

Add the new authenticator to `newAuthManager()`:

```go
func newAuthManager() *sdkAuth.Manager {
    store := sdkAuth.GetTokenStore()
    manager := sdkAuth.NewManager(store,
        // ... existing authenticators ...
        sdkAuth.New<Provider>Authenticator(),  // ADD THIS
    )
    return manager
}
```

### Task 5: CLI Flag Registration (`cmd/server/main.go`)

Three changes required:

**5a.** Add flag fields to `commandModeOptions` struct:
```go
type commandModeOptions struct {
    // ... existing fields ...
    <provider>Login bool   // ADD THIS
}
```

**5b.** Add to `isOneShotCommandMode()`:
```go
func isOneShotCommandMode(opts commandModeOptions) bool {
    return // ... existing checks ...
        opts.<provider>Login   // ADD THIS
}
```

**5c.** Add flag definition and dispatch in `main()`:
```go
// Flag definition
var <provider>Login bool
flag.BoolVar(&<provider>Login, "<provider>-login", false, "Login to <Provider> using OAuth")

// Command dispatch (in the if/else chain)
} else if <provider>Login {
    cmd.Do<Provider>Login(cfg, options)
}
```

### Task 6: Config (if needed)

If the provider requires config fields, add them to:
- `internal/config/config.go` — Config struct fields
- `config.example.yaml` — Example configuration

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---------|---------|-----|
| Missing `newAuthManager()` registration | Login works but saved tokens not found | Add authenticator to `auth_manager.go` |
| Missing `isOneShotCommandMode()` | Server starts instead of running login | Add flag check to the function |
| Missing `commandModeOptions` field | Compilation error | Add field to struct |
| Wrong file naming in `createAuthRecord` | Duplicate tokens or overwrite issues | Use unique identifier (email, account ID) |
| Forgetting `RefreshLead()` | Tokens never refresh proactively | Return a duration like `20 * time.Minute` |

## Verification Commands

```bash
# Build check (REQUIRED after changes)
go build -o cli-proxy-api ./cmd/server

# Run login
./cli-proxy-api --<provider>-login

# Run with no-browser mode
./cli-proxy-api --<provider>-login --no-browser
```

## Reference Implementations

- **Simple provider** (single login method): `sdk/auth/xai.go`, `internal/cmd/xai_login.go`
- **Complex provider** (multiple login methods): `sdk/auth/kiro.go`, `internal/cmd/kiro_login.go`
- **Device code flow**: `sdk/auth/codex_device.go`, `internal/auth/codex/`
