# portabledesktop

Run a real Linux desktop for AI agents.

`portabledesktop` is a standalone Go binary that embeds a compressed Linux
runtime, unpacks it on first run, and manages desktop sessions directly. Give
your agent a controllable desktop with real GUI apps, mouse/keyboard input,
screenshots, and recordings — no containers or display servers to configure.

## Install

Download a prebuilt binary from
[GitHub Releases](https://github.com/coder/portabledesktop/releases), or
install with `go install`:

```bash
go install github.com/coder/portabledesktop/pd/cmd/portabledesktop@latest
```

## Quick Start

```bash
# Start a desktop session.
portabledesktop up --json

# Launch an app inside the session.
portabledesktop open -- google-chrome-stable --new-window

# Take a screenshot.
portabledesktop screenshot shot.png

# Record the session to MP4 (Ctrl+C to stop).
portabledesktop record run.mp4

# Tear down the session.
portabledesktop down
```

`up --json` prints session metadata (display number, VNC port, environment
variables) so agent tooling can connect programmatically.

## Human-in-the-Loop Viewer

`portabledesktop viewer` starts a local HTTP server that serves a noVNC
client, letting a human watch and interact with the desktop in a browser:

```bash
portabledesktop viewer
# Opens http://localhost:6080 by default.
```

This is useful for debugging agent runs or intervening when the agent gets
stuck.

## Security

The VNC endpoint is **unauthenticated by default**
(`SecurityTypes None`). Expose it only on trusted boundaries — localhost,
a private network, VPN, or SSH tunnel.

## Platform

| Architecture | Supported |
|--------------|-----------|
| Linux x64    | ✓         |
| Linux arm64  | ✓         |

## Environment Variables

| Variable                    | Description                       |
|-----------------------------|-----------------------------------|
| `PORTABLEDESKTOP_RUNTIME_DIR` | Skip unpack, use this runtime dir |
| `PORTABLEDESKTOP_STATE_FILE`  | Override default state file path  |

## Development

See [`pd/README.md`](pd/README.md) for build instructions, Makefile targets,
and development workflow.

## Examples

- [`examples/demo/`](examples/demo/) — Docker container that runs an AI agent
  loop (Anthropic Claude + computer-use tool) on the virtual desktop.
- [`examples/agent/`](examples/agent/) — Bun/TypeScript agent that drives the
  desktop via the CLI binary, with support for both Anthropic and OpenAI
  providers.
