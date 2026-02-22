#!/usr/bin/env node

import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { Command } from "commander";
import WebSocket, { WebSocketServer, type RawData } from "ws";

import { Desktop, createDesktop } from "../index";
import type { BackgroundImageMode, DesktopSizeMode, DesktopState } from "../session";

const defaultStateFile =
  process.env.PORTABLEDESKTOP_STATE_FILE || path.join(os.homedir(), ".cache", "portabledesktop", "session.json");

interface StateFileOption {
  stateFile?: string;
}

interface UpCommandOptions extends StateFileOption {
  json?: boolean;
  foreground?: boolean;
  openbox?: boolean;
  xvncArg?: string[];
  runtimeDir?: string;
  sessionDir?: string;
  display?: string;
  port?: string;
  geometry?: string;
  depth?: string;
  dpi?: number;
  desktopSizeMode?: DesktopSizeMode;
  background?: string;
  backgroundImage?: string;
  backgroundMode?: BackgroundImageMode;
}

interface InfoCommandOptions extends StateFileOption {
  json?: boolean;
}

interface OpenCommandOptions extends StateFileOption {
  cwd?: string;
}

interface RunCommandOptions extends StateFileOption {
  cwd?: string;
  timeoutMs?: number;
  json?: boolean;
  allowNonZero?: boolean;
}

interface CursorCommandOptions extends StateFileOption {
  json?: boolean;
}

interface BackgroundImageCommandOptions extends StateFileOption {
  mode?: BackgroundImageMode;
}

interface ScreenshotCommandOptions extends StateFileOption {
  file?: string;
  json?: boolean;
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  scaleToGeometry?: boolean;
  timeoutMs?: number;
}

interface RecordCommandOptions extends StateFileOption {
  fps?: number;
}

interface ViewerCommandOptions extends StateFileOption {
  host?: string;
  port?: number;
  scale?: ViewerScale;
  open?: boolean;
}

type ViewerScale = "fit" | "1:1";

interface StoredDesktopState extends DesktopState {
  stateFile: string;
  startedAt: string;
}

interface OpenCommandResult {
  pid: number;
  command: string;
  args: string[];
}

interface CommandResult {
  code: number;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
}

function parseInteger(value: string, label: string): number {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) {
    throw new Error(`invalid integer for ${label}: ${value}`);
  }
  return parsed;
}

function parseIntegerOptionValue(name: string): (value: string) => number {
  return (value: string): number => parseInteger(value, `--${name}`);
}

function parseDesktopSizeMode(value: string): DesktopSizeMode {
  const normalized = value.toLowerCase();
  if (normalized === "fixed" || normalized === "dynamic") {
    return normalized;
  }
  throw new Error(`invalid desktop size mode: ${value}. expected fixed|dynamic`);
}

function parseDesktopSizeModeOption(value: string): DesktopSizeMode {
  return parseDesktopSizeMode(value);
}

function parseViewerScaleOption(value: string): ViewerScale {
  const normalized = value.toLowerCase();
  if (normalized === "fit") {
    return "fit";
  }
  if (normalized === "1:1" || normalized === "1x1" || normalized === "native") {
    return "1:1";
  }
  throw new Error(`invalid viewer scale: ${value}. expected fit|1:1`);
}

function parseBackgroundImageModeOption(value: string): BackgroundImageMode {
  const normalized = value.toLowerCase();
  switch (normalized) {
    case "center":
    case "fill":
    case "fit":
    case "stretch":
    case "tile":
      return normalized;
    default:
      throw new Error(`invalid background mode: ${value}. expected center|fill|fit|stretch|tile`);
  }
}

function resolveStateFilePath(stateFile?: string): string {
  return path.resolve(stateFile || defaultStateFile);
}

function parseScreenshotRegion(options: ScreenshotCommandOptions): [number, number, number, number] | undefined {
  const providedCount = [options.x, options.y, options.width, options.height].filter((value) => value !== undefined)
    .length;
  if (providedCount === 0) {
    return undefined;
  }
  if (providedCount !== 4) {
    throw new Error("screenshot region requires --x, --y, --width, and --height together");
  }

  const x = options.x ?? 0;
  const y = options.y ?? 0;
  const width = options.width ?? 0;
  const height = options.height ?? 0;
  if (width <= 0 || height <= 0) {
    throw new Error("screenshot --width and --height must be positive integers");
  }

  return [x, y, x + width, y + height];
}

