# examples/agent

`examples/agent` is a local reference for computer-use automation with `portabledesktop` + Vercel AI SDK.

It uses:
- `ToolLoopAgent` (imported as `Agent`)
- Anthropic computer-use tool (`computer_20251124`)
- Extra local tools: `launchBrowser`, `launchApp`, `runShell`
- `claude-opus-4-6`

## Install

```bash
cd examples/agent
bun install
```

## Run

```bash
bun run start
```

Quick live-watch mode:

```bash
bun run watch
```

Default goal:
- Open the Dropbox customer story page on coder.com
- Confirm the Dropbox customer story page is visible
- Capture a screenshot

By default this also:
- Starts a local live viewer server
- Proxies VNC to WebSocket for noVNC
- Auto-opens your host browser so you can watch the run in real time

With an app launched inside the desktop:

```bash
bun run start -- \
  --app "google-chrome-stable --user-data-dir=/tmp/pd-agent-chrome https://example.com" \
  --prompt "Take a screenshot, describe the page title, then stop."
```

Save a final screenshot:

```bash
bun run start -- --screenshot-path ./tmp/agent-final.png
```

Save recording to a specific path:

```bash
bun run start -- --record-path ./tmp/agent-run.mp4
```

Viewer controls:

```bash
# disable viewer server + auto-open
bun run start -- --no-viewer

# keep viewer server but do not auto-open host browser
bun run start -- --no-open-browser

# bind viewer server to a specific host/port
bun run start -- --viewer-host 127.0.0.1 --viewer-port 46080
```

## Notes

- `ANTHROPIC_API_KEY` is loaded from repo root `.env.local` if not already in the environment.
- The script starts `portabledesktop`, runs the agent, then shuts the desktop down automatically unless `--keep-alive` is set.
- `bun run start` and `bun run watch` auto-build the local viewer bundle (`dist/viewer-client.js`) before launching.
- The example records every run and saves an MP4 under `examples/agent/tmp/` by default (or `--record-path`).
- Recording uses aggressive demo defaults that speed up idle segments: `idleSpeedup=20`, `idleMinDurationSec=0.35`, `idleNoiseTolerance=-38dB`.
