(function () {
  const script = document.currentScript;
  const assetVersion = encodeURIComponent(window.OHMF_RUNTIME_CONFIG?.asset_version || "dev");

  const cssPath = script?.dataset?.css;
  if (cssPath) {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = `${cssPath}?v=${assetVersion}`;
    document.head.appendChild(link);
  }

  const jsPath = script?.dataset?.js;
  if (jsPath) {
    const nextScript = document.createElement("script");
    nextScript.src = `${jsPath}?v=${assetVersion}`;
    if (document.body) document.body.appendChild(nextScript);
    else document.head.appendChild(nextScript);
  }
})();
