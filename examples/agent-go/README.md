# examples/agent-go

Minimal computer-use demo using `portabledesktop` and the [Fantasy AI SDK](https://github.com/hugodutka/fantasy) for Go. The example supports both Anthropic and OpenAI computer-use tools.

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

Set the API key for whichever provider you plan to run, in repo-root `.env.local` or your shell:

- Anthropic: `ANTHROPIC_API_KEY`
- OpenAI: `OPENAI_API_KEY`

## Run

### Anthropic (default)

```bash
go run . --prompt "Open coder.com and find the Dropbox customer story"
```

### OpenAI

```bash
go run . --provider openai --prompt "Open coder.com and find the Dropbox customer story"
```

The OpenAI flow uses the Responses API computer tool with reasoning effort set to `medium`.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--prompt` | *(news.ycombinator.com top story)* | Prompt to send to the agent |
| `--provider` | `anthropic` | AI provider to use (`anthropic` or `openai`) |
| `--model` | provider-specific | Model ID. Defaults to `claude-opus-4-6` (anthropic) or `gpt-5.4` (openai) |
| `--max-steps` | `100` | Maximum agent loop iterations |

Override the `portabledesktop` binary path with `PORTABLEDESKTOP_BIN`.

## Notes

- The example launches a desktop browser automatically.
- Recordings are saved under `examples/agent-go/tmp/`.
- Idle segments in the recording are auto-sped up for demo readability.
- OpenAI's computer tool does not support a zoom action; coordinates always
  refer to the full declared display.
- OpenAI's `click` action exposes `back` and `forward` buttons that the
  `portabledesktop` mouse CLI cannot emit. The example returns an error to
  the model rather than silently mapping them onto a left click.