function viewerHtml(config: { scale: ViewerScale; desktopSizeMode: DesktopSizeMode }): string {
  const serializedConfig = JSON.stringify(config);
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>portabledesktop viewer</title>
    <style>
      html, body {
        margin: 0;
        width: 100%;
        height: 100%;
        background: #12161e;
        color: #e7ebf3;
        font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
      }
      #topbar {
        box-sizing: border-box;
        height: 42px;
        padding: 10px 14px;
        border-bottom: 1px solid #2a3342;
        font-size: 13px;
        display: flex;
        align-items: center;
      }
      #viewer {
        width: 100%;
        height: calc(100% - 42px);
        overflow: hidden;
      }
    </style>
  </head>
  <body>
    <div id="topbar">connecting...</div>
    <div id="viewer"></div>
    <script>globalThis.PORTABLEDESKTOP_VIEWER_CONFIG = ${serializedConfig};</script>
    <script type="module" src="/viewer.js"></script>
  </body>
</html>`;
}

async function pathExists(filePath: string): Promise<boolean> {
  try {
    await fs.access(filePath);
    return true;
  } catch {
    return false;
  }
}

async function loadViewerClientScript(): Promise<string> {
  const currentFilePath = fileURLToPath(import.meta.url);
  const currentDir = path.dirname(currentFilePath);
  const executableDir = path.dirname(process.execPath);

  const bundledCandidates = [
    path.join(currentDir, "viewer-client.js"),
    path.join(executableDir, "viewer-client.js"),
    path.resolve(currentDir, "..", "..", "dist", "bin", "viewer-client.js"),
    path.resolve(process.cwd(), "dist", "bin", "viewer-client.js"),
    path.resolve(currentDir, "..", "..", "dist", "viewer-client.js"),
    path.resolve(process.cwd(), "dist", "viewer-client.js")
  ];

  for (const candidate of bundledCandidates) {
    // eslint-disable-next-line no-await-in-loop
    if (await pathExists(candidate)) {
      // eslint-disable-next-line no-await-in-loop
      return await fs.readFile(candidate, "utf8");
    }
  }

  throw new Error(
    "missing viewer client bundle (dist/bin/viewer-client.js). run the build first or install the published package with dist assets."
  );
}

function spawnDetachedIgnoreErrors(command: string, args: string[]): void {
  const child = spawn(command, args, { detached: true, stdio: "ignore" });
  child.once("error", () => {
    // ignore browser launcher errors (for example: command not in PATH)
  });
  child.unref();
}

function openHostBrowser(url: string): void {
  try {
    if (process.platform === "darwin") {
      spawnDetachedIgnoreErrors("open", [url]);
      return;
    }

    if (process.platform === "win32") {
      spawnDetachedIgnoreErrors("cmd", ["/c", "start", "", url]);
      return;
    }

    spawnDetachedIgnoreErrors("xdg-open", [url]);
  } catch {
    // ignore browser open failures
  }
}

function notFound(res: ServerResponse): void {
  res.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
  res.end("not found");
}

function nowIso(): string {
  return new Date().toISOString();
}

function desktopFromState(state: DesktopState): Desktop {
  return new Desktop({
    runtimeDir: state.runtimeDir,
    display: state.display,
    vncPort: state.vncPort,
    geometry: state.geometry,
    depth: state.depth,
    dpi: state.dpi,
    desktopSizeMode: state.desktopSizeMode,
    sessionDir: state.sessionDir,
    cleanupSessionDirOnStop: state.cleanupSessionDirOnStop,
    xvncPid: state.xvncPid,
    openboxPid: state.openboxPid,
    detached: state.detached
  });
}

async function openCommand(
  desktop: Desktop,
  command: string,
  args: string[],
  options: { cwd?: string } = {}
): Promise<OpenCommandResult> {
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
  return { pid: child.pid, command, args };
}

async function runCommand(
  desktop: Desktop,
  command: string,
  args: string[],
  options: { cwd?: string; timeoutMs?: number } = {}
): Promise<CommandResult> {
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

function isChromeLike(command: string): boolean {
  const base = path.basename(command).toLowerCase();
  return base.includes("chrome") || base.includes("chromium");
}

function assertObject(value: unknown, label: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as Record<string, unknown>;
}

function readRequiredString(obj: Record<string, unknown>, field: string): string {
  const value = obj[field];
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`state field ${field} must be a non-empty string`);
  }
  return value;
}

function readOptionalString(obj: Record<string, unknown>, field: string): string | undefined {
  const value = obj[field];
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`state field ${field} must be a non-empty string`);
  }
  return value;
}

function readRequiredNumber(obj: Record<string, unknown>, field: string): number {
  const value = obj[field];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Error(`state field ${field} must be a finite number`);
  }
  return value;
}

function readOptionalNumber(obj: Record<string, unknown>, field: string): number | null {
  const value = obj[field];
  if (value === undefined || value === null) {
    return null;
  }
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Error(`state field ${field} must be a finite number or null`);
  }
  return value;
}

function readRequiredBoolean(obj: Record<string, unknown>, field: string): boolean {
  const value = obj[field];
  if (typeof value !== "boolean") {
    throw new Error(`state field ${field} must be a boolean`);
  }
  return value;
}

function readOptionalBoolean(obj: Record<string, unknown>, field: string): boolean | undefined {
  const value = obj[field];
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value !== "boolean") {
    throw new Error(`state field ${field} must be a boolean`);
  }
  return value;
}

function parseStoredState(value: unknown, stateFilePath: string): StoredDesktopState {
  const root = assertObject(value, "state");
  const vncPort = readOptionalNumber(root, "vncPort") ?? readOptionalNumber(root, "port");
  if (vncPort == null) {
    throw new Error("state field vncPort must be a finite number");
  }

  return {
    runtimeDir: readRequiredString(root, "runtimeDir"),
    display: readRequiredNumber(root, "display"),
    vncPort,
    geometry: readRequiredString(root, "geometry"),
    depth: readRequiredNumber(root, "depth"),
    dpi: readOptionalNumber(root, "dpi") ?? 96,
    desktopSizeMode: parseDesktopSizeMode(readRequiredString(root, "desktopSizeMode")),
    sessionDir: readRequiredString(root, "sessionDir"),
    cleanupSessionDirOnStop: readOptionalBoolean(root, "cleanupSessionDirOnStop") ?? false,
    xvncPid: readOptionalNumber(root, "xvncPid"),
    openboxPid: readOptionalNumber(root, "openboxPid"),
    detached: readRequiredBoolean(root, "detached"),
    stateFile: readOptionalString(root, "stateFile") || stateFilePath,
    startedAt: readOptionalString(root, "startedAt") || nowIso()
  };
}

async function loadState(stateFilePath: string): Promise<StoredDesktopState> {
  const raw = await fs.readFile(stateFilePath, "utf8");
  const parsed = JSON.parse(raw) as unknown;
  return parseStoredState(parsed, stateFilePath);
}

async function saveState(stateFilePath: string, state: StoredDesktopState): Promise<void> {
  await fs.mkdir(path.dirname(stateFilePath), { recursive: true });
  await fs.writeFile(stateFilePath, `${JSON.stringify(state, null, 2)}\n`);
}

async function cmdUp(options: UpCommandOptions): Promise<void> {
  const stateFilePath = resolveStateFilePath(options.stateFile);
  if (options.background && options.backgroundImage) {
    throw new Error("--background and --background-image are mutually exclusive");
  }
  if (options.backgroundMode && !options.backgroundImage) {
    throw new Error("--background-mode requires --background-image");
  }

  const background =
    options.backgroundImage != null
      ? {
          image: options.backgroundImage,
          mode: options.backgroundMode
        }
      : options.background != null
        ? {
            color: options.background
          }
        : undefined;

  const desktop = await createDesktop({
    detached: options.foreground !== true,
    runtimeDir: options.runtimeDir,
    tempDir: options.sessionDir,
    vnc: {
      displayNumber: options.display,
      vncPort: options.port,
      xvncArgs: options.xvncArg,
      geometry: options.geometry,
      depth: options.depth,
      dpi: options.dpi,
      desktopSizeMode: options.desktopSizeMode
    },
    openbox: options.openbox,
    background
  });

  const state: StoredDesktopState = {
    runtimeDir: desktop.runtimeDir,
    display: desktop.display,
    vncPort: desktop.vncPort,
    geometry: desktop.geometry,
    depth: desktop.depth,
    dpi: desktop.dpi,
    desktopSizeMode: desktop.desktopSizeMode,
    sessionDir: desktop.sessionDir,
    cleanupSessionDirOnStop: desktop.cleanupSessionDirOnStop,
    xvncPid: desktop.xvncPid,
    openboxPid: desktop.openboxPid,
    detached: desktop.detached,
    stateFile: stateFilePath,
    startedAt: nowIso()
  };

  await saveState(stateFilePath, state);

  if (options.json) {
    process.stdout.write(`${JSON.stringify(state)}\n`);
  } else {
    process.stdout.write(`state: ${stateFilePath}\n`);
    process.stdout.write(`display: :${state.display}\n`);
    process.stdout.write(`vnc: 127.0.0.1:${state.vncPort}\n`);
    process.stdout.write(`dpi: ${state.dpi}\n`);
    process.stdout.write(`desktopSizeMode: ${state.desktopSizeMode}\n`);
    process.stdout.write(`runtime: ${state.runtimeDir}\n`);
    process.stdout.write(`session: ${state.sessionDir}\n`);
  }

  if (options.foreground !== true) {
    return;
  }

  let stopping = false;
  const stopAndExit = (): void => {
    if (stopping) {
      return;
    }
    stopping = true;
    void (async () => {
      try {
        await desktop.kill();
      } finally {
        await fs.rm(stateFilePath, { force: true });
        process.exit(0);
      }
    })();
  };

  process.on("SIGINT", stopAndExit);
  process.on("SIGTERM", stopAndExit);

  await new Promise<void>(() => {
    // Keep the foreground process alive until it receives a signal.
  });
}

async function cmdDown(options: StateFileOption): Promise<void> {
  const stateFilePath = resolveStateFilePath(options.stateFile);
  const state = await loadState(stateFilePath);

  const desktop = desktopFromState(state);
  await desktop.kill();

  await fs.rm(stateFilePath, { force: true });
  process.stdout.write("stopped\n");
}

async function cmdInfo(options: InfoCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));

  if (options.json) {
    process.stdout.write(`${JSON.stringify(state)}\n`);
    return;
  }

  process.stdout.write(`state: ${state.stateFile}\n`);
  process.stdout.write(`display: :${state.display}\n`);
  process.stdout.write(`vnc: 127.0.0.1:${state.vncPort}\n`);
  process.stdout.write(`dpi: ${state.dpi}\n`);
  process.stdout.write(`desktopSizeMode: ${state.desktopSizeMode}\n`);
  process.stdout.write(`runtime: ${state.runtimeDir}\n`);
  process.stdout.write(`session: ${state.sessionDir}\n`);
  process.stdout.write(`started: ${state.startedAt}\n`);
}

async function cmdOpen(command: string, commandArgs: string[], options: OpenCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);

  if (isChromeLike(command) && !commandArgs.some((arg) => arg.startsWith("--user-data-dir"))) {
    const profileDir = path.join(state.sessionDir, "profiles", `chrome-${Date.now()}`);
    await fs.mkdir(profileDir, { recursive: true });
    commandArgs.push(`--user-data-dir=${profileDir}`);
  }

  const launched = await openCommand(desktop, command, commandArgs, {
    cwd: options.cwd
  });

  process.stdout.write(`${JSON.stringify(launched)}\n`);
}

async function cmdRun(command: string, commandArgs: string[], options: RunCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);

  const result = await runCommand(desktop, command, commandArgs, {
    cwd: options.cwd,
    timeoutMs: options.timeoutMs
  });

  if (result.code !== 0 && !options.allowNonZero) {
    throw new Error(`${command} exited with code ${result.code}: ${result.stderr || result.stdout}`.trim());
  }

  if (options.json) {
    process.stdout.write(`${JSON.stringify(result)}\n`);
  } else {
    if (result.stdout) {
      process.stdout.write(result.stdout);
    }
    if (result.stderr) {
      process.stderr.write(result.stderr);
    }
    if (result.code !== 0) {
      process.stderr.write(`command exited with code ${result.code}\n`);
    }
  }

  if (result.code !== 0) {
    process.exitCode = result.code;
  }
}

async function cmdBackground(color: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.setBackground({ color });
  process.stdout.write("background updated\n");
}

async function cmdBackgroundImage(
  imagePath: string,
  options: BackgroundImageCommandOptions
): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.setBackground({ image: imagePath, mode: options.mode });
  process.stdout.write("background updated\n");
}

async function cmdMouseMove(x: number, y: number, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.moveMouse(x, y);
  process.stdout.write("ok\n");
}

async function cmdMouseClick(button: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.click(button);
  process.stdout.write("ok\n");
}

async function cmdMouseDown(button: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.mouseDown(button);
  process.stdout.write("ok\n");
}

async function cmdMouseUp(button: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.mouseUp(button);
  process.stdout.write("ok\n");
}

async function cmdMouseScroll(dx: number, dy: number, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.scroll(dx, dy);
  process.stdout.write("ok\n");
}

async function cmdKeyboardType(text: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.type(text);
  process.stdout.write("ok\n");
}

async function cmdKeyboardKey(combo: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.key(combo);
  process.stdout.write("ok\n");
}

async function cmdKeyboardDown(key: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.keyDown(key);
  process.stdout.write("ok\n");
}

async function cmdKeyboardUp(key: string, options: StateFileOption): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  await desktop.keyUp(key);
  process.stdout.write("ok\n");
}

async function cmdCursor(options: CursorCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);
  const cursor = await desktop.mousePosition();

  if (options.json) {
    process.stdout.write(`${JSON.stringify(cursor)}\n`);
    return;
  }

  process.stdout.write(`${cursor.x},${cursor.y}\n`);
}

async function cmdScreenshot(fileArg: string | undefined, options: ScreenshotCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);

  const screenshot = await desktop.screenshot({
    region: parseScreenshotRegion(options),
    scaleToGeometry: options.scaleToGeometry === true,
    timeoutMs: options.timeoutMs
  });

  const outFile = options.file || fileArg;
  const resolvedOutFile = outFile ? path.resolve(outFile) : null;
  if (resolvedOutFile) {
    await fs.mkdir(path.dirname(resolvedOutFile), { recursive: true });
    await fs.writeFile(resolvedOutFile, Buffer.from(screenshot.data, "base64"));
  }

  if (options.json) {
    process.stdout.write(
      `${JSON.stringify({
        ...screenshot,
        file: resolvedOutFile
      })}\n`
    );
    return;
  }

  if (resolvedOutFile) {
    process.stdout.write(`${resolvedOutFile}\n`);
    return;
  }

  process.stdout.write(`${screenshot.data}\n`);
}

async function cmdRecord(fileArg: string | undefined, options: RecordCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const desktop = desktopFromState(state);

  const outFile = fileArg || path.join(state.sessionDir, `recording-${Date.now()}.mp4`);
  const recording = await desktop.record({
    file: outFile,
    fps: options.fps ?? 30
  });

  process.stdout.write(`recording: ${recording.file}\n`);
  process.stdout.write("press Ctrl+C to stop\n");

  await new Promise<void>((resolve, reject) => {
    let stopping = false;
    const stop = (): void => {
      if (stopping) {
        return;
      }
      stopping = true;
      process.off("SIGINT", stop);
      process.off("SIGTERM", stop);
      void (async () => {
        try {
          await recording.stop();
          process.stdout.write(`saved: ${recording.file}\n`);
          resolve();
        } catch (error) {
          reject(error);
        }
      })();
    };

    process.on("SIGINT", stop);
    process.on("SIGTERM", stop);
  });
}

async function cmdViewer(options: ViewerCommandOptions): Promise<void> {
  const state = await loadState(resolveStateFilePath(options.stateFile));
  const targetVncHost = "127.0.0.1";
  const targetVncPort = state.vncPort;

  const host = options.host || "127.0.0.1";
  const listenPort = options.port;
  if (listenPort !== undefined && (!Number.isInteger(listenPort) || listenPort < 0 || listenPort > 65535)) {
    throw new Error(`invalid viewer port: ${String(listenPort)}`);
  }
  const viewerClientScript = await loadViewerClientScript();
  const viewerScale = options.scale || "fit";
  const desktopSizeMode = parseDesktopSizeMode(state.desktopSizeMode);
  const viewerConfig = { scale: viewerScale, desktopSizeMode };

  const server = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    try {
      const url = new URL(req.url || "/", `http://${host}`);
      const pathname = url.pathname;

      if (pathname === "/" || pathname === "/index.html") {
        res.writeHead(200, {
          "content-type": "text/html; charset=utf-8",
          "cache-control": "no-store"
        });
        res.end(viewerHtml(viewerConfig));
        return;
      }

      if (pathname === "/viewer.js") {
        res.writeHead(200, {
          "content-type": "text/javascript; charset=utf-8",
          "cache-control": "no-store"
        });
        res.end(viewerClientScript);
        return;
      }

      if (pathname === "/healthz") {
        res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
        res.end("ok");
        return;
      }

      notFound(res);
    } catch (error) {
      const err = error as NodeJS.ErrnoException;
      if (err.code === "ENOENT") {
        notFound(res);
        return;
      }
      res.writeHead(500, { "content-type": "text/plain; charset=utf-8" });
      res.end(err.message || String(error));
    }
  });

  const sockets = new Set<net.Socket>();
  const wsToTcp = new Map<WebSocket, net.Socket>();

  server.on("connection", (socket) => {
    sockets.add(socket);
    socket.on("close", () => {
      sockets.delete(socket);
    });
  });

  const wss = new WebSocketServer({ noServer: true });

  server.on("upgrade", (req, socket, head) => {
    const url = new URL(req.url || "/", `http://${host}`);
    if (url.pathname !== "/ws") {
      socket.destroy();
      return;
    }

    wss.handleUpgrade(req, socket, head, (ws) => {
      wss.emit("connection", ws, req);
    });
  });

  wss.on("connection", (ws) => {
    const tcp = net.connect({ host: targetVncHost, port: targetVncPort });
    wsToTcp.set(ws, tcp);

    tcp.on("data", (chunk: Buffer) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(chunk);
      }
    });

    tcp.on("error", () => {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close();
      }
    });

    tcp.on("close", () => {
      wsToTcp.delete(ws);
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close();
      }
    });

    ws.on("message", (data: RawData, isBinary: boolean) => {
      if (tcp.destroyed) {
        return;
      }
      if (typeof data === "string") {
        tcp.write(data, "utf8");
        return;
      }
      if (data instanceof ArrayBuffer) {
        tcp.write(Buffer.from(data));
        return;
      }
      if (Array.isArray(data)) {
        for (const chunk of data) {
          tcp.write(Buffer.from(chunk));
        }
        return;
      }
      if (isBinary) {
        tcp.write(data);
      } else {
        tcp.write(data.toString());
      }
    });

    ws.on("close", () => {
      wsToTcp.delete(ws);
      tcp.destroy();
    });

    ws.on("error", () => {
      wsToTcp.delete(ws);
      tcp.destroy();
    });
  });

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(listenPort ?? 0, host, () => {
      server.off("error", reject);
      resolve();
    });
  });

  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("failed to determine viewer server address");
  }

  const url = `http://${host}:${address.port}`;
  process.stdout.write(`viewer: ${url}\n`);
  process.stdout.write(`vnc: ${targetVncHost}:${targetVncPort}\n`);

  if (options.open !== false) {
    openHostBrowser(url);
  }

  await new Promise<void>((resolve) => {
    let stopping = false;
    const stop = (): void => {
      if (stopping) {
        return;
      }
      stopping = true;
      process.off("SIGINT", stop);
      process.off("SIGTERM", stop);

      for (const ws of wss.clients) {
        ws.close();
      }
      for (const socket of wsToTcp.values()) {
        socket.destroy();
      }
      wsToTcp.clear();
      for (const socket of sockets) {
        socket.destroy();
      }
      sockets.clear();

      wss.close(() => {
        server.close(() => {
          resolve();
        });
      });
    };

    process.on("SIGINT", stop);
    process.on("SIGTERM", stop);
  });
}

