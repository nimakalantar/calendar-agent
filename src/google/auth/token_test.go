package auth

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type staticTokenSource struct {
	token *oauth2.Token
}

func (s staticTokenSource) Token() (*oauth2.Token, error) { return s.token, nil }

func TestPersistingTokenSourceSavesRefreshTokenRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	expiry := time.Now().Add(time.Hour)
	initial := &oauth2.Token{AccessToken: "access", RefreshToken: "refresh-1", Expiry: expiry}
	if err := SaveToken(path, initial); err != nil {
		t.Fatal(err)
	}

	refreshed := &oauth2.Token{AccessToken: "access", RefreshToken: "refresh-2", Expiry: expiry}
	source := &persistingTokenSource{
		base:             staticTokenSource{token: refreshed},
		path:             path,
		lastAccessToken:  initial.AccessToken,
		lastRefreshToken: initial.RefreshToken,
		lastExpiry:       initial.Expiry,
	}
	if _, err := source.Token(); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "refresh-2" {
		t.Fatalf("refresh token = %q, want refresh-2", loaded.RefreshToken)
	}
}
