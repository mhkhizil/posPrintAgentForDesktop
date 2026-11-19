package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesAndKeepsTheToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	first, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(first.Token) != 64 {
		t.Fatalf("token length %d, want 64 hex characters", len(first.Token))
	}
	if first.Listen != "127.0.0.1:8735" {
		t.Fatalf("listen %q, want the loopback default", first.Listen)
	}
	if !first.AllowsEveryOrigin() {
		t.Fatal("a fresh config is permissive until an origin is configured")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("config file was not written: %v", statErr)
	}

	second, err := Load(path)
	if err != nil {
		t.Fatalf("Load again: %v", err)
	}
	if second.Token != first.Token {
		t.Fatal("the token must survive a restart")
	}
}

func TestAllowsOrigin(t *testing.T) {
	permissive := &Config{AllowOrigins: []string{"*"}}
	if !permissive.AllowsOrigin("https://pos.example.com") {
		t.Fatal("* allows every origin")
	}

	locked := &Config{AllowOrigins: []string{"https://pos.example.com"}}
	if !locked.AllowsOrigin("https://pos.example.com") {
		t.Fatal("the configured origin must be allowed")
	}
	if !locked.AllowsOrigin("") {
		t.Fatal("a request without an Origin header is allowed; the token still guards it")
	}
	if locked.AllowsOrigin("https://evil.example.com") {
		t.Fatal("an unlisted origin must be rejected")
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("broken JSON must be reported, not silently replaced")
	}
}
