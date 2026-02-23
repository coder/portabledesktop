import { anthropic as anthropicProvider } from "@ai-sdk/anthropic";
import { jsonSchema, tool } from "ai";

import type { Desktop } from "../session";

interface ComputerImageOutput {
  kind: "image";
  data: string;
  mediaType: "image/png";
}

interface ComputerTextOutput {
  kind: "text";
  text: string;
}

type ComputerToolOutput = ComputerImageOutput | ComputerTextOutput;

type AnthropicComputer20250124ToolArgs = Parameters<typeof anthropicProvider.tools.computer_20250124>[0];
type AnthropicComputer20251124ToolArgs = Parameters<typeof anthropicProvider.tools.computer_20251124>[0];

type AnthropicComputer20250124ToolExecute = NonNullable<AnthropicComputer20250124ToolArgs["execute"]>;
type AnthropicComputer20251124ToolExecute = NonNullable<AnthropicComputer20251124ToolArgs["execute"]>;
type AnthropicComputer20250124ExecuteOptions = Parameters<AnthropicComputer20250124ToolExecute>[1];
type AnthropicComputer20251124ExecuteOptions = Parameters<AnthropicComputer20251124ToolExecute>[1];

type AnthropicComputer20250124Action = Parameters<AnthropicComputer20250124ToolExecute>[0];
type AnthropicComputer20251124Action = Parameters<AnthropicComputer20251124ToolExecute>[0];

type ComputerAction = AnthropicComputer20251124Action;

type SupportedComputerAction = AnthropicComputer20250124Action | AnthropicComputer20251124Action;

interface DesktopComputerOptions {
  desktop: Desktop;
  displayWidthPx: number;
  displayHeightPx: number;
  screenshotTimeoutMs?: number;
}

interface AnthropicComputerToolSharedOptions extends DesktopComputerOptions {
  displayNumber?: number;
}

interface AnthropicComputer20250124ToolOptions extends AnthropicComputerToolSharedOptions {}

interface AnthropicComputer20251124ToolOptions extends AnthropicComputerToolSharedOptions {
  enableZoom?: boolean;
}

interface OpenAIComputerToolOptions extends DesktopComputerOptions {
  description?: string;
  enableZoom?: boolean;
}

const DEFAULT_SCREENSHOT_TIMEOUT_MS = 20_000;
const ANTHROPIC_MAX_SCREENSHOT_LONG_EDGE_PX = 1568;
const ANTHROPIC_MAX_SCREENSHOT_TOTAL_PIXELS = 1_150_000;

interface ExecuteOptions {
  abortSignal?: AbortSignal;
}

interface CaptureViewport {
  nativeLeft: number;
  nativeTop: number;
  nativeWidth: number;
  nativeHeight: number;
  scaledWidth: number;
  scaledHeight: number;
}

interface CapturePlan {
  region?: [number, number, number, number];
  scaleToGeometry: boolean;
  viewport: CaptureViewport;
}

function abortError(signal?: AbortSignal): Error {
  const reason = signal?.reason;
  if (reason instanceof Error) {
    return reason;
  }

  const error = new Error(typeof reason === "string" && reason.length > 0 ? reason : "The operation was aborted");
  error.name = "AbortError";
  return error;
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) {
    throw abortError(signal);
  }
}

function withAbort<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  throwIfAborted(signal);

  if (!signal) {
    return promise;
  }

  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(abortError(signal));
    signal.addEventListener("abort", onAbort, { once: true });

    promise.then(
      (value) => {
        signal.removeEventListener("abort", onAbort);
        resolve(value);
      },
      (error) => {
        signal.removeEventListener("abort", onAbort);
        reject(error);
      }
    );
  });
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  throwIfAborted(signal);

  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      if (signal) {
        signal.removeEventListener("abort", onAbort);
      }
      resolve();
    }, ms);

    const onAbort = () => {
      clearTimeout(timer);
      reject(abortError(signal));
    };

    if (signal) {
      signal.addEventListener("abort", onAbort, { once: true });
    }
  });
}

