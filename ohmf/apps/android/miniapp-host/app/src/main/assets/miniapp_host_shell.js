(function () {
  const params = new URLSearchParams(window.location.search);
  const frame = document.getElementById("miniapp-frame");
  const entrypoint = params.get("entrypoint");
  const appId = params.get("app_id");
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
  frame.src = entrypointUrl.toString();

  window.addEventListener("message", (event) => {
    if (event.source !== frame.contentWindow) return;
    const payload = typeof event.data === "object" ? event.data : null;
    if (!payload || payload.channel !== channel) return;
    const raw = window.MiniAppHostBridge.handleRequest(JSON.stringify(payload));
    if (!raw) return;
    frame.contentWindow.postMessage(JSON.parse(raw), "*");
  });
})();
