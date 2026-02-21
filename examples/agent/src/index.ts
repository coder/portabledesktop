import { constants } from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

import { anthropic } from "@ai-sdk/anthropic";
import { ToolLoopAgent as Agent, stepCountIs, tool } from "ai";
import { z } from "zod";
import { start, type Desktop } from "../../../src/index.ts";

import { startViewer, type ViewerHandle } from "./viewer";

interface CliOptions {
  prompt: string;
  model: string;
  geometry: string;
  background: string;
  maxSteps: number;
  appCommand: string | null;
  screenshotPath: string | null;
  recordPath: string | null;
  viewerHost: string;
  viewerPort: number;
  viewerEnabled: boolean;
  autoOpenBrowser: boolean;
  keepAlive: boolean;
}

interface ImageOutput {
  kind: "image";
  data: string;
  mediaType: "image/png";
}

interface TextOutput {
  kind: "text";
  text: string;
}

type ComputerToolOutput = ImageOutput | TextOutput;

type ComputerToolArgs = Parameters<typeof anthropic.tools.computer_20251124>[0];
type ComputerToolExecute = NonNullable<ComputerToolArgs["execute"]>;
type ComputerActionInput = Parameters<ComputerToolExecute>[0];

interface OpenInDesktopResult {
  pid: number;
}

interface RunInDesktopResult {
  code: number;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
}

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const exampleRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(__dirname, "../../..");

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function usage(): string {
  return [
    "Usage: bun run src/index.ts [options]",
    "",
    "Options:",
    "  --prompt <text>            Prompt for the agent (default: navigate to coder.com Dropbox customer story)",
    "  --model <id>               Anthropic model (default: claude-opus-4-6)",
    "  --geometry <WxH>           Desktop geometry (default: 1280x800)",
    "  --background <color>       X background color (default: #1f252f)",
    "  --max-steps <n>            Max agent loop steps (default: 100)",
    "  --app <shell-command>      Optional app command to launch inside desktop",
    "  --screenshot-path <file>   Save final screenshot PNG",
    "  --record-path <file>       Save session recording MP4 (default: ./tmp/agent-<timestamp>.mp4)",
    "  --viewer-host <host>       Host bind for live viewer server (default: 127.0.0.1)",
    "  --viewer-port <port>       Port bind for live viewer server (default: random)",
    "  --no-viewer                Disable live VNC viewer server",
    "  --no-open-browser          Do not auto-open host browser for viewer",
    "  --keep-alive               Leave desktop running after completion",
    "  --help                     Show this help"
  ].join("\n");
}

function parseArgs(argv: readonly string[]): CliOptions {
  const options: CliOptions = {
    prompt:
      "Use launchBrowser to start a browser, then use computer actions to navigate to the Dropbox customer story page on coder.com. If redirected, recover and get back to that page. Take a screenshot of the Dropbox story page and then briefly confirm success.",
    model: "claude-opus-4-6",
    geometry: "1280x800",
    background: "#1f252f",
    maxSteps: 100,
    appCommand: null,
    screenshotPath: null,
    recordPath: null,
    viewerHost: "127.0.0.1",
    viewerPort: 0,
    viewerEnabled: true,
    autoOpenBrowser: true,
    keepAlive: false
  };

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    switch (token) {
      case "--prompt": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--prompt requires a value");
        }
        options.prompt = value;
        i += 1;
        break;
      }
      case "--model": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--model requires a value");
        }
        options.model = value;
        i += 1;
        break;
      }
      case "--geometry": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--geometry requires a value");
        }
        options.geometry = value;
        i += 1;
        break;
      }
      case "--background": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--background requires a value");
        }
        options.background = value;
        i += 1;
        break;
      }
      case "--max-steps": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--max-steps requires a value");
        }
        const parsed = Number.parseInt(value, 10);
        if (!Number.isFinite(parsed) || parsed < 1) {
          throw new Error(`invalid --max-steps value: ${value}`);
        }
        options.maxSteps = parsed;
        i += 1;
        break;
      }
      case "--app": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--app requires a value");
        }
        options.appCommand = value;
        i += 1;
        break;
      }
      case "--screenshot-path": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--screenshot-path requires a value");
        }
        options.screenshotPath = value;
        i += 1;
        break;
      }
      case "--record-path": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--record-path requires a value");
        }
        options.recordPath = value;
        i += 1;
        break;
      }
      case "--viewer-host": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--viewer-host requires a value");
        }
        options.viewerHost = value;
        i += 1;
        break;
      }
      case "--viewer-port": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--viewer-port requires a value");
        }
        const parsed = Number.parseInt(value, 10);
        if (!Number.isFinite(parsed) || parsed < 0 || parsed > 65535) {
          throw new Error(`invalid --viewer-port value: ${value}`);
        }
        options.viewerPort = parsed;
        i += 1;
        break;
      }
      case "--no-viewer": {
        options.viewerEnabled = false;
        break;
      }
      case "--no-open-browser": {
        options.autoOpenBrowser = false;
        break;
      }
      case "--keep-alive": {
        options.keepAlive = true;
        break;
      }
      case "--help": {
        process.stdout.write(`${usage()}\n`);
        process.exit(0);
      }
      default: {
        throw new Error(`unknown argument: ${token}`);
      }
    }
  }

  return options;
}

