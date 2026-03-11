/**
 * Portable Desktop AI Agent Example
 *
 * Drives a virtual desktop via the `portabledesktop` CLI binary and
 * lets an LLM (Anthropic Claude or OpenAI) interact with it through
 * Anthropic's computer-use tool protocol.
 *
 * Usage:
 *   bun run src/index.ts --prompt "Open coder.com and confirm the homepage title."
 *   bun run src/index.ts --provider openai --model gpt-5 --prompt "Do something."
 */

import {
  spawn,
  execFileSync,
  type ChildProcess,
} from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { anthropic } from "@ai-sdk/anthropic";
import { openai } from "@ai-sdk/openai";
import { ToolLoopAgent as Agent, stepCountIs } from "ai";

import {
  createAnthropicComputer20251124Tool,
  createOpenAIComputerTool,
} from "../../shared/computer.ts";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_PROMPT =
  "Navigate to news.ycombinator.com and tell me what the top story is.";
const PORTABLEDESKTOP = process.env.PORTABLEDESKTOP_BIN || "portabledesktop";
const DEFAULT_WIDTH = 1280;
const DEFAULT_HEIGHT = 800;
const DEFAULT_VIEWER_PORT = 6080;
const DEFAULT_ANTHROPIC_MODEL = "claude-opus-4-6";
const DEFAULT_OPENAI_MODEL = "gpt-5.4";
const DEFAULT_MAX_STEPS = 100;

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const exampleRoot = path.resolve(__dirname, "..");

// ---------------------------------------------------------------------------
// CLI argument parsing
// ---------------------------------------------------------------------------

type ProviderName = "anthropic" | "openai";

interface CLIArgs {
  prompt: string;
  provider: ProviderName;
  model: string;
  maxSteps: number;
}

function parseArgs(): CLIArgs {
  const args = process.argv.slice(2);
  let prompt = "";
  let provider: ProviderName = "anthropic";
  let model = "";
  let maxSteps = DEFAULT_MAX_STEPS;

  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case "--prompt":
        prompt = args[++i] || "";
        break;
      case "--provider": {
        const value = args[++i] || "";
        if (value !== "anthropic" && value !== "openai") {
          throw new Error(
            "--provider must be either 'anthropic' or 'openai'",
          );
        }
        provider = value;
        break;
      }
      case "--model":
        model = args[++i] || "";
        break;
      case "--max-steps": {
        const raw = args[++i] || String(DEFAULT_MAX_STEPS);
        const parsed = parseInt(raw, 10);
        if (!Number.isFinite(parsed) || parsed < 1) {
          throw new Error("--max-steps must be a positive integer");
        }
        maxSteps = parsed;
        break;
      }
      case "--help":
        process.stdout.write(
          "Usage: bun run src/index.ts [--prompt <text>] [--provider anthropic|openai] [--model <id>] [--max-steps <n>]\n",
        );
        process.exit(0);
    }
  }

  if (!model) {
    model =
      provider === "anthropic" ? DEFAULT_ANTHROPIC_MODEL : DEFAULT_OPENAI_MODEL;
  }

  if (!prompt) {
    prompt = DEFAULT_PROMPT;
  }

  return { prompt, provider, model, maxSteps };
}

// ---------------------------------------------------------------------------
// API key validation
// ---------------------------------------------------------------------------

function requireProviderApiKey(provider: ProviderName): void {
  if (provider === "anthropic" && !process.env.ANTHROPIC_API_KEY) {
    throw new Error(
      "ANTHROPIC_API_KEY is missing. Set it in environment or .env.local at repo root.",
    );
  }

  if (provider === "openai" && !process.env.OPENAI_API_KEY) {
    throw new Error(
      "OPENAI_API_KEY is missing. Set it in environment or .env.local at repo root.",
    );
  }
}

// ---------------------------------------------------------------------------
// .env.local loader (minimal — no external dependency)
// ---------------------------------------------------------------------------

