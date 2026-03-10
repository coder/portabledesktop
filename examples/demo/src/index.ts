/**
 * Portable Desktop Demo
 *
 * Containerized demo that runs an Anthropic computer-use loop on a
 * live portable desktop session with Chromium.  Designed to be built
 * with `bun build` and executed under plain Node.js inside Docker.
 */

import {
  spawn,
  type ChildProcess,
} from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";

import { anthropic } from "@ai-sdk/anthropic";
import { ToolLoopAgent as Agent, stepCountIs } from "ai";

import {
  DesktopComputer,
  createAnthropicComputer20251124Tool,
} from "../../shared/computer.ts";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const BIN = process.env.PORTABLEDESKTOP_BIN || "portabledesktop";
const VIEWER_HOST = process.env.VIEWER_HOST || "0.0.0.0";
const VIEWER_PORT = normalizePort(process.env.PORT, 5190, "PORT");
const WALLPAPER_PATH = process.env.WALLPAPER_PATH || "/app/wallpaper.jpg";
const DEFAULT_PROMPT =
  "Play a game of chess in the browser. Explain each move briefly as you play and finish after at least 10 moves.";
const DEFAULT_MODEL = process.env.ANTHROPIC_MODEL || "claude-opus-4-6";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function normalizePort(
  value: string | undefined,
  fallback: number,
  name: string,
): number {
  if (value == null || value === "") return fallback;
  const parsed = Number.parseInt(value, 10);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
    throw new Error(`invalid ${name}: ${String(value)}`);
  }
  return parsed;
}

function parseGeometry(geometry: string): { width: number; height: number } {
  const match = /^(\d+)x(\d+)$/.exec(geometry);
  if (!match) throw new Error(`invalid geometry: ${geometry}`);
  const width = Number.parseInt(match[1], 10);
  const height = Number.parseInt(match[2], 10);
  if (
    !Number.isFinite(width) ||
    !Number.isFinite(height) ||
    width < 1 ||
    height < 1
  ) {
    throw new Error(`invalid geometry dimensions: ${geometry}`);
  }
  return { width, height };
}

async function pathExists(filePath: string): Promise<boolean> {
  try {
    await fs.access(filePath);
    return true;
  } catch {
    return false;
  }
}

async function resolveExecutable(
  candidates: readonly string[],
): Promise<string | null> {
  const pathEntries = (process.env.PATH || "")
    .split(":")
    .map((e) => e.trim())
    .filter((e) => e.length > 0);

  for (const candidate of candidates) {
    if (candidate.includes("/")) {
      if (await pathExists(candidate)) return candidate;
      continue;
    }

    for (const entry of pathEntries) {
      const resolved = path.join(entry, candidate);
      if (await pathExists(resolved)) return resolved;
    }
  }

  return null;
}

function getPrompt(): string {
  const fromArgv = process.argv.slice(2).join(" ").trim();
  if (fromArgv.length > 0) return fromArgv;
  const fromEnv = (process.env.PROMPT || "").trim();
  if (fromEnv.length > 0) return fromEnv;
  return DEFAULT_PROMPT;
}

// ---------------------------------------------------------------------------
// formatToolValue — truncates screenshots and large payloads for logging
// ---------------------------------------------------------------------------

function formatToolValue(value: unknown): string {
  const seen = new WeakSet();
  try {
    const serialized = JSON.stringify(value, (key, current) => {
      if (typeof current === "string") {
        if (
          /(image|screenshot|base64|png|jpeg|jpg)/i.test(key) &&
          current.length > 40
        ) {
          return `[omitted ${current.length} chars]`;
        }
        if (current.length > 220) {
          return `${current.slice(0, 217)}...`;
        }
        return current;
      }

      if (Buffer.isBuffer(current)) {
        return `[buffer ${current.length} bytes]`;
      }

      if (ArrayBuffer.isView(current)) {
        return `[typed-array ${current.byteLength} bytes]`;
      }

      if (current instanceof ArrayBuffer) {
        return `[array-buffer ${current.byteLength} bytes]`;
      }

      if (typeof current === "object" && current !== null) {
        if (seen.has(current)) {
          return "[circular]";
        }
        seen.add(current);
      }

      return current;
    });

    return serialized ?? String(value);
  } catch {
    return String(value);
  }
}

// ---------------------------------------------------------------------------
// Desktop session info — matches `portabledesktop up --json` output
// ---------------------------------------------------------------------------

interface DesktopInfo {
  runtimeDir: string;
  display: number;
  vncPort: number;
  geometry: string;
  depth: number;
  dpi: number;
  desktopSizeMode: string;
  sessionDir: string;
  cleanupSessionDirOnStop: boolean;
  detached: boolean;
  stateFile: string;
  startedAt: string;
}

