import { constants } from "node:fs";
import fs from "node:fs/promises";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { WebSocketServer, type RawData, type WebSocket } from "ws";

export interface StartViewerOptions {
  host?: string;
  port?: number;
  vncHost?: string;
  vncPort?: number;
  title?: string;
  clientScriptPath?: string;
}

export interface ViewerHandle {
  url: string;
  stop: () => Promise<void>;
}

function escapeHtml(value: string): string {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function buildViewerHtml(title: string): string {
  const escapedTitle = escapeHtml(title);
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>${escapedTitle}</title>
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

async function resolveViewerClientPath(customPath?: string): Promise<string> {
  const moduleDir = path.dirname(fileURLToPath(import.meta.url));
  const candidates = [
    customPath ? path.resolve(customPath) : "",
    path.resolve(moduleDir, "..", "dist", "viewer-client.js")
  ].filter((value) => value.length > 0);

  for (const candidate of candidates) {
    try {
      // eslint-disable-next-line no-await-in-loop
      await fs.access(candidate, constants.R_OK);
      return candidate;
    } catch {
      // try next candidate
    }
  }

  throw new Error("viewer client bundle not found. Run `bun run build:viewer` in examples/agent.");
}

export async function startViewer(
  desktop: { port: number },
  options: StartViewerOptions = {}
): Promise<ViewerHandle> {
  const bindHost = options.host ?? "127.0.0.1";
  const bindPort = options.port ?? 0;
  const vncHost = options.vncHost ?? "127.0.0.1";
  const vncPort = options.vncPort ?? desktop.port;
  const title = options.title ?? "portabledesktop live viewer";
  const clientScriptPath = await resolveViewerClientPath(options.clientScriptPath);

  const activeSockets = new Set<net.Socket>();
  const activeClients = new Set<WebSocket>();

  const server = http.createServer((req, res) => {
    if (req.url === "/" || req.url === "/index.html") {
      res.writeHead(200, {
        "content-type": "text/html; charset=utf-8",
        "cache-control": "no-store"
      });
      res.end(buildViewerHtml(title));
      return;
    }

    if (req.url === "/viewer.js") {
      void (async () => {
        try {
          const body = await fs.readFile(clientScriptPath);
          res.writeHead(200, {
            "content-type": "text/javascript; charset=utf-8",
            "cache-control": "no-store"
          });
          res.end(body);
        } catch {
          res.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
          res.end("not found");
        }
      })();
      return;
    }

    if (req.url === "/healthz") {
      res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
      res.end("ok");
      return;
    }

    res.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
    res.end("not found");
  });

  const wss = new WebSocketServer({
    noServer: true,
    perMessageDeflate: false
  });

  server.on("upgrade", (req, socket, head) => {
    if (req.url !== "/ws") {
      socket.destroy();
      return;
    }

    wss.handleUpgrade(req, socket, head, (ws: WebSocket) => {
      wss.emit("connection", ws, req);
    });
  });

  wss.on("connection", (ws: WebSocket) => {
    activeClients.add(ws);

    const tcp = net.connect({ host: vncHost, port: vncPort });
    activeSockets.add(tcp);

    ws.on("message", (data: RawData) => {
      if (tcp.destroyed) {
        return;
      }

      if (typeof data === "string") {
        tcp.write(data, "utf8");
      } else if (Buffer.isBuffer(data)) {
        tcp.write(data);
      } else if (Array.isArray(data)) {
        tcp.write(Buffer.concat(data));
      } else {
        tcp.write(Buffer.from(data));
      }
    });

    ws.on("close", () => {
      activeClients.delete(ws);
      tcp.destroy();
    });

    ws.on("error", () => {
      activeClients.delete(ws);
      tcp.destroy();
    });

    tcp.on("data", (chunk: Buffer) => {
      if (ws.readyState === 1) {
        ws.send(chunk, { binary: true });
      }
    });

    tcp.on("close", () => {
      activeSockets.delete(tcp);
      if (ws.readyState <= 1) {
        ws.close();
      }
    });

    tcp.on("error", () => {
      activeSockets.delete(tcp);
      if (ws.readyState <= 1) {
        ws.close();
      }
    });
  });

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(bindPort, bindHost, () => {
      server.off("error", reject);
      resolve();
    });
  });

  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("failed to resolve viewer server address");
  }

  const openHost = bindHost === "0.0.0.0" || bindHost === "::" ? "127.0.0.1" : bindHost;
  const url = `http://${openHost}:${address.port}`;

  return {
    url,
    stop: async () => {
      for (const client of activeClients) {
        try {
          client.close();
        } catch {
          // ignore close errors
        }
      }
      activeClients.clear();

      for (const socket of activeSockets) {
        socket.destroy();
      }
      activeSockets.clear();

      await new Promise<void>((resolve) => {
        wss.close(() => resolve());
      });

      await new Promise<void>((resolve) => {
        server.close(() => resolve());
      });
    }
  };
}