function loadEnvLocal(): void {
  const dir = path.dirname(fileURLToPath(import.meta.url));
  // Walk up to the repo root (two levels above src/).
  const candidates = [
    path.resolve(dir, "../../.env.local"),
    path.resolve(dir, "../../../.env.local"),
    path.resolve(dir, "../../../../.env.local"),
  ];

  for (const envPath of candidates) {
    if (!fs.existsSync(envPath)) continue;
    const lines = fs.readFileSync(envPath, "utf8").split("\n");
    for (const raw of lines) {
      const line = raw.trim();
      if (!line || line.startsWith("#")) continue;
      // Strip optional `export ` prefix.
      const stripped = line.startsWith("export ") ? line.slice(7) : line;
      const eqIdx = stripped.indexOf("=");
      if (eqIdx === -1) continue;
      const key = stripped.slice(0, eqIdx).trim();
      let value = stripped.slice(eqIdx + 1).trim();
      // Strip surrounding quotes.
      if (
        (value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))
      ) {
        value = value.slice(1, -1);
      }
      if (!process.env[key]) {
        process.env[key] = value;
      }
    }
    break;
  }
}

// ---------------------------------------------------------------------------
// Desktop state returned by `portabledesktop up --json`
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
// DesktopSession — wraps the portabledesktop CLI binary lifecycle
// ---------------------------------------------------------------------------

interface StartOptions {
  geometry?: string;
  background?: string;
  backgroundImage?: string;
  backgroundMode?: string;
  runtimeDir?: string;
  desktopSizeMode?: string;
  noOpenbox?: boolean;
}

interface RecordingHandle {
  stop: () => Promise<void>;
}

class DesktopSession {
  readonly #info: DesktopInfo;
  readonly #proc: ChildProcess;

  private constructor(info: DesktopInfo, proc: ChildProcess) {
    this.#info = info;
    this.#proc = proc;
  }

  get display(): number {
    return this.#info.display;
  }
  get vncPort(): number {
    return this.#info.vncPort;
  }
  get sessionDir(): string {
    return this.#info.sessionDir;
  }
  get geometry(): string {
    return this.#info.geometry;
  }
  get stateFile(): string {
    return this.#info.stateFile;
  }