// ---------------------------------------------------------------------------
// Desktop session management
// ---------------------------------------------------------------------------

async function startDesktop(
  wallpaperExists: boolean,
): Promise<{ info: DesktopInfo; proc: ChildProcess }> {
  const args = [
    "up",
    "--json",
    "--foreground",
    "--geometry",
    "1920x1080",
    "--desktop-size-mode",
    "fixed",
  ];

  if (wallpaperExists) {
    args.push("--background-image", WALLPAPER_PATH, "--background-mode", "fill");
  } else {
    args.push("--background", "#1f252f");
  }

  const proc = spawn(BIN, args, {
    stdio: ["ignore", "pipe", "inherit"],
  });

  // The first line of stdout is the JSON session info. After that
  // the process blocks on a signal (foreground mode).
  const info = await new Promise<DesktopInfo>((resolve, reject) => {
    let buf = "";
    const onData = (chunk: Buffer) => {
      buf += chunk.toString();
      const nl = buf.indexOf("\n");
      if (nl !== -1) {
        proc.stdout!.off("data", onData);
        try {
          resolve(JSON.parse(buf.slice(0, nl)));
        } catch (e) {
          reject(
            new Error(`failed to parse desktop info: ${buf.slice(0, nl)}`),
          );
        }
      }
    };
    proc.stdout!.on("data", onData);
    proc.once("error", reject);
    proc.once("exit", (code) => {
      if (!buf.includes("\n")) {
        reject(new Error(`portabledesktop up exited with code ${code}`));
      }
    });
  });

  return { info, proc };
}

// ---------------------------------------------------------------------------
// Browser launch
// ---------------------------------------------------------------------------

async function launchDesktopBrowser(
  desktopInfo: DesktopInfo,
): Promise<{ pid: number; browser: string }> {
  const browserPath = await resolveExecutable(["chromium", "/usr/bin/chromium"]);
  if (!browserPath) {
    throw new Error("chromium is not installed in the container");
  }

  const browserName = path.basename(browserPath);
  const profileDir = path.join(
    desktopInfo.sessionDir,
    "profiles",
    `${browserName}-${Date.now()}`,
  );
  await fs.mkdir(profileDir, { recursive: true });

  const browserLogPath = path.join(
    desktopInfo.sessionDir,
    `${browserName}.log`,
  );
  const browserLog = await fs.open(browserLogPath, "a");

  const child = spawn(
    browserPath,
    [
      "--new-window",
      "about:blank",
      "--start-minimized",
      `--user-data-dir=${profileDir}`,
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-dev-shm-usage",
      "--disable-gpu",
      "--no-sandbox",
    ],
    {
      env: { ...process.env, DISPLAY: `:${desktopInfo.display}` },
      detached: true,
      stdio: ["ignore", browserLog.fd, browserLog.fd],
    },
  );

  await browserLog.close();

  if (!child.pid) {
    throw new Error("failed to start chromium inside desktop session");
  }

  // Detect an early exit. If the browser exits within the first
  // 1200 ms it almost certainly failed to start properly.
  const earlyExit = await Promise.race([
    new Promise<{ code: number | null; signal: string | null }>((resolve) => {
      child.once("exit", (code, signal) => {
        resolve({ code, signal });
      });
    }),
    new Promise<null>((resolve) => {
      setTimeout(() => resolve(null), 1200);
    }),
  ]);

  if (earlyExit) {
    const browserLogText = await fs
      .readFile(browserLogPath, "utf8")
      .catch(() => "");
    const exitInfo =
      earlyExit.signal != null
        ? `signal ${earlyExit.signal}`
        : `code ${String(earlyExit.code)}`;
    const logTail = browserLogText
      .trim()
      .split("\n")
      .slice(-3)
      .join(" | ")
      .trim();
    throw new Error(
      `chromium exited early (${exitInfo})${logTail ? `: ${logTail}` : ""}`,
    );
  }

  child.unref();
  return { pid: child.pid, browser: browserName };
}

// ---------------------------------------------------------------------------
// Recording
// ---------------------------------------------------------------------------

interface RecordingHandle {
  path: string;
  stop: () => Promise<void>;
}

function startRecording(recordingPath: string): RecordingHandle {
  const recordingProc = spawn(
    BIN,
    [
      "record",
      "--idle-speedup",
      "20",
      "--idle-min-duration",
      "0.35",
      "--idle-noise-tolerance",
      "-38dB",
      recordingPath,
    ],
    { stdio: "ignore" },
  );

  return {
    path: recordingPath,
    stop: () =>
      new Promise<void>((resolve) => {
        recordingProc.once("exit", () => resolve());
        recordingProc.kill("SIGINT");
      }),
  };
}

