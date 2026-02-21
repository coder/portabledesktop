import fs from "node:fs";
import fsp from "node:fs/promises";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { spawn, type ChildProcess } from "node:child_process";

import { ensureRuntime, resolveRuntimeBinary, type EnsureRuntimeOptions } from "./runtime";

interface WaitForPortOptions {
  host?: string;
  port: number;
  timeoutMs?: number;
}

interface ExitStatus {
  exited: boolean;
  signal: NodeJS.Signals | null;
  code: number | null;
}

interface RunAndCaptureOptions {
  env?: NodeJS.ProcessEnv;
  cwd?: string;
  timeoutMs?: number;
}

interface RunAndCaptureResult {
  code: number | null;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
}

interface RunAndCaptureBinaryResult {
  code: number | null;
  signal: NodeJS.Signals | null;
  stdout: Buffer;
  stderr: string;
}

export interface StartOptions extends EnsureRuntimeOptions {
  sessionDir?: string;
  cleanupSessionDirOnStop?: boolean;
  display?: number | string;
  port?: number | string;
  geometry?: string;
  depth?: number | string;
  startupTimeoutMs?: number | string;
  openbox?: boolean;
  detached?: boolean;
  background?: {
    color?: string;
  };
}

export interface KillOptions {
  cleanup?: boolean;
}

export interface CursorPosition {
  x: number;
  y: number;
}

export interface ScreenshotOptions {
  region?: [number, number, number, number];
  scaleToGeometry?: boolean;
  timeoutMs?: number | string;
}

export interface ScreenshotImage {
  data: string;
  mediaType: "image/png";
}

export interface StartRecordingOptions {
  file?: string;
  fps?: number | string;
  detached?: boolean;
  idleSpeedup?: number | string;
  idleMinDurationSec?: number | string;
  idleNoiseTolerance?: number | string;
}

export interface RecordingHandle {
  pid: number;
  file: string;
  logPath: string;
  detached: boolean;
  stop: () => Promise<void>;
}

export interface DetachedRecordingResult {
  pid: number;
  file: string;
  logPath: string;
  detached: true;
}

export interface AttachedRecordingResult {
  pid: number;
  file: string;
  logPath: string;
  detached: false;
}

export type RecordingResult = DetachedRecordingResult | AttachedRecordingResult;

export interface DesktopState {
  runtimeDir: string;
  display: number;
  port: number;
  geometry: string;
  depth: number;
  sessionDir: string;
  cleanupSessionDirOnStop: boolean;
  xvncPid: number | null;
  openboxPid: number | null;
  detached: boolean;
}

interface DesktopCtor {
  runtimeDir: string;
  display: number;
  port: number;
  geometry: string;
  depth: number;
  sessionDir: string;
  cleanupSessionDirOnStop?: boolean;
  xvncPid?: number | null;
  openboxPid?: number | null;
  detached?: boolean;
}

interface IdleSpeedupConfig {
  factor: number;
  minDurationSec: number;
  noiseTolerance: string;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function assertLinux(): void {
  if (process.platform !== "linux") {
    throw new Error(`portabledesktop only supports linux right now (got ${process.platform})`);
  }
}

async function isPortFree(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.unref();
    server.once("error", () => resolve(false));
    server.listen({ host: "127.0.0.1", port }, () => {
      server.close(() => resolve(true));
    });
  });
}

async function waitForPort({ host = "127.0.0.1", port, timeoutMs = 15000 }: WaitForPortOptions): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const ok = await new Promise<boolean>((resolve) => {
      const socket = net.connect({ host, port });
      socket.once("connect", () => {
        socket.destroy();
        resolve(true);
      });
      socket.once("error", () => {
        resolve(false);
      });
    });

    if (ok) {
      return;
    }

    await delay(100);
  }

  throw new Error(`timed out waiting for TCP ${host}:${port}`);
}

