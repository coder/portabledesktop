import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import net from "node:net";
import os from "node:os";
import path from "node:path";

import { anthropic } from "@ai-sdk/anthropic";
import { ToolLoopAgent as Agent, stepCountIs } from "ai";
import { WebSocket, WebSocketServer } from "ws";

import { createDesktop } from "portabledesktop";
import { anthropic as pdAnthropic } from "portabledesktop/ai";

const VIEWER_HOST = process.env.VIEWER_HOST || "0.0.0.0";
const VIEWER_PORT = normalizePort(process.env.PORT, 5190, "PORT");
const WALLPAPER_PATH = process.env.WALLPAPER_PATH || "/app/wallpaper.jpg";
const DEFAULT_PROMPT =
  "Play a game of chess in the browser. Explain each move briefly as you play and finish after at least 10 moves.";
const DEFAULT_MODEL = process.env.ANTHROPIC_MODEL || "claude-opus-4-6";

function normalizePort(value, fallback, name) {
  if (value == null || value === "") {
    return fallback;
  }

  const parsed = Number.parseInt(String(value), 10);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65535) {
    throw new Error(`invalid ${name}: ${String(value)}`);
  }
  return parsed;
}

function parseGeometry(geometry) {
  const match = /^(\d+)x(\d+)$/.exec(String(geometry));
  if (!match) {
    throw new Error(`invalid geometry: ${String(geometry)}`);
  }

  const width = Number.parseInt(match[1], 10);
  const height = Number.parseInt(match[2], 10);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width < 1 || height < 1) {
    throw new Error(`invalid geometry dimensions: ${String(geometry)}`);
  }

  return { width, height };
}

async function pathExists(filePath) {
  try {
    await fs.access(filePath);
    return true;
  } catch {
    return false;
  }
}

async function resolveExecutable(candidates) {
  const pathEntries = (process.env.PATH || "")
    .split(":")
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);

  for (const candidate of candidates) {
    if (candidate.includes("/")) {
      // eslint-disable-next-line no-await-in-loop
      if (await pathExists(candidate)) {
        return candidate;
      }
      continue;
    }

    for (const entry of pathEntries) {
      const resolved = path.join(entry, candidate);
      // eslint-disable-next-line no-await-in-loop
      if (await pathExists(resolved)) {
        return resolved;
      }
    }
  }

  return null;
}

async function loadViewerClientScript() {
  const require = createRequire(import.meta.url);
  const packageRoot = path.dirname(require.resolve("portabledesktop/package.json"));
  const viewerClientPath = path.join(packageRoot, "dist", "bin", "viewer-client.js");
  return fs.readFile(viewerClientPath, "utf8");
}

