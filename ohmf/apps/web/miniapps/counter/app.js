"use strict";

const params = new URLSearchParams(window.location.search);
const channelId = params.get("channel") || "";
const parentOrigin = params.get("parent_origin") || "";

const state = {
  counter: 0,
  launchContext: null,
  recentMessages: [],
  logEntries: [],
};

const el = {
  status: document.getElementById("status-pill"),
  counterValue: document.getElementById("counter-value"),
  noteInput: document.getElementById("note-input"),
  viewerLine: document.getElementById("viewer-line"),
  participantsLine: document.getElementById("participants-line"),
  permissionsLine: document.getElementById("permissions-line"),
  recentMessages: document.getElementById("recent-messages"),
  appLog: document.getElementById("app-log"),
  decrementBtn: document.getElementById("decrement-btn"),
  incrementBtn: document.getElementById("increment-btn"),
  resetBtn: document.getElementById("reset-btn"),
  loadNoteBtn: document.getElementById("load-note-btn"),
  saveNoteBtn: document.getElementById("save-note-btn"),
  refreshContextBtn: document.getElementById("refresh-context-btn"),
  sendSummaryBtn: document.getElementById("send-summary-btn"),
};

function sanitizeText(value, limit = 240) {
  return String(value || "").replace(/[\u0000-\u001f\u007f]/g, "").trim().slice(0, limit);
}

