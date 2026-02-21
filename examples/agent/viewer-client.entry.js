import RfbModule from "@novnc/novnc/lib/rfb.js";

function resolveRfbConstructor(moduleValue) {
  if (typeof moduleValue === "function") {
    return moduleValue;
  }

  if (moduleValue && typeof moduleValue.default === "function") {
    return moduleValue.default;
  }

  if (moduleValue && moduleValue.default && typeof moduleValue.default.default === "function") {
    return moduleValue.default.default;
  }

  throw new Error("unable to resolve noVNC RFB constructor");
}

const RFB = resolveRfbConstructor(RfbModule);

const statusNode = document.getElementById("topbar");
const viewerNode = document.getElementById("viewer");

if (!(statusNode instanceof HTMLElement) || !(viewerNode instanceof HTMLElement)) {
  throw new Error("viewer DOM nodes are missing");
}

const protocol = location.protocol === "https:" ? "wss" : "ws";
const wsUrl = `${protocol}://${location.host}/ws`;

const rfb = new RFB(viewerNode, wsUrl, { shared: true });
rfb.scaleViewport = true;
rfb.resizeSession = true;
rfb.focusOnClick = true;

rfb.addEventListener("connect", () => {
  statusNode.textContent = `connected: ${wsUrl}`;
});

rfb.addEventListener("disconnect", (event) => {
  statusNode.textContent =
    event.detail && event.detail.clean === false ? "disconnected (unclean)" : "disconnected";
});

rfb.addEventListener("credentialsrequired", () => {
  statusNode.textContent = "credentials required";
});

rfb.addEventListener("securityfailure", () => {
  statusNode.textContent = "security failure";
});
