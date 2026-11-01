// Package google implements the Google Calendar backend and OAuth2 desktop flow.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"golang.org/x/oauth2"
)

// OAuthClient is a parsed Google OAuth desktop client credential.
type OAuthClient struct {
	ClientID     string
	ClientSecret string
	AuthURI      string
	TokenURI     string
}

type credentialPayload struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AuthURI      string `json:"auth_uri"`
	TokenURI     string `json:"token_uri"`
}

type credentialsFile struct {
	Installed *credentialPayload `json:"installed"`
	Web       *credentialPayload `json:"web"`
}

// ParseClient parses a Google credentials JSON document.
func ParseClient(raw []byte) (OAuthClient, error) {
	var file credentialsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return OAuthClient{}, err
	}
	var creds credentialPayload
	switch {
	case file.Installed != nil:
		creds = *file.Installed
	case file.Web != nil:
		creds = *file.Web
	default:
		if err := json.Unmarshal(raw, &creds); err != nil {
			return OAuthClient{}, err
		}
	}
	if creds.ClientID == "" {
		return OAuthClient{}, fmt.Errorf("credentials file is missing client_id")
	}
	if creds.AuthURI == "" {
		creds.AuthURI = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if creds.TokenURI == "" {
		creds.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return OAuthClient(creds), nil
}

// LoadClient reads a credentials file.
func LoadClient(path string) (OAuthClient, error) {
	warnIfPermissive(path, "Google credentials")
	raw, err := os.ReadFile(path)
	if err != nil {
		return OAuthClient{}, err
	}
	return ParseClient(raw)
}

// warnIfPermissive logs a warning when a secret file is readable by group or
// other users. It never fails: deployments on unusual filesystems or platforms
// may not expose POSIX permission bits.
func warnIfPermissive(path, label string) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if permission := info.Mode().Perm(); permission&0o077 != 0 {
		slog.Warn("secret file is readable by other users", "label", label, "path", path, "mode", permission.String())
	}
}

// Scopes requested by the agent. Only the event read/write scope and
// read-only calendar-list scope are needed; the broader "calendar" scope is
// intentionally avoided to limit the blast radius of a leaked token.
var Scopes = []string{
	"https://www.googleapis.com/auth/calendar.events",
	"https://www.googleapis.com/auth/calendar.calendarlist.readonly",
}

// Config builds an oauth2.Config for the desktop flow with the given redirect
// URI.
func (c OAuthClient) Config(redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  redirectURI,
		Scopes:       Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  c.AuthURI,
			TokenURL: c.TokenURI,
		},
	}
}

// GeneratePKCE returns a PKCE code verifier. The S256 challenge is derived from
// it when building the authorization URL and exchanging the code.
func GeneratePKCE() string {
	return oauth2.GenerateVerifier()
}

// GenerateState returns a random OAuth state parameter.
func GenerateState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// BuildAuthURL constructs the authorization URL for the desktop flow.
func BuildAuthURL(cfg *oauth2.Config, state, codeVerifier string) string {
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
		oauth2.S256ChallengeOption(codeVerifier),
	)
}

// ExchangeCode exchanges an authorization code for tokens.
func ExchangeCode(ctx context.Context, cfg *oauth2.Config, code, codeVerifier string) (*oauth2.Token, error) {
	return cfg.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
}
