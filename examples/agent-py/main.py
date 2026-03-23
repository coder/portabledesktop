"""
Portable Desktop AI Agent Example (Python / OpenAI Agents SDK)

Drives a virtual desktop via the `portabledesktop` CLI binary and lets
GPT-5.4 interact with it through OpenAI's computer-use tool, using the
official OpenAI Agents SDK for Python.

Usage:
    uv run main.py
    uv run main.py --prompt "Open a terminal and run 'uname -a'"
"""

import argparse
import asyncio
import base64
import json
import os
import signal
import subprocess
import sys
import time

import httpx
from openai import AsyncOpenAI
from openai.types.shared import Reasoning

from agents import (
    Agent,
    Button,
    Computer,
    ComputerTool,
    ModelSettings,
    RunHooks,
    Runner,
    set_default_openai_client,
)

# ---------------------------------------------------------------------------
# Workaround for openai-agents SDK bug (as of 0.12.5):
# The SDK includes `acknowledged_safety_checks: null` in the
# computer_call_output payload, but the GA "computer" tool rejects that
# field entirely.  Strip it before the request is sent.
# ---------------------------------------------------------------------------
import agents.run_internal.tool_actions as _ta  # noqa: E402

_orig_computer_execute = _ta.ComputerAction.execute.__func__


@classmethod  # type: ignore[misc]
async def _patched_computer_execute(cls, **kwargs):  # type: ignore[no-untyped-def]
    item = await _orig_computer_execute(cls, **kwargs)
    if hasattr(item, "raw_item") and isinstance(item.raw_item, dict):
        item.raw_item.pop("acknowledged_safety_checks", None)
    return item


_ta.ComputerAction.execute = _patched_computer_execute  # type: ignore[assignment]
# ---------------------------------------------------------------------------

WIDTH = 1280
HEIGHT = 800
SCREENSHOT_TIMEOUT_MS = 20_000
DEFAULT_PROMPT = "Describe what you see on the screen."
DEFAULT_MODEL = "gpt-5.4"
DEFAULT_MAX_STEPS = 100
DEFAULT_VIEWER_PORT = 6080


def _portabledesktop_bin() -> str:
    return os.environ.get("PORTABLEDESKTOP_BIN", "portabledesktop")


def _build_cmd(subcommand: str, state_file: str, *args: str) -> list[str]:
    """Build a CLI command with --state-file injected after the subcommand."""
    return [_portabledesktop_bin(), subcommand, "--state-file", state_file, *args]


def _run(subcommand: str, state_file: str, *args: str) -> str:
    """Run a portabledesktop CLI command and return stdout."""
    cmd = _build_cmd(subcommand, state_file, *args)
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    return result.stdout


def _run_void(subcommand: str, state_file: str, *args: str) -> None:
    """Run a portabledesktop CLI command, ignoring stdout."""
    _run(subcommand, state_file, *args)


# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------

def _log(msg: str) -> None:
    sys.stderr.write(msg + "\n")
    sys.stderr.flush()


def _format_action(action: object) -> str:
    """Format a computer action object into a readable string."""
    action_type = getattr(action, "type", "unknown")
    parts = [action_type]

    for attr in ("x", "y"):
        val = getattr(action, attr, None)
        if val is not None:
            parts.append(f"{attr}={val}")

    button = getattr(action, "button", None)
    if button is not None:
        parts.append(f"button={button}")

    for attr in ("scroll_x", "scroll_y"):
        val = getattr(action, attr, None)
        if val is not None:
            parts.append(f"{attr}={val}")

    text = getattr(action, "text", None)
    if text is not None:
        display = text if len(text) <= 60 else text[:57] + "..."
        parts.append(f"text={display!r}")

    keys = getattr(action, "keys", None)
    if keys is not None:
        parts.append(f"keys={keys}")

    path = getattr(action, "path", None)
    if path is not None:
        parts.append(f"path=[{len(path)} points]")

    return " ".join(parts)


# ---------------------------------------------------------------------------
# Run hooks — prints every LLM turn and tool execution to stderr.
# ---------------------------------------------------------------------------

class LoggingHooks(RunHooks):
    def __init__(self):
        self.step = 0

    async def on_llm_end(self, context, agent, response):
        from openai.types.responses import (
            ResponseComputerToolCall,
            ResponseOutputMessage,
            ResponseReasoningItem,
        )
        for item in response.output:
            if isinstance(item, ResponseReasoningItem):
                for part in (item.summary or []):
                    text = getattr(part, "text", None)
                    if text:
                        _log(f"  [step {self.step}] reasoning: {text}")
            elif isinstance(item, ResponseComputerToolCall):
                actions = getattr(item, "actions", None) or []
                action = getattr(item, "action", None)
                if action and not actions:
                    actions = [action]
                for a in actions:
                    _log(f"  [step {self.step}] {_format_action(a)}")
            elif isinstance(item, ResponseOutputMessage):
                for part in item.content:
                    text = getattr(part, "text", None)
                    if text:
                        _log(f"  [step {self.step}] text: {text}")

    async def on_tool_start(self, context, agent, tool):
        _log(f"  [step {self.step}] executing...")

    async def on_tool_end(self, context, agent, tool, result):
        _log(f"  [step {self.step}] done")
        self.step += 1


