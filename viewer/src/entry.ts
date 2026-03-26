import type RFB from "@novnc/novnc/lib/rfb.js";
import { createClient } from "./client";

type ViewerScale = "fit" | "1:1";
type DesktopSizeMode = "fixed" | "dynamic";
type ClipboardLogLevel = "debug" | "info" | "warn" | "error";

interface ViewerConfig {
  scale: ViewerScale;
  desktopSizeMode: DesktopSizeMode;
}

interface ClipboardLogContext {
  [key: string]: unknown;
}

interface PendingPasteShortcut {
  attemptID: number;
  key: string;
  code: string;
  startedAt: number;
  clipboardTextPromise: Promise<string | null>;
}

const CLIPBOARD_LOG_NAMESPACE = "[pd/viewer/clipboard]";
const CLIPBOARD_PREVIEW_LIMIT = 160;
const statusNode = document.getElementById("topbar");
const viewerNode = document.getElementById("viewer");

if (
  !(statusNode instanceof HTMLElement) ||
  !(viewerNode instanceof HTMLElement)
) {
  throw new Error("viewer DOM nodes are missing");
}

function logClipboard(
  level: ClipboardLogLevel,
  message: string,
  context: ClipboardLogContext = {},
): void {
  const payload = Object.keys(context).length === 0 ? undefined : context;
  const prefix = `${CLIPBOARD_LOG_NAMESPACE} ${message}`;

  switch (level) {
    case "debug":
      if (payload === undefined) {
        console.debug(prefix);
      } else {
        console.debug(prefix, payload);
      }
      return;
    case "info":
      if (payload === undefined) {
        console.info(prefix);
      } else {
        console.info(prefix, payload);
      }
      return;
    case "warn":
      if (payload === undefined) {
        console.warn(prefix);
      } else {
        console.warn(prefix, payload);
      }
      return;
    case "error":
      if (payload === undefined) {
        console.error(prefix);
      } else {
        console.error(prefix, payload);
      }
      return;
  }
}

function summarizeClipboardText(text: string): ClipboardLogContext {
  const preview =
    text.length > CLIPBOARD_PREVIEW_LIMIT
      ? `${text.slice(0, CLIPBOARD_PREVIEW_LIMIT)}…`
      : text;

  return {
    textLength: text.length,
    textLineCount: text === "" ? 0 : text.split(/\r\n|\r|\n/).length,
    textPreview: JSON.stringify(preview),
  };
}

function describeEventTarget(target: EventTarget | null): string {
  if (target === null) {
    return "null";
  }

  if (target === window) {
    return "window";
  }

  if (target === document) {
    return "document";
  }

  if (target instanceof HTMLElement) {
    const id = target.id !== "" ? `#${target.id}` : "";
    const className =
      typeof target.className === "string" && target.className.trim() !== ""
        ? `.${target.className.trim().split(/\s+/).join(".")}`
        : "";
    return `<${target.tagName.toLowerCase()}${id}${className}>`;
  }

  if (target instanceof Element) {
    return `<${target.tagName.toLowerCase()}>`;
  }

  if (target instanceof Node) {
    return target.nodeName;
  }

  return Object.prototype.toString.call(target);
}

function describeKeyboardEvent(event: KeyboardEvent): ClipboardLogContext {
  return {
    key: event.key,
    code: event.code,
    ctrlKey: event.ctrlKey,
    metaKey: event.metaKey,
    altKey: event.altKey,
    shiftKey: event.shiftKey,
    repeat: event.repeat,
    isTrusted: event.isTrusted,
    defaultPrevented: event.defaultPrevented,
    target: describeEventTarget(event.target),
    activeElement: describeEventTarget(document.activeElement),
  };
}

function describeClipboardEvent(event: ClipboardEvent): ClipboardLogContext {
  const clipboardTypes = event.clipboardData
    ? Array.from(event.clipboardData.types)
    : [];

  return {
    isTrusted: event.isTrusted,
    defaultPrevented: event.defaultPrevented,
    target: describeEventTarget(event.target),
    activeElement: describeEventTarget(document.activeElement),
    clipboardTypes,
    hasClipboardData: event.clipboardData !== null,
  };
}

