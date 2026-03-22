(function () {
  const params = new URLSearchParams(window.location.search);
  const frame = document.getElementById("miniapp-frame");
  const entrypoint = params.get("entrypoint");
  const appId = params.get("app_id");
  const appOrigin = params.get("app_origin") || null; // P3.2: Isolated runtime origin
  const channel = params.get("channel");
  const parentOrigin = params.get("parent_origin") || "app://ohmf-miniapp-host";

  if (!entrypoint || !channel || !appId) {
    document.body.textContent = "Mini-app host is missing launch parameters.";
    return;
  }

  const entrypointUrl = new URL(entrypoint);
  entrypointUrl.searchParams.set("channel", channel);
  entrypointUrl.searchParams.set("parent_origin", parentOrigin);
  entrypointUrl.searchParams.set("app_id", appId);
  if (appOrigin) {
    entrypointUrl.searchParams.set("app_origin", appOrigin);
  }
  frame.src = entrypointUrl.toString();

  // P3.2: Validate origin if app_origin is provided
  // P4.3: Handle SESSION_EVENT messages from parent WebSocket v2 connection
  window.addEventListener("message", (event) => {
    if (event.source !== frame.contentWindow) return;

    // Origin validation for isolated runtime
    if (appOrigin) {
      const expectedOriginUrl = new URL(`http://${appOrigin}`);
      const messageOrigin = new URL(event.origin || "");
      if (messageOrigin.host && messageOrigin.host !== expectedOriginUrl.host) {
        // Reject message from wrong origin
        return;
      }
    }

    const payload = typeof event.data === "object" ? event.data : null;
    if (!payload || payload.channel !== channel) return;

    // P4.3: Pass SESSION_EVENT messages directly to miniapp
    if (payload.type === "SESSION_EVENT") {
      frame.contentWindow.postMessage(payload, "*");
      return;
    }

    const raw = window.MiniAppHostBridge.handleRequest(JSON.stringify(payload));
    if (!raw) return;
    frame.contentWindow.postMessage(JSON.parse(raw), "*");
  });
})();