function normalizeDisplay(value: number | string | undefined): number | null {
  if (value == null) {
    return null;
  }
  if (typeof value === "number") {
    if (!Number.isInteger(value) || value < 0) {
      throw new Error(`invalid display value: ${value}`);
    }
    return value;
  }
  const text = value.trim();
  const stripped = text.startsWith(":") ? text.slice(1) : text;
  const parsed = Number.parseInt(stripped, 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error(`invalid display value: ${value}`);
  }
  return parsed;
}

function normalizePort(value: number | string | undefined): number | null {
  if (value == null) {
    return null;
  }
  const parsed = typeof value === "number" ? value : Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed < 1 || parsed > 65535) {
    throw new Error(`invalid port value: ${value}`);
  }
  return parsed;
}

function normalizeInteger(value: number | string | undefined, defaultValue: number): number {
  if (value == null) {
    return defaultValue;
  }
  const parsed = typeof value === "number" ? value : Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) {
    throw new Error(`invalid integer value: ${value}`);
  }
  return parsed;
}

function normalizePositiveNumber(value: number | string | undefined, defaultValue: number, fieldName: string): number {
  if (value == null) {
    return defaultValue;
  }

  const parsed = typeof value === "number" ? value : Number.parseFloat(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error(`${fieldName} must be a positive number`);
  }
  return parsed;
}

async function pickDisplayAndPort(
  displayOption: number | string | undefined,
  portOption: number | string | undefined
): Promise<{ display: number; port: number }> {
  const display = normalizeDisplay(displayOption);
  const port = normalizePort(portOption);

  if (display != null && port != null) {
    return { display, port };
  }

  if (display != null) {
    return { display, port: port ?? (5900 + display) };
  }

  if (port != null) {
    const implied = port >= 5900 && port <= 5999 ? port - 5900 : null;
    return { display: implied ?? 1, port };
  }

  for (let d = 1; d <= 99; d += 1) {
    const p = 5900 + d;
    if (fs.existsSync(`/tmp/.X11-unix/X${d}`)) {
      continue;
    }
    // eslint-disable-next-line no-await-in-loop
    if (await isPortFree(p)) {
      return { display: d, port: p };
    }
  }

  throw new Error("no available display/port pairs in :1-:99 / 5901-5999");
}

function buttonToNumber(button: number | string): number {
  if (typeof button === "number") {
    return button;
  }
  switch (button.toLowerCase()) {
    case "left":
      return 1;
    case "middle":
      return 2;
    case "right":
      return 3;
    default:
      return Number.parseInt(button, 10) || 1;
  }
}

async function waitForExit(child: ChildProcess, timeoutMs = 5000): Promise<ExitStatus> {
  return new Promise((resolve) => {
    let settled = false;
    const timer = setTimeout(() => {
      if (!settled) {
        settled = true;
        resolve({ exited: false, signal: null, code: null });
      }
    }, timeoutMs);

    child.once("exit", (code, signal) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        resolve({ exited: true, signal, code });
      }
    });
  });
}

async function killPid(pid: number, { timeoutMs = 5000 }: { timeoutMs?: number } = {}): Promise<void> {
  try {
    process.kill(pid, "SIGTERM");
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code === "ESRCH") {
      return;
    }
    throw err;
  }

  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0);
      await delay(100);
    } catch (error) {
      const err = error as NodeJS.ErrnoException;
      if (err.code === "ESRCH") {
        return;
      }
      throw err;
    }
  }

  try {
    process.kill(pid, "SIGKILL");
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code !== "ESRCH") {
      throw err;
    }
  }
}

async function runAndCapture(command: string, args: string[], options: RunAndCaptureOptions = {}): Promise<RunAndCaptureResult> {
  const env = options.env ?? process.env;
  const cwd = options.cwd ?? process.cwd();
  const timeoutMs = options.timeoutMs;

  const child = spawn(command, args, {
    env,
    cwd,
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

  let timer: NodeJS.Timeout | undefined;
  let timedOut = false;
  if (typeof timeoutMs === "number" && timeoutMs > 0) {
    timer = setTimeout(() => {
      timedOut = true;
      try {
        child.kill("SIGKILL");
      } catch {
        // ignore
      }
    }, timeoutMs);
  }

  const exit = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });

  if (timer) {
    clearTimeout(timer);
  }

  if (timedOut) {
    throw new Error(`${command} timed out after ${timeoutMs}ms`);
  }

  return {
    code: exit.code,
    signal: exit.signal,
    stdout,
    stderr
  };
}