function withStateFileOption(command: Command): Command {
  command.option("--state-file <path>", "path to desktop state file", defaultStateFile);
  return command;
}

function createProgram(): Command {
  const program = new Command();
  program.name("portabledesktop");
  program.description("Portable Linux desktop runtime CLI");
  program.enablePositionalOptions();
  program.showHelpAfterError();

  withStateFileOption(program.command("up").description("start a desktop session"))
    .option("--json", "print session info as JSON")
    .option("--foreground", "run in foreground until interrupted")
    .option("--no-openbox", "disable Openbox (lightweight window manager)")
    .option(
      "--xvnc-arg <arg>",
      "extra Xvnc argument (repeatable; use --xvnc-arg=<value> for values that start with '-')",
      (value: string, previous: string[] | undefined) => [...(previous ?? []), value]
    )
    .option("--runtime-dir <path>", "runtime directory")
    .option("--session-dir <path>", "session directory")
    .option("--display <n>", "display number")
    .option("--port <n>", "VNC port")
    .option("--geometry <WxH>", "desktop geometry")
    .option("--depth <n>", "color depth")
    .option("--dpi <n>", "desktop DPI", parseIntegerOptionValue("dpi"))
    .option("--desktop-size-mode <mode>", "desktop size mode (fixed|dynamic)", parseDesktopSizeModeOption)
    .option("--background <color>", "background color")
    .option("--background-image <file>", "background image file")
    .option(
      "--background-mode <mode>",
      "background image mode (center|fill|fit|stretch|tile)",
      parseBackgroundImageModeOption
    )
    .action(async (options: UpCommandOptions) => {
      await cmdUp(options);
    });

  withStateFileOption(program.command("down").description("stop a desktop session")).action(
    async (options: StateFileOption) => {
      await cmdDown(options);
    }
  );

  withStateFileOption(program.command("info").description("show session info"))
    .option("--json", "print state JSON")
    .action(async (options: InfoCommandOptions) => {
      await cmdInfo(options);
    });

  withStateFileOption(
    program
      .command("open")
      .description("launch a detached program in the desktop environment")
      .allowUnknownOption(true)
      .passThroughOptions()
      .option("--cwd <path>", "working directory")
      .argument("<command>", "program to launch")
      .argument("[args...]", "program arguments")
  ).action(async (command: string, args: string[] | undefined, options: OpenCommandOptions) => {
    await cmdOpen(command, args ?? [], options);
  });

  withStateFileOption(
    program
      .command("run")
      .description("run a command in the desktop environment and capture output")
      .allowUnknownOption(true)
      .passThroughOptions()
      .option("--cwd <path>", "working directory")
      .option("--timeout-ms <n>", "command timeout in milliseconds", parseIntegerOptionValue("timeout-ms"))
      .option("--json", "print structured JSON output")
      .option("--allow-non-zero", "allow non-zero exit code")
      .argument("<command>", "program to run")
      .argument("[args...]", "program arguments")
  ).action(async (command: string, args: string[] | undefined, options: RunCommandOptions) => {
    await cmdRun(command, args ?? [], options);
  });

  withStateFileOption(
    program.command("background").description("set solid background color").argument("<color>", "hex/rgb")
  )
    .action(async (color: string, options: StateFileOption) => {
      await cmdBackground(color, options);
    });

  withStateFileOption(
    program
      .command("background-image")
      .description("set background image")
      .argument("<file>", "path to image file")
      .option(
        "--mode <mode>",
        "background image mode (center|fill|fit|stretch|tile)",
        parseBackgroundImageModeOption
      )
  ).action(async (filePath: string, options: BackgroundImageCommandOptions) => {
    await cmdBackgroundImage(filePath, options);
  });

  withStateFileOption(program.command("cursor").description("print cursor position"))
    .option("--json", "print cursor position as JSON")
    .action(async (options: CursorCommandOptions) => {
      await cmdCursor(options);
    });

  withStateFileOption(
    program
      .command("screenshot")
      .description("capture screenshot as file path or base64")
      .argument("[file]", "output PNG path")
      .option("--file <path>", "output PNG path")
      .option("--json", "print result as JSON")
      .option("--x <n>", "capture region x", parseIntegerOptionValue("x"))
      .option("--y <n>", "capture region y", parseIntegerOptionValue("y"))
      .option("--width <n>", "capture region width", parseIntegerOptionValue("width"))
      .option("--height <n>", "capture region height", parseIntegerOptionValue("height"))
      .option("--scale-to-geometry", "scale cropped region to full desktop geometry")
      .option("--timeout-ms <n>", "capture timeout", parseIntegerOptionValue("timeout-ms"))
  ).action(async (fileArg: string | undefined, options: ScreenshotCommandOptions) => {
    await cmdScreenshot(fileArg, options);
  });

  withStateFileOption(
    program
      .command("viewer")
      .description("serve a live browser viewer for the current desktop session")
      .option("--host <host>", "viewer listen host", "127.0.0.1")
      .option("--port <n>", "viewer listen port (0 = random)", parseIntegerOptionValue("port"))
      .option("--scale <mode>", "viewer scale mode (fit|1:1)", parseViewerScaleOption)
      .option("--no-open", "do not auto-open browser")
  ).action(async (options: ViewerCommandOptions) => {
    await cmdViewer(options);
  });

  const mouse = program.command("mouse").description("mouse actions");
  withStateFileOption(mouse.command("move").argument("<x>", "x coordinate").argument("<y>", "y coordinate")).action(
    async (x: string, y: string, options: StateFileOption) => {
      await cmdMouseMove(parseInteger(x, "move x"), parseInteger(y, "move y"), options);
    }
  );
  withStateFileOption(
    mouse.command("click").argument("[button]", "left|middle|right", "left")
  ).action(async (button: string, options: StateFileOption) => {
    await cmdMouseClick(button, options);
  });
  withStateFileOption(
    mouse.command("down").argument("[button]", "left|middle|right", "left")
  ).action(async (button: string, options: StateFileOption) => {
    await cmdMouseDown(button, options);
  });
  withStateFileOption(mouse.command("up").argument("[button]", "left|middle|right", "left")).action(
    async (button: string, options: StateFileOption) => {
      await cmdMouseUp(button, options);
    }
  );
  withStateFileOption(mouse.command("scroll").argument("[dx]", "horizontal amount", "0").argument("[dy]", "vertical amount", "0"))
    .action(async (dx: string, dy: string, options: StateFileOption) => {
      await cmdMouseScroll(parseInteger(dx, "scroll dx"), parseInteger(dy, "scroll dy"), options);
    });

  const keyboard = program.command("keyboard").description("keyboard actions");
  withStateFileOption(keyboard.command("type").argument("<text...>", "text to type")).action(
    async (textParts: string[], options: StateFileOption) => {
      await cmdKeyboardType(textParts.join(" "), options);
    }
  );
  withStateFileOption(keyboard.command("key").argument("<combo...>", "key combo")).action(
    async (comboParts: string[], options: StateFileOption) => {
      await cmdKeyboardKey(comboParts.join(" "), options);
    }
  );
  withStateFileOption(keyboard.command("down").argument("<key...>", "key")).action(
    async (keyParts: string[], options: StateFileOption) => {
      await cmdKeyboardDown(keyParts.join(" "), options);
    }
  );
  withStateFileOption(keyboard.command("up").argument("<key...>", "key")).action(
    async (keyParts: string[], options: StateFileOption) => {
      await cmdKeyboardUp(keyParts.join(" "), options);
    }
  );

  withStateFileOption(
    program.command("record").description("record desktop session until interrupted").argument("[file]", "recording output file")
  )
    .option("--fps <n>", "recording frames per second", parseIntegerOptionValue("fps"))
    .action(async (fileArg: string | undefined, options: RecordCommandOptions) => {
      await cmdRecord(fileArg, options);
    });

  return program;
}

async function main(): Promise<void> {
  const program = createProgram();

  if (process.argv.length <= 2) {
    program.outputHelp();
    return;
  }

  await program.parseAsync(process.argv);
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
});
