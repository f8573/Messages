import { BRIDGE_VERSION } from "../sdk-web/index.js";

function cloneJson(value) {
  return value === undefined ? null : JSON.parse(JSON.stringify(value));
}

function randomId(prefix) {
  if (globalThis.crypto && typeof globalThis.crypto.randomUUID === "function") {
    return `${prefix}_${globalThis.crypto.randomUUID().replace(/-/g, "")}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

export function createMiniAppTestHarness({
  frameWindow,
  targetOrigin = globalThis.location?.origin || "http://localhost",
  channel = randomId("chan"),
  launchContext = {},
} = {}) {
  const transcript = [];
  const sessionStorage = {};
  const sharedConversationStorage = {};
  let stateVersion = Number(launchContext.state_version || 1) || 1;
  let stateSnapshot = cloneJson(launchContext.state_snapshot || {});

  function baseLaunchContext() {
    return {
      bridge_version: BRIDGE_VERSION,
      app_id: launchContext.app_id || "app.test.harness",
      app_version: launchContext.app_version || "0.0.0-dev",
      app_session_id: launchContext.app_session_id || randomId("aps"),
      conversation_id: launchContext.conversation_id || randomId("conv"),
      viewer: cloneJson(launchContext.viewer || { user_id: "usr_test", role: "PLAYER", display_name: "Harness User" }),
      participants: cloneJson(launchContext.participants || [{ user_id: "usr_test", role: "PLAYER", display_name: "Harness User" }]),
      capabilities_granted: cloneJson(launchContext.capabilities_granted || []),
      host_capabilities: cloneJson(launchContext.host_capabilities || []),
      state_snapshot: cloneJson(stateSnapshot),
      state_version: stateVersion,
      joinable: launchContext.joinable !== false,
    };
  }

  async function handle(method, params = {}) {
    switch (method) {
      case "host.getLaunchContext":
        return baseLaunchContext();
      case "conversation.readContext":
        return {
          conversation_id: baseLaunchContext().conversation_id,
          title: launchContext.title || "Harness Conversation",
          recent_messages: cloneJson(transcript),
        };
      case "participants.readBasic":
        return { participants: baseLaunchContext().participants };
      case "storage.session.get":
        return { key: params.key, value: cloneJson(sessionStorage[params.key]) };
      case "storage.session.set":
        sessionStorage[params.key] = cloneJson(params.value);
        stateVersion += 1;
        return { key: params.key, value: cloneJson(sessionStorage[params.key]), state_version: stateVersion };
      case "storage.sharedConversation.get":
        return { key: params.key, value: cloneJson(sharedConversationStorage[params.key]) };
      case "storage.sharedConversation.set":
        sharedConversationStorage[params.key] = cloneJson(params.value);
        stateVersion += 1;
        return { key: params.key, value: cloneJson(sharedConversationStorage[params.key]), state_version: stateVersion };
      case "session.updateState":
        stateSnapshot = { ...stateSnapshot, ...cloneJson(params || {}) };
        stateVersion += 1;
        return { state_snapshot: cloneJson(stateSnapshot), state_version: stateVersion };
      case "conversation.sendMessage":
        transcript.push({ id: randomId("msg"), author: "Mini-App", text: params.text || "Harness message", createdAt: new Date().toISOString() });
        stateVersion += 1;
        return { message_id: randomId("msg"), state_version: stateVersion };
      case "notifications.inApp.show":
        return { displayed: true };
      default:
        throw Object.assign(new Error(`Unknown bridge method: ${method}`), { code: "method_not_found" });
    }
  }

  async function onMessage(event) {
    if (frameWindow && event.source !== frameWindow) return;
    if (event.origin !== targetOrigin) return;
    const message = event.data;
    if (!message || typeof message !== "object" || message.channel !== channel || !message.request_id) return;
    try {
      const result = await handle(message.method, message.params);
      event.source.postMessage({ bridge_version: BRIDGE_VERSION, channel, request_id: message.request_id, ok: true, result }, event.origin);
    } catch (error) {
      event.source.postMessage({
        bridge_version: BRIDGE_VERSION,
        channel,
        request_id: message.request_id,
        ok: false,
        error: {
          code: error.code || "bridge_error",
          message: error.message || "Bridge call failed",
        },
      }, event.origin);
    }
  }

  globalThis.addEventListener("message", onMessage);

  return {
    channel,
    targetOrigin,
    getLaunchContext: baseLaunchContext,
    destroy() {
      globalThis.removeEventListener("message", onMessage);
    },
    dispatchEvent(name, payload) {
      if (!frameWindow) return;
      frameWindow.postMessage({ bridge_version: BRIDGE_VERSION, channel, bridge_event: name, payload: cloneJson(payload) }, targetOrigin);
    },
  };
}