async function runAndCaptureBinary(
  command: string,
  args: string[],
  options: RunAndCaptureOptions = {}
): Promise<RunAndCaptureBinaryResult> {
  const env = options.env ?? process.env;
  const cwd = options.cwd ?? process.cwd();
  const timeoutMs = options.timeoutMs;

  const child = spawn(command, args, {
    env,
    cwd,
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stdoutChunks: Buffer[] = [];
  let stderr = "";

  if (child.stdout) {
    child.stdout.on("data", (chunk: Buffer) => {
      stdoutChunks.push(chunk);
    });
  }
  if (child.stderr) {
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString();
    });
  }

  let timer: NodeJS.Timeout | undefined;
  let timedOut = false;
  if (typeof timeoutMs === "number" && timeoutMs > 0) {
    timer = setTimeout(() => {
      timedOut = true;
      try {
        child.kill("SIGKILL");
      } catch {
        // ignore
      }
    }, timeoutMs);
  }

  const exit = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });

  if (timer) {
    clearTimeout(timer);
  }

  if (timedOut) {
    throw new Error(`${command} timed out after ${timeoutMs}ms`);
  }

  return {
    code: exit.code,
    signal: exit.signal,
    stdout: Buffer.concat(stdoutChunks),
    stderr
  };
}

function parseGeometrySize(geometry: string): { width: number; height: number } {
  const match = /^(\d+)x(\d+)$/.exec(geometry);
  if (!match) {
    throw new Error(`invalid geometry: ${geometry}. expected WxH`);
  }

  const width = Number.parseInt(match[1], 10);
  const height = Number.parseInt(match[2], 10);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width < 1 || height < 1) {
    throw new Error(`invalid geometry dimensions: ${geometry}`);
  }

  return { width, height };
}

function ensureString(value: unknown, fieldName: string): asserts value is string {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`${fieldName} must be a non-empty string`);
  }
}

export class Desktop {
  public readonly runtimeDir: string;
  public readonly display: number;
  public readonly port: number;
  public readonly geometry: string;
  public readonly depth: number;
  public readonly sessionDir: string;
  public readonly cleanupSessionDirOnStop: boolean;
  public readonly detached: boolean;

  public xvncPid: number | null;
  public openboxPid: number | null;

  private _recordingChild: ChildProcess | null;

  constructor(input: DesktopCtor) {
    this.runtimeDir = input.runtimeDir;
    this.display = input.display;
    this.port = input.port;
    this.geometry = input.geometry;
    this.depth = input.depth;
    this.sessionDir = input.sessionDir;
    this.cleanupSessionDirOnStop = input.cleanupSessionDirOnStop ?? false;
    this.detached = input.detached ?? false;

    this.xvncPid = input.xvncPid ?? null;
    this.openboxPid = input.openboxPid ?? null;

    this._recordingChild = null;
  }

  private displayString(): string {
    return `:${this.display}`;
  }

  public get env(): NodeJS.ProcessEnv {
    return this.baseEnv();
  }

