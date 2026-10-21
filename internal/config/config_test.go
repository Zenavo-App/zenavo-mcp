package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeAPI(t *testing.T) {
	for in, want := range map[string]string{"https://zenavo.app": "https://zenavo.app", "https://zenavo.app/": "https://zenavo.app",
		"http://localhost:4180": "http://localhost:4180", "http://127.0.0.1:8080": "http://127.0.0.1:8080", " https://c.example.com ": "https://c.example.com"} {
		if got, err := NormalizeAPI(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	// The key must never travel in the clear or to a surprising place.
	for _, in := range []string{"http://zenavo.app", "zenavo.app", "ftp://zenavo.app", "https://user:pw@zenavo.app", "https://zenavo.app/api", "https://zenavo.app?x=1", "http://localhost.evil.com"} {
		if got, err := NormalizeAPI(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	t.Setenv("ZENAVO_MCP_CONFIG", path)
	t.Setenv("ZENAVO_MCP_TOKEN", "")
	t.Setenv("ZENAVO_MCP_API_URL", "")
	t.Setenv("ZENAVO_TOKEN", "zv_pat_cli_key") // the CLI's variable is never picked up
	if c, err := Load(); err != nil || c.Token != "" || c.API != DefaultAPI {
		t.Fatalf("%+v %v", c, err)
	}
	if err := Save(&Config{API: "https://zenavo.app", Token: "zv_pat_x", Scope: "deploy", Projects: []string{"proj_1"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	if d, _ := os.Stat(filepath.Dir(path)); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", d.Mode())
	}
	c, err := Load()
	if err != nil || c.Token != "zv_pat_x" || c.Scope != "deploy" || len(c.Projects) != 1 {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("ZENAVO_MCP_TOKEN", "zv_pat_env")
	if c, _ := Load(); c.Token != "zv_pat_env" || c.Scope != "" || c.Projects != nil {
		t.Fatalf("env key keeps the saved key's scope: %+v", c)
	}
	t.Setenv("ZENAVO_MCP_API_URL", "http://example.com")
	if _, err := Load(); err == nil {
		t.Fatal("plain http to another host accepted")
	}
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
}
