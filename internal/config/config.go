// Package config loads and stores the print agent's settings.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// DefaultPort is the loopback port the POS page calls.
	DefaultPort = 8735
	// DefaultMaxBodyBytes caps one ESC/POS job (a full-width raster logo fits).
	DefaultMaxBodyBytes = 8 << 20
	// DefaultTimeoutMS is used when a job does not name a timeout.
	DefaultTimeoutMS = 8000
)

// Config is the on-disk configuration.
type Config struct {
	Listen           string   `json:"listen"`
	AllowOrigins     []string `json:"allowOrigins"`
	Token            string   `json:"token"`
	LogLevel         string   `json:"logLevel"`
	LogDir           string   `json:"logDir,omitempty"`
	MaxBodyBytes     int64    `json:"maxBodyBytes"`
	DefaultTimeoutMS int      `json:"defaultTimeoutMs"`
}

// DefaultDir is where the config, token and logs live.
func DefaultDir() string {
	if runtime.GOOS == "windows" {
		if programData := os.Getenv("PROGRAMDATA"); programData != "" {
			return filepath.Join(programData, "LnkitsPrintAgent")
		}
		return `C:\ProgramData\LnkitsPrintAgent`
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "lnkits-print-agent")
	}
	return "."
}

// DefaultPath is the configuration file used when --config is not passed.
func DefaultPath() string { return filepath.Join(DefaultDir(), "config.json") }

// Load reads path, creates it on first run, and always returns a usable config
// with a token. The token is generated once and reused on every start.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", path, jsonErr)
		}
	case errors.Is(err, os.ErrNotExist):
		// First run: defaults are written below.
	default:
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	cfg.applyDefaults()

	if cfg.Token == "" {
		token, tokenErr := newToken()
		if tokenErr != nil {
			return nil, tokenErr
		}
		cfg.Token = token
		if saveErr := Save(path, cfg); saveErr != nil {
			return nil, saveErr
		}
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if strings.TrimSpace(c.Listen) == "" {
		c.Listen = fmt.Sprintf("127.0.0.1:%d", DefaultPort)
	}
	if len(c.AllowOrigins) == 0 {
		c.AllowOrigins = []string{"*"}
	}
	if strings.TrimSpace(c.LogLevel) == "" {
		c.LogLevel = "info"
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if c.DefaultTimeoutMS <= 0 {
		c.DefaultTimeoutMS = DefaultTimeoutMS
	}
	if strings.TrimSpace(c.LogDir) == "" {
		c.LogDir = DefaultDir()
	}
}

// Save writes the configuration, creating its directory if needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// AllowsOrigin reports whether a browser origin may talk to the agent. A request
// without an Origin header (curl, health checks) is allowed: the token still
// guards every printing endpoint.
func (c *Config) AllowsOrigin(origin string) bool {
	if strings.TrimSpace(origin) == "" {
		return true
	}
	for _, allowed := range c.AllowOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

// AllowsEveryOrigin reports whether the permissive default is still in place.
func (c *Config) AllowsEveryOrigin() bool {
	for _, allowed := range c.AllowOrigins {
		if allowed == "*" {
			return true
		}
	}
	return false
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot generate an access token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