function requirePositiveInteger(value: number, fieldName: string): number {
  if (!Number.isFinite(value) || !Number.isInteger(value) || value <= 0) {
    throw new Error(`${fieldName} must be a positive integer`);
  }
  return value;
}

function computeScaledScreenshotSize(width: number, height: number): { width: number; height: number } {
  const longEdge = Math.max(width, height);
  const totalPixels = width * height;
  const longEdgeScale = ANTHROPIC_MAX_SCREENSHOT_LONG_EDGE_PX / longEdge;
  const totalPixelsScale = Math.sqrt(ANTHROPIC_MAX_SCREENSHOT_TOTAL_PIXELS / totalPixels);
  const scale = Math.min(1, longEdgeScale, totalPixelsScale);

  if (scale >= 1) {
    return { width, height };
  }

  return {
    width: Math.max(1, Math.floor(width * scale)),
    height: Math.max(1, Math.floor(height * scale))
  };
}

function parsePngDimensions(data: string): { width: number; height: number } | null {
  let decoded: Buffer;
  try {
    decoded = Buffer.from(data, "base64");
  } catch {
    return null;
  }

  if (decoded.length < 24) {
    return null;
  }

  const isPng =
    decoded[0] === 0x89 &&
    decoded[1] === 0x50 &&
    decoded[2] === 0x4e &&
    decoded[3] === 0x47 &&
    decoded[4] === 0x0d &&
    decoded[5] === 0x0a &&
    decoded[6] === 0x1a &&
    decoded[7] === 0x0a &&
    decoded.toString("ascii", 12, 16) === "IHDR";

  if (!isPng) {
    return null;
  }

  const width = decoded.readUInt32BE(16);
  const height = decoded.readUInt32BE(20);
  if (width < 1 || height < 1) {
    return null;
  }

  return { width, height };
}

