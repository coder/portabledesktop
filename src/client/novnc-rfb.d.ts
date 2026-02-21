declare module "@novnc/novnc/lib/rfb.js" {
  export type UrlOrChannel = string | WebSocket | RTCDataChannel;

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

  export default class RFB {
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
    scaleViewport: boolean;
    resizeSession: boolean;
    viewOnly: boolean;
    qualityLevel: number;
    compressionLevel: number;
  }
}
