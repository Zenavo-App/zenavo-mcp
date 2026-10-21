package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoRedirectsAndErrors(t *testing.T) {
	var leaked bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer elsewhere.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/moved":
			http.Redirect(w, r, elsewhere.URL+"/steal", http.StatusFound)
		case "/api/blocked":
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"detail":["web: unresolved variable POSTGRES_HOST","db: no image"]}`))
		case "/api/invalid":
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"detail":[{"loc":["body","name"],"msg":"Field required"}]}`))
		default:
			if r.Header.Get("Authorization") != "" && r.Header.Get("X-Zenavo-Request") != "" {
				t.Error("a bearer request does not need the browser header")
			}
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"detail":"nope"}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "zv_pat_secret", "test")
	if err := c.Get(context.Background(), "/moved", nil); err == nil || leaked {
		t.Fatalf("followed a redirect (leaked=%v): %v", leaked, err)
	}
	if err := c.Get(context.Background(), "/invalid", nil); err == nil || err.Error() != "body.name: Field required" || !IsStatus(err, 422) {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), "/blocked", nil); err == nil || err.Error() != "web: unresolved variable POSTGRES_HOST; db: no image" {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), "/x", nil); !IsStatus(err, 403) || err.Error() != "nope" {
		t.Fatal(err)
	}
}
