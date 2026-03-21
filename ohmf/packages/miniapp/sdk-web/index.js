export const BRIDGE_VERSION = "1.0";

export const KNOWN_CAPABILITIES = Object.freeze([
  "conversation.read_context",
  "conversation.send_message",
  "participants.read_basic",
  "storage.session",
  "storage.shared_conversation",
  "realtime.session",
  "media.pick_user",
  "notifications.in_app",
]);

function sanitizeText(value, limit = 240) {
  return String(value || "").replace(/[\u0000-\u001f\u007f]/g, "").trim().slice(0, limit);
}

function randomId(prefix) {
  if (globalThis.crypto && typeof globalThis.crypto.randomUUID === "function") {
    return `${prefix}_${globalThis.crypto.randomUUID().replace(/-/g, "")}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

export class OHMFMiniAppClient {
  constructor({ channel, targetOrigin, targetWindow = globalThis.parent }) {
    this.channel = channel;
    this.targetOrigin = targetOrigin;
    this.targetWindow = targetWindow;
    this.pending = new Map();
    this.listeners = new Map();
    this.boundOnMessage = (event) => this.onMessage(event);
    globalThis.addEventListener("message", this.boundOnMessage);
  }

  destroy() {
    globalThis.removeEventListener("message", this.boundOnMessage);
    for (const pending of this.pending.values()) {
      pending.reject(new Error("Bridge disposed"));
    }
    this.pending.clear();
    this.listeners.clear();
  }

  on(eventName, handler) {
    const listeners = this.listeners.get(eventName) || new Set();
    listeners.add(handler);
    this.listeners.set(eventName, listeners);
    return () => this.off(eventName, handler);
  }

  off(eventName, handler) {
    const listeners = this.listeners.get(eventName);
    if (!listeners) return;
    listeners.delete(handler);
    if (listeners.size === 0) {
      this.listeners.delete(eventName);
    }
  }

  emit(eventName, payload) {
    const listeners = this.listeners.get(eventName);
    if (!listeners) return;
    for (const listener of listeners) {
      listener(payload);
    }
  }

  onMessage(event) {
    if (event.source !== this.targetWindow) return;
    if (event.origin !== this.targetOrigin) return;
    const message = event.data;
    if (!message || typeof message !== "object" || message.channel !== this.channel) return;

    if (message.bridge_event) {
      this.emit(message.bridge_event, message.payload);
      return;
    }

    const pending = this.pending.get(message.request_id);
    if (!pending) return;
    this.pending.delete(message.request_id);
    if (message.ok) {
      pending.resolve(message.result);
      return;
    }
    const error = new Error(message.error?.message || "Bridge call failed");
    error.code = message.error?.code || "bridge_error";
    error.details = message.error?.details;
    pending.reject(error);
  }

  call(method, params = {}) {
    const requestId = randomId("req");
    return new Promise((resolve, reject) => {
      this.pending.set(requestId, { resolve, reject });
      this.targetWindow.postMessage(
        {
          bridge_version: BRIDGE_VERSION,
          channel: this.channel,
          request_id: requestId,
          method: sanitizeText(method, 120),
          params,
        },
        this.targetOrigin,
      );
    });
  }

  getLaunchContext() {
    return this.call("host.getLaunchContext");
  }

  readConversationContext() {
    return this.call("conversation.readContext");
  }

  sendConversationMessage(params) {
    return this.call("conversation.sendMessage", params);
  }

  readParticipants() {
    return this.call("participants.readBasic");
  }

  getSessionStorage(key) {
    return this.call("storage.session.get", { key });
  }

  setSessionStorage(key, value) {
    return this.call("storage.session.set", { key, value });
  }

  getSharedConversationStorage(key) {
    return this.call("storage.sharedConversation.get", { key });
  }

  setSharedConversationStorage(key, value) {
    return this.call("storage.sharedConversation.set", { key, value });
  }

  updateSessionState(snapshot) {
    return this.call("session.updateState", snapshot);
  }

  pickUserMedia(options = {}) {
    return this.call("media.pickUser", options);
  }

  showInAppNotification(notification) {
    return this.call("notifications.inApp.show", notification);
  }
}

export function createMiniAppClientFromLocation(search = globalThis.location?.search || "") {
  const params = new URLSearchParams(search);
  const channel = params.get("channel") || "";
  const targetOrigin = params.get("parent_origin") || "";
  if (!channel || !targetOrigin) {
    throw new Error("Missing runtime channel information.");
  }
  return new OHMFMiniAppClient({ channel, targetOrigin });
}