  /**
   * Start a new desktop session in foreground mode. The returned
   * session holds a reference to the long-running `portabledesktop up`
   * process; calling `stop()` sends SIGTERM to tear it down.
   */
  static async start(options: StartOptions = {}): Promise<DesktopSession> {
    const args = ["up", "--json", "--foreground"];
    if (options.geometry) args.push("--geometry", options.geometry);
    if (options.background) args.push("--background", options.background);
    if (options.backgroundImage)
      args.push("--background-image", options.backgroundImage);
    if (options.backgroundMode)
      args.push("--background-mode", options.backgroundMode);
    if (options.runtimeDir) args.push("--runtime-dir", options.runtimeDir);
    if (options.desktopSizeMode)
      args.push("--desktop-size-mode", options.desktopSizeMode);
    if (options.noOpenbox) args.push("--no-openbox");

    const proc = spawn(PORTABLEDESKTOP, args, {
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
              new Error(
                `failed to parse desktop info: ${buf.slice(0, nl)}`,
              ),
            );
          }
        }
      };
      proc.stdout!.on("data", onData);
      proc.once("error", reject);
      proc.once("exit", (code) => {
        if (!buf.includes("\n")) {
          reject(
            new Error(`portabledesktop up exited with code ${code}`),
          );
        }
      });
    });

    return new DesktopSession(info, proc);
  }

  // -----------------------------------------------------------------------
  // Recording
  // -----------------------------------------------------------------------

  /** Start recording the desktop to a video file. */
  startRecording(file: string): RecordingHandle {
    const proc = spawn(
      PORTABLEDESKTOP,
      [
        "record",
        "--idle-speedup",
        "20",
        "--idle-min-duration",
        "0.35",
        "--idle-noise-tolerance",
        "-38dB",
        file,
      ],
      { stdio: "ignore" },
    );
    return {
      stop: () =>
        new Promise<void>((resolve) => {
          proc.once("exit", () => resolve());
          proc.kill("SIGINT");
        }),
    };
  }

  // -----------------------------------------------------------------------
  // Viewer
  // -----------------------------------------------------------------------

  /** Start the built-in noVNC viewer HTTP server. */
  startViewer(port: number, host = "127.0.0.1"): ChildProcess {
    return spawn(
      PORTABLEDESKTOP,
      ["viewer", "--port", String(port), "--host", host, "--no-open"],
      { stdio: "ignore", detached: true },
    );
  }

  // -----------------------------------------------------------------------
  // Open — spawn a detached process inside the desktop
  // -----------------------------------------------------------------------

  open(command: string, ...args: string[]): void {
    execFileSync(PORTABLEDESKTOP, ["open", "--", command, ...args]);
  }

  // -----------------------------------------------------------------------
  // Lifecycle
  // -----------------------------------------------------------------------

  async stop(): Promise<void> {
    if (this.#proc && !this.#proc.killed) {
      this.#proc.kill("SIGTERM");
      await new Promise<void>((resolve) =>
        this.#proc.once("exit", () => resolve()),
      );
    }
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Try to find a browser executable. */
function resolveDesktopBrowser(): string | null {
  const candidates = [
    "google-chrome-stable",
    "google-chrome",
    "chromium-browser",
    "chromium",
    "firefox",
  ];
  for (const name of candidates) {
    try {
      const out = execFileSync("which", [name], {
        encoding: "utf8",
      }).trim();
      if (out) return out;
    } catch {
      // Not found — try the next one.
    }
  }
  return null;
}

/** Open a browser on the host machine pointing at the viewer. */
function openHostBrowser(url: string): void {
  const commands: [string, string[]][] =
    process.platform === "darwin"
      ? [["open", [url]]]
      : [
          ["xdg-open", [url]],
          ["sensible-browser", [url]],
        ];

  for (const [cmd, args] of commands) {
    try {
      spawn(cmd, args, { stdio: "ignore", detached: true }).unref();
      return;
    } catch {
      // Try the next one.
    }
  }
  process.stdout.write(`  Open manually: ${url}\n`);
}

/** Launch a browser inside the virtual desktop. */
function launchDesktopBrowser(session: DesktopSession, url: string): void {
  const browser = resolveDesktopBrowser();
  if (!browser) {
    process.stderr.write(
      "warning: no browser found inside the desktop. Install chromium or firefox.\n",
    );
    return;
  }

  const args = [browser, "--no-first-run", "--disable-session-crashed-bubble"];

  // Chrome/Chromium-specific flags for a cleaner kiosk-ish experience.
  const base = path.basename(browser);
  if (base.includes("chrom")) {
    args.push(
      "--disable-infobars",
      "--no-default-browser-check",
      `--window-size=${DEFAULT_WIDTH},${DEFAULT_HEIGHT}`,
      url,
    );
  } else {
    args.push(url);
  }

  // Use `portabledesktop open` so it runs detached in the desktop env.
  const [cmd, ...rest] = args;
  session.open(cmd, ...rest);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  loadEnvLocal();
  const cli = parseArgs();

  requireProviderApiKey(cli.provider);

  process.stdout.write("starting portable desktop...\n");
  const session = await DesktopSession.start({
    geometry: `${DEFAULT_WIDTH}x${DEFAULT_HEIGHT}`,
    background: "#1f252f",
  });
  process.stdout.write(
    `display :${session.display}  vnc :${session.vncPort}  geometry ${session.geometry}\n`,
  );

  // Start recording so we have a video artifact of the run.
  const recordingPath = path.resolve(
    path.join(exampleRoot, "tmp", `agent-${Date.now()}.mp4`),
  );
  fs.mkdirSync(path.dirname(recordingPath), { recursive: true });
  const recording = session.startRecording(recordingPath);
  process.stdout.write(`recording: ${recordingPath}\n`);

  // Start the viewer and open a browser on the host so the operator
  // can watch in real time.
  const viewerProc = session.startViewer(DEFAULT_VIEWER_PORT);
  viewerProc.unref();
  const viewerUrl = `http://127.0.0.1:${DEFAULT_VIEWER_PORT}`;
  process.stdout.write(`viewer: ${viewerUrl}\n`);
  openHostBrowser(viewerUrl);

  // Give the desktop a moment to settle before launching a browser
  // inside it.
  await new Promise((resolve) => setTimeout(resolve, 1500));
  launchDesktopBrowser(session, "about:blank");
  // Let the browser window finish opening.
  await new Promise((resolve) => setTimeout(resolve, 2000));

  // Build the tool using the shared computer module.
  const computerTool =
    cli.provider === "openai"
      ? createOpenAIComputerTool({
          bin: PORTABLEDESKTOP,
          displayWidthPx: DEFAULT_WIDTH,
          displayHeightPx: DEFAULT_HEIGHT,
          enableZoom: true,
          screenshotTimeoutMs: 20_000,
        })
      : createAnthropicComputer20251124Tool({
          bin: PORTABLEDESKTOP,
          displayWidthPx: DEFAULT_WIDTH,
          displayHeightPx: DEFAULT_HEIGHT,
          displayNumber: session.display,
          enableZoom: true,
          screenshotTimeoutMs: 20_000,
        });

  const modelInstance =
    cli.provider === "openai" ? openai(cli.model) : anthropic(cli.model);

  const agent = new Agent({
    model: modelInstance,
    instructions:
      "Use the computer tool to complete the user prompt in the already-open browser window. " +
      "Prefer direct actions and keep steps concise. Do not ask any questions, just perform the task.",
    stopWhen: stepCountIs(cli.maxSteps),
    tools: {
      computer: computerTool,
    },
    providerOptions: {
      openai: {
        truncation: "auto",
      },
    },
  });

  process.stdout.write(
    `provider: ${cli.provider}  model: ${cli.model}  max steps: ${cli.maxSteps}\n`,
  );
  process.stdout.write(`prompt: "${cli.prompt}"\n\n`);

  // SIGINT/SIGTERM handler for graceful shutdown.
  let cleanedUp = false;
  const cleanup = async () => {
    if (cleanedUp) return;
    cleanedUp = true;
    try {
      await recording.stop();
      process.stdout.write(`saved recording: ${recordingPath}\n`);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      process.stderr.write(
        `warning: failed to finalize recording: ${message}\n`,
      );
    }
    viewerProc.kill("SIGTERM");
    await session.stop();
  };

  const onSignal = (signal: string) => {
    process.stderr.write(`\nreceived ${signal}, shutting down...\n`);
    void cleanup().finally(() => process.exit(0));
  };
  process.on("SIGINT", () => onSignal("SIGINT"));
  process.on("SIGTERM", () => onSignal("SIGTERM"));

  try {
    process.stdout.write("agent output (streaming):\n");
    const result = await agent.stream({ prompt: cli.prompt });
    let emittedText = false;

    for await (const textDelta of result.textStream) {
      emittedText = true;
      process.stdout.write(textDelta);
    }

    if (!emittedText) {
      process.stdout.write("(no text output)");
    }
    process.stdout.write("\n");
  } catch (err) {
    process.stderr.write(
      `agent loop failed: ${err instanceof Error ? err.message : String(err)}\n`,
    );
  } finally {
    await cleanup();

    const recordingUrl = pathToFileURL(recordingPath).toString();
    openHostBrowser(recordingUrl);
    process.stdout.write(`opened recording: ${recordingUrl}\n`);
  }
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
});
