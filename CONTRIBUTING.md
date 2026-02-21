# Contributing to `portabledesktop`

Thanks for contributing.

## Scope

This repository has two primary deliverables:

1. The npm package (`portabledesktop`) with TypeScript API + CLI.
2. The Nix runtime artifact (`result/output.tar`, `result/output/`, `result/manifest.json`).

Changes should keep both deliverables working.

## Local Setup

```bash
bun install
bun run build
./scripts/build.sh
./scripts/smoke.sh --skip-build
```

For broader compatibility checks:

```bash
./scripts/matrix.sh --skip-build
```

## Building Libraries on Top of `portabledesktop`

If you are creating another library that depends on `portabledesktop`, prefer composition over wrappers around shell scripts.

Recommended pattern:

1. Start a desktop with `start(...)`.
2. Launch child processes with `node:child_process` and `env: desktop.env`.
3. Use the typed desktop methods (`moveMouse`, `click`, `type`, `screenshot`, `record`, etc.) for interaction.
4. Call `desktop.kill({ cleanup: true })` in `finally`.

Minimal shape:

```ts
import { spawn } from "node:child_process";
import { start } from "portabledesktop";

const desktop = await start();
try {
  const app = spawn("firefox", [], { env: desktop.env, detached: true, stdio: "ignore" });
  app.unref();
  await desktop.type("hello");
} finally {
  await desktop.kill({ cleanup: true });
}
```

## Pull Requests

Before opening a PR:

1. Run `bun run build`.
2. Run `./scripts/build.sh`.
3. Run `./scripts/smoke.sh --skip-build`.
4. If runtime-related, run `./scripts/matrix.sh --skip-build`.

Include in your PR description:

1. Behavior change summary.
2. Compatibility impact (x64/arm64, distro notes).
3. Exact commands used for verification.

## Release Model

Releases are tag-driven:

1. Bump `package.json` version.
2. Create and push tag `vX.Y.Z`.
3. GitHub Actions publishes npm + GitHub release artifacts.
