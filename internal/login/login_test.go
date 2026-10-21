package login

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zenavo-App/zenavo-mcp/internal/api"
)

func TestDeviceSignIn(t *testing.T) {
	Unit = time.Millisecond
	polls := 0
	var started map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Zenavo-Request") != "1" {
			t.Error("sign-in requests carry no key but the request header")
		}
		switch r.URL.Path {
		case "/api/auth/device/code":
			_ = json.NewDecoder(r.Body).Decode(&started)
			// A hostile server's verification_uri is ignored: the CLI opens its own console's page.
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "BCDF-GHJK", "expires_in": 600, "interval": 1, "verification_uri": "https://evil.example/device"})
		case "/api/auth/device/token":
			polls++
			if polls < 3 {
				w.WriteHeader(202)
				_, _ = w.Write([]byte(`{"status":"pending"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "zv_pat_new", "name": "MCP on laptop", "scope": "read", "project_ids": []string{"proj_1"}})
		}
	}))
	defer srv.Close()
	var opened string
	OpenBrowser = func(url string) error { opened = url; return nil }
	var out bytes.Buffer
	res, err := Run(context.Background(), api.New(srv.URL, "", "test"), "laptop", "deploy", true, &out)
	if err != nil {
		t.Fatal(err)
	}
	if started["client"] != "mcp" || started["scope"] != "deploy" || started["client_name"] != "laptop" {
		t.Fatalf("started with %v", started)
	}
	if opened != srv.URL+"/device" || strings.Contains(out.String(), "evil") || !strings.Contains(out.String(), "BCDF-GHJK") {
		t.Fatalf("opened %q, printed %q", opened, out.String())
	}
	if res.Token != "zv_pat_new" || res.Scope != "read" || len(res.Projects) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestDeniedSignIn(t *testing.T) {
	Unit = time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/device/code" {
			_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"BCDF-GHJK","expires_in":600,"interval":1}`))
			return
		}
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"detail":"Sign-in was denied in the console"}`))
	}))
	defer srv.Close()
	var out bytes.Buffer
	if _, err := Run(context.Background(), api.New(srv.URL, "", "test"), "laptop", "deploy", false, &out); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatal(err)
	}
}