function describeError(error: unknown): ClipboardLogContext {
  if (error instanceof Error) {
    return {
      errorName: error.name,
      errorMessage: error.message,
    };
  }

  return {
    errorValue: String(error),
  };
}

function isMacLikePlatform(): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(navigator.platform);
}

function isPasteShortcut(event: KeyboardEvent): boolean {
  const normalizedKey = event.key.toLowerCase();

  if (
    normalizedKey === "insert" &&
    event.shiftKey &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey
  ) {
    return true;
  }

  if (normalizedKey !== "v" || event.altKey) {
    return false;
  }

  if (isMacLikePlatform()) {
    return event.metaKey && !event.ctrlKey;
  }

  return event.ctrlKey && !event.metaKey && !event.shiftKey;
}

function isViewerClipboardContext(
  viewerElement: HTMLElement,
  target: EventTarget | null,
): boolean {
  if (target instanceof Node && viewerElement.contains(target)) {
    return true;
  }

  return (
    document.activeElement instanceof Node &&
    viewerElement.contains(document.activeElement)
  );
}

async function readClipboardText(
  source: string,
  context: ClipboardLogContext = {},
): Promise<string | null> {
  const clipboard = navigator.clipboard;

  if (!clipboard || typeof clipboard.readText !== "function") {
    logClipboard("warn", "navigator.clipboard.readText() is unavailable", {
      source,
      secureContext: window.isSecureContext,
      hasNavigatorClipboard: Boolean(clipboard),
      ...context,
    });
    return null;
  }

  logClipboard("debug", "starting navigator.clipboard.readText()", {
    source,
    secureContext: window.isSecureContext,
    ...context,
  });

  try {
    const text = await clipboard.readText();
    logClipboard("debug", "navigator.clipboard.readText() resolved", {
      source,
      ...summarizeClipboardText(text),
      ...context,
    });
    return text;
  } catch (error) {
    logClipboard("error", "navigator.clipboard.readText() failed", {
      source,
      secureContext: window.isSecureContext,
      ...describeError(error),
      ...context,
    });
    return null;
  }
}

async function writeClipboardText(
  text: string,
  source: string,
  context: ClipboardLogContext = {},
): Promise<void> {
  const clipboard = navigator.clipboard;

  if (!clipboard || typeof clipboard.writeText !== "function") {
    logClipboard("warn", "navigator.clipboard.writeText() is unavailable", {
      source,
      secureContext: window.isSecureContext,
      hasNavigatorClipboard: Boolean(clipboard),
      ...context,
      ...summarizeClipboardText(text),
    });
    return;
  }

  logClipboard("debug", "starting navigator.clipboard.writeText()", {
    source,
    secureContext: window.isSecureContext,
    ...context,
    ...summarizeClipboardText(text),
  });

  try {
    await clipboard.writeText(text);
    logClipboard("info", "navigator.clipboard.writeText() resolved", {
      source,
      ...context,
      ...summarizeClipboardText(text),
    });
  } catch (error) {
    logClipboard("error", "navigator.clipboard.writeText() failed", {
      source,
      secureContext: window.isSecureContext,
      ...describeError(error),
      ...context,
      ...summarizeClipboardText(text),
    });
  }
}

