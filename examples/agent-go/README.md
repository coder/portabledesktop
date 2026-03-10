# examples/agent-go

Minimal computer-use demo using `portabledesktop` and the [Fantasy AI SDK](https://github.com/hugodutka/fantasy) for Go.

## What it does

1. Starts a desktop session.
2. Starts a live VNC viewer and opens it in your host browser.
3. Runs an agent loop for your `--prompt`, streaming text to stdout as it arrives.
4. Saves an MP4 recording and opens it in your host browser.

## Setup

```bash
cd examples/agent-go
go mod download
```

Set `ANTHROPIC_API_KEY` in repo-root `.env.local` or your shell.

## Run

```bash
go run . --prompt "Open coder.com and find the Dropbox customer story"
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--prompt` | *(news.ycombinator.com top story)* | Prompt to send to the agent |
| `--model` | `claude-opus-4-6` | Anthropic model ID |
| `--max-steps` | `100` | Maximum agent loop iterations |

Override the `portabledesktop` binary path with `PORTABLEDESKTOP_BIN`.

## Notes

- The example launches a desktop browser automatically.
- Recordings are saved under `examples/agent-go/tmp/`.
- Idle segments in the recording are auto-sped up for demo readability.
