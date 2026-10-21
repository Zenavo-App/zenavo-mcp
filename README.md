# zenavo-mcp

A [Model Context Protocol](https://modelcontextprotocol.io) server for [Zenavo](https://zenavo.app). It lets an AI
agent (Claude Code, Claude Desktop, Cursor and others) check on, deploy and fix the projects you run on Zenavo,
with an API key that can do no more than you allow.

For ready-made workflows in Claude Code (deploy, debug a failed deploy, roll back), install
[zenavo-skills](https://github.com/Zenavo-App/zenavo-skills) too; it uses this server.

## Install

Download the binary for your system from [Releases](https://github.com/Zenavo-App/zenavo-mcp/releases), check it,
and put it on your `PATH`:

```sh
# macOS on Apple silicon; see the release for other systems
curl -LO https://github.com/Zenavo-App/zenavo-mcp/releases/latest/download/zenavo-mcp_darwin_arm64
curl -LO https://github.com/Zenavo-App/zenavo-mcp/releases/latest/download/SHA256SUMS
shasum -a 256 --ignore-missing -c SHA256SUMS
gh attestation verify zenavo-mcp_darwin_arm64 --repo Zenavo-App/zenavo-mcp   # built by this repository's workflow
chmod +x zenavo-mcp_darwin_arm64 && mv zenavo-mcp_darwin_arm64 /usr/local/bin/zenavo-mcp
```

Or build it yourself with Go 1.26+: `go install github.com/Zenavo-App/zenavo-mcp/cmd/zenavo-mcp@latest`.

## Sign in

```sh
zenavo-mcp login                    # asks for the Deploy permission
zenavo-mcp login --scope read       # only look: projects, status, logs
zenavo-mcp login --api https://console.example.com   # a self-hosted control plane
```

The terminal shows a code and opens the Zenavo console. Check that the code matches, then approve. On that page
you can lower the permission and **limit the key to some projects**, which we recommend for agents. The key is saved
in `~/.config/zenavo-mcp/config.json`, readable only by you; it never goes in your MCP client's configuration.
`zenavo-mcp status` shows how you are signed in; `zenavo-mcp logout` forgets the key here (delete it in the console
under **Account settings → API Keys** to revoke it).

The agent cannot sign in by itself: there is no sign-in tool, and approving needs you in the console.

## Add it to your MCP client

**Claude Code**

```sh
claude mcp add zenavo -- zenavo-mcp
```

**Claude Desktop, Cursor and other clients** (`claude_desktop_config.json`, `.cursor/mcp.json`, …)

```json
{
  "mcpServers": {
    "zenavo": { "command": "zenavo-mcp" }
  }
}
```

## Tools

| Tool | What it does | Needs |
| --- | --- | --- |
| `whoami` | The account, teams and roles the key acts with, and its permission | Read |
| `list_projects` | Projects and their services | Read |
| `get_project` | Services, ports and domains, live state (ready, restarts, CPU, memory), active job, deploy blockers | Read |
| `get_logs` | The newest log lines of a service (at most 200), optionally filtered | Read |
| `list_deployments` | Recent deployments of a service, marking the current one | Read |
| `get_job` | A job's status, progress and log; can wait for it to finish | Read |
| `list_env` | Names of a service's environment variables, **never their values** | Deploy |
| `deploy` | Deploy a project or one service | Deploy |
| `rollback` | Run an earlier successful deployment again | Deploy |
| `restart_service` | Restart a service without rebuilding | Deploy |
| `set_env` / `unset_env` | Set or remove one environment variable (the value is never shown back) | Deploy |

Tools that change something are marked `destructiveHint`, so clients ask you before running them. There are no tools
to delete projects, open a shell, read files or database rows, download backups, change servers, or touch billing
and team members: do those in the console.

## Security

What a key can do is enforced by the Zenavo API, not by this program: a Read key cannot deploy even if someone
changed this code. On top of that, zenavo-mcp:

- never shows configuration values to the model. `list_env` returns names; `set_env` takes a value and never echoes it.
  Logs come from the server with the project's variable values already replaced.
- marks logs and job output as untrusted data, so text a service prints cannot pose as instructions to the agent;
- talks only to the control plane you signed in to, over HTTPS (plain HTTP only for `localhost`), and never follows
  redirects, so the key cannot be sent anywhere else;
- reads its key from its own file or from `ZENAVO_MCP_TOKEN`, never the Zenavo CLI's `ZENAVO_TOKEN`, so a CLI key in
  your shell is not handed to an agent by accident.

Even so, an agent with the Deploy permission can deploy code that does anything your services can. Give it a key
limited to the projects it works on, and review what it proposes before you approve. To report a vulnerability, see
[SECURITY.md](SECURITY.md).

## CI and containers

Set `ZENAVO_MCP_TOKEN` to an API key (create one in the console with the permission and projects the job needs) and,
for a self-hosted control plane, `ZENAVO_MCP_API_URL`. `ZENAVO_MCP_CONFIG` moves the saved sign-in.

## Development

```sh
make test     # gofmt, go vet, go test -race
make build    # ./zenavo-mcp
make dist     # every platform, with SHA256SUMS, in dist/
```

Releases are built by [the release workflow](.github/workflows/release.yml) from a `v*` tag, with build provenance
attestations.

## License

[Apache License 2.0](LICENSE)
