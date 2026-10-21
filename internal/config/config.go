// Package config stores zenavo-mcp's sign-in in a file only the user can read. The MCP client's own
// configuration (claude_desktop_config.json, .mcp.json) never holds the key: it only starts zenavo-mcp.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultAPI is the hosted control plane.
const DefaultAPI = "https://zenavo.app"

// Config is the saved sign-in. The key belongs to API and is never sent to another host.
type Config struct {
	API   string `json:"api"`
	Token string `json:"token,omitempty"`
	// Scope and Projects describe the key as the console approved it, for `zenavo-mcp status`.
	Scope    string   `json:"scope,omitempty"`
	Projects []string `json:"project_ids,omitempty"`
}

// Path is $ZENAVO_MCP_CONFIG, or zenavo-mcp/config.json in the user's config directory.
func Path() (string, error) {
	if p := os.Getenv("ZENAVO_MCP_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if runtime.GOOS != "windows" {
		// ~/.config on macOS too, where people look for it, not ~/Library/Application Support.
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dir, err = xdg, nil
		} else if home, herr := os.UserHomeDir(); herr == nil {
			dir, err = filepath.Join(home, ".config"), nil
		}
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "zenavo-mcp", "config.json"), nil
}

// Load reads the saved sign-in; a missing file is an empty config. ZENAVO_MCP_TOKEN and ZENAVO_MCP_API_URL
// override it, for CI and containers. They are deliberately not the CLI's ZENAVO_TOKEN, so a CLI key exported in
// a shell is never handed to an agent by accident.
func Load() (*Config, error) {
	c := &Config{}
	path, err := Path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if v := os.Getenv("ZENAVO_MCP_API_URL"); v != "" {
		c.API = v
	}
	if v := os.Getenv("ZENAVO_MCP_TOKEN"); v != "" {
		c.Token, c.Scope, c.Projects = v, "", nil
	}
	if c.API == "" {
		c.API = DefaultAPI
	}
	api, err := NormalizeAPI(c.API)
	if err != nil {
		return nil, err
	}
	c.API = api
	return c, nil
}

// Save writes the config with mode 0600 in a 0700 directory, replacing the file atomically.
func Save(c *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Remove deletes the saved sign-in.
func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// NormalizeAPI checks a control plane address and returns its origin. It must be https, except on this machine
// (development), so the key never travels in the clear.
func NormalizeAPI(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("invalid control plane address %q: use https://host", raw)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid control plane address %q: give only the origin, like https://zenavo.app", raw)
	}
	if u.Scheme == "http" && !loopback(u.Hostname()) {
		return "", fmt.Errorf("refusing %q: the control plane must use https (http is allowed only for localhost)", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