  private baseEnv(extraEnv: NodeJS.ProcessEnv = {}): NodeJS.ProcessEnv {
    const runtimeBin = path.join(this.runtimeDir, "bin");
    const runtimeShare = path.join(this.runtimeDir, "share");
    const fontconfigFile = path.join(this.runtimeDir, "etc", "fonts", "fonts.conf");
    const fontconfigPath = path.join(this.runtimeDir, "etc", "fonts");
    const pathPrefix = process.env.PATH ? `${runtimeBin}:${process.env.PATH}` : runtimeBin;
    const env: NodeJS.ProcessEnv = {
      ...process.env,
      DISPLAY: this.displayString(),
      PATH: pathPrefix,
      ...extraEnv
    };

    if (fs.existsSync(runtimeShare)) {
      env.XDG_DATA_DIRS = process.env.XDG_DATA_DIRS ? `${runtimeShare}:${process.env.XDG_DATA_DIRS}` : runtimeShare;
    }

    if (fs.existsSync(fontconfigFile)) {
      env.FONTCONFIG_FILE = fontconfigFile;
    }

    if (fs.existsSync(fontconfigPath)) {
      env.FONTCONFIG_PATH = fontconfigPath;
    }

    return env;
  }

  private runtimeBinary(name: string): string {
    return resolveRuntimeBinary(this.runtimeDir, name);
  }

  private async runToolCapture(
    binaryName: string,
    args: string[],
    extraEnv: NodeJS.ProcessEnv = {},
    options: { timeoutMs?: number } = {}
  ): Promise<RunAndCaptureResult> {
    const command = this.runtimeBinary(binaryName);
    const result = await runAndCapture(command, args, {
      env: this.baseEnv(extraEnv),
      timeoutMs: options.timeoutMs
    });

    if (result.code !== 0) {
      throw new Error(`${command} failed with code ${result.code ?? "null"}: ${result.stderr || result.stdout}`.trim());
    }

    return result;
  }

  private async runTool(binaryName: string, args: string[], extraEnv: NodeJS.ProcessEnv = {}): Promise<void> {
    await this.runToolCapture(binaryName, args, extraEnv);
  }

  async setBackground(color: string): Promise<void> {
    ensureString(color, "color");
    await this.runTool("xsetroot", ["-solid", color]);
  }

  async moveMouse(x: number, y: number): Promise<void> {
    await this.runTool("xdotool", ["mousemove", "--sync", String(x), String(y)]);
  }

  async mousePosition(): Promise<CursorPosition> {
    const result = await this.runToolCapture("xdotool", ["getmouselocation", "--shell"]);
    const xMatch = /X=(\d+)/.exec(result.stdout);
    const yMatch = /Y=(\d+)/.exec(result.stdout);
    if (!xMatch || !yMatch) {
      throw new Error(`failed to parse cursor position from xdotool output: ${result.stdout}`);
    }

    return {
      x: Number.parseInt(xMatch[1], 10),
      y: Number.parseInt(yMatch[1], 10)
    };
  }

  async click(button: number | string = "left"): Promise<void> {
    await this.runTool("xdotool", ["click", String(buttonToNumber(button))]);
  }

  async mouseDown(button: number | string = "left"): Promise<void> {
    await this.runTool("xdotool", ["mousedown", String(buttonToNumber(button))]);
  }

  async mouseUp(button: number | string = "left"): Promise<void> {
    await this.runTool("xdotool", ["mouseup", String(buttonToNumber(button))]);
  }

  async scroll(dx = 0, dy = 0): Promise<void> {
    const clicks: string[] = [];

    if (dy < 0) {
      for (let i = 0; i < Math.abs(dy); i += 1) {
        clicks.push("4");
      }
    }
    if (dy > 0) {
      for (let i = 0; i < dy; i += 1) {
        clicks.push("5");
      }
    }
    if (dx < 0) {
      for (let i = 0; i < Math.abs(dx); i += 1) {
        clicks.push("6");
      }
    }
    if (dx > 0) {
      for (let i = 0; i < dx; i += 1) {
        clicks.push("7");
      }
    }

    for (const button of clicks) {
      // eslint-disable-next-line no-await-in-loop
      await this.runTool("xdotool", ["click", button]);
    }
  }

  async type(text: string): Promise<void> {
    ensureString(text, "text");
    await this.runTool("xdotool", ["type", "--delay", "1", "--", text]);
  }

  async key(keyCombo: string): Promise<void> {
    ensureString(keyCombo, "keyCombo");
    await this.runTool("xdotool", ["key", "--clearmodifiers", keyCombo]);
  }

