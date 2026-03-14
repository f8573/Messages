const assetVersion = encodeURIComponent(window.parent?.OHMF_RUNTIME_CONFIG?.asset_version || "dev");

await import(`./app.js?v=${assetVersion}`);