function setupClipboardBridge(viewerElement: HTMLElement, rfb: RFB): void {
  let nextPasteAttemptID = 0;
  let pendingPasteShortcut: PendingPasteShortcut | null = null;
  let lastLocalToRemoteClipboard: {
    source: string;
    text: string;
    at: number;
  } | null = null;

  logClipboard("info", "installing clipboard bridge", {
    viewerElement: describeEventTarget(viewerElement),
    secureContext: window.isSecureContext,
    hasNavigatorClipboard: Boolean(navigator.clipboard),
    canReadText: typeof navigator.clipboard?.readText === "function",
    canWriteText: typeof navigator.clipboard?.writeText === "function",
    platform: navigator.platform,
    userAgent: navigator.userAgent,
  });

  const sendClipboardTextToRemote = (
    source: string,
    text: string,
    context: ClipboardLogContext = {},
  ): void => {
    logClipboard("info", "calling rfb.clipboardPasteFrom()", {
      source,
      ...context,
      ...summarizeClipboardText(text),
    });

    try {
      rfb.clipboardPasteFrom(text);
      lastLocalToRemoteClipboard = {
        source,
        text,
        at: Date.now(),
      };
      logClipboard("debug", "rfb.clipboardPasteFrom() returned", {
        source,
        sentAt: lastLocalToRemoteClipboard.at,
        ...context,
      });
    } catch (error) {
      logClipboard("error", "rfb.clipboardPasteFrom() threw", {
        source,
        ...describeError(error),
        ...context,
        ...summarizeClipboardText(text),
      });
    }
  };

  const handlePasteShortcutFallback = async (
    pendingShortcut: PendingPasteShortcut,
  ): Promise<void> => {
    if (pendingPasteShortcut?.attemptID !== pendingShortcut.attemptID) {
      logClipboard(
        "debug",
        "canceling paste shortcut fallback because a native paste event already arrived",
        {
          attemptID: pendingShortcut.attemptID,
          elapsedMS: Date.now() - pendingShortcut.startedAt,
          key: pendingShortcut.key,
          code: pendingShortcut.code,
        },
      );
      return;
    }

    pendingPasteShortcut = null;
    logClipboard(
      "info",
      "native paste event did not arrive; using keydown clipboard read fallback",
      {
        attemptID: pendingShortcut.attemptID,
        elapsedMS: Date.now() - pendingShortcut.startedAt,
        key: pendingShortcut.key,
        code: pendingShortcut.code,
      },
    );

    const text = await pendingShortcut.clipboardTextPromise;
    if (text === null) {
      logClipboard(
        "warn",
        "paste shortcut fallback could not read clipboard text",
        {
          attemptID: pendingShortcut.attemptID,
          key: pendingShortcut.key,
          code: pendingShortcut.code,
        },
      );
      return;
    }

    sendClipboardTextToRemote("paste-shortcut-fallback", text, {
      attemptID: pendingShortcut.attemptID,
      key: pendingShortcut.key,
      code: pendingShortcut.code,
      elapsedMS: Date.now() - pendingShortcut.startedAt,
    });
  };

  viewerElement.addEventListener(
    "keydown",
    (event) => {
      if (!isPasteShortcut(event)) {
        return;
      }

      if (!isViewerClipboardContext(viewerElement, event.target)) {
        logClipboard(
          "debug",
          "ignoring paste shortcut outside viewer context",
          {
            ...describeKeyboardEvent(event),
          },
        );
        return;
      }

      if (event.repeat) {
        logClipboard("debug", "ignoring repeated paste shortcut keydown", {
          ...describeKeyboardEvent(event),
        });
        return;
      }

      const pendingShortcut: PendingPasteShortcut = {
        attemptID: ++nextPasteAttemptID,
        key: event.key,
        code: event.code,
        startedAt: Date.now(),
        // Start the clipboard read while the keydown is still a trusted user
        // gesture. We wait a macrotask before using the result so a native
        // paste event can win when the browser provides one.
        clipboardTextPromise: readClipboardText("paste-shortcut-keydown", {
          attemptID: nextPasteAttemptID,
          ...describeKeyboardEvent(event),
        }),
      };

      pendingPasteShortcut = pendingShortcut;
      logClipboard("info", "detected paste shortcut keydown inside viewer", {
        attemptID: pendingShortcut.attemptID,
        ...describeKeyboardEvent(event),
        note: "allowing the key event to continue so noVNC keyboard handling is not obviously disrupted",
      });

      window.setTimeout(() => {
        void handlePasteShortcutFallback(pendingShortcut);
      }, 0);
    },
    true,
  );

  window.addEventListener(
    "paste",
    (event) => {
      if (!isViewerClipboardContext(viewerElement, event.target)) {
        return;
      }

      const activePasteAttemptID = pendingPasteShortcut?.attemptID ?? null;
      pendingPasteShortcut = null;

      const clipboardTypes = event.clipboardData
        ? Array.from(event.clipboardData.types)
        : [];
      const textType =
        clipboardTypes.find((type) => type === "text/plain") ??
        clipboardTypes.find((type) => type === "text") ??
        null;
      const pastedText =
        textType !== null && event.clipboardData !== null
          ? event.clipboardData.getData(textType)
          : null;

      logClipboard("info", "received native paste event for viewer", {
        pendingPasteAttemptID: activePasteAttemptID,
        ...describeClipboardEvent(event),
        textType,
        pastedTextAvailable: pastedText !== null,
        pastedTextSummary:
          pastedText === null ? null : summarizeClipboardText(pastedText),
      });

      if (pastedText !== null) {
        sendClipboardTextToRemote("native-paste-event", pastedText, {
          pendingPasteAttemptID: activePasteAttemptID,
          textType,
        });
        return;
      }

      void (async () => {
        const text = await readClipboardText("native-paste-event-fallback", {
          pendingPasteAttemptID: activePasteAttemptID,
          ...describeClipboardEvent(event),
        });

        if (text === null) {
          logClipboard(
            "warn",
            "native paste fallback could not read clipboard text",
            {
              pendingPasteAttemptID: activePasteAttemptID,
            },
          );
          return;
        }

        sendClipboardTextToRemote("native-paste-event-fallback", text, {
          pendingPasteAttemptID: activePasteAttemptID,
        });
      })();
    },
    true,
  );

  rfb.addEventListener("connect", () => {
    logClipboard("info", "rfb connected; clipboard bridge is active", {
      viewerElement: describeEventTarget(viewerElement),
    });
  });

  rfb.addEventListener("disconnect", (event) => {
    logClipboard(
      "warn",
      "rfb disconnected; clipboard bridge listeners remain attached",
      {
        clean: event.detail?.clean ?? null,
      },
    );
  });

  rfb.addEventListener("clipboard", (event) => {
    const text = event.detail.text;
    const recentLocalToRemoteEcho =
      lastLocalToRemoteClipboard !== null &&
      lastLocalToRemoteClipboard.text === text &&
      Date.now() - lastLocalToRemoteClipboard.at < 5000;

    logClipboard("info", "received noVNC clipboard event from remote desktop", {
      recentLocalToRemoteEcho,
      lastLocalToRemoteSource: lastLocalToRemoteClipboard?.source ?? null,
      ...summarizeClipboardText(text),
    });

    void writeClipboardText(text, "remote-clipboard-event", {
      recentLocalToRemoteEcho,
      lastLocalToRemoteSource: lastLocalToRemoteClipboard?.source ?? null,
    });
  });
}

