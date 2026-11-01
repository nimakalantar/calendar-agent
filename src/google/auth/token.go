package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/nimakalantar/calendar-agent/src/platform/fsutil"
)

// LoadToken reads a stored token. Expiry is authoritative; a legacy token file
// (the Nim/early-Go format with a "timestamp") is migrated so an already-expired
// access token triggers a refresh instead of being trusted forever.
func LoadToken(path string) (*oauth2.Token, error) {
	warnIfPermissive(path, "Google token")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, err
	}
	if token.Expiry.IsZero() {
		var legacy struct {
			Timestamp json.RawMessage `json:"timestamp"`
		}
		if err := json.Unmarshal(raw, &legacy); err == nil && len(legacy.Timestamp) > 0 {
			if issued, ok := parseStoredTimestamp(legacy.Timestamp); ok {
				token.Expiry = issued.Add(time.Duration(token.ExpiresIn) * time.Second)
			}
		}
	}
	return &token, nil
}

// parseStoredTimestamp accepts either an epoch number or an RFC3339 string,
// matching the two formats the token file has used over time.
func parseStoredTimestamp(raw json.RawMessage) (time.Time, bool) {
	var epoch int64
	if err := json.Unmarshal(raw, &epoch); err == nil {
		return time.Unix(epoch, 0).UTC(), true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// SaveToken writes a token atomically with restrictive permissions.
func SaveToken(path string, token *oauth2.Token) error {
	if path == "" {
		return nil
	}
	return fsutil.WriteJSONAtomic(path, token, 0o600)
}

// NewTokenSource loads a stored token and returns a token source that refreshes
// it and persists the refreshed token back to disk.
func NewTokenSource(ctx context.Context, cfg *oauth2.Config, tokenPath string) (oauth2.TokenSource, error) {
	token, err := LoadToken(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("no stored Google access token found. Run setup-auth first: %w", err)
	}
	if token.AccessToken == "" && token.RefreshToken == "" {
		return nil, fmt.Errorf("stored token is empty. Run setup-auth first")
	}
	source := cfg.TokenSource(ctx, token)
	if tokenPath == "" {
		return source, nil
	}
	return &persistingTokenSource{
		base:             source,
		path:             tokenPath,
		lastAccessToken:  token.AccessToken,
		lastRefreshToken: token.RefreshToken,
		lastExpiry:       token.Expiry,
	}, nil
}

type persistingTokenSource struct {
	mu               sync.Mutex
	base             oauth2.TokenSource
	path             string
	lastAccessToken  string
	lastRefreshToken string
	lastExpiry       time.Time
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	changed := token.AccessToken != s.lastAccessToken ||
		token.RefreshToken != s.lastRefreshToken ||
		!token.Expiry.Equal(s.lastExpiry)
	if changed {
		if err := SaveToken(s.path, token); err != nil {
			slog.Warn("failed to persist refreshed Google token", "path", s.path, "error", err)
		} else {
			s.lastAccessToken = token.AccessToken
			s.lastRefreshToken = token.RefreshToken
			s.lastExpiry = token.Expiry
		}
	}
	return token, nil
}
