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

interface ExecuteOptions {
  abortSignal?: AbortSignal;
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

  constructor(options: DesktopComputerOptions) {
    this.desktop = options.desktop;
    this.displayWidthPx = requirePositiveInteger(options.displayWidthPx, "displayWidthPx");
    this.displayHeightPx = requirePositiveInteger(options.displayHeightPx, "displayHeightPx");
    this.screenshotTimeoutMs = options.screenshotTimeoutMs ?? DEFAULT_SCREENSHOT_TIMEOUT_MS;
  }

  private clampPoint(point: readonly [number, number]): [number, number] {
    const x = Math.max(0, Math.min(this.displayWidthPx - 1, Math.round(point[0])));
    const y = Math.max(0, Math.min(this.displayHeightPx - 1, Math.round(point[1])));
    return [x, y];
  }

  private requirePoint(point: [number, number] | undefined, label: string): [number, number] {
    if (!point) {
      throw new Error(`${label} is required for this action`);
    }
    return this.clampPoint(point);
  }

  private async run<T>(operation: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    throwIfAborted(signal);
    return withAbort(operation(), signal);
  }

  private async currentCursor(signal?: AbortSignal): Promise<[number, number]> {
    const position = await this.run(() => this.desktop.mousePosition(), signal);
    return this.clampPoint([position.x, position.y]);
  }

  private async capturePng(region: [number, number, number, number] | undefined, signal?: AbortSignal): Promise<string> {
    const screenshot = await this.run(
      () =>
        this.desktop.screenshot({
        region,
        scaleToGeometry: region != null,
        timeoutMs: this.screenshotTimeoutMs
        }),
      signal
    );
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
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.run(() => this.desktop.click("left"), signal);
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
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.run(() => this.desktop.click("right"), signal);
        return { kind: "text", text: "right click" };
      }
      case "middle_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.run(() => this.desktop.click("middle"), signal);
        return { kind: "text", text: "middle click" };
      }
      case "double_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.repeatClick("left", 2, signal);
        return { kind: "text", text: "double click" };
      }
      case "triple_click": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
          await this.run(() => this.desktop.moveMouse(x, y), signal);
        }
        await this.repeatClick("left", 3, signal);
        return { kind: "text", text: "triple click" };
      }
      case "scroll": {
        if (input.coordinate) {
          const [x, y] = this.clampPoint(input.coordinate);
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

        await this.run(() => this.desktop.scroll(delta.dx, delta.dy), signal);
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
        const data = await this.capturePng(zoomInput.region, signal);
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
