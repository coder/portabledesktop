# portabledesktop

`portabledesktop` is a Linux-first TypeScript npm library + CLI for launching a portable X/VNC desktop session.

It is built around the Nix artifact contract:

- `result/output.tar`
- `result/output/`
- `result/manifest.json`

## Install

```bash
bun add portabledesktop
```

## Node API

```ts
import { start } from "portabledesktop";
import { spawn } from "node:child_process";

const desktop = await start({
  geometry: "1280x800",
  background: { color: "#202428" }
});

const chrome = spawn("google-chrome-stable", ["--new-window"], {
  env: desktop.env,
  detached: true,
  stdio: "ignore"
});
chrome.unref();

await desktop.moveMouse(400, 300);
await desktop.click("left");
await desktop.type("hello world");

const recording = await desktop.record({
  file: "./session.mp4",
  idleSpeedup: 10,
  idleMinDurationSec: 0.8,
  idleNoiseTolerance: "-45dB"
});
// ...
await recording.stop();

const screenshot = await desktop.screenshot();
await desktop.kill({ cleanup: true });
```

### API surface

- `start(options)`
- `desktop.env`
- `desktop.setBackground(color)`
- `desktop.moveMouse(x, y)`
- `desktop.mousePosition()`
- `desktop.click(button?)`
- `desktop.mouseDown(button?)`
- `desktop.mouseUp(button?)`
- `desktop.scroll(dx, dy)`
- `desktop.type(text)`
- `desktop.key(combo)`
- `desktop.keyDown(key)`
- `desktop.keyUp(key)`
- `desktop.screenshot(options?)`
- `desktop.record(options?) -> { pid, file, logPath, detached, stop() }`
- `record` options include `idleSpeedup` to accelerate low-motion segments after stop
- `desktop.kill({ cleanup? })`

## Browser Client

Use `portabledesktop/client` to render VNC with noVNC primitives:

```ts
import { createClient } from "portabledesktop/client";

const client = createClient(document.getElementById("vnc")!, {
  url: "ws://127.0.0.1:6080/websockify"
});
```

## CLI

```bash
portabledesktop up --json
portabledesktop info
portabledesktop open -- google-chrome-stable https://example.com
portabledesktop mouse move 400 300
portabledesktop mouse click left
portabledesktop keyboard type "hello"
portabledesktop background "#202428"
portabledesktop record start ./session.mp4
portabledesktop record stop
portabledesktop down
```

Notes:
- CLI persists session state in `~/.cache/portabledesktop/session.json` by default.
- `open` auto-injects a unique `--user-data-dir` for Chrome/Chromium if not provided.

## Runtime Asset Flow (for package maintainers)

Build runtime artifact:

```bash
./scripts/build.sh
```

Bundle assets into the npm package payload:

```bash
./scripts/bundle-assets.sh
```

Run compatibility checks:

```bash
./scripts/smoke.sh
./scripts/matrix.sh
./scripts/run-all.sh
```

Build JS outputs with Bun:

```bash
bun run build
```

Build a standalone CLI binary with Bun:

```bash
bun run build:binary
```

## Examples

- `examples/agent`: Bun + Vercel AI SDK computer-use example using `claude-opus-4-6`.
  Run `cd examples/agent && bun install && bun run start`.