function toModelOutput(output: ComputerToolOutput): {
  type: "content";
  value: Array<{ type: "image-data"; data: string; mediaType: "image/png" } | { type: "text"; text: string }>;
} {
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

class DesktopComputer {
  public readonly desktop: Desktop;
  public readonly displayWidthPx: number;
  public readonly displayHeightPx: number;
  public readonly screenshotTimeoutMs: number;

  private latestViewport: CaptureViewport | null = null;

  constructor(options: DesktopComputerOptions) {
    this.desktop = options.desktop;
    this.displayWidthPx = requirePositiveInteger(options.displayWidthPx, "displayWidthPx");
    this.displayHeightPx = requirePositiveInteger(options.displayHeightPx, "displayHeightPx");
    this.screenshotTimeoutMs = options.screenshotTimeoutMs ?? DEFAULT_SCREENSHOT_TIMEOUT_MS;
  }

  private clampPoint(point: readonly [number, number]): [number, number] {
    const rawX = Number.isFinite(point[0]) ? point[0] : 0;
    const rawY = Number.isFinite(point[1]) ? point[1] : 0;
    const x = Math.max(0, Math.min(this.displayWidthPx - 1, Math.round(rawX)));
    const y = Math.max(0, Math.min(this.displayHeightPx - 1, Math.round(rawY)));
    return [x, y];
  }

  private clampRegion(region: readonly [number, number, number, number]): [number, number, number, number] {
    const safe = region.map((value) => (Number.isFinite(value) ? value : 0)) as [number, number, number, number];
    const [x1, y1, x2, y2] = safe;
    const left = Math.max(0, Math.min(this.displayWidthPx - 1, Math.floor(Math.min(x1, x2))));
    const top = Math.max(0, Math.min(this.displayHeightPx - 1, Math.floor(Math.min(y1, y2))));
    const right = Math.max(left + 1, Math.min(this.displayWidthPx, Math.ceil(Math.max(x1, x2))));
    const bottom = Math.max(top + 1, Math.min(this.displayHeightPx, Math.ceil(Math.max(y1, y2))));
    return [left, top, right, bottom];
  }

  private scaledToNativeAxis(value: number, scaledSpan: number, nativeSpan: number): number {
    if (nativeSpan <= 0 || scaledSpan <= 0) {
      return 0;
    }
    const safe = Number.isFinite(value) ? value : 0;
    const clamped = Math.max(0, Math.min(scaledSpan - 1, safe));
    return ((clamped + 0.5) * nativeSpan) / scaledSpan - 0.5;
  }

  private nativeToScaledAxis(value: number, nativeSpan: number, scaledSpan: number): number {
    if (nativeSpan <= 0 || scaledSpan <= 0) {
      return 0;
    }
    const safe = Number.isFinite(value) ? value : 0;
    const clamped = Math.max(0, Math.min(nativeSpan - 1, safe));
    return ((clamped + 0.5) * scaledSpan) / nativeSpan - 0.5;
  }

  private unscalePoint(point: readonly [number, number]): [number, number] {
    if (!this.latestViewport) {
      return [point[0], point[1]];
    }

    const viewport = this.latestViewport;
    const localX = this.scaledToNativeAxis(point[0], viewport.scaledWidth, viewport.nativeWidth);
    const localY = this.scaledToNativeAxis(point[1], viewport.scaledHeight, viewport.nativeHeight);
    return [viewport.nativeLeft + localX, viewport.nativeTop + localY];
  }

  private scalePoint(point: readonly [number, number]): [number, number] {
    if (!this.latestViewport) {
      return [point[0], point[1]];
    }

    const viewport = this.latestViewport;
    const localX = point[0] - viewport.nativeLeft;
    const localY = point[1] - viewport.nativeTop;
    const x = this.nativeToScaledAxis(localX, viewport.nativeWidth, viewport.scaledWidth);
    const y = this.nativeToScaledAxis(localY, viewport.nativeHeight, viewport.scaledHeight);
    return [Math.round(x), Math.round(y)];
  }

  private unscaleRegion(
    region: readonly [number, number, number, number] | undefined
  ): [number, number, number, number] | undefined {
    if (!region) {
      return undefined;
    }

    const [startX, startY] = this.clampPoint(this.unscalePoint([region[0], region[1]]));
    const [endX, endY] = this.clampPoint(this.unscalePoint([region[2], region[3]]));
    const left = Math.max(0, Math.min(startX, endX));
    const top = Math.max(0, Math.min(startY, endY));
    const right = Math.max(left + 1, Math.min(this.displayWidthPx, Math.max(startX, endX) + 1));
    const bottom = Math.max(top + 1, Math.min(this.displayHeightPx, Math.max(startY, endY) + 1));
    return [left, top, right, bottom];
  }

  private requirePoint(point: readonly [number, number] | undefined, label: string): [number, number] {
    if (!point) {
      throw new Error(`${label} is required for this action`);
    }
    return this.clampPoint(this.unscalePoint(point));
  }

  private optionalPoint(point: readonly [number, number] | undefined): [number, number] | null {
    if (!point) {
      return null;
    }
    return this.clampPoint(this.unscalePoint(point));
  }

  private parseModifierKeys(text: string | undefined): string[] {
    if (!text) {
      return [];
    }

    return text
      .split("+")
      .map((key) => key.trim())
      .filter((key) => key.length > 0);
  }

  private async withModifierKeys<T>(
    modifiers: string | undefined,
    signal: AbortSignal | undefined,
    action: () => Promise<T>
  ): Promise<T> {
    const keys = this.parseModifierKeys(modifiers);
    if (keys.length === 0) {
      return action();
    }

    const pressedKeys: string[] = [];
    try {
      for (const key of keys) {
        throwIfAborted(signal);
        // eslint-disable-next-line no-await-in-loop
        await this.run(() => this.desktop.keyDown(key), signal);
        pressedKeys.push(key);
      }

      return await action();
    } finally {
      for (const key of [...pressedKeys].reverse()) {
        try {
          // Ensure modifiers are released even when execution is aborted.
          // eslint-disable-next-line no-await-in-loop
          await this.desktop.keyUp(key);
        } catch {
          // ignore key release cleanup failures
        }
      }
    }
  }

  private async run<T>(operation: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    throwIfAborted(signal);
    return withAbort(operation(), signal);
  }

  private async currentCursor(signal?: AbortSignal): Promise<[number, number]> {
    const position = await this.run(() => this.desktop.mousePosition(), signal);
    const nativePoint = this.clampPoint([position.x, position.y]);
    const modelPoint = this.scalePoint(nativePoint);
    return this.clampPoint(modelPoint);
  }

  private buildCapturePlan(region: [number, number, number, number] | undefined): CapturePlan {
    if (!region) {
      const scaled = computeScaledScreenshotSize(this.displayWidthPx, this.displayHeightPx);
      return {
        scaleToGeometry: false,
        viewport: {
          nativeLeft: 0,
          nativeTop: 0,
          nativeWidth: this.displayWidthPx,
          nativeHeight: this.displayHeightPx,
          scaledWidth: scaled.width,
          scaledHeight: scaled.height
        }
      };
    }

    const [left, top, right, bottom] = this.clampRegion(region);
    const scaled = computeScaledScreenshotSize(this.displayWidthPx, this.displayHeightPx);
    return {
      region: [left, top, right, bottom],
      scaleToGeometry: true,
      viewport: {
        nativeLeft: left,
        nativeTop: top,
        nativeWidth: right - left,
        nativeHeight: bottom - top,
        scaledWidth: scaled.width,
        scaledHeight: scaled.height
      }
    };
  }

  private async capturePng(region: [number, number, number, number] | undefined, signal?: AbortSignal): Promise<string> {
    const capturePlan = this.buildCapturePlan(region);

    const screenshot = await this.run(
      () =>
        this.desktop.screenshot({
          region: capturePlan.region,
          scaleToGeometry: capturePlan.scaleToGeometry,
          targetWidth: capturePlan.viewport.scaledWidth,
          targetHeight: capturePlan.viewport.scaledHeight,
          timeoutMs: this.screenshotTimeoutMs
        }),
      signal
    );

    const actualDimensions = parsePngDimensions(screenshot.data);
    this.latestViewport = {
      ...capturePlan.viewport,
      scaledWidth: actualDimensions?.width ?? capturePlan.viewport.scaledWidth,
      scaledHeight: actualDimensions?.height ?? capturePlan.viewport.scaledHeight
    };

    return screenshot.data;
  }

  private async repeatClick(button: "left" | "middle" | "right", count: number, signal?: AbortSignal): Promise<void> {
    const repeatCount = Math.max(1, Math.round(count));
    for (let i = 0; i < repeatCount; i += 1) {
      throwIfAborted(signal);
      // eslint-disable-next-line no-await-in-loop
      await this.run(() => this.desktop.click(button), signal);
    }
  }

  async execute(input: SupportedComputerAction, options: ExecuteOptions = {}): Promise<ComputerToolOutput> {
    const signal = options.abortSignal;
    throwIfAborted(signal);

    switch (input.action) {
      case "key": {
        if (!input.text) {
          throw new Error("text is required for key action");
        }
        const text = input.text;
        await this.run(() => this.desktop.key(text), signal);
        return { kind: "text", text: `pressed key combo: ${text}` };
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

        const pressedKeys: string[] = [];
        for (const key of keys) {
          throwIfAborted(signal);
          // eslint-disable-next-line no-await-in-loop
          await this.run(() => this.desktop.keyDown(key), signal);
          pressedKeys.push(key);
        }

        const durationMs = Math.max(10, Math.round((input.duration ?? 0.25) * 1000));
        try {
          await delay(durationMs, signal);
        } finally {
          for (const key of [...pressedKeys].reverse()) {
            try {
              // Always release keys, even when the action is aborted.
              // eslint-disable-next-line no-await-in-loop
              await this.desktop.keyUp(key);
            } catch {
              // ignore key release cleanup failures
            }
          }
        }

        throwIfAborted(signal);
        return { kind: "text", text: `held keys for ${durationMs}ms: ${keys.join("+")}` };
      }
      case "type": {
        if (!input.text) {
          throw new Error("text is required for type action");
        }
        const text = input.text;
        await this.run(() => this.desktop.type(text), signal);
        return { kind: "text", text: `typed ${text.length} characters` };
      }
      case "cursor_position": {
        const [x, y] = await this.currentCursor(signal);
        return { kind: "text", text: `cursor at ${x},${y}` };
      }
      case "mouse_move": {
        const [x, y] = this.requirePoint(input.coordinate, "coordinate");
        await this.run(() => this.desktop.moveMouse(x, y), signal);
        return { kind: "text", text: `moved mouse to ${x},${y}` };
      }
      case "left_mouse_down": {
        await this.run(() => this.desktop.mouseDown("left"), signal);
        return { kind: "text", text: "left mouse down" };
      }
      case "left_mouse_up": {
        await this.run(() => this.desktop.mouseUp("left"), signal);
        return { kind: "text", text: "left mouse up" };
      }
      case "left_click": {
        const point = this.optionalPoint(input.coordinate);
        if (point) {
          const [x, y] = point;
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.withModifierKeys(input.text, signal, () => this.run(() => this.desktop.click("left"), signal));
        return { kind: "text", text: "left click" };
      }
      case "left_click_drag": {
        const [startX, startY] = this.requirePoint(input.start_coordinate, "start_coordinate");
        const [endX, endY] = this.requirePoint(input.coordinate, "coordinate");

        await this.run(() => this.desktop.moveMouse(startX, startY), signal);
        await this.run(() => this.desktop.mouseDown("left"), signal);

        try {
          await this.run(() => this.desktop.moveMouse(endX, endY), signal);
        } finally {
          try {
            // Ensure button state is restored when a drag is aborted mid-flight.
            await this.desktop.mouseUp("left");
          } catch {
            // ignore cleanup failures
          }
        }

        throwIfAborted(signal);
        return { kind: "text", text: `dragged from ${startX},${startY} to ${endX},${endY}` };
      }
      case "right_click": {
        const point = this.optionalPoint(input.coordinate);
        if (point) {
          const [x, y] = point;
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.withModifierKeys(input.text, signal, () => this.run(() => this.desktop.click("right"), signal));
        return { kind: "text", text: "right click" };
      }
      case "middle_click": {
        const point = this.optionalPoint(input.coordinate);
        if (point) {
          const [x, y] = point;
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.withModifierKeys(input.text, signal, () => this.run(() => this.desktop.click("middle"), signal));
        return { kind: "text", text: "middle click" };
      }
      case "double_click": {
        const point = this.optionalPoint(input.coordinate);
        if (point) {
          const [x, y] = point;
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.withModifierKeys(input.text, signal, () => this.repeatClick("left", 2, signal));
        return { kind: "text", text: "double click" };
      }
      case "triple_click": {
        const point = this.optionalPoint(input.coordinate);
        if (point) {
          const [x, y] = point;
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.withModifierKeys(input.text, signal, () => this.repeatClick("left", 3, signal));
        return { kind: "text", text: "triple click" };
      }
      case "scroll": {
        const point = this.optionalPoint(input.coordinate);
        if (point) {
          const [x, y] = point;
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }

        const amount = Math.max(1, Math.round(input.scroll_amount ?? 3));
        const direction = input.scroll_direction ?? "down";
        const delta =
          direction === "up"
            ? { dx: 0, dy: -amount }
            : direction === "down"
              ? { dx: 0, dy: amount }
              : direction === "left"
                ? { dx: -amount, dy: 0 }
                : { dx: amount, dy: 0 };

        await this.withModifierKeys(input.text, signal, () =>
          this.run(() => this.desktop.scroll(delta.dx, delta.dy), signal)
        );
        return { kind: "text", text: `scrolled ${direction} by ${amount}` };
      }
      case "wait": {
        const waitMs = Math.max(10, Math.round((input.duration ?? 1) * 1000));
        await delay(waitMs, signal);
        return { kind: "text", text: `waited ${waitMs}ms` };
      }
      case "screenshot": {
        const data = await this.capturePng(undefined, signal);
        return { kind: "image", data, mediaType: "image/png" };
      }
      case "zoom": {
        const zoomInput = input as AnthropicComputer20251124Action;
        const data = await this.capturePng(this.unscaleRegion(zoomInput.region), signal);
        return { kind: "image", data, mediaType: "image/png" };
      }
      default: {
        throw new Error(`unsupported action: ${String((input as { action?: string }).action ?? "unknown")}`);
      }
    }
  }
}

export const anthropic = {
  tools: {
    computer_20250124: (options: AnthropicComputer20250124ToolOptions) => {
      const computer = new DesktopComputer(options);
      return anthropicProvider.tools.computer_20250124<ComputerToolOutput>({
        displayWidthPx: computer.displayWidthPx,
        displayHeightPx: computer.displayHeightPx,
        displayNumber: options.displayNumber,
        execute: async (input, executeOptions: AnthropicComputer20250124ExecuteOptions) =>
          computer.execute(input, { abortSignal: executeOptions.abortSignal }),
        toModelOutput: ({ output }) => toModelOutput(output)
      });
    },
    computer_20251124: (options: AnthropicComputer20251124ToolOptions) => {
      const computer = new DesktopComputer(options);
      return anthropicProvider.tools.computer_20251124<ComputerToolOutput>({
        displayWidthPx: computer.displayWidthPx,
        displayHeightPx: computer.displayHeightPx,
        displayNumber: options.displayNumber,
        enableZoom: options.enableZoom,
        execute: async (input, executeOptions: AnthropicComputer20251124ExecuteOptions) =>
          computer.execute(input, { abortSignal: executeOptions.abortSignal }),
        toModelOutput: ({ output }) => toModelOutput(output)
      });
    }
  }
} as const;

export const openai = {
  tools: {
    computer: (options: OpenAIComputerToolOptions) => {
      const computer = new DesktopComputer(options);
      const enableZoom = options.enableZoom ?? true;

      return tool<ComputerAction, ComputerToolOutput>({
        description:
          options.description ??
          "Control a Linux desktop session with keyboard, mouse, scroll, wait, and screenshot actions.",
        inputSchema: jsonSchema<ComputerAction>({
          type: "object",
          additionalProperties: false,
          required: ["action"],
          properties: {
            action: {
              type: "string",
              enum: [
                "key",
                "hold_key",
                "type",
                "cursor_position",
                "mouse_move",
                "left_mouse_down",
                "left_mouse_up",
                "left_click",
                "left_click_drag",
                "right_click",
                "middle_click",
                "double_click",
                "triple_click",
                "scroll",
                "wait",
                "screenshot",
                "zoom"
              ]
            },
            coordinate: {
              type: "array",
              items: { type: "number" },
              minItems: 2,
              maxItems: 2
            },
            start_coordinate: {
              type: "array",
              items: { type: "number" },
              minItems: 2,
              maxItems: 2
            },
            region: {
              type: "array",
              items: { type: "number" },
              minItems: 4,
              maxItems: 4
            },
            text: { type: "string" },
            duration: { type: "number" },
            scroll_amount: { type: "number" },
            scroll_direction: {
              type: "string",
              enum: ["up", "down", "left", "right"]
            }
          }
        }),
        execute: async (input, executeOptions) => {
          if (!enableZoom && input.action === "zoom") {
            return { kind: "text", text: "zoom action is disabled" };
          }
          return computer.execute(input, { abortSignal: executeOptions.abortSignal });
        },
        toModelOutput: ({ output }) => toModelOutput(output)
      });
    }
  }
} as const;
