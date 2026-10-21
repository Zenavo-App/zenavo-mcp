// Package api is a small client for the Zenavo REST API (https://docs.zenavo.app/api/overview).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls one control plane with one API key.
type Client struct {
	Base      string // origin, e.g. https://zenavo.app
	Token     string // zv_pat_…; empty before sign-in
	UserAgent string
	HTTP      *http.Client
}

// maxResponse bounds what the client reads from one response (logs are the largest, at about 100 kB).
const maxResponse = 4 << 20

// New returns a client that never follows redirects, so the key only ever reaches Base.
func New(base, token, userAgent string) *Client {
	return &Client{Base: base, Token: token, UserAgent: userAgent, HTTP: &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("the control plane answered with a redirect; check the address you signed in to")
		},
	}}
}

// Error is an API error response: {"detail": …}.
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("Zenavo API error %d", e.Status)
	}
	return e.Detail
}

// IsStatus reports whether err is an API error with this status.
func IsStatus(err error, status int) bool {
	var ae *Error
	return errors.As(err, &ae) && ae.Status == status
}

// P escapes one path segment.
func P(s string) string { return url.PathEscape(s) }

// Do sends a request to /api<path> and decodes a JSON response into out (if not nil).
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/api"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else {
		// Unauthenticated calls (the device sign-in) come from a program, not a browser page.
		req.Header.Set("X-Zenavo-Request", "1")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Detail: detail(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unexpected response from %s: %w", path, err)
	}
	return nil
}

// Get and Post are shorthands for Do.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	if body == nil {
		body = struct{}{}
	}
	return c.Do(ctx, http.MethodPost, path, body, out)
}

// detail extracts {"detail": …}; validation errors are a list of issues.
func detail(raw []byte) string {
	var v struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Detail) == 0 {
		return strings.TrimSpace(string(raw[:min(len(raw), 300)]))
	}
	var s string
	if json.Unmarshal(v.Detail, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(v.Detail, &list) == nil && len(list) > 0 {
		return strings.Join(list, "; ")
	}
	var issues []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(v.Detail, &issues) == nil && len(issues) > 0 {
		parts := make([]string, 0, len(issues))
		for _, i := range issues {
			loc := make([]string, 0, len(i.Loc))
			for _, l := range i.Loc {
				loc = append(loc, fmt.Sprint(l))
			}
			parts = append(parts, strings.Join(loc, ".")+": "+i.Msg)
		}
		return strings.Join(parts, "; ")
	}
	return string(v.Detail)
}