function randomId(prefix) {
  if (window.crypto && typeof window.crypto.randomUUID === "function") {
    return `${prefix}_${window.crypto.randomUUID().replace(/-/g, "")}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

function setStatus(message, isError = false) {
  el.status.textContent = sanitizeText(message, 180);
  el.status.classList.toggle("error", Boolean(isError));
}

function addLog(summary, detail) {
  state.logEntries.unshift({
    id: randomId("log"),
    summary: sanitizeText(summary, 160),
    detail: detail === undefined ? "" : JSON.stringify(detail, null, 2),
    time: new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" }),
  });
  state.logEntries = state.logEntries.slice(0, 30);
  renderLog();
}

function renderLog() {
  el.appLog.replaceChildren();
  if (!state.logEntries.length) {
    const item = document.createElement("li");
    item.className = "log-item";
    item.textContent = "No app events yet.";
    el.appLog.append(item);
    return;
  }

  state.logEntries.forEach((entry) => {
    const item = document.createElement("li");
    item.className = "log-item";
    const header = document.createElement("header");
    const title = document.createElement("strong");
    title.textContent = entry.summary;
    const time = document.createElement("span");
    time.textContent = entry.time;
    header.append(title, time);

    item.append(header);
    if (entry.detail) {
      const pre = document.createElement("pre");
      pre.textContent = entry.detail;
      item.append(pre);
    }
    el.appLog.append(item);
  });
}

function renderCounter() {
  el.counterValue.textContent = String(state.counter);
}

function renderContext() {
  const viewer = state.launchContext?.viewer;
  const participants = Array.isArray(state.launchContext?.participants) ? state.launchContext.participants : [];
  const permissions = Array.isArray(state.launchContext?.capabilities_granted) ? state.launchContext.capabilities_granted : [];

  el.viewerLine.textContent = viewer ? `Viewer: ${viewer.display_name || viewer.user_id} (${viewer.role})` : "Viewer: unavailable";
  el.participantsLine.textContent = participants.length
    ? `Participants: ${participants.map((participant) => participant.display_name || participant.user_id).join(", ")}`
    : "Participants: unavailable";
  el.permissionsLine.textContent = permissions.length ? `Permissions: ${permissions.join(", ")}` : "Permissions: none granted";
}

function renderRecentMessages() {
  el.recentMessages.replaceChildren();
  if (!state.recentMessages.length) {
    const item = document.createElement("li");
    item.className = "message-item";
    item.textContent = "No recent thread messages available.";
    el.recentMessages.append(item);
    return;
  }

  state.recentMessages.forEach((message) => {
    const item = document.createElement("li");
    item.className = "message-item";
    const header = document.createElement("header");
    const author = document.createElement("strong");
    author.textContent = sanitizeText(message.author, 60) || "Unknown";
    const time = document.createElement("span");
    const d = new Date(message.createdAt);
    time.textContent = Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    header.append(author, time);
    const body = document.createElement("div");
    body.textContent = sanitizeText(message.text, 240);
    item.append(header, body);
    el.recentMessages.append(item);
  });
}

class HostBridge {
  constructor(channel, targetOrigin) {
    this.channel = channel;
    this.targetOrigin = targetOrigin;
    this.pending = new Map();
    window.addEventListener("message", (event) => this.onMessage(event));
  }

  onMessage(event) {
    if (event.source !== window.parent) return;
    if (event.origin !== this.targetOrigin) return;
    const message = event.data;
    if (!message || typeof message !== "object") return;
    if (message.channel !== this.channel) return;

    if (message.bridge_event) {
      this.onBridgeEvent?.(message.bridge_event, message.payload);
      return;
    }

    const pending = this.pending.get(message.request_id);
    if (!pending) return;
    this.pending.delete(message.request_id);
    if (message.ok) {
      pending.resolve(message.result);
    } else {
      const err = new Error(message.error?.message || "Bridge call failed");
      err.code = message.error?.code || "bridge_error";
      err.details = message.error?.details;
      pending.reject(err);
    }
  }

  call(method, params = {}) {
    const requestId = randomId("req");
    return new Promise((resolve, reject) => {
      this.pending.set(requestId, { resolve, reject });
      window.parent.postMessage(
        {
          bridge_version: "1.0",
          channel: this.channel,
          request_id: requestId,
          method,
          params,
        },
        this.targetOrigin
      );
    });
  }
}

const bridge = new HostBridge(channelId, parentOrigin);

bridge.onBridgeEvent = (name, payload) => {
  addLog(`event ${name}`, payload);
  if (name === "session.stateUpdated" && payload?.state_snapshot) {
    state.counter = Number(payload.state_snapshot.counter) || 0;
    renderCounter();
    setStatus(`Shared counter updated to ${state.counter}.`);
  }
  if (name === "session.permissionsUpdated") {
    if (!state.launchContext) state.launchContext = {};
    state.launchContext.capabilities_granted = Array.isArray(payload?.capabilities_granted) ? payload.capabilities_granted : [];
    renderContext();
    setStatus("Host permission grants changed.");
  }
};

async function refreshLaunchContext() {
  const launchContext = await bridge.call("host.getLaunchContext");
  state.launchContext = launchContext;
  state.counter = Number(launchContext?.state_snapshot?.counter) || 0;
  renderCounter();
  renderContext();
  addLog("host.getLaunchContext", launchContext);
}

async function refreshThreadContext() {
  const context = await bridge.call("conversation.readContext");
  state.recentMessages = Array.isArray(context?.recent_messages) ? context.recent_messages : [];
  renderRecentMessages();
  addLog("conversation.readContext", context);
}

async function loadNote() {
  const result = await bridge.call("storage.session.get", { key: "session_note" });
  el.noteInput.value = typeof result?.value === "string" ? result.value : "";
  addLog("storage.session.get", result);
  setStatus("Loaded session note.");
}

async function saveNote() {
  const value = sanitizeText(el.noteInput.value, 240);
  const result = await bridge.call("storage.session.set", { key: "session_note", value });
  addLog("storage.session.set", result);
  setStatus("Saved session note.");
}

async function updateCounter(nextValue) {
  const result = await bridge.call("session.updateState", { counter: nextValue });
  state.counter = Number(result?.state_snapshot?.counter) || 0;
  renderCounter();
  addLog("session.updateState", result);
  setStatus(`Counter updated to ${state.counter}.`);
}

async function sendSummary() {
  const text = `Counter Lab summary: shared counter is ${state.counter}.`;
  const result = await bridge.call("conversation.sendMessage", { text });
  addLog("conversation.sendMessage", result);
  setStatus("Projected summary into host transcript.");
  await refreshThreadContext();
}

async function bootstrap() {
  if (!channelId || !parentOrigin) {
    setStatus("Missing runtime channel information.", true);
    return;
  }

  try {
    await refreshLaunchContext();
    await refreshThreadContext();
    await loadNote();
    setStatus("Bridge ready.");
  } catch (error) {
    addLog(`bootstrap error ${error.code || "error"}`, { message: error.message, details: error.details });
    setStatus(error.message || "Failed to initialize app.", true);
  }
}

el.decrementBtn.addEventListener("click", async () => {
  try {
    await updateCounter(state.counter - 1);
  } catch (error) {
    setStatus(error.message || "Unable to decrement counter.", true);
  }
});

el.incrementBtn.addEventListener("click", async () => {
  try {
    await updateCounter(state.counter + 1);
  } catch (error) {
    setStatus(error.message || "Unable to increment counter.", true);
  }
});

el.resetBtn.addEventListener("click", async () => {
  try {
    await updateCounter(0);
  } catch (error) {
    setStatus(error.message || "Unable to reset counter.", true);
  }
});

el.loadNoteBtn.addEventListener("click", async () => {
  try {
    await loadNote();
  } catch (error) {
    setStatus(error.message || "Unable to load note.", true);
  }
});

el.saveNoteBtn.addEventListener("click", async () => {
  try {
    await saveNote();
  } catch (error) {
    setStatus(error.message || "Unable to save note.", true);
  }
});

el.refreshContextBtn.addEventListener("click", async () => {
  try {
    await refreshLaunchContext();
    await refreshThreadContext();
    setStatus("Context refreshed.");
  } catch (error) {
    setStatus(error.message || "Unable to refresh context.", true);
  }
});

el.sendSummaryBtn.addEventListener("click", async () => {
  try {
    await sendSummary();
  } catch (error) {
    setStatus(error.message || "Unable to send summary.", true);
  }
});

renderCounter();
renderContext();
renderRecentMessages();
renderLog();
bootstrap();
