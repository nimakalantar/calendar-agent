package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestParseClientInstalled(t *testing.T) {
	raw := []byte(`{"installed":{"client_id":"id","client_secret":"secret","redirect_uris":["http://localhost"]}}`)
	client, err := ParseClient(raw)
	if err != nil {
		t.Fatal(err)
	}
	if client.ClientID != "id" || client.ClientSecret != "secret" {
		t.Fatalf("client = %+v", client)
	}
	if client.AuthURI == "" || client.TokenURI == "" {
		t.Fatalf("expected default endpoints, got %+v", client)
	}
}

func TestLoadTokenMigratesLegacyTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	issued := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	content := `{"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600,"timestamp":"` +
		issued.Format(time.RFC3339) + `"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	want := issued.Add(time.Hour)
	if !token.Expiry.Equal(want) {
		t.Fatalf("expiry = %v, want %v", token.Expiry, want)
	}
}

func TestSaveTokenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "token.json")
	want := &oauth2.Token{
		AccessToken:  "a",
		RefreshToken: "r",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	if err := SaveToken(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}
