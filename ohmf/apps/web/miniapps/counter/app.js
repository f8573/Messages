import { createMiniAppClientFromLocation } from "../../miniapp-sdk.js";

const runtimeParams = new URLSearchParams(window.location.search);
const isPreviewMode = runtimeParams.get("preview") === "1";
const hasBridgeContext = Boolean(runtimeParams.get("channel") && runtimeParams.get("parent_origin"));

const state = {
  counter: 0,
  launchContext: null,
  recentMessages: [],
  logEntries: [],
  previewCounter: 3,
};

const el = {
  appShell: document.getElementById("app-shell"),
  previewShell: document.getElementById("preview-shell"),
  previewCounterValue: document.getElementById("preview-counter-value"),
  previewCaption: document.getElementById("preview-caption"),
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

const bridge = hasBridgeContext ? createMiniAppClientFromLocation() : null;
let previewTicker = 0;

function sanitizeText(value, limit = 240) {
  return String(value || "").replace(/[\u0000-\u001f\u007f]/g, "").trim().slice(0, limit);
}

function randomId(prefix) {
  if (window.crypto && typeof window.crypto.randomUUID === "function") {
    return `${prefix}_${window.crypto.randomUUID().replace(/-/g, "")}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

function setStatus(message) {
  el.status.textContent = sanitizeText(message, 180);
  el.status.classList.remove("error");
}

function setErrorStatus(message) {
  el.status.textContent = sanitizeText(message, 180);
  el.status.classList.add("error");
} // removed: boolean status flag split into named helpers

function requireBridge() {
  if (bridge) return bridge;
  throw new Error(isPreviewMode ? "Preview mode does not have host bridge context." : "Missing runtime channel information.");
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
  renderPreview();
}

function renderPreview() {
  const previewValue = state.launchContext ? state.counter : state.previewCounter;
  el.previewCounterValue.textContent = String(previewValue);

  const participants = Array.isArray(state.launchContext?.participants) ? state.launchContext.participants : [];
  if (participants.length) {
    el.previewCaption.textContent = `${participants.length} participant${participants.length === 1 ? "" : "s"} in the shared session.`;
    return;
  }

  el.previewCaption.textContent = "Live square preview for shared app cards.";
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

if (bridge) {
  bridge.on("session.stateUpdated", (payload) => {
    addLog("event session.stateUpdated", payload);
    if (payload?.state_snapshot) {
      state.counter = Number(payload.state_snapshot.counter) || 0;
      renderCounter();
      setStatus(`Shared counter updated to ${state.counter}.`);
    }
  });

  bridge.on("session.permissionsUpdated", (payload) => {
    addLog("event session.permissionsUpdated", payload);
    if (!state.launchContext) state.launchContext = {};
    state.launchContext.capabilities_granted = Array.isArray(payload?.capabilities_granted) ? payload.capabilities_granted : [];
    renderContext();
    setStatus("Host permission grants changed.");
  });
}

async function refreshLaunchContext() {
  const launchContext = await requireBridge().getLaunchContext();
  state.launchContext = launchContext;
  state.counter = Number(launchContext?.state_snapshot?.counter) || 0;
  renderCounter();
  renderContext();
  addLog("host.getLaunchContext", launchContext);
}

async function refreshThreadContext() {
  const context = await requireBridge().readConversationContext();
  state.recentMessages = Array.isArray(context?.recent_messages) ? context.recent_messages : [];
  renderRecentMessages();
  addLog("conversation.readContext", context);
}

async function loadNote() {
  const result = await requireBridge().getSessionStorage("session_note");
  el.noteInput.value = typeof result?.value === "string" ? result.value : "";
  addLog("storage.session.get", result);
  setStatus("Loaded session note.");
}

async function saveNote() {
  const value = sanitizeText(el.noteInput.value, 240);
  const result = await requireBridge().setSessionStorage("session_note", value);
  addLog("storage.session.set", result);
  setStatus("Saved session note.");
}

async function updateCounter(nextValue) {
  const result = await requireBridge().updateSessionState({ counter: nextValue });
  state.counter = Number(result?.state_snapshot?.counter) || 0;
  renderCounter();
  addLog("session.updateState", result);
  setStatus(`Counter updated to ${state.counter}.`);
}

async function sendSummary() {
  const result = await requireBridge().sendConversationMessage({
    content_type: "app_event",
    content: {
      event_name: "COUNTER_SUMMARY",
      body: { counter: state.counter },
    },
    text: `Counter Lab summary: shared counter is ${state.counter}.`,
  });
  addLog("conversation.sendMessage", result);
  setStatus("Projected summary into host transcript.");
  await refreshThreadContext();
}

async function bootstrapPreview() {
  document.body.classList.add("preview-mode");
  el.appShell.hidden = true;
  el.previewShell.hidden = false;
  renderPreview();
  window.clearInterval(previewTicker);
  previewTicker = window.setInterval(() => {
    if (state.launchContext) return;
    state.previewCounter = (state.previewCounter + 1) % 10;
    renderPreview();
  }, 1600);
  if (!bridge) return;

  try {
    await refreshLaunchContext();
    setStatus("Preview bridge ready.");
  } catch (error) {
    addLog(`preview bootstrap error ${error.code || "error"}`, { message: error.message, details: error.details });
  }
} // removed: single-use preview helpers inlined into bootstrap

async function bootstrap() {
  if (isPreviewMode) {
    await bootstrapPreview();
    return;
  }

  try {
    await refreshLaunchContext();
    await refreshThreadContext();
    await loadNote();
    setStatus("Bridge ready.");
  } catch (error) {
    addLog(`bootstrap error ${error.code || "error"}`, { message: error.message, details: error.details });
    setErrorStatus(error.message || "Failed to initialize app.");
  }
}

el.decrementBtn.addEventListener("click", async () => {
  try {
    await updateCounter(state.counter - 1);
  } catch (error) {
    setErrorStatus(error.message || "Unable to decrement counter.");
  }
});

el.incrementBtn.addEventListener("click", async () => {
  try {
    await updateCounter(state.counter + 1);
  } catch (error) {
    setErrorStatus(error.message || "Unable to increment counter.");
  }
});

el.resetBtn.addEventListener("click", async () => {
  try {
    await updateCounter(0);
  } catch (error) {
    setErrorStatus(error.message || "Unable to reset counter.");
  }
});

el.loadNoteBtn.addEventListener("click", async () => {
  try {
    await loadNote();
  } catch (error) {
    setErrorStatus(error.message || "Unable to load note.");
  }
});

el.saveNoteBtn.addEventListener("click", async () => {
  try {
    await saveNote();
  } catch (error) {
    setErrorStatus(error.message || "Unable to save note.");
  }
});

el.refreshContextBtn.addEventListener("click", async () => {
  try {
    await refreshLaunchContext();
    await refreshThreadContext();
    setStatus("Context refreshed.");
  } catch (error) {
    setErrorStatus(error.message || "Unable to refresh context.");
  }
});

el.sendSummaryBtn.addEventListener("click", async () => {
  try {
    await sendSummary();
  } catch (error) {
    setErrorStatus(error.message || "Unable to send summary.");
  }
});

bootstrap();