function parseGeometry(geometry: string): { width: number; height: number } {
  const match = /^(\d+)x(\d+)$/.exec(geometry);
  if (!match) {
    throw new Error(`invalid geometry: ${geometry}. expected WxH, e.g. 1280x800`);
  }

  const width = Number.parseInt(match[1], 10);
  const height = Number.parseInt(match[2], 10);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width < 64 || height < 64) {
    throw new Error(`invalid geometry values: ${geometry}`);
  }

  return { width, height };
}

async function isExecutable(filePath: string): Promise<boolean> {
  try {
    await fs.access(filePath, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

async function resolveExecutable(candidates: readonly string[]): Promise<string | null> {
  const pathEntries = (process.env.PATH || "")
    .split(":")
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);

  for (const candidate of candidates) {
    if (candidate.includes("/")) {
      if (await isExecutable(candidate)) {
        return candidate;
      }
      continue;
    }

    for (const entry of pathEntries) {
      const resolved = path.join(entry, candidate);
      // eslint-disable-next-line no-await-in-loop
      if (await isExecutable(resolved)) {
        return resolved;
      }
    }
  }

  return null;
}

function openHostBrowser(url: string): void {
  try {
    if (process.platform === "darwin") {
      const child = spawn("open", [url], { detached: true, stdio: "ignore" });
      child.unref();
      return;
    }

    if (process.platform === "win32") {
      const child = spawn("cmd", ["/c", "start", "", url], { detached: true, stdio: "ignore" });
      child.unref();
      return;
    }

    const child = spawn("xdg-open", [url], { detached: true, stdio: "ignore" });
    child.unref();
  } catch {
    // ignore browser open failures
  }
}

async function openInDesktop(
  desktop: Desktop,
  command: string,
  args: string[],
  options: { cwd?: string } = {}
): Promise<OpenInDesktopResult> {
  const child = spawn(command, args, {
    cwd: options.cwd || process.cwd(),
    env: desktop.env,
    detached: true,
    stdio: "ignore"
  });

  const spawnError = await Promise.race<Error | null>([
    new Promise((resolve) => {
      child.once("error", (error) => resolve(error as Error));
    }),
    new Promise<null>((resolve) => setTimeout(() => resolve(null), 30))
  ]);

  if (spawnError) {
    throw spawnError;
  }
  if (!child.pid) {
    throw new Error(`failed to launch command: ${command}`);
  }

  child.unref();
  return { pid: child.pid };
}

async function runInDesktop(
  desktop: Desktop,
  command: string,
  args: string[],
  options: { cwd?: string; timeoutMs?: number } = {}
): Promise<RunInDesktopResult> {
  const child = spawn(command, args, {
    cwd: options.cwd || process.cwd(),
    env: desktop.env,
    stdio: ["ignore", "pipe", "pipe"]
  });

  let stdout = "";
  let stderr = "";
  if (child.stdout) {
    child.stdout.on("data", (chunk: Buffer) => {
      stdout += chunk.toString();
    });
  }
  if (child.stderr) {
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString();
    });
  }

  let timedOut = false;
  let timer: NodeJS.Timeout | null = null;
  if (options.timeoutMs && options.timeoutMs > 0) {
    timer = setTimeout(() => {
      timedOut = true;
      try {
        child.kill("SIGKILL");
      } catch {
        // ignore
      }
    }, options.timeoutMs);
  }

  const exit = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });

  if (timer) {
    clearTimeout(timer);
  }
  if (timedOut) {
    throw new Error(`${command} timed out after ${options.timeoutMs}ms`);
  }

  return {
    code: typeof exit.code === "number" ? exit.code : -1,
    signal: exit.signal,
    stdout,
    stderr
  };
}

