# portabledesktop

`portabledesktop` is a Linux-first TypeScript library and CLI for launching and controlling a portable X/VNC desktop runtime.

## Install

```bash
npm install portabledesktop
```

or

```bash
bun add portabledesktop
```

## Quick Start (API)

```ts
import { spawn } from "node:child_process";
import { start } from "portabledesktop";

const desktop = await start({
  geometry: "1280x800",
  background: { color: "#202428" }
});

const browser = spawn("google-chrome-stable", ["--new-window"], {
  env: desktop.env,
  detached: true,
  stdio: "ignore"
});
browser.unref();

await desktop.moveMouse(500, 340);
await desktop.click("left");
await desktop.type("hello from portabledesktop");

const recording = await desktop.record({
  file: "./session.mp4",
  idleSpeedup: 10,
  idleMinDurationSec: 0.8,
  idleNoiseTolerance: "-45dB"
});
await recording.stop();

const screenshot = await desktop.screenshot();
await desktop.kill({ cleanup: true });
```

## API Surface

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
- `desktop.kill({ cleanup? })`

`record(...)` supports idle acceleration:

- `idleSpeedup`
- `idleMinDurationSec`
- `idleNoiseTolerance`

## CLI

```bash
portabledesktop up --json
portabledesktop info
portabledesktop open -- firefox
portabledesktop mouse move 400 300
portabledesktop keyboard type "hello"
portabledesktop record start ./session.mp4
portabledesktop record stop
portabledesktop down
```

## Browser Client

`portabledesktop/client` wraps noVNC primitives:

```ts
import { createClient } from "portabledesktop/client";

const client = createClient(document.getElementById("vnc")!, {
  url: "ws://127.0.0.1:6080/websockify"
});
```

## Platform Support

- npm package runtime bundle is built and published for Linux `x64`.
- Linux `arm64` runtime artifacts are published with every GitHub Release.
- On `arm64`, point the library at a downloaded runtime via:
  - `PORTABLEDESKTOP_RUNTIME_DIR=/path/to/runtime`
  - or `start({ runtimeDir: "/path/to/runtime" })`

## Development

Build JS + types:

```bash
bun run build
```

Build runtime artifact (`result/` contract):

```bash
./scripts/build.sh
```

Compatibility checks:

```bash
./scripts/smoke.sh
./scripts/matrix.sh
```

## Release Process

Releases are tag-driven with GitHub Actions.

1. Bump `package.json` version.
2. Push commit.
3. Create and push a tag: `vX.Y.Z`.

This triggers:

- npm publish (`portabledesktop`)
- GitHub Release creation
- Release assets for Linux `x64` and Linux `arm64`:
  - runtime archive + manifest
  - compiled CLI binary

Required repository secret:

- `NPM_TOKEN` (publish token for npm)

## Contributing

See `CONTRIBUTING.md`.

## Examples

- `examples/agent`: Vercel AI SDK computer-use example.
  - `cd examples/agent && bun install && bun run start`
