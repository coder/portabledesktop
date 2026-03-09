import * as RfbModule from "@novnc/novnc/lib/rfb.js";
import type RFB from "@novnc/novnc/lib/rfb.js";
import type { RfbOptions, UrlOrChannel } from "@novnc/novnc/lib/rfb.js";

export interface CreateClientOptions extends RfbOptions {
  url: UrlOrChannel;
  /**
   * Scale the remote framebuffer to fit the target element.
   * Defaults to `true`.
   */
  scaleViewport?: boolean;
  /**
   * Focus the canvas when clicked, so keyboard input works immediately.
   * Defaults to `true`.
   */
  focusOnClick?: boolean;
}

type RfbCtor = new (target: Element, urlOrChannel: UrlOrChannel, options?: RfbOptions) => RFB;

function resolveRfbConstructor(moduleValue: unknown): RfbCtor {
  if (typeof moduleValue === "function") {
    return moduleValue as RfbCtor;
  }

  if (moduleValue && typeof moduleValue === "object") {
    const moduleRecord = moduleValue as Record<string, unknown>;
    const defaultValue = moduleRecord.default;

    if (typeof defaultValue === "function") {
      return defaultValue as RfbCtor;
    }

    if (defaultValue && typeof defaultValue === "object") {
      const nestedDefault = (defaultValue as Record<string, unknown>).default;
      if (typeof nestedDefault === "function") {
        return nestedDefault as RfbCtor;
      }
    }

    const namedValue = moduleRecord.RFB;
    if (typeof namedValue === "function") {
      return namedValue as RfbCtor;
    }
  }

  throw new Error("unable to resolve noVNC RFB constructor");
}

const RFBConstructor = resolveRfbConstructor(RfbModule);

export function createClient(targetElement: Element, options: CreateClientOptions): RFB {
  if (!targetElement) {
    throw new Error("createClient requires a target DOM element");
  }

  if (!options.url) {
    throw new Error("createClient requires options.url");
  }

  const {
    url,
    shared = true,
    scaleViewport = true,
    focusOnClick = true,
    ...rfbOptions
  } = options;

  const client = new RFBConstructor(targetElement, url, {
    ...rfbOptions,
    shared
  });

  client.scaleViewport = scaleViewport;
  client.focusOnClick = focusOnClick;
  return client;
}
