# portabledesktop

Run a real Linux desktop for AI agents.

`portabledesktop` gives your agent a controllable desktop session with real GUI apps, mouse/keyboard actions, screenshots, and recordings.

- Built for agent loops
- API-first (CLI is mainly for local demos/debugging)
- Linux runtime included in the npm package

## Install

```bash
pnpm add portabledesktop
```

For a Vercel AI SDK computer-use loop:

```bash
pnpm add portabledesktop ai @ai-sdk/anthropic
```

For OpenAI models, install `@ai-sdk/openai` instead of `@ai-sdk/anthropic`.

## Container Demo

```bash
export ANTHROPIC_API_KEY=<key>
```

```bash
docker run -it --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -p 5190:5190 \
  ghcr.io/coder/portabledesktop \
  "play a game of chess"
```

The container prints a viewer URL (typically `http://localhost:5190`) so you can watch and intervene while the agent runs.

## What You Get

- Screenshots of the full desktop or a specific region.
- MP4 recording with automatic idle-time trimming/speedup to keep replays short.
- A browser client path for human-in-the-loop check-ins when the agent gets stuck.

## Usage

### Agent Loop

Prereqs:

- Linux host
- `ANTHROPIC_API_KEY` in your environment

```ts
import { spawn } from "node:child_process";
import { ToolLoopAgent as Agent, stepCountIs } from "ai";
import { anthropic as anthropicProvider } from "@ai-sdk/anthropic";
import { createDesktop } from "portabledesktop";
import { anthropic } from "portabledesktop/ai";

const desktop = await createDesktop({
  vnc: {
    geometry: "1280x800",
    desktopSizeMode: "fixed"
  },
  background: { color: "#1b1f24" }
});

const browser = spawn("google-chrome-stable", ["--new-window"], {
  env: desktop.env,
  detached: true,
  stdio: "ignore"
});
browser.unref();

const recording = await desktop.record({
  file: "./run.mp4",
  idleSpeedup: 10,
  idleMinDurationSec: 0.8,
  idleNoiseTolerance: "-45dB"
});

const agent = new Agent({
  model: anthropicProvider("claude-opus-4-6"),
  stopWhen: stepCountIs(100),
  tools: {
    computer: anthropic.tools.computer_20251124({
      desktop,
      displayWidthPx: 1280,
      displayHeightPx: 800,
      displayNumber: desktop.display,
      enableZoom: true
    })
  }
});

const result = await agent.stream({
  prompt: "Complete the task using the open browser."
});

for await (const text of result.textStream) {
  process.stdout.write(text);
}
process.stdout.write("\n");

await recording.stop();
await desktop.kill();
```

### Human-in-the-Loop Client

Expose the desktop to a browser viewer by proxying TCP VNC traffic (`desktop.vncPort`) over WebSocket.

```bash
pnpm add ws
```

```ts
import { createServer } from "node:http";
import net from "node:net";
import { WebSocketServer } from "ws";

const server = createServer();
const wss = new WebSocketServer({ noServer: true });

server.on("upgrade", (req, socket, head) => {
  if (req.url !== "/ws") {
    socket.destroy();
    return;
  }

  wss.handleUpgrade(req, socket, head, (ws) => {
    const tcp = net.connect({ host: "127.0.0.1", port: desktop.vncPort });

    ws.on("message", (data) => tcp.write(data as Buffer));
    tcp.on("data", (chunk) => ws.send(chunk));

    ws.on("close", () => tcp.destroy());
    tcp.on("close", () => ws.close());
    tcp.on("error", () => ws.close());
  });
});

server.listen(6080);
console.log("viewer websocket: ws://127.0.0.1:6080/ws");
```

Connect from the browser:

```ts
import { createClient } from "portabledesktop/client";

const client = createClient(document.getElementById("viewer")!, {
  url: "ws://127.0.0.1:6080/ws"
});
```

## CLI (Demo/Debug)

```bash
portabledesktop up --json
portabledesktop open -- firefox
portabledesktop screenshot shot.png
portabledesktop record run.mp4
# Ctrl+C to stop recording
portabledesktop down
```

## Security

The remote desktop endpoint is unauthenticated by default (`SecurityTypes None`).

Expose it only on trusted boundaries (localhost, private network, VPN, SSH tunnel).

## Platform

- npm runtime bundle: Linux `x64`
- Linux `arm64`: use a release runtime via `PORTABLEDESKTOP_RUNTIME_DIR=/path/to/runtime` or `createDesktop({ runtimeDir: "/path/to/runtime" })`

## Example Project

See `examples/agent` for a complete loop (viewer + run + recording output).
