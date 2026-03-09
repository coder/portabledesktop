import { createClient } from "./client";

type ViewerScale = "fit" | "1:1";
type DesktopSizeMode = "fixed" | "dynamic";

interface ViewerConfig {
  scale: ViewerScale;
  desktopSizeMode: DesktopSizeMode;
}
const statusNode = document.getElementById("topbar");
const viewerNode = document.getElementById("viewer");

if (!(statusNode instanceof HTMLElement) || !(viewerNode instanceof HTMLElement)) {
  throw new Error("viewer DOM nodes are missing");
}

const protocol = location.protocol === "https:" ? "wss" : "ws";
const wsUrl = `${protocol}://${location.host}/ws`;
const rfb = createClient(viewerNode, { url: wsUrl });

const configSource =
  (globalThis as unknown as { PORTABLEDESKTOP_VIEWER_CONFIG?: Partial<ViewerConfig> })
    .PORTABLEDESKTOP_VIEWER_CONFIG || {};
const viewerConfig: ViewerConfig = {
  scale: configSource.scale === "1:1" ? "1:1" : "fit",
  desktopSizeMode: configSource.desktopSizeMode === "dynamic" ? "dynamic" : "fixed"
};

rfb.scaleViewport = viewerConfig.scale === "fit";
rfb.resizeSession = viewerConfig.desktopSizeMode === "dynamic";

rfb.addEventListener("connect", () => {
  statusNode.textContent = `connected: ${wsUrl} | scale=${viewerConfig.scale} | sizeMode=${viewerConfig.desktopSizeMode}`;
});

rfb.addEventListener("disconnect", (disconnectEvent) => {
  statusNode.textContent = disconnectEvent.detail?.clean === false ? "disconnected (unclean)" : "disconnected";
});

rfb.addEventListener("credentialsrequired", () => {
  statusNode.textContent = "credentials required";
});

rfb.addEventListener("securityfailure", () => {
  statusNode.textContent = "security failure";
});