  async keyDown(key: string): Promise<void> {
    ensureString(key, "key");
    await this.runTool("xdotool", ["keydown", key]);
  }

  async keyUp(key: string): Promise<void> {
    ensureString(key, "key");
    await this.runTool("xdotool", ["keyup", key]);
  }

  async screenshot(options: ScreenshotOptions = {}): Promise<ScreenshotImage> {
    const { width, height } = parseGeometrySize(this.geometry);
    const timeoutMs = normalizeInteger(options.timeoutMs, 20000);

    const ffmpegPath = this.runtimeBinary("ffmpeg");
    const ffmpegArgs = [
      "-loglevel",
      "error",
      "-f",
      "x11grab",
      "-video_size",
      `${width}x${height}`,
      "-i",
      this.displayString(),
      "-frames:v",
      "1"
    ];

    const region = options.region;
    if (region) {
      const [x1, y1, x2, y2] = region;
      const left = Math.max(0, Math.min(x1, x2));
      const top = Math.max(0, Math.min(y1, y2));
      const right = Math.max(left + 1, Math.min(width, Math.max(x1, x2)));
      const bottom = Math.max(top + 1, Math.min(height, Math.max(y1, y2)));
      const cropWidth = right - left;
      const cropHeight = bottom - top;
      const filters = [`crop=${cropWidth}:${cropHeight}:${left}:${top}`];

      if (options.scaleToGeometry === true) {
        filters.push(`scale=${width}:${height}:flags=neighbor`);
      }

      ffmpegArgs.push("-vf", filters.join(","));
    }

    ffmpegArgs.push("-f", "image2pipe", "-vcodec", "png", "pipe:1");

    const result = await runAndCaptureBinary(ffmpegPath, ffmpegArgs, {
      env: this.baseEnv(),
      timeoutMs
    });

    if (result.code !== 0) {
      throw new Error(`ffmpeg screenshot failed with code ${result.code ?? "null"}: ${result.stderr}`.trim());
    }

    return {
      data: result.stdout.toString("base64"),
      mediaType: "image/png"
    };
  }

  private resolveIdleSpeedupConfig(options: StartRecordingOptions): IdleSpeedupConfig | null {
    const factor = normalizePositiveNumber(options.idleSpeedup, 1, "idleSpeedup");
    if (factor <= 1) {
      return null;
    }

    const minDurationSec = normalizePositiveNumber(options.idleMinDurationSec, 0.75, "idleMinDurationSec");
    const rawNoise = options.idleNoiseTolerance ?? "-45dB";
    const noiseTolerance = typeof rawNoise === "number" ? String(rawNoise) : rawNoise.trim();
    if (noiseTolerance.length === 0) {
      throw new Error("idleNoiseTolerance must be non-empty");
    }

    return {
      factor,
      minDurationSec,
      noiseTolerance
    };
  }

  private async getMediaDurationSeconds(filePath: string): Promise<number> {
    const ffmpegPath = this.runtimeBinary("ffmpeg");
    const result = await runAndCapture(ffmpegPath, ["-hide_banner", "-i", filePath, "-f", "null", "-"], {
      env: this.baseEnv()
    });
    const combined = `${result.stdout}\n${result.stderr}`;
    const match = /Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)/.exec(combined);
    if (!match) {
      throw new Error(`failed to parse media duration for ${filePath}`);
    }

    const hours = Number.parseInt(match[1], 10);
    const minutes = Number.parseInt(match[2], 10);
    const seconds = Number.parseFloat(match[3]);
    const total = hours * 3600 + minutes * 60 + seconds;
    if (!Number.isFinite(total) || total <= 0) {
      throw new Error(`invalid media duration for ${filePath}`);
    }

