import { describe, expect, it } from "bun:test";

import { openai } from "../src/ai";

interface Call {
  name: string;
  args: unknown[];
}

function createPngStub(width: number, height: number): string {
  const data = Buffer.alloc(24);
  data[0] = 0x89;
  data[1] = 0x50;
  data[2] = 0x4e;
  data[3] = 0x47;
  data[4] = 0x0d;
  data[5] = 0x0a;
  data[6] = 0x1a;
  data[7] = 0x0a;
  data.write("IHDR", 12, "ascii");
  data.writeUInt32BE(width, 16);
  data.writeUInt32BE(height, 20);
  return data.toString("base64");
}

function createDesktopMock(screenshotData: string) {
  const calls: Call[] = [];

  const desktop = {
    async screenshot(options: unknown) {
      calls.push({ name: "screenshot", args: [options] });
      return { data: screenshotData, mediaType: "image/png" as const };
    },
    async mousePosition() {
      calls.push({ name: "mousePosition", args: [] });
      return { x: 0, y: 0 };
    },
    async moveMouse(x: number, y: number) {
      calls.push({ name: "moveMouse", args: [x, y] });
    },
    async click(button: string) {
      calls.push({ name: "click", args: [button] });
    },
    async mouseDown(button: string) {
      calls.push({ name: "mouseDown", args: [button] });
    },
    async mouseUp(button: string) {
      calls.push({ name: "mouseUp", args: [button] });
    },
    async scroll(dx: number, dy: number) {
      calls.push({ name: "scroll", args: [dx, dy] });
    },
    async type(text: string) {
      calls.push({ name: "type", args: [text] });
    },
    async key(text: string) {
      calls.push({ name: "key", args: [text] });
    },
    async keyDown(key: string) {
      calls.push({ name: "keyDown", args: [key] });
    },
    async keyUp(key: string) {
      calls.push({ name: "keyUp", args: [key] });
    }
  };

  const tool = openai.tools.computer({
    desktop: desktop as never,
    displayWidthPx: 1512,
    displayHeightPx: 982
  });

  return { tool, calls };
}

function lastCall(calls: Call[], name: string): Call | undefined {
  for (let i = calls.length - 1; i >= 0; i -= 1) {
    if (calls[i]?.name === name) {
      return calls[i];
    }
  }
  return undefined;
}

describe("Anthropic-style computer scaling", () => {
  it("downscales screenshot uploads to Anthropic constraints", async () => {
    const { tool, calls } = createDesktopMock(createPngStub(1330, 864));

    await tool.execute({ action: "screenshot" }, {});

    const screenshotCall = lastCall(calls, "screenshot");
    expect(screenshotCall).toBeDefined();

    const options = screenshotCall?.args[0] as {
      region?: [number, number, number, number];
      scaleToGeometry?: boolean;
      targetWidth?: number;
      targetHeight?: number;
    };

    expect(options.region).toBeUndefined();
    expect(options.scaleToGeometry).toBe(false);
    expect(options.targetWidth).toBe(1330);
    expect(options.targetHeight).toBe(864);
  });

  it("uses actual screenshot dimensions when mapping coordinates back to native pixels", async () => {
    const { tool, calls } = createDesktopMock(createPngStub(1329, 863));

    await tool.execute({ action: "screenshot" }, {});
    await tool.execute({ action: "mouse_move", coordinate: [1328, 862] }, {});

    const move = lastCall(calls, "moveMouse");
    expect(move?.args).toEqual([1511, 981]);
  });

  it("maps zoom regions from scaled space back to native capture region", async () => {
    const { tool, calls } = createDesktopMock(createPngStub(1330, 864));

    await tool.execute({ action: "screenshot" }, {});
    await tool.execute({ action: "zoom", region: [0, 0, 1329, 863] }, {});

    const screenshotCalls = calls.filter((call) => call.name === "screenshot");
    expect(screenshotCalls).toHaveLength(2);

    const zoomOptions = screenshotCalls[1]?.args[0] as {
      region?: [number, number, number, number];
      scaleToGeometry?: boolean;
      targetWidth?: number;
      targetHeight?: number;
    };

    expect(zoomOptions.region).toEqual([0, 0, 1512, 982]);
    expect(zoomOptions.scaleToGeometry).toBe(true);
    expect(zoomOptions.targetWidth).toBe(1330);
    expect(zoomOptions.targetHeight).toBe(864);
  });
});

describe("Modifier keys for click/scroll actions", () => {
  it("holds and releases modifiers around click actions", async () => {
    const { tool, calls } = createDesktopMock(createPngStub(1330, 864));

    await tool.execute({ action: "left_click", text: "shift+ctrl" }, {});

    expect(calls.map((call) => `${call.name}:${call.args.join(",")}`)).toEqual([
      "keyDown:shift",
      "keyDown:ctrl",
      "click:left",
      "keyUp:ctrl",
      "keyUp:shift"
    ]);
  });

  it("holds and releases modifiers around scroll actions", async () => {
    const { tool, calls } = createDesktopMock(createPngStub(1330, 864));

    await tool.execute(
      {
        action: "scroll",
        scroll_direction: "down",
        scroll_amount: 3,
        text: "shift"
      },
      {}
    );

    expect(calls.map((call) => `${call.name}:${call.args.join(",")}`)).toEqual([
      "keyDown:shift",
      "scroll:0,3",
      "keyUp:shift"
    ]);
  });
});