async function loadEnvFileIfPresent(filePath: string): Promise<void> {
  try {
    const raw = await fs.readFile(filePath, "utf8");
    for (const line of raw.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) {
        continue;
      }

      const eq = trimmed.indexOf("=");
      if (eq <= 0) {
        continue;
      }

      const key = trimmed.slice(0, eq).trim();
      if (!key || process.env[key] !== undefined) {
        continue;
      }

      let value = trimmed.slice(eq + 1).trim();
      if (
        (value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))
      ) {
        value = value.slice(1, -1);
      }

      process.env[key] = value;
    }
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code !== "ENOENT") {
      throw err;
    }
  }
}

class DesktopComputer {
  constructor(private readonly desktop: Desktop, private readonly width: number, private readonly height: number) {}

  private clampPoint(point: readonly [number, number]): [number, number] {
    const x = Math.max(0, Math.min(this.width - 1, Math.round(point[0])));
    const y = Math.max(0, Math.min(this.height - 1, Math.round(point[1])));
    return [x, y];
  }

  private requirePoint(point: [number, number] | undefined, label: string): [number, number] {
    if (!point) {
      throw new Error(`${label} is required for this action`);
    }
    return this.clampPoint(point);
  }

  private async currentCursor(): Promise<[number, number]> {
    const position = await this.desktop.mousePosition();
    return this.clampPoint([position.x, position.y]);
  }

  private async capturePng(region?: [number, number, number, number]): Promise<string> {
    const screenshot = await this.desktop.screenshot({
      region,
      scaleToGeometry: region != null,
      timeoutMs: 20000
    });
    return screenshot.data;
  }

  private async repeatClick(button: "left" | "middle" | "right", count: number): Promise<void> {
    for (let i = 0; i < Math.max(1, count); i += 1) {
      // eslint-disable-next-line no-await-in-loop
      await this.desktop.click(button);
    }
  }

  async execute(input: ComputerActionInput): Promise<ComputerToolOutput> {
    switch (input.action) {
      case "key": {
        if (!input.text) {
          throw new Error("text is required for key action");
        }
        await this.desktop.key(input.text);
        return { kind: "text", text: `pressed key combo: ${input.text}` };
      }
      case "hold_key": {
        if (!input.text) {
          throw new Error("text is required for hold_key action");
        }

        const keys = input.text
          .split("+")
          .map((key) => key.trim())
          .filter((key) => key.length > 0);

        if (keys.length === 0) {
          throw new Error("hold_key requires at least one key");
        }

        for (const key of keys) {
          await this.desktop.keyDown(key);
        }

        const durationMs = Math.max(10, Math.round((input.duration ?? 0.25) * 1000));
        await delay(durationMs);

        for (const key of [...keys].reverse()) {
          await this.desktop.keyUp(key);
        }

        return { kind: "text", text: `held keys for ${durationMs}ms: ${keys.join("+")}` };
      }
      case "type": {
        if (!input.text) {
          throw new Error("text is required for type action");
        }
        await this.desktop.type(input.text);
        return { kind: "text", text: `typed ${input.text.length} characters` };
      }
      case "cursor_position": {
        const [x, y] = await this.currentCursor();
        return { kind: "text", text: `cursor at ${x},${y}` };
      }
      case "mouse_move": {
        const [x, y] = this.requirePoint(input.coordinate, "coordinate");
        await this.desktop.moveMouse(x, y);
        return { kind: "text", text: `moved mouse to ${x},${y}` };
      }
      case "left_mouse_down": {
        await this.desktop.mouseDown("left");
        return { kind: "text", text: "left mouse down" };
      }
      case "left_mouse_up": {
        await this.desktop.mouseUp("left");
        return { kind: "text", text: "left mouse up" };
      }
      case "left_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.desktop.moveMouse(x, y);
        }
        await this.desktop.click("left");
        return { kind: "text", text: "left click" };
      }
      case "left_click_drag": {
        const [startX, startY] = this.requirePoint(input.start_coordinate, "start_coordinate");
        const [endX, endY] = this.requirePoint(input.coordinate, "coordinate");

        await this.desktop.moveMouse(startX, startY);
        await this.desktop.mouseDown("left");
        await this.desktop.moveMouse(endX, endY);
        await this.desktop.mouseUp("left");

        return { kind: "text", text: `dragged from ${startX},${startY} to ${endX},${endY}` };
      }
      case "right_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.desktop.moveMouse(x, y);
        }
        await this.desktop.click("right");
        return { kind: "text", text: "right click" };
      }
      case "middle_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.desktop.moveMouse(x, y);
        }
        await this.desktop.click("middle");
        return { kind: "text", text: "middle click" };
      }
      case "double_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.desktop.moveMouse(x, y);
        }
        await this.repeatClick("left", 2);
        return { kind: "text", text: "double click" };
      }
      case "triple_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.desktop.moveMouse(x, y);
        }
        await this.repeatClick("left", 3);
        return { kind: "text", text: "triple click" };
      }
      case "scroll": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.desktop.moveMouse(x, y);
        }

        const amount = Math.max(1, Math.round(input.scroll_amount ?? 3));
        const direction = input.scroll_direction ?? "down";
        const button =
          direction === "up"
            ? { dx: 0, dy: -amount }
            : direction === "down"
              ? { dx: 0, dy: amount }
              : direction === "left"
                ? { dx: -amount, dy: 0 }
                : { dx: amount, dy: 0 };

        await this.desktop.scroll(button.dx, button.dy);
        return { kind: "text", text: `scrolled ${direction} by ${amount}` };
      }
      case "wait": {
        const waitMs = Math.max(10, Math.round((input.duration ?? 1) * 1000));
        await delay(waitMs);
        return { kind: "text", text: `waited ${waitMs}ms` };
      }
      case "screenshot": {
        const data = await this.capturePng();
        return { kind: "image", data, mediaType: "image/png" };
      }
      case "zoom": {
        const region = input.region;
        const data = await this.capturePng(region);
        return { kind: "image", data, mediaType: "image/png" };
      }
      default: {
        const exhaustiveCheck: never = input.action;
        throw new Error(`unsupported action: ${String(exhaustiveCheck)}`);
      }
    }
  }

  async screenshotBase64(): Promise<string> {
    return this.capturePng();
  }
}