    return total;
  }

  private parseFreezeIntervals(output: string, durationSec: number): Array<{ start: number; end: number }> {
    const intervals: Array<{ start: number; end: number }> = [];
    let currentStart: number | null = null;

    for (const line of output.split(/\r?\n/)) {
      const startMatch = /freeze_start:\s*([0-9.]+)/.exec(line);
      if (startMatch) {
        currentStart = Number.parseFloat(startMatch[1]);
        continue;
      }

      const endMatch = /freeze_end:\s*([0-9.]+)/.exec(line);
      if (endMatch) {
        const end = Number.parseFloat(endMatch[1]);
        if (currentStart != null && Number.isFinite(end) && end > currentStart) {
          intervals.push({
            start: Math.max(0, Math.min(currentStart, durationSec)),
            end: Math.max(0, Math.min(end, durationSec))
          });
        }
        currentStart = null;
      }
    }

    if (currentStart != null && durationSec > currentStart) {
      intervals.push({
        start: Math.max(0, Math.min(currentStart, durationSec)),
        end: durationSec
      });
    }

    if (intervals.length < 2) {
      return intervals.filter((interval) => interval.end - interval.start > 0.02);
    }

    intervals.sort((a, b) => a.start - b.start);
    const merged: Array<{ start: number; end: number }> = [];

    for (const interval of intervals) {
      if (interval.end - interval.start <= 0.02) {
        continue;
      }
      const last = merged[merged.length - 1];
      if (!last || interval.start > last.end + 0.02) {
        merged.push({ ...interval });
        continue;
      }
      last.end = Math.max(last.end, interval.end);
    }

    return merged;
  }

  private async speedupIdleSegments(filePath: string, config: IdleSpeedupConfig): Promise<void> {
    const ffmpegPath = this.runtimeBinary("ffmpeg");
    const detectArgs = [
      "-hide_banner",
      "-loglevel",
      "info",
      "-i",
      filePath,
      "-vf",
      `freezedetect=n=${config.noiseTolerance}:d=${config.minDurationSec}`,
      "-an",
      "-f",
      "null",
      "-"
    ];

    const detect = await runAndCapture(ffmpegPath, detectArgs, { env: this.baseEnv() });
    if (detect.code !== 0) {
      throw new Error(`ffmpeg freezedetect failed with code ${detect.code ?? "null"}: ${detect.stderr}`.trim());
    }

    const durationSec = await this.getMediaDurationSeconds(filePath);
    const freezeIntervals = this.parseFreezeIntervals(`${detect.stdout}\n${detect.stderr}`, durationSec);
    if (freezeIntervals.length === 0) {
      return;
    }

    const segments: Array<{ start: number; end: number; speed: number }> = [];
    let cursor = 0;
    for (const freeze of freezeIntervals) {
      if (freeze.start > cursor + 0.01) {
        segments.push({ start: cursor, end: freeze.start, speed: 1 });
      }
      if (freeze.end > freeze.start + 0.01) {
        segments.push({ start: freeze.start, end: freeze.end, speed: config.factor });
      }
      cursor = Math.max(cursor, freeze.end);
    }
    if (durationSec > cursor + 0.01) {
      segments.push({ start: cursor, end: durationSec, speed: 1 });
    }
    if (segments.length === 0) {
      return;
    }

    const filterLines: string[] = [];
    const labels: string[] = [];
    segments.forEach((segment, index) => {
      const label = `v${index}`;
      const setpts = segment.speed === 1 ? "PTS-STARTPTS" : `(PTS-STARTPTS)/${segment.speed}`;
      filterLines.push(
        `[0:v]trim=start=${segment.start.toFixed(6)}:end=${segment.end.toFixed(6)},setpts=${setpts}[${label}]`
      );
      labels.push(`[${label}]`);
    });
    filterLines.push(`${labels.join("")}concat=n=${labels.length}:v=1:a=0[vout]`);

    const filterScriptPath = path.join(this.sessionDir, `record-speedup-${Date.now()}.fcs`);
    const tempOutputPath = `${filePath}.speedup.tmp.mp4`;
    await fsp.writeFile(filterScriptPath, `${filterLines.join(";\n")}\n`);

    try {
      const render = await runAndCapture(
        ffmpegPath,
        [
          "-hide_banner",
          "-loglevel",
          "error",
          "-y",
          "-i",
          filePath,
          "-filter_complex_script",
          filterScriptPath,
          "-map",
          "[vout]",
          "-an",
          "-c:v",
          "libx264",
          "-preset",
          "veryfast",
          "-pix_fmt",
          "yuv420p",
          tempOutputPath
        ],
        { env: this.baseEnv() }
      );
      if (render.code !== 0) {
        throw new Error(`ffmpeg speedup render failed with code ${render.code ?? "null"}: ${render.stderr}`.trim());
      }

      await fsp.rename(tempOutputPath, filePath);
    } finally {
      await fsp.rm(filterScriptPath, { force: true });
      await fsp.rm(tempOutputPath, { force: true }).catch(() => {});
    }
  }

  private async startRecordingInternal(options: StartRecordingOptions = {}): Promise<RecordingResult> {
    if (this._recordingChild) {
      throw new Error("recording already in progress");
    }

    const fps = normalizeInteger(options.fps, 30);
    const outputPath = options.file
      ? path.resolve(options.file)
      : path.join(this.sessionDir, `recording-${Date.now()}.mp4`);
    const detached = options.detached ?? false;

    await fsp.mkdir(path.dirname(outputPath), { recursive: true });

    const ffmpegPath = this.runtimeBinary("ffmpeg");
    const ffmpegArgs = [
      "-y",
      "-f",
      "x11grab",
      "-framerate",
      String(fps),
      "-video_size",
      this.geometry,
      "-i",
      this.displayString(),
      "-codec:v",
      "libx264",
      "-preset",
      "ultrafast",
      "-pix_fmt",
      "yuv420p",
      outputPath
    ];

    const logPath = path.join(this.sessionDir, "record.log");
    const logFd = fs.openSync(logPath, "a");

    const child = spawn(ffmpegPath, ffmpegArgs, {
      env: this.baseEnv(),
      detached,
      stdio: ["ignore", logFd, logFd]
    });

    const spawnError = await Promise.race<Error | null>([
      new Promise((resolve) => {
        child.once("error", (error) => resolve(error as Error));
      }),
      delay(30).then(() => null)
    ]);

    if (spawnError) {
      throw new Error(`failed to start ffmpeg recorder: ${spawnError.message}`);
    }

    if (!child.pid) {
      throw new Error("failed to start ffmpeg recorder");
    }

    if (detached) {
      child.unref();
      return {
        pid: child.pid,
        file: outputPath,
        logPath,
        detached: true
      };
    }

    this._recordingChild = child;

    const earlyExit = await Promise.race<ExitStatus | null>([
      waitForExit(child, 400),
      delay(400).then(() => null)
    ]);

    if (earlyExit?.exited) {
      this._recordingChild = null;
      throw new Error(`ffmpeg exited immediately while starting recording (code=${earlyExit.code ?? "null"})`);
    }

    return {
      pid: child.pid,
      file: outputPath,
      logPath,
      detached: false
    };
  }

  private async stopRecordingInternal(): Promise<void> {
    if (!this._recordingChild) {
      return;
    }

    const child = this._recordingChild;
    this._recordingChild = null;

    try {
      child.kill("SIGINT");
    } catch {
      return;
    }

    const result = await waitForExit(child, 6000);
    if (!result.exited) {
      try {
        child.kill("SIGKILL");
      } catch {
        // ignore
      }
    }
  }

  async record(options: StartRecordingOptions = {}): Promise<RecordingHandle> {
    const idleSpeedupConfig = this.resolveIdleSpeedupConfig(options);
    const recording = await this.startRecordingInternal(options);
    if (recording.detached) {
      return {
        ...recording,
        stop: async () => {
          await killPid(recording.pid);
          if (idleSpeedupConfig) {
            await this.speedupIdleSegments(recording.file, idleSpeedupConfig);
          }
        }
      };
    }

    return {
      ...recording,
      stop: async () => {
        await this.stopRecordingInternal();
        if (idleSpeedupConfig) {
          await this.speedupIdleSegments(recording.file, idleSpeedupConfig);
        }
      }
    };
  }

  async kill(options: KillOptions = {}): Promise<void> {
    await this.stopRecordingInternal();

    if (this.openboxPid) {
      await killPid(this.openboxPid);
      this.openboxPid = null;
    }
    if (this.xvncPid) {
      await killPid(this.xvncPid);
      this.xvncPid = null;
    }

    const cleanup = options.cleanup ?? this.cleanupSessionDirOnStop;
    if (cleanup) {
      await fsp.rm(this.sessionDir, { recursive: true, force: true });
    }
  }
}

