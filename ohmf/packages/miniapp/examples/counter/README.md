Counter Lab example mini-app

This example adds a concrete, runnable test app for the web mini-app runtime.

Files
- `manifest.json`: canonical sample manifest for the app.
- `../../../apps/web/miniapps/counter/`: static assets served by the web demo runtime.

Behavior covered
- launch context bootstrap
- bounded conversation read access
- projected message send through the host
- session-scoped key/value storage
- session state updates with host-pushed `session.stateUpdated` events
- square live preview mode at `index.html?preview=1` for app-card/message previews without requiring bridge parameters

Local run
- Serve `ohmf/apps/web` with a static server on `http://localhost:5174`.
- Open `http://localhost:5174/miniapp-runtime.html`.
