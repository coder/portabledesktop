import RFB, { type RfbOptions, type UrlOrChannel } from "@novnc/novnc/lib/rfb.js";

export interface CreateClientOptions extends RfbOptions {
  url: UrlOrChannel;
}

export function createClient(targetElement: Element, options: CreateClientOptions): RFB {
  if (!targetElement) {
    throw new Error("createClient requires a target DOM element");
  }

  if (!options.url) {
    throw new Error("createClient requires options.url");
  }

  const { url, ...rfbOptions } = options;
  return new RFB(targetElement, url, rfbOptions);
}