# ---------------------------------------------------------------------------
# Request logger — captures raw API request bodies via httpx event hooks.
# ---------------------------------------------------------------------------

class RequestLogger:
    """Hooks into the httpx client to save every Responses API request body."""

    def __init__(self, requests_path: str, screenshots_dir: str):
        self.requests_path = requests_path
        self.screenshots_dir = screenshots_dir
        self.requests: list[dict] = []
        self._img_seq = 0
        self._seen_call_ids: set[str] = set()

    async def on_request(self, request: httpx.Request) -> None:
        if "/responses" not in str(request.url):
            return
        try:
            body = json.loads(request.content)
        except (json.JSONDecodeError, UnicodeDecodeError):
            return
        self._extract_images(body)
        self.requests.append(body)
        self._save()

    def _extract_images(self, body: dict) -> None:
        """Pull base64 PNGs out of computer_call_output items and save them."""
        for item in body.get("input", []):
            if not isinstance(item, dict):
                continue
            if item.get("type") != "computer_call_output":
                continue
            call_id = item.get("call_id", "")
            if call_id in self._seen_call_ids:
                continue
            self._seen_call_ids.add(call_id)
            output = item.get("output", {})
            image_url = output.get("image_url", "")
            if not image_url.startswith("data:image/png;base64,"):
                continue
            b64 = image_url[len("data:image/png;base64,"):]
            try:
                png_data = base64.b64decode(b64)
            except Exception:
                continue
            self._img_seq += 1
            filename = f"{self._img_seq:04d}.png"
            filepath = os.path.join(self.screenshots_dir, filename)
            with open(filepath, "wb") as f:
                f.write(png_data)
            _log(f"  saved {filepath}")

    def _save(self) -> None:
        tmp = self.requests_path + ".tmp"
        with open(tmp, "w") as f:
            json.dump(self.requests, f, indent=2, default=str)
        os.replace(tmp, self.requests_path)


# ---------------------------------------------------------------------------
# PortableDesktopComputer
# ---------------------------------------------------------------------------

class PortableDesktopComputer(Computer):
    """Implements the OpenAI Agents SDK Computer interface using portabledesktop."""

    def __init__(self, state_file: str):
        self._state_file = state_file

    @property
    def environment(self):
        return "ubuntu"

    @property
    def dimensions(self) -> tuple[int, int]:
        return (WIDTH, HEIGHT)

    def screenshot(self) -> str:
        out = _run(
            "screenshot",
            self._state_file,
            "--json",
            "--target-width", str(WIDTH),
            "--target-height", str(HEIGHT),
            "--timeout-ms", str(SCREENSHOT_TIMEOUT_MS),
        )
        data = json.loads(out.strip())
        return data["data"]

    def click(self, x: int, y: int, button: Button = "left") -> None:
        _run_void("mouse", self._state_file, "move", str(x), str(y))
        _run_void("mouse", self._state_file, "click", button)

    def double_click(self, x: int, y: int) -> None:
        _run_void("mouse", self._state_file, "move", str(x), str(y))
        _run_void("mouse", self._state_file, "click", "left")
        _run_void("mouse", self._state_file, "click", "left")

    def scroll(self, x: int, y: int, scroll_x: int, scroll_y: int) -> None:
        _run_void("mouse", self._state_file, "move", str(x), str(y))
        _run_void("mouse", self._state_file, "scroll", str(scroll_x), str(scroll_y))

    def type(self, text: str) -> None:
        _run_void("keyboard", self._state_file, "type", text)

    def wait(self) -> None:
        time.sleep(1)

    def move(self, x: int, y: int) -> None:
        _run_void("mouse", self._state_file, "move", str(x), str(y))

    def keypress(self, keys: list[str]) -> None:
        for key in keys:
            _run_void("keyboard", self._state_file, "down", key)
        for key in reversed(keys):
            _run_void("keyboard", self._state_file, "up", key)

    def drag(self, path: list[tuple[int, int]]) -> None:
        if not path:
            return
        _run_void("mouse", self._state_file, "move", str(path[0][0]), str(path[0][1]))
        _run_void("mouse", self._state_file, "down", "left")
        for x, y in path[1:]:
            _run_void("mouse", self._state_file, "move", str(x), str(y))
        _run_void("mouse", self._state_file, "up", "left")


# ---------------------------------------------------------------------------
# Desktop lifecycle
# ---------------------------------------------------------------------------

