(() => {
  const runtimeConfig = window.OHMF_RUNTIME_CONFIG || {};
  const DEFAULT_FRONTEND_PORT = String(runtimeConfig.frontend_port || "5173");
  const DEFAULT_API_HOST_PORT = String(runtimeConfig.api_host_port || "18080");

  const storedFrontendPort = window.localStorage.getItem("ohmf.frontend_port") || DEFAULT_FRONTEND_PORT;
  const storedAPIHostPort = window.localStorage.getItem("ohmf.api_host_port") || DEFAULT_API_HOST_PORT;
  const storedAPIBaseURL = window.localStorage.getItem("ohmf.apiBaseUrl") || runtimeConfig.api_base_url || `http://localhost:${DEFAULT_API_HOST_PORT}`;

  function normalizeAPIBaseURL(value) {
    const fallback = `http://localhost:${storedAPIHostPort || DEFAULT_API_HOST_PORT}`;
    if (!value) return fallback;
    try {
      const url = new URL(value);
      const localHosts = new Set(["localhost", "127.0.0.1"]);
      if (localHosts.has(url.hostname) && (url.port === "18080" || url.port === "8080")) {
        url.port = storedAPIHostPort || DEFAULT_API_HOST_PORT;
        const normalized = url.toString().replace(/\/+$/, "");
        window.localStorage.setItem("ohmf.apiBaseUrl", normalized);
        return normalized;
      }
      return url.toString().replace(/\/+$/, "");
    } catch {
      return fallback;
    }
  }

  window.OHMF_WEB_CONFIG = Object.freeze({
    frontend_port: storedFrontendPort,
    api_host_port: storedAPIHostPort,
    api_base_url: normalizeAPIBaseURL(storedAPIBaseURL),
  });
})();
