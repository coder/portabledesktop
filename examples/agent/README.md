# examples/agent

Minimal computer-use demo using `portabledesktop` + Vercel AI SDK.

## What it does

1. Starts a desktop session.
2. Starts a live VNC viewer with `Bun.serve` and opens it in your host browser.
3. Runs the agent for your `--prompt`.
4. Saves an MP4 recording and opens that MP4 in your host browser.

## Setup

```bash
cd examples/agent
bun install
```

Set `ANTHROPIC_API_KEY` in repo-root `.env.local` or your shell.

## Run

```bash
bun run start -- --prompt "Open coder.com and find the Dropbox customer story"
```

If `--prompt` is omitted, a default web-navigation prompt is used.

## Notes

- The example launches a desktop browser automatically.
- Recordings are saved under `examples/agent/tmp/`.
- Idle segments in the recording are auto-sped up for demo readability.