// ---------------------------------------------------------------------------
// Viewer
// ---------------------------------------------------------------------------

function startViewer(): ChildProcess {
  const viewerProc = spawn(
    BIN,
    ["viewer", "--port", String(VIEWER_PORT), "--host", VIEWER_HOST, "--no-open"],
    { stdio: "ignore", detached: true },
  );
  viewerProc.unref();
  return viewerProc;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  if (!process.env.ANTHROPIC_API_KEY) {
    throw new Error("ANTHROPIC_API_KEY is required");
  }

  const prompt = getPrompt();
  const wallpaperExists = await pathExists(WALLPAPER_PATH);

  const { info: desktopInfo, proc: desktopProc } =
    await startDesktop(wallpaperExists);

  let viewerProc: ChildProcess | null = null;
  let recording: RecordingHandle | null = null;
  let cleanedUp = false;

  const cleanup = async () => {
    if (cleanedUp) return;
    cleanedUp = true;

    if (recording) {
      try {
        await recording.stop();
      } catch {
        // Ignore cleanup errors.
      }
    }

    if (viewerProc) {
      try {
        viewerProc.kill("SIGTERM");
      } catch {
        // Ignore cleanup errors.
      }
    }

    try {
      desktopProc.kill("SIGTERM");
      await new Promise<void>((resolve) =>
        desktopProc.once("exit", () => resolve()),
      );
    } catch {
      // Ignore cleanup errors.
    }
  };

  const onSignal = (signal: string) => {
    process.stderr.write(`\nreceived ${signal}, shutting down...\n`);
    void cleanup().finally(() => process.exit(0));
  };

  process.on("SIGINT", () => onSignal("SIGINT"));
  process.on("SIGTERM", () => onSignal("SIGTERM"));

  try {
    const displaySize = parseGeometry(desktopInfo.geometry);
    const browser = await launchDesktopBrowser(desktopInfo);

    const recordingPath = path.resolve(
      path.join(os.tmpdir(), `portabledesktop-demo-${Date.now()}.mp4`),
    );
    recording = startRecording(recordingPath);

    viewerProc = startViewer();

    process.stdout.write(`viewer: http://localhost:${VIEWER_PORT}\n`);
    process.stdout.write(`vnc: 127.0.0.1:${desktopInfo.vncPort}\n`);
    process.stdout.write(`browser: ${browser.browser}\n`);
    process.stdout.write(`model: ${DEFAULT_MODEL}\n`);
    process.stdout.write(`recording: ${recordingPath}\n`);
    process.stdout.write(`prompt: ${prompt}\n\n`);

    const computerTool = createAnthropicComputer20251124Tool({
      bin: BIN,
      displayWidthPx: displaySize.width,
      displayHeightPx: displaySize.height,
      displayNumber: desktopInfo.display,
      enableZoom: true,
      screenshotTimeoutMs: 20_000,
    });

    const agent = new Agent({
      model: anthropic(DEFAULT_MODEL),
      instructions:
        "Use the computer tool to complete the user's prompt in the already-open browser. Keep actions direct and efficient.",
      stopWhen: stepCountIs(120),
      tools: {
        computer: computerTool,
      },
    });

    process.stdout.write("agent output:\n");
    const result = await agent.stream({ prompt });
    let emitted = false;
    let currentText = "";

    const flushText = () => {
      if (!/\S/.test(currentText)) {
        currentText = "";
        return;
      }

      emitted = true;
      process.stdout.write(`${currentText.trim()}\n`);
      currentText = "";
    };

    for await (const part of result.fullStream) {
      if (part.type === "text-delta") {
        currentText += part.text;
        continue;
      }

      if (part.type === "text-end") {
        flushText();
        continue;
      }

      if (part.type === "tool-call") {
        flushText();
        const id = part.toolCallId ? ` id=${part.toolCallId}` : "";
        process.stdout.write(
          `[tool call] ${part.toolName}${id} input=${formatToolValue(part.input)}\n`,
        );
        continue;
      }

      if (part.type === "tool-result") {
        flushText();
        const id = part.toolCallId ? ` id=${part.toolCallId}` : "";
        process.stdout.write(
          `[tool result] ${part.toolName}${id} output=${formatToolValue(part.output)}\n`,
        );
        continue;
      }

      if (part.type === "tool-error") {
        flushText();
        const id = part.toolCallId ? ` id=${part.toolCallId}` : "";
        process.stdout.write(
          `[tool error] ${part.toolName}${id} error=${formatToolValue(part.error)}\n`,
        );
      }
    }

    flushText();

    if (!emitted) {
      process.stdout.write("(no text output)\n");
    }
  } finally {
    await cleanup();
  }
}

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
});