function viewerHtml() {
  const viewerConfig = JSON.stringify({ scale: "fit", desktopSizeMode: "fixed" });
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>portabledesktop demo viewer</title>
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
    <script>globalThis.PORTABLEDESKTOP_VIEWER_CONFIG = ${viewerConfig};</script>
    <script type="module" src="/viewer.js"></script>
  </body>
</html>`;
}

async function startViewerServer(vncPort) {
  const viewerClientScript = await loadViewerClientScript();
  const sockets = new Set();
  const wsToTcp = new Map();

  const server = createServer(async (req, res) => {
    const url = new URL(req.url || "/", `http://${VIEWER_HOST}:${VIEWER_PORT}`);

    if (url.pathname === "/" || url.pathname === "/index.html") {
      res.writeHead(200, {
        "content-type": "text/html; charset=utf-8",
        "cache-control": "no-store"
      });
      res.end(viewerHtml());
      return;
    }

    if (url.pathname === "/viewer.js") {
      res.writeHead(200, {
        "content-type": "text/javascript; charset=utf-8",
        "cache-control": "no-store"
      });
      res.end(viewerClientScript);
      return;
    }

    if (url.pathname === "/healthz") {
      res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
      res.end("ok");
      return;
    }

    res.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
    res.end("not found");
  });

  server.on("connection", (socket) => {
    sockets.add(socket);
    socket.on("close", () => sockets.delete(socket));
  });

  const wss = new WebSocketServer({ noServer: true });

  server.on("upgrade", (req, socket, head) => {
    const url = new URL(req.url || "/", `http://${VIEWER_HOST}:${VIEWER_PORT}`);
    if (url.pathname !== "/ws") {
      socket.destroy();
      return;
    }

    wss.handleUpgrade(req, socket, head, (ws) => {
      wss.emit("connection", ws, req);
    });
  });

  wss.on("connection", (ws) => {
    const tcp = net.connect({ host: "127.0.0.1", port: vncPort });
    wsToTcp.set(ws, tcp);

    tcp.on("data", (chunk) => {
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

    ws.on("message", (data, isBinary) => {
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

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(VIEWER_PORT, VIEWER_HOST, () => {
      server.off("error", reject);
      resolve();
    });
  });

  return {
    viewerUrl: `http://localhost:${VIEWER_PORT}`,
    stop: async () => {
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

      await new Promise((resolve) => {
        wss.close(() => {
          server.close(() => resolve());
        });
      });
    }
  };
}

async function launchDesktopBrowser(desktop) {
  const browserPath = await resolveExecutable(["chromium", "/usr/bin/chromium"]);
  if (!browserPath) {
    throw new Error("chromium is not installed in the container");
  }

  const browserName = path.basename(browserPath);
  const profileDir = path.join(desktop.sessionDir, "profiles", `${browserName}-${Date.now()}`);
  await fs.mkdir(profileDir, { recursive: true });

  const browserLogPath = path.join(desktop.sessionDir, `${browserName}.log`);
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
      "--no-sandbox"
    ],
    {
      env: desktop.env,
      detached: true,
      stdio: ["ignore", browserLog.fd, browserLog.fd]
    }
  );
  await browserLog.close();

  if (!child.pid) {
    throw new Error("failed to start chromium inside desktop session");
  }

  const earlyExit = await Promise.race([
    new Promise((resolve) => {
      child.once("exit", (code, signal) => {
        resolve({ code, signal });
      });
    }),
    new Promise((resolve) => {
      setTimeout(() => resolve(null), 1200);
    })
  ]);

  if (earlyExit) {
    const browserLogText = await fs.readFile(browserLogPath, "utf8").catch(() => "");
    const exitInfo =
      earlyExit.signal != null
        ? `signal ${earlyExit.signal}`
        : `code ${String(earlyExit.code)}`;
    const logTail = browserLogText.trim().split("\n").slice(-3).join(" | ").trim();
    throw new Error(`chromium exited early (${exitInfo})${logTail ? `: ${logTail}` : ""}`);
  }

  child.unref();
  return {
    pid: child.pid,
    browser: browserName
  };
}

function getPrompt() {
  const fromArgv = process.argv.slice(2).join(" ").trim();
  if (fromArgv.length > 0) {
    return fromArgv;
  }

  const fromEnv = (process.env.PROMPT || "").trim();
  if (fromEnv.length > 0) {
    return fromEnv;
  }

  return DEFAULT_PROMPT;
}

function formatToolValue(value) {
  const seen = new WeakSet();
  try {
    const serialized = JSON.stringify(value, (key, current) => {
      if (typeof current === "string") {
        if (/(image|screenshot|base64|png|jpeg|jpg)/i.test(key) && current.length > 40) {
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

async function main() {
  if (!process.env.ANTHROPIC_API_KEY) {
    throw new Error("ANTHROPIC_API_KEY is required");
  }

  const prompt = getPrompt();
  const wallpaperExists = await pathExists(WALLPAPER_PATH);

  const desktop = await createDesktop({
    vnc: {
      geometry: "1920x1080",
      desktopSizeMode: "fixed"
    },
    background: wallpaperExists ? { image: WALLPAPER_PATH, mode: "fill" } : { color: "#1f252f" },
    detached: false
  });

  let viewer;
  let recording;
  let cleanedUp = false;

  const cleanup = async () => {
    if (cleanedUp) {
      return;
    }
    cleanedUp = true;

    if (recording) {
      try {
        await recording.stop();
      } catch {
        // ignore cleanup errors
      }
    }

    if (viewer) {
      try {
        await viewer.stop();
      } catch {
        // ignore cleanup errors
      }
    }

    try {
      await desktop.kill({ cleanup: true });
    } catch {
      // ignore cleanup errors
    }
  };

  const onSignal = (signal) => {
    process.stderr.write(`\nreceived ${signal}, shutting down...\n`);
    void cleanup().finally(() => {
      process.exit(0);
    });
  };

  process.on("SIGINT", onSignal);
  process.on("SIGTERM", onSignal);

  try {
    const displaySize = parseGeometry(desktop.geometry);
    const browser = await launchDesktopBrowser(desktop);

    const recordingPath = path.resolve(path.join(os.tmpdir(), `portabledesktop-demo-${Date.now()}.mp4`));
    recording = await desktop.record({
      file: recordingPath,
      idleSpeedup: 20,
      idleMinDurationSec: 0.35,
      idleNoiseTolerance: "-38dB"
    });

    viewer = await startViewerServer(desktop.vncPort);

    process.stdout.write(`viewer: ${viewer.viewerUrl}\n`);
    process.stdout.write(`vnc: 127.0.0.1:${desktop.vncPort}\n`);
    process.stdout.write(`browser: ${browser.browser}\n`);
    process.stdout.write(`model: ${DEFAULT_MODEL}\n`);
    process.stdout.write(`recording: ${recordingPath}\n`);
    process.stdout.write(`prompt: ${prompt}\n\n`);

    const computerTool = pdAnthropic.tools.computer_20251124({
      desktop,
      displayWidthPx: displaySize.width,
      displayHeightPx: displaySize.height,
      displayNumber: desktop.display,
      enableZoom: true,
      screenshotTimeoutMs: 20_000
    });

    const agent = new Agent({
      model: anthropic(DEFAULT_MODEL),
      instructions:
        "Use the computer tool to complete the user's prompt in the already-open browser. Keep actions direct and efficient.",
      stopWhen: stepCountIs(120),
      tools: {
        computer: computerTool
      }
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
          `[tool call] ${part.toolName}${id} input=${formatToolValue(part.input)}\n`
        );
        continue;
      }

      if (part.type === "tool-result") {
        flushText();
        const id = part.toolCallId ? ` id=${part.toolCallId}` : "";
        process.stdout.write(
          `[tool result] ${part.toolName}${id} output=${formatToolValue(part.output)}\n`
        );
        continue;
      }

      if (part.type === "tool-error") {
        flushText();
        const id = part.toolCallId ? ` id=${part.toolCallId}` : "";
        process.stdout.write(
          `[tool error] ${part.toolName}${id} error=${formatToolValue(part.error)}\n`
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