def start_desktop() -> tuple[subprocess.Popen, str, dict]:
    """Start a portabledesktop session. Returns (process, state_file, info)."""
    cmd = [
        _portabledesktop_bin(),
        "up", "--json", "--foreground",
        "--geometry", f"{WIDTH}x{HEIGHT}",
        "--background", "#1f252f",
    ]
    proc = subprocess.Popen(
        cmd,
        stdout=subprocess.PIPE,
        stderr=sys.stderr,
        text=True,
    )
    first_line = proc.stdout.readline()
    if not first_line:
        proc.kill()
        raise RuntimeError("No output from portabledesktop up")

    info = json.loads(first_line)
    state_file = info["stateFile"]
    print(f"display :{info['display']}  vnc :{info['vncPort']}  geometry {info['geometry']}")
    return proc, state_file, info


def start_viewer(state_file: str) -> subprocess.Popen:
    """Start the built-in noVNC viewer."""
    cmd = [
        _portabledesktop_bin(),
        "viewer", "--state-file", state_file,
        "--port", str(DEFAULT_VIEWER_PORT),
        "--host", "127.0.0.1",
        "--no-open",
    ]
    return subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def open_browser(url: str) -> None:
    """Try to open a URL in the host browser."""
    import platform
    if platform.system() == "Darwin":
        cmds = [["open", url]]
    else:
        cmds = [["xdg-open", url], ["sensible-browser", url]]
    for cmd in cmds:
        try:
            subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            return
        except FileNotFoundError:
            continue
    print(f"  open manually: {url}")


def load_env_local() -> None:
    """Load .env.local from parent directories."""
    here = os.path.dirname(os.path.abspath(__file__))
    candidates = [
        os.path.join(here, "..", "..", ".env.local"),
        os.path.join(here, "..", "..", "..", ".env.local"),
    ]
    for path in candidates:
        if not os.path.isfile(path):
            continue
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                if line.startswith("export "):
                    line = line[7:]
                eq = line.find("=")
                if eq == -1:
                    continue
                key = line[:eq].strip()
                val = line[eq + 1:].strip()
                if len(val) >= 2 and (
                    (val[0] == '"' and val[-1] == '"')
                    or (val[0] == "'" and val[-1] == "'")
                ):
                    val = val[1:-1]
                if not os.environ.get(key):
                    os.environ[key] = val
        break


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

async def main() -> None:
    parser = argparse.ArgumentParser(description="Portable Desktop AI Agent (Python)")
    parser.add_argument("--prompt", default=DEFAULT_PROMPT, help="Prompt for the agent")
    parser.add_argument("--model", default=DEFAULT_MODEL, help="OpenAI model ID")
    parser.add_argument("--max-steps", type=int, default=DEFAULT_MAX_STEPS, help="Max agent steps")
    args = parser.parse_args()

    load_env_local()

    if not os.environ.get("OPENAI_API_KEY"):
        print("error: OPENAI_API_KEY is missing. Set it in environment or .env.local at repo root.", file=sys.stderr)
        sys.exit(1)

    tmp_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "tmp")
    os.makedirs(tmp_dir, exist_ok=True)
    run_ts = str(int(time.time() * 1000))
    requests_path = os.path.join(tmp_dir, f"requests-{run_ts}.json")
    screenshots_dir = os.path.join(tmp_dir, f"screenshots-{run_ts}")
    os.makedirs(screenshots_dir, exist_ok=True)

    request_logger = RequestLogger(requests_path, screenshots_dir)

    client = AsyncOpenAI(
        api_key=os.environ.get("OPENAI_API_KEY"),
    )
    client._client.event_hooks["request"].append(request_logger.on_request)
    set_default_openai_client(client, use_for_tracing=False)

    print("starting portable desktop...")
    desktop_proc, state_file, _info = start_desktop()

    viewer_proc = start_viewer(state_file)
    viewer_url = f"http://127.0.0.1:{DEFAULT_VIEWER_PORT}"
    print(f"viewer: {viewer_url}")
    open_browser(viewer_url)

    def cleanup() -> None:
        if viewer_proc.poll() is None:
            viewer_proc.kill()
            viewer_proc.wait()
        if desktop_proc.poll() is None:
            desktop_proc.send_signal(signal.SIGTERM)
            desktop_proc.wait()

    try:
        computer = PortableDesktopComputer(state_file)

        agent = Agent(
            name="Desktop user",
            instructions="Use the computer tool to complete the task.",
            tools=[ComputerTool(computer)],
            model=args.model,
            model_settings=ModelSettings(
                truncation="auto",
                reasoning=Reasoning(effort="medium", summary="detailed"),
            ),
        )

        print(f"model: {args.model}  max steps: {args.max_steps}")
        print(f'prompt: "{args.prompt}"')
        print(f"requests: {requests_path}")
        print(f"screenshots: {screenshots_dir}")
        print()

        hooks = LoggingHooks()
        result = await Runner.run(agent, args.prompt, max_turns=args.max_steps, hooks=hooks)
        print(result.final_output)
    except KeyboardInterrupt:
        print("\ninterrupted", file=sys.stderr)
    finally:
        cleanup()


if __name__ == "__main__":
    asyncio.run(main())
