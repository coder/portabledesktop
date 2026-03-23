# Portable Desktop AI Agent — Python Example

A minimal example that drives a virtual desktop via the `portabledesktop` CLI
and lets GPT-5.4 interact with it through OpenAI's computer-use tool, using the
official [OpenAI Agents SDK for Python](https://github.com/openai/openai-agents-python).

## Prerequisites

- `portabledesktop` CLI binary on `$PATH` (or set `PORTABLEDESKTOP_BIN`)
- Python 3.10+
- An OpenAI API key

## Setup

```sh
uv sync
```

Set your API key:

```sh
export OPENAI_API_KEY=sk-...
```

Or create a `.env.local` file at the repository root:

```
OPENAI_API_KEY=sk-...
```

## Usage

```sh
uv run main.py
uv run main.py --prompt "Open a terminal and run 'uname -a'"
uv run main.py --model gpt-5.4 --max-steps 50
```

## How it works

The example implements the SDK's `Computer` interface by shelling out to the
`portabledesktop` CLI for every action (screenshot, click, type, scroll, etc.).
The desktop runs at a hardcoded 1280×800 resolution with no coordinate scaling.
No browser or viewer is launched automatically — just a bare desktop session and
the agent loop.
