declare module "@novnc/novnc/lib/rfb.js" {
  export type UrlOrChannel = string | WebSocket | RTCDataChannel;

  export interface RfbDisconnectDetail {
    clean?: boolean;
  }

  export interface RfbEventMap {
    connect: Event;
    disconnect: CustomEvent<RfbDisconnectDetail>;
    credentialsrequired: Event;
    securityfailure: Event;
  }

  export interface RfbCredentials {
    username?: string;
    password?: string;
    target?: string;
    token?: string;
  }

  export interface RfbOptions {
    credentials?: RfbCredentials;
    shared?: boolean;
    repeaterID?: string;
    wsProtocols?: string[];
  }

  export default class RFB extends EventTarget {
    constructor(target: Element, urlOrChannel: UrlOrChannel, options?: RfbOptions);
    connect(): void;
    disconnect(): void;
    focus(): void;
    blur(): void;
    sendCtrlAltDel(): void;
    clipboardPasteFrom(text: string): void;
    machineShutdown(): void;
    machineReboot(): void;
    machineReset(): void;
    focusOnClick: boolean;
    scaleViewport: boolean;
    resizeSession: boolean;
    viewOnly: boolean;
    qualityLevel: number;
    compressionLevel: number;
    addEventListener<K extends keyof RfbEventMap>(
      type: K,
      listener: (this: RFB, event: RfbEventMap[K]) => void,
      options?: boolean | AddEventListenerOptions
    ): void;
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions
    ): void;
  }
}