async function main(): Promise<void> {
  await loadEnvFileIfPresent(path.join(repoRoot, ".env.local"));

  if (!process.env.ANTHROPIC_API_KEY) {
    throw new Error("ANTHROPIC_API_KEY is missing. Set it in environment or .env.local at repo root.");
  }

  const options = parseArgs(process.argv.slice(2));
  const { width, height } = parseGeometry(options.geometry);

  const desktop = await start({
    geometry: options.geometry,
    background: { color: options.background },
    openbox: true,
    detached: false
  });

  let shouldStopDesktop = !options.keepAlive;
  let viewerHandle: ViewerHandle | null = null;
  const recordingPath = path.resolve(options.recordPath ?? path.join(exampleRoot, "tmp", `agent-${Date.now()}.mp4`));
  await fs.mkdir(path.dirname(recordingPath), { recursive: true });
  const recordingHandle = await desktop.record({
    file: recordingPath,
    idleSpeedup: 20,
    idleMinDurationSec: 0.35,
    idleNoiseTolerance: "-38dB"
  });
  process.stdout.write(`recording: ${recordingPath}\n`);

  try {
    if (options.viewerEnabled) {
      viewerHandle = await startViewer(desktop, {
        host: options.viewerHost,
        port: options.viewerPort,
        clientScriptPath: path.join(exampleRoot, "dist", "viewer-client.js")
      });
      const viewerUrl = viewerHandle.url;
      process.stdout.write(`viewer: ${viewerUrl}\n`);
      if (options.autoOpenBrowser) {
        openHostBrowser(viewerUrl);
      }
    }

    if (options.appCommand) {
      await openInDesktop(desktop, "bash", ["-lc", options.appCommand], { cwd: repoRoot });
      await delay(1200);
    }

    const computer = new DesktopComputer(desktop, width, height);

    const browserCandidates = [
      "google-chrome",
      "google-chrome-stable",
      "chromium",
      "chromium-browser",
      "firefox"
    ] as const;
    const defaultBrowserPath = await resolveExecutable(browserCandidates);

    const computerTool = anthropic.tools.computer_20251124<ComputerToolOutput>({
      displayWidthPx: width,
      displayHeightPx: height,
      displayNumber: desktop.display,
      enableZoom: true,
      execute: async (input) => computer.execute(input),
      toModelOutput({ output }) {
        if (output.kind === "image") {
          return {
            type: "content",
            value: [
              {
                type: "image-data",
                data: output.data,
                mediaType: output.mediaType
              }
            ]
          };
        }

        return {
          type: "content",
          value: [
            {
              type: "text",
              text: output.text
            }
          ]
        };
      }
    });

    const launchBrowserTool = tool({
      description:
        "Launch a desktop web browser in the current X session. After launching, use computer actions to navigate/type/click.",
      inputSchema: z.object({
        browser: z.enum(["auto", "chrome", "chromium", "firefox"]).optional().default("auto"),
        newWindow: z.boolean().optional().default(true)
      }),
      execute: async ({ browser, newWindow }) => {
        const requested = browser ?? "auto";
        const chosenPath =
          requested === "auto"
            ? defaultBrowserPath
            : await resolveExecutable(
                requested === "chrome"
                  ? ["google-chrome", "google-chrome-stable"]
                  : requested === "chromium"
                    ? ["chromium", "chromium-browser"]
                    : ["firefox"]
              );

        if (!chosenPath) {
          throw new Error(
            `no supported browser found in PATH. looked for: ${[...browserCandidates].join(", ")}`
          );
        }

        const baseName = path.basename(chosenPath);
        const browserArgs: string[] = [];
        if (newWindow) {
          browserArgs.push("--new-window");
        }

        if (baseName.includes("chrome") || baseName.includes("chromium")) {
          const profileDir = path.join(desktop.sessionDir, "profiles", `browser-${Date.now()}`);
          await fs.rm(profileDir, { recursive: true, force: true });
          await fs.mkdir(profileDir, { recursive: true });
          browserArgs.push(`--user-data-dir=${profileDir}`);
          browserArgs.push("--no-first-run", "--no-default-browser-check");
        }

        const launched = await openInDesktop(desktop, chosenPath, browserArgs, { cwd: repoRoot });
        await delay(1200);

        return {
          ok: true,
          browser: chosenPath,
          pid: launched.pid
        };
      }
    });

    const launchAppTool = tool({
      description: "Launch any GUI application in the desktop session.",
      inputSchema: z.object({
        command: z.string().min(1),
        args: z.array(z.string()).optional().default([])
      }),
      execute: async ({ command, args }) => {
        const launched = await openInDesktop(desktop, command, args, { cwd: repoRoot });
        return {
          ok: true,
          pid: launched.pid,
          command,
          args
        };
      }
    });

    const runShellTool = tool({
      description:
        "Run a shell command in the desktop environment and return stdout/stderr. Useful for diagnostics or app launching.",
      inputSchema: z.object({
        command: z.string().min(1),
        timeoutMs: z.number().int().min(100).max(120000).optional().default(20000)
      }),
      execute: async ({ command, timeoutMs }) => {
        const result = await runInDesktop(desktop, "bash", ["-lc", command], {
          timeoutMs
        });

        return {
          code: result.code,
          stdout: result.stdout.slice(0, 4000),
          stderr: result.stderr.slice(0, 4000)
        };
      }
    });

    const agent = new Agent({
      model: anthropic(options.model),
      instructions:
        "You control a remote Linux desktop through tools. Use launchBrowser for web tasks, then computer for mouse/keyboard/screenshot. Be concise and use the fewest actions needed.",
      stopWhen: stepCountIs(options.maxSteps),
      tools: {
        computer: computerTool,
        launchBrowser: launchBrowserTool,
        launchApp: launchAppTool,
        runShell: runShellTool
      }
    });

    process.stdout.write(`model: ${options.model}\n`);
    process.stdout.write(`display: :${desktop.display}\n`);
    process.stdout.write(`vnc: 127.0.0.1:${desktop.port}\n`);

    const result = await agent.generate({
      prompt: options.prompt,
      experimental_onToolCallStart(event) {
        process.stdout.write(`[tool:start] ${event.toolCall.toolName}\n`);
      },
      experimental_onToolCallFinish(event) {
        if (event.success) {
          process.stdout.write(`[tool:done] ${event.toolCall.toolName}\n`);
        } else {
          process.stdout.write(`[tool:error] ${event.toolCall.toolName}: ${String(event.error)}\n`);
        }
      }
    });

    process.stdout.write("\nagent output:\n");
    process.stdout.write(`${result.text || "(no text output)"}\n`);

    if (options.screenshotPath) {
      const finalShot = await computer.screenshotBase64();
      const outPath = path.resolve(options.screenshotPath);
      await fs.mkdir(path.dirname(outPath), { recursive: true });
      await fs.writeFile(outPath, Buffer.from(finalShot, "base64"));
      process.stdout.write(`saved screenshot: ${outPath}\n`);
    }

    if (options.keepAlive) {
      shouldStopDesktop = false;
      process.stdout.write("desktop kept alive (--keep-alive). Stop manually when done.\n");
    }
  } finally {
    try {
      await recordingHandle.stop();
      process.stdout.write(`saved recording: ${recordingPath}\n`);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`warning: failed to finalize recording: ${message}\n`);
    }

    if (viewerHandle) {
      await viewerHandle.stop();
    }

    if (shouldStopDesktop) {
      await desktop.kill();
    }
  }
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
});
