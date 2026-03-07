# OHMF Architecture (MVP Foundation)

This repository follows the OHMF spec-first monorepo layout.

MVP implementation is a modular monolith deployed as `services/gateway` with internal boundaries:
- auth
- devices
- conversations
- messages

Future extraction targets are preserved under `services/*` placeholders.
