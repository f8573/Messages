EightBall example mini-app

This folder contains a minimal `manifest.json` demonstrating the mini-app manifest format.

Serving
- A registry can serve this manifest at an HTTP endpoint such as `/miniapps/com.example.eightball/manifest.json`.
- Hosts should fetch the manifest, validate it against `packages/protocol/schemas/miniapp_manifest.schema.json`, prompt users for requested `permissions`, then load the `entrypoint.url` into a sandboxed webview.