export async function start(options: StartOptions = {}): Promise<Desktop> {
  assertLinux();

  const runtime = await ensureRuntime(options);
  const { display, port } = await pickDisplayAndPort(options.display, options.port);

  const geometry = options.geometry || "1280x800";
  const depth = normalizeInteger(options.depth, 24);
  const startupTimeoutMs = normalizeInteger(options.startupTimeoutMs, 15000);
  const openboxEnabled = options.openbox !== false;
  const detached = options.detached ?? false;
  const autoSessionDir = options.sessionDir == null;
  const cleanupSessionDirOnStop = options.cleanupSessionDirOnStop ?? autoSessionDir;

  const sessionDir = options.sessionDir
    ? path.resolve(options.sessionDir)
    : await fsp.mkdtemp(path.join(os.tmpdir(), "portabledesktop-"));

  await fsp.mkdir(sessionDir, { recursive: true });

  const xvncLogPath = path.join(sessionDir, "xvnc.log");
  const openboxLogPath = path.join(sessionDir, "openbox.log");

  const xvncPath = resolveRuntimeBinary(runtime.runtimeDir, "Xvnc");
  const xvncArgs = [
    `:${display}`,
    "-geometry",
    geometry,
    "-depth",
    String(depth),
    "-rfbport",
    String(port),
    "-SecurityTypes",
    "None",
    "-ac",
    "-nolisten",
    "tcp",
    "-localhost",
    "no"
  ];

  const runtimeBinDir = path.join(runtime.runtimeDir, "bin");
  const runtimePath = process.env.PATH ? `${runtimeBinDir}:${process.env.PATH}` : runtimeBinDir;

  const xvncFd = fs.openSync(xvncLogPath, "a");
  const xvncChild = spawn(xvncPath, xvncArgs, {
    env: {
      ...process.env,
      PATH: runtimePath
    },
    detached,
    stdio: ["ignore", xvncFd, xvncFd]
  });

  if (!xvncChild.pid) {
    throw new Error("failed to start Xvnc");
  }

  if (detached) {
    xvncChild.unref();
  }

  try {
    await waitForPort({ port, timeoutMs: startupTimeoutMs });
  } catch (error) {
    await killPid(xvncChild.pid).catch(() => {});
    throw error;
  }

  let openboxPid: number | null = null;
  if (openboxEnabled) {
    const openboxPath = resolveRuntimeBinary(runtime.runtimeDir, "openbox");
    const openboxFd = fs.openSync(openboxLogPath, "a");
    const openboxChild = spawn(openboxPath, [], {
      env: {
        ...process.env,
        DISPLAY: `:${display}`,
        PATH: runtimePath
      },
      detached,
      stdio: ["ignore", openboxFd, openboxFd]
    });

    openboxPid = openboxChild.pid ?? null;

    if (detached) {
      openboxChild.unref();
    }
  }

  const desktop = new Desktop({
    runtimeDir: runtime.runtimeDir,
    display,
    port,
    geometry,
    depth,
    sessionDir,
    cleanupSessionDirOnStop,
    xvncPid: xvncChild.pid,
    openboxPid,
    detached
  });

  if (options.background?.color) {
    await desktop.setBackground(options.background.color);
  }

  return desktop;
}
