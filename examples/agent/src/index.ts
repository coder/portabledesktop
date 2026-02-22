import { constants } from "node:fs";
import fs from "node:fs/promises";
import net from "node:net";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";

import { anthropic } from "@ai-sdk/anthropic";
import { openai } from "@ai-sdk/openai";
import { ToolLoopAgent as Agent, stepCountIs } from "ai";

import { createDesktop, type Desktop } from "../../../src/index.ts";
import { anthropic as pdAnthropic, openai as pdOpenAI } from "../../../src/ai/index.ts";

type ProviderName = "anthropic" | "openai";

const DEFAULT_PROMPT =
  "Open a browser and navigate to coder.com. Find the Dropbox customer story and confirm when you are on that page.";
const DEFAULT_ANTHROPIC_MODEL = "claude-opus-4-6";
const DEFAULT_OPENAI_MODEL = "computer-use-preview";
const DEFAULT_MAX_STEPS = 100;

interface CliOptions {
  prompt: string;
  provider: ProviderName;
  model?: string;
  maxSteps: number;
}

interface OpenInDesktopResult {
  pid: number;
}

interface ViewerServerHandle {
  url: string;
  stop: () => Promise<void>;
}

interface ViewerSocketData {
  tcp: net.Socket | null;
}

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const exampleRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(__dirname, "../../..");
const bunRuntime = (globalThis as unknown as { Bun?: any }).Bun;