const protocol = location.protocol === "https:" ? "wss" : "ws";
const wsUrl = `${protocol}://${location.host}/ws`;
const rfb = createClient(viewerNode, { url: wsUrl });

const configSource =
  (
    globalThis as unknown as {
      PORTABLEDESKTOP_VIEWER_CONFIG?: Partial<ViewerConfig>;
    }
  ).PORTABLEDESKTOP_VIEWER_CONFIG || {};
const viewerConfig: ViewerConfig = {
  scale: configSource.scale === "1:1" ? "1:1" : "fit",
  desktopSizeMode:
    configSource.desktopSizeMode === "dynamic" ? "dynamic" : "fixed",
};

rfb.scaleViewport = viewerConfig.scale === "fit";
rfb.resizeSession = viewerConfig.desktopSizeMode === "dynamic";
setupClipboardBridge(viewerNode, rfb);

rfb.addEventListener("connect", () => {
  statusNode.textContent = `connected: ${wsUrl} | scale=${viewerConfig.scale} | sizeMode=${viewerConfig.desktopSizeMode}`;
});

rfb.addEventListener("disconnect", (disconnectEvent) => {
  statusNode.textContent =
    disconnectEvent.detail?.clean === false
      ? "disconnected (unclean)"
      : "disconnected";
});

rfb.addEventListener("credentialsrequired", () => {
  statusNode.textContent = "credentials required";
});

rfb.addEventListener("securityfailure", () => {
  statusNode.textContent = "security failure";
});
