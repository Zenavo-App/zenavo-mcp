// Command zenavo-mcp is a Model Context Protocol server for Zenavo. MCP clients start it with no arguments and talk
// to it over stdin and stdout; people sign it in once with `zenavo-mcp login`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/Zenavo-App/zenavo-mcp/internal/api"
	"github.com/Zenavo-App/zenavo-mcp/internal/config"
	"github.com/Zenavo-App/zenavo-mcp/internal/login"
	"github.com/Zenavo-App/zenavo-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"
)

// version is set by the release build (-ldflags "-X main.version=…").
var version = "dev"

const usage = `zenavo-mcp: a Model Context Protocol server for Zenavo

Usage:
  zenavo-mcp                 run the MCP server on stdin/stdout (what MCP clients start)
  zenavo-mcp login [flags]   sign in through the Zenavo console
  zenavo-mcp logout          forget the saved key on this computer
  zenavo-mcp status          show where and how this computer is signed in
  zenavo-mcp version

login flags:
  --scope read|deploy|full   the permission to ask for (default deploy); you can lower it when you approve
  --api URL                  a self-hosted control plane (default https://zenavo.app)
  --no-browser               print the address instead of opening a browser

Environment:
  ZENAVO_MCP_TOKEN, ZENAVO_MCP_API_URL   use this key and control plane instead of the saved sign-in
  ZENAVO_MCP_CONFIG                      where the sign-in is saved (default ~/.config/zenavo-mcp/config.json)
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "zenavo-mcp:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(args) == 0 || args[0] == "serve" {
		return serve(ctx)
	}
	switch args[0] {
	case "login":
		return loginCmd(ctx, args[1:], stderr)
	case "logout":
		if err := config.Remove(); err != nil {
			return err
		}
		fmt.Fprintln(stderr, "Signed out on this computer. The key still works until you delete it in the console under Account settings → API Keys.")
		return nil
	case "status":
		return status(ctx, stdout)
	case "version", "--version":
		fmt.Fprintln(stdout, "zenavo-mcp", version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	}
	fmt.Fprint(stderr, usage)
	return fmt.Errorf("unknown command %q", args[0])
}

func userAgent() string { return "zenavo-mcp/" + version }

// serve runs the MCP server. Nothing but protocol messages may go to stdout.
func serve(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "zenavo", Title: "Zenavo", Version: version, WebsiteURL: "https://zenavo.app"},
		&mcp.ServerOptions{Instructions: tools.Instructions})
	tools.Register(server, api.New(cfg.API, cfg.Token, userAgent()), cfg.Token != "", cfg.Scope)
	err = server.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func loginCmd(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	scope := fs.String("scope", "deploy", "read, deploy or full")
	apiURL := fs.String("api", "", "control plane address")
	noBrowser := fs.Bool("no-browser", false, "do not open a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *scope != "read" && *scope != "deploy" && *scope != "full" {
		return fmt.Errorf("--scope must be read, deploy or full")
	}
	if os.Getenv("ZENAVO_MCP_TOKEN") != "" {
		return errors.New("ZENAVO_MCP_TOKEN is set, so a saved sign-in would not be used; unset it first")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *apiURL != "" {
		if cfg.API, err = config.NormalizeAPI(*apiURL); err != nil {
			return err
		}
	}
	host, _ := os.Hostname()
	res, err := login.Run(ctx, api.New(cfg.API, "", userAgent()), host, *scope, !*noBrowser && term.IsTerminal(int(os.Stderr.Fd())), stderr)
	if err != nil {
		return err
	}
	cfg.Token, cfg.Scope, cfg.Projects = res.Token, res.Scope, res.Projects
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	fmt.Fprintf(stderr, "\nSigned in to %s as key %q with the %s permission%s.\nSaved to %s (readable only by you).\n",
		cfg.API, res.Name, res.Scope, projectsNote(res.Projects), path)
	fmt.Fprintln(stderr, "Your MCP client can now start zenavo-mcp; no key goes in its configuration.")
	return nil
}

func projectsNote(ids []string) string {
	if ids == nil {
		return ", for all your projects"
	}
	return ", limited to " + fmt.Sprint(len(ids)) + " project(s)"
}

func status(ctx context.Context, w io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		fmt.Fprintf(w, "Not signed in (control plane %s). Run: zenavo-mcp login\n", cfg.API)
		return nil
	}
	var me struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := api.New(cfg.API, cfg.Token, userAgent()).Get(ctx, "/auth/me", &me); err != nil {
		return fmt.Errorf("the saved key does not work (%v); run zenavo-mcp login", err)
	}
	source := "saved sign-in"
	if os.Getenv("ZENAVO_MCP_TOKEN") != "" {
		source = "ZENAVO_MCP_TOKEN"
	}
	scope := cfg.Scope
	if scope == "" {
		scope = "unknown"
	}
	fmt.Fprintf(w, "Signed in to %s as %s (%s).\nPermission: %s%s\n", cfg.API, me.User.Email, source, scope, strings.TrimPrefix(projectsNote(cfg.Projects), ","))
	return nil
}