if (!bunRuntime) {
  throw new Error("This example must be run with Bun.");
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function usage(): string {
  return [
    "Usage: bun run src/index.ts [--prompt <text>] [--provider anthropic|openai] [--model <id>] [--max-steps <n>]",
    "",
    "Options:",
    "  --provider   Agent provider (default: anthropic)",
    "  --model      Override model id",
    "  --max-steps  Maximum agent steps (default: 100)",
    "",
    "Behavior:",
    "  1) Starts a portable desktop session",
    "  2) Opens a live browser viewer on your host",
    "  3) Runs the agent for your prompt and streams text output",
    "  4) Opens the recorded MP4 in your host browser"
  ].join("\n");
}

function parseArgs(argv: readonly string[]): CliOptions {
  const options: CliOptions = {
    prompt: DEFAULT_PROMPT,
    provider: "anthropic",
    maxSteps: DEFAULT_MAX_STEPS
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
      case "--provider": {
        const value = argv[i + 1];
        if (value !== "anthropic" && value !== "openai") {
          throw new Error("--provider must be either 'anthropic' or 'openai'");
        }
        options.provider = value;
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
      case "--max-steps": {
        const value = argv[i + 1];
        if (!value) {
          throw new Error("--max-steps requires a value");
        }

        const parsed = Number.parseInt(value, 10);
        if (!Number.isFinite(parsed) || parsed < 1) {
          throw new Error("--max-steps must be a positive integer");
        }

        options.maxSteps = parsed;
        i += 1;
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

function requireProviderApiKey(provider: ProviderName): void {
  if (provider === "anthropic" && !process.env.ANTHROPIC_API_KEY) {
    throw new Error("ANTHROPIC_API_KEY is missing. Set it in environment or .env.local at repo root.");
  }

  if (provider === "openai" && !process.env.OPENAI_API_KEY) {
    throw new Error("OPENAI_API_KEY is missing. Set it in environment or .env.local at repo root.");
  }
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
      if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
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
      // eslint-disable-next-line no-await-in-loop
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

async function launchDesktopBrowser(desktop: Desktop): Promise<{ browser: string; pid: number }> {
  const browserPath = await resolveExecutable([
    "google-chrome",
    "google-chrome-stable",
    "chromium",
    "chromium-browser",
    "firefox"
  ]);

  if (!browserPath) {
    throw new Error("no browser found in PATH (tried: chrome/chromium/firefox)");
  }

  const args: string[] = ["--new-window"];
  const baseName = path.basename(browserPath).toLowerCase();
  if (baseName.includes("chrome") || baseName.includes("chromium")) {
    const profileDir = path.join(desktop.sessionDir, "profiles", `agent-browser-${Date.now()}`);
    await fs.rm(profileDir, { recursive: true, force: true });
    await fs.mkdir(profileDir, { recursive: true });
    args.push(`--user-data-dir=${profileDir}`, "--no-first-run", "--no-default-browser-check");
  }

  const launched = await openInDesktop(desktop, browserPath, args, { cwd: repoRoot });
  return {
    browser: browserPath,
    pid: launched.pid
  };
}

function buildViewerHtml(): string {
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>portabledesktop live viewer</title>
    <style>
      html, body {
        margin: 0;
        width: 100%;
        height: 100%;
        background: #141820;
        color: #e8edf2;
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
    <script type="module" src="/viewer.js"></script>
  </body>
</html>`;
}

function formatBuildErrors(logs: readonly unknown[]): string {
  return logs
    .map((entry) => {
      const log = entry as {
        level?: string;
        message?: string;
        position?: { file?: string; line?: number; column?: number };
      };
      if (log.position) {
        return `${log.level || "error"}: ${log.message || "build error"} (${log.position.file || "unknown"}:${String(
          log.position.line ?? "?"
        )}:${String(log.position.column ?? "?")})`;
      }
      return `${log.level || "error"}: ${log.message || "build error"}`;
    })
    .join("\n");
}

async function buildViewerClientScript(): Promise<string> {
  const entryPath = path.join(exampleRoot, "viewer-client.entry.js");

  const result = await bunRuntime.build({
    entrypoints: [entryPath],
    target: "browser",
    format: "esm",
    minify: true,
    write: false
  });

  if (!result.success) {
    throw new Error(`failed to bundle viewer client:\n${formatBuildErrors(result.logs)}`);
  }

  const output =
    result.outputs.find((item: { path: string }) => item.path.endsWith(".js")) || result.outputs[0];
  if (!output) {
    throw new Error("viewer bundle produced no output files");
  }

  return await output.text();
}

async function startViewerServer(vncPort: number): Promise<ViewerServerHandle> {
  const viewerScript = await buildViewerClientScript();
  const sockets = new Set<net.Socket>();

  const server = bunRuntime.serve({
    hostname: "127.0.0.1",
    port: 0,
    fetch(req: Request, srv: { upgrade: (request: Request, options?: unknown) => boolean }) {
      const url = new URL(req.url);

      if (url.pathname === "/ws") {
        if (srv.upgrade(req, { data: { tcp: null } })) {
          return;
        }
        return new Response("upgrade failed", { status: 500 });
      }

      if (url.pathname === "/" || url.pathname === "/index.html") {
        return new Response(buildViewerHtml(), {
          headers: {
            "content-type": "text/html; charset=utf-8",
            "cache-control": "no-store"
          }
        });
      }

      if (url.pathname === "/viewer.js") {
        return new Response(viewerScript, {
          headers: {
            "content-type": "text/javascript; charset=utf-8",
            "cache-control": "no-store"
          }
        });
      }

      if (url.pathname === "/healthz") {
        return new Response("ok", {
          headers: {
            "content-type": "text/plain; charset=utf-8"
          }
        });
      }

      return new Response("not found", { status: 404 });
    },
    websocket: {
      open(ws: { data: ViewerSocketData; send: (data: Buffer) => void; close: () => void }) {
        const tcp = net.connect({ host: "127.0.0.1", port: vncPort });
        ws.data.tcp = tcp;
        sockets.add(tcp);

        tcp.on("data", (chunk: Buffer) => {
          ws.send(chunk);
        });

        tcp.on("error", () => {
          sockets.delete(tcp);
          try {
            ws.close();
          } catch {
            // ignore
          }
        });

        tcp.on("close", () => {
          sockets.delete(tcp);
          try {
            ws.close();
          } catch {
            // ignore
          }
        });
      },
      message(ws: { data: ViewerSocketData }, message: unknown) {
        const tcp = ws.data.tcp;
        if (!tcp || tcp.destroyed) {
          return;
        }

        if (typeof message === "string") {
          tcp.write(message, "utf8");
          return;
        }

        if (message instanceof ArrayBuffer) {
          tcp.write(Buffer.from(message));
          return;
        }

        if (ArrayBuffer.isView(message)) {
          tcp.write(Buffer.from(message.buffer, message.byteOffset, message.byteLength));
        }
      },
      close(ws: { data: ViewerSocketData }) {
        const tcp = ws.data.tcp;
        ws.data.tcp = null;
        if (tcp) {
          sockets.delete(tcp);
          tcp.destroy();
        }
      }
    }
  });

  return {
    url: `http://127.0.0.1:${server.port}`,
    stop: async () => {
      for (const socket of sockets) {
        socket.destroy();
      }
      sockets.clear();
      server.stop(true);
    }
  };
}

async function main(): Promise<void> {
  await loadEnvFileIfPresent(path.join(repoRoot, ".env.local"));
  const options = parseArgs(process.argv.slice(2));

  requireProviderApiKey(options.provider);

  const modelId =
    options.model ?? (options.provider === "openai" ? DEFAULT_OPENAI_MODEL : DEFAULT_ANTHROPIC_MODEL);

  const desktop = await createDesktop({
    vnc: {
      geometry: "1280x800"
    },
    background: { color: "#1f252f" },
    openbox: true,
    detached: false
  });

  const recordingPath = path.resolve(path.join(exampleRoot, "tmp", `agent-${Date.now()}.mp4`));
  await fs.mkdir(path.dirname(recordingPath), { recursive: true });

  const recordingHandle = await desktop.record({
    file: recordingPath,
    idleSpeedup: 20,
    idleMinDurationSec: 0.35,
    idleNoiseTolerance: "-38dB"
  });

  const viewer = await startViewerServer(desktop.vncPort);
  openHostBrowser(viewer.url);

  const launchedBrowser = await launchDesktopBrowser(desktop);
  await delay(1200);

  process.stdout.write(`viewer: ${viewer.url}\n`);
  process.stdout.write(`desktop browser: ${launchedBrowser.browser}\n`);
  process.stdout.write(`recording: ${recordingPath}\n`);
  process.stdout.write(`provider: ${options.provider}\n`);
  process.stdout.write(`model: ${modelId}\n`);

  const computerTool =
    options.provider === "openai"
      ? pdOpenAI.tools.computer({
          desktop,
          displayWidthPx: 1280,
          displayHeightPx: 800,
          screenshotTimeoutMs: 20_000
        })
      : pdAnthropic.tools.computer_20251124({
          desktop,
          displayWidthPx: 1280,
          displayHeightPx: 800,
          displayNumber: desktop.display,
          enableZoom: true,
          screenshotTimeoutMs: 20_000
        });

  const agent = new Agent({
    model: options.provider === "openai" ? openai(modelId) : anthropic(modelId),
    instructions:
      "Use the computer tool to complete the user prompt in the already-open browser window. Prefer direct actions and keep steps concise. Do not ask any questions, just perform the task.",
    stopWhen: stepCountIs(options.maxSteps),
    tools: {
      computer: computerTool
    },
    providerOptions: {
      openai: {
        truncation: "auto",
      },
    },
  });

  try {
    process.stdout.write("\nagent output (streaming):\n");
    const result = await agent.stream({ prompt: options.prompt });
    let emittedText = false;

    for await (const textDelta of result.textStream) {
      emittedText = true;
      process.stdout.write(textDelta);
    }

    if (!emittedText) {
      process.stdout.write("(no text output)");
    }
    process.stdout.write("\n");
  } finally {
    try {
      await recordingHandle.stop();
      process.stdout.write(`saved recording: ${recordingPath}\n`);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      process.stderr.write(`warning: failed to finalize recording: ${message}\n`);
    }

    await viewer.stop();
    await desktop.kill({ cleanup: true });

    const recordingUrl = pathToFileURL(recordingPath).toString();
    openHostBrowser(recordingUrl);
    process.stdout.write(`opened recording: ${recordingUrl}\n`);
  }
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
});
