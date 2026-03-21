EightBall example mini-app

This folder contains the portable manifest example for the open-source 8-ball demo.

Runtime example
- A runnable browser version lives at `apps/web/miniapps/eightball`.
- The OHMF web host publishes that manifest into the local app catalog during dev startup so other users on the same stack can install and launch it by `app_id`.

Publishing model
- Registries should publish by immutable `app_id` and `version`, not by user-supplied arbitrary URLs.
- Hosts should fetch the manifest from the catalog, validate it against `packages/protocol/schemas/miniapp_manifest.schema.json`, prompt users for requested `permissions`, and then load the `entrypoint.url` into the runtime container.
