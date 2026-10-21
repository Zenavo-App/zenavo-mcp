// Package login signs zenavo-mcp in through the console (the OAuth 2.0 device authorization grant, RFC 8628).
// It runs only from a terminal (`zenavo-mcp login`), never as a tool: an agent cannot start a sign-in.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/Zenavo-App/zenavo-mcp/internal/api"
)

// Unit is the unit of the server's poll interval (seconds); tests shorten it.
var Unit = time.Second

// OpenBrowser opens a URL; tests replace it.
var OpenBrowser = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// Result is the key the console issued, as it was approved (the approver may narrow the scope).
type Result struct {
	Token    string
	Name     string
	Scope    string
	Projects []string
}

// Run asks for a key with the given scope for this device and waits until the user approves it in the console.
// c has no token. Messages for the user go to w.
func Run(ctx context.Context, c *api.Client, device, scope string, open bool, w io.Writer) (*Result, error) {
	var start struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
	}
	err := c.Post(ctx, "/auth/device/code", map[string]string{"client_name": device, "scope": scope, "client": "mcp"}, &start)
	if err != nil {
		if api.IsStatus(err, 404) || api.IsStatus(err, 422) {
			return nil, fmt.Errorf("this control plane does not support MCP sign-in yet (%v); create an API key in the console and set ZENAVO_MCP_TOKEN", err)
		}
		return nil, err
	}
	// The address is built from the control plane the user chose, never taken from the response, so this only
	// ever opens that console.
	url := c.Base + "/device"
	fmt.Fprintf(w, "Open %s and enter the code:\n\n    %s\n\n", url, start.UserCode)
	if open {
		if err := OpenBrowser(url); err == nil {
			fmt.Fprintln(w, "(Opened your browser. Check that the code there matches before you approve.)")
		}
	}
	fmt.Fprintln(w, "On that page you can lower the permission and limit the key to some projects.")
	fmt.Fprintln(w, "Waiting for approval… (Ctrl-C to cancel)")

	if start.Interval < 1 {
		start.Interval = 5
	}
	interval := time.Duration(start.Interval) * Unit
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("cancelled")
		case <-time.After(interval):
		}
		var out struct {
			Token    string   `json:"token"`
			Name     string   `json:"name"`
			Scope    string   `json:"scope"`
			Projects []string `json:"project_ids"`
		}
		err := c.Post(ctx, "/auth/device/token", map[string]string{"device_code": start.DeviceCode}, &out)
		var ae *api.Error
		switch {
		case err == nil && out.Token != "":
			return &Result{Token: out.Token, Name: out.Name, Scope: out.Scope, Projects: out.Projects}, nil
		case err == nil: // 202: still waiting
		case errors.As(err, &ae) && ae.Status == 429:
			interval += interval / 2
		case errors.As(err, &ae) && (ae.Status == 403 || ae.Status == 410):
			return nil, errors.New(ae.Detail)
		default:
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the sign-in request expired; run zenavo-mcp login again")
		}
	}
}
