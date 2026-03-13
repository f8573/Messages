"use strict";

const DEFAULT_MANIFEST_URL = "./miniapps/counter/manifest.json";
const STORAGE_PREFIX = "ohmf.miniapp.runtime.v1";
const PERMISSION_DESCRIPTIONS = Object.freeze({
  "conversation.read_context": "Lets the app inspect a small recent window from the active conversation.",
  "conversation.send_message": "Lets the app project app-generated messages into the conversation transcript.",
  "storage.session": "Lets the app persist session-scoped key/value state in the host.",
  "realtime.session": "Lets the app update session state and receive state change events.",
});

const state = {
  manifest: null,
  grantedPermissions: new Set(),
  frameWindow: null,
  channelId: "",
  launchContext: null,
  sessionState: null,
  logEntries: [],
};

const el = {
  status: document.getElementById("runtime-status"),
  manifestForm: document.getElementById("manifest-form"),
  manifestUrl: document.getElementById("manifest-url"),
  relaunchBtn: document.getElementById("relaunch-btn"),
  clearSessionBtn: document.getElementById("clear-session-btn"),
  permissionsList: document.getElementById("permissions-list"),
  contextJson: document.getElementById("context-json"),
  manifestJson: document.getElementById("manifest-json"),
  transcriptList: document.getElementById("transcript-list"),
  logList: document.getElementById("log-list"),
  frame: document.getElementById("app-frame"),
};

function nowTime() {
  return new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" });
}

function sanitizeText(value, limit = 240) {
  return String(value || "").replace(/[\u0000-\u001f\u007f]/g, "").trim().slice(0, limit);
}

function randomId(prefix) {
  if (window.crypto && typeof window.crypto.randomUUID === "function") {
    return `${prefix}_${window.crypto.randomUUID().replace(/-/g, "")}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

function storageKey(appId) {
  return `${STORAGE_PREFIX}.${appId}`;
}

function defaultTranscript() {
  return [
    {
      id: "msg_seed_1",
      author: "Avery",
      text: "Shared lists work better if the app can project summaries back into the thread.",
      createdAt: new Date(Date.now() - 1000 * 60 * 13).toISOString(),
    },
    {
      id: "msg_seed_2",
      author: "Jordan",
      text: "Use the counter app to test state sync, storage, and message projection.",
      createdAt: new Date(Date.now() - 1000 * 60 * 9).toISOString(),
    },
  ];
}

function loadSavedSession(appId) {
  const raw = window.localStorage.getItem(storageKey(appId));
  if (!raw) {
    return {
      stateVersion: 1,
      stateSnapshot: { counter: 0, updated_by: "host" },
      storage: {},
      transcript: defaultTranscript(),
    };
  }
  try {
    const parsed = JSON.parse(raw);
    return {
      stateVersion: Number(parsed.stateVersion) > 0 ? Number(parsed.stateVersion) : 1,
      stateSnapshot: parsed.stateSnapshot && typeof parsed.stateSnapshot === "object" ? parsed.stateSnapshot : { counter: 0, updated_by: "host" },
      storage: parsed.storage && typeof parsed.storage === "object" ? parsed.storage : {},
      transcript: Array.isArray(parsed.transcript) ? parsed.transcript : defaultTranscript(),
    };
  } catch {
    return {
      stateVersion: 1,
      stateSnapshot: { counter: 0, updated_by: "host" },
      storage: {},
      transcript: defaultTranscript(),
    };
  }
}

function saveSession() {
  if (!state.manifest?.app_id || !state.sessionState) return;
  window.localStorage.setItem(storageKey(state.manifest.app_id), JSON.stringify(state.sessionState));
}

function clearSavedSession() {
  if (!state.manifest?.app_id) return;
  window.localStorage.removeItem(storageKey(state.manifest.app_id));
}

function setStatus(message, isError = false) {
  el.status.textContent = sanitizeText(message, 280) || "Ready.";
  el.status.classList.toggle("error", Boolean(isError));
}

function addLog(kind, summary, detail) {
  state.logEntries.unshift({
    id: randomId("log"),
    kind,
    summary: sanitizeText(summary, 280),
    detail: detail === undefined ? "" : JSON.stringify(detail, null, 2),
    time: nowTime(),
  });
  state.logEntries = state.logEntries.slice(0, 60);
  renderLog();
}

function renderLog() {
  el.logList.replaceChildren();
  if (!state.logEntries.length) {
    const li = document.createElement("li");
    li.className = "log-item";
    li.textContent = "No bridge traffic yet.";
    el.logList.append(li);
    return;
  }

  state.logEntries.forEach((entry) => {
    const item = document.createElement("li");
    item.className = `log-item ${entry.kind}`;

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
    el.logList.append(item);
  });
}

function renderTranscript() {
  el.transcriptList.replaceChildren();
  const transcript = state.sessionState?.transcript || [];
  if (!transcript.length) {
    const li = document.createElement("li");
    li.className = "transcript-item";
    li.textContent = "No projected messages yet.";
    el.transcriptList.append(li);
    return;
  }

  transcript.forEach((message) => {
    const item = document.createElement("li");
    item.className = "transcript-item";

    const header = document.createElement("header");
    const author = document.createElement("strong");
    author.textContent = sanitizeText(message.author, 60) || "System";
    const time = document.createElement("span");
    const d = new Date(message.createdAt);
    time.textContent = Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    header.append(author, time);

    const body = document.createElement("div");
    body.textContent = sanitizeText(message.text, 280) || "(empty)";

    item.append(header, body);
    el.transcriptList.append(item);
  });
}

function renderManifest() {
  el.manifestJson.textContent = state.manifest ? JSON.stringify(state.manifest, null, 2) : "";
}

function renderContext() {
  el.contextJson.textContent = state.launchContext ? JSON.stringify(state.launchContext, null, 2) : "";
}

function renderPermissions() {
  el.permissionsList.replaceChildren();
  const permissions = Array.isArray(state.manifest?.permissions) ? state.manifest.permissions : [];
  if (!permissions.length) {
    const empty = document.createElement("p");
    empty.className = "empty-note";
    empty.textContent = "No permissions declared.";
    el.permissionsList.append(empty);
    return;
  }

  permissions.forEach((permission) => {
    const item = document.createElement("label");
    item.className = "permission-item";

    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = state.grantedPermissions.has(permission);
    input.addEventListener("change", () => {
      if (input.checked) {
        state.grantedPermissions.add(permission);
      } else {
        state.grantedPermissions.delete(permission);
      }
      syncLaunchContextPermissions();
      pushBridgeEvent("session.permissionsUpdated", {
        app_session_id: state.launchContext?.app_session_id,
        capabilities_granted: currentGrantedPermissions(),
      });
      setStatus(`Updated permission grants for ${state.manifest?.name || "app"}.`);
      renderContext();
    });

    const copy = document.createElement("div");
    copy.className = "permission-copy";
    const title = document.createElement("strong");
    title.textContent = permission;
    const description = document.createElement("span");
    description.textContent = PERMISSION_DESCRIPTIONS[permission] || "Custom permission declared by the manifest.";
    copy.append(title, description);

    item.append(input, copy);
    el.permissionsList.append(item);
  });
}

function currentGrantedPermissions() {
  return Array.from(state.grantedPermissions).sort();
}

function syncLaunchContextPermissions() {
  if (!state.launchContext) return;
  state.launchContext.capabilities_granted = currentGrantedPermissions();
}

function rewriteLocalDevEntrypoint(rawUrl) {
  const url = new URL(rawUrl, window.location.href);
  const localOrigins = new Set(["http://localhost:5173", "http://127.0.0.1:5173"]);
  if (localOrigins.has(url.origin) && url.origin !== window.location.origin) {
    url.protocol = window.location.protocol;
    url.host = window.location.host;
  }
  return url.toString();
}

function validateManifest(manifest) {
  if (!manifest || typeof manifest !== "object") {
    throw new Error("Manifest must be a JSON object.");
  }
  if (!sanitizeText(manifest.app_id, 120)) throw new Error("Manifest is missing app_id.");
  if (!sanitizeText(manifest.name, 120)) throw new Error("Manifest is missing name.");
  if (!sanitizeText(manifest.version, 40)) throw new Error("Manifest is missing version.");
  if (!manifest.entrypoint || typeof manifest.entrypoint !== "object") throw new Error("Manifest entrypoint is required.");
  if (!sanitizeText(manifest.entrypoint.url, 400)) throw new Error("Manifest entrypoint.url is required.");
  if (!Array.isArray(manifest.permissions)) throw new Error("Manifest permissions must be an array.");
  if (!manifest.capabilities || typeof manifest.capabilities !== "object") throw new Error("Manifest capabilities must be an object.");
  if (!manifest.signature || typeof manifest.signature !== "object") throw new Error("Manifest signature is required.");
}

async function fetchManifest(url) {
  const response = await fetch(url, { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`Manifest request failed with ${response.status}.`);
  }
  const manifest = await response.json();
  validateManifest(manifest);
  manifest.entrypoint.url = rewriteLocalDevEntrypoint(manifest.entrypoint.url);
  return manifest;
}

function buildLaunchContext() {
  const saved = loadSavedSession(state.manifest.app_id);
  state.sessionState = saved;
  state.launchContext = {
    app_id: state.manifest.app_id,
    app_session_id: randomId("aps"),
    conversation_id: "cnv_demo_runtime",
    viewer: { user_id: "usr_demo_1", role: "PLAYER", display_name: "Avery" },
    participants: [
      { user_id: "usr_demo_1", role: "PLAYER", display_name: "Avery" },
      { user_id: "usr_demo_2", role: "PLAYER", display_name: "Jordan" },
    ],
    capabilities_granted: currentGrantedPermissions(),
    state_snapshot: state.sessionState.stateSnapshot,
  };
}

function buildFrameUrl() {
  const url = new URL(state.manifest.entrypoint.url, window.location.href);
  url.searchParams.set("channel", state.channelId);
  url.searchParams.set("parent_origin", window.location.origin);
  url.searchParams.set("app_id", state.manifest.app_id);
  return url.toString();
}

function launchFrame() {
  state.channelId = randomId("chan");
  state.frameWindow = null;
  el.frame.setAttribute("sandbox", "allow-scripts");
  el.frame.src = buildFrameUrl();
  setStatus(`Launching ${state.manifest.name}.`);
  addLog("ok", "runtime.launch", { entrypoint: state.manifest.entrypoint.url, channel: state.channelId });
}

function requirePermission(permission) {
  if (!permission) return;
  if (!state.grantedPermissions.has(permission)) {
    const err = new Error(`Permission denied: ${permission}`);
    err.code = "miniapp_capability_denied";
    err.details = { required_capability: permission };
    throw err;
  }
}

function cloneJson(value) {
  return value === undefined ? null : JSON.parse(JSON.stringify(value));
}

function pushBridgeEvent(name, payload) {
  if (!state.frameWindow) return;
  const message = {
    bridge_event: name,
    channel: state.channelId,
    payload: cloneJson(payload),
  };
  state.frameWindow.postMessage(message, "*");
  addLog("ok", name, payload);
}

function sendBridgeResponse(targetWindow, requestId, ok, result, error) {
  targetWindow.postMessage(
    {
      channel: state.channelId,
      request_id: requestId,
      ok,
      result: ok ? cloneJson(result) : undefined,
      error: ok ? undefined : error,
    },
    "*"
  );
}

function appendProjectedMessage(text) {
  state.sessionState.transcript.push({
    id: randomId("msg"),
    author: state.manifest.name,
    text,
    createdAt: new Date().toISOString(),
  });
  state.sessionState.transcript = state.sessionState.transcript.slice(-20);
  saveSession();
  renderTranscript();
}

function applyStateUpdate(params) {
  requirePermission("realtime.session");
  const nextCounter = Math.max(0, Math.min(9999, Number(params?.counter) || 0));
  state.sessionState.stateVersion += 1;
  state.sessionState.stateSnapshot = {
    counter: nextCounter,
    updated_by: state.launchContext?.viewer?.display_name || "app",
    updated_at: new Date().toISOString(),
  };
  state.launchContext.state_snapshot = state.sessionState.stateSnapshot;
  saveSession();
  renderContext();
  pushBridgeEvent("session.stateUpdated", {
    app_session_id: state.launchContext.app_session_id,
    state_version: state.sessionState.stateVersion,
    delta: { counter: nextCounter },
    state_snapshot: state.sessionState.stateSnapshot,
  });
  return {
    state_version: state.sessionState.stateVersion,
    state_snapshot: state.sessionState.stateSnapshot,
  };
}

function handleBridgeCall(message) {
  const method = sanitizeText(message.method, 120);
  switch (method) {
    case "host.getLaunchContext":
      syncLaunchContextPermissions();
      return cloneJson(state.launchContext);
    case "conversation.readContext":
      requirePermission("conversation.read_context");
      return {
        conversation_id: state.launchContext.conversation_id,
        title: "Mini-App Runtime Test Thread",
        recent_messages: state.sessionState.transcript.slice(-6),
      };
    case "conversation.sendMessage": {
      requirePermission("conversation.send_message");
      const text = sanitizeText(message.params?.text, 220);
      if (!text) throw new Error("conversation.sendMessage requires params.text");
      appendProjectedMessage(text);
      return { message_id: randomId("msg") };
    }
    case "storage.session.get": {
      requirePermission("storage.session");
      const key = sanitizeText(message.params?.key, 80);
      if (!key) throw new Error("storage.session.get requires params.key");
      return { key, value: cloneJson(state.sessionState.storage[key]) };
    }
    case "storage.session.set": {
      requirePermission("storage.session");
      const key = sanitizeText(message.params?.key, 80);
      if (!key) throw new Error("storage.session.set requires params.key");
      state.sessionState.storage[key] = cloneJson(message.params?.value);
      saveSession();
      return { key, value: cloneJson(state.sessionState.storage[key]) };
    }
    case "session.updateState":
      return applyStateUpdate(message.params);
    default: {
      const err = new Error(`Unknown bridge method: ${method}`);
      err.code = "method_not_found";
      throw err;
    }
  }
}

async function loadAndLaunch(manifestUrl) {
  const cleanUrl = sanitizeText(manifestUrl, 300) || DEFAULT_MANIFEST_URL;
  state.manifest = await fetchManifest(cleanUrl);
  state.grantedPermissions = new Set(state.manifest.permissions);
  buildLaunchContext();
  renderManifest();
  renderPermissions();
  renderContext();
  renderTranscript();
  launchFrame();
  el.manifestUrl.value = cleanUrl;
}

window.addEventListener("message", (event) => {
  if (event.source !== el.frame.contentWindow) return;
  if (event.origin !== "null") return;
  const message = event.data;
  if (!message || typeof message !== "object") return;
  if (message.channel !== state.channelId) return;

  state.frameWindow = event.source;

  const requestId = sanitizeText(message.request_id, 80);
  if (!requestId) return;

  addLog("ok", `request ${sanitizeText(message.method, 120)}`, message);

  try {
    const result = handleBridgeCall(message);
    sendBridgeResponse(event.source, requestId, true, result);
  } catch (error) {
    const payload = {
      code: sanitizeText(error.code || "bridge_error", 80),
      message: sanitizeText(error.message || "Bridge call failed", 220),
      details: error.details && typeof error.details === "object" ? error.details : undefined,
    };
    addLog("error", `error ${sanitizeText(message.method, 120)}`, payload);
    sendBridgeResponse(event.source, requestId, false, null, payload);
  }
});

el.manifestForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    await loadAndLaunch(el.manifestUrl.value);
  } catch (error) {
    setStatus(error.message || "Failed to load manifest.", true);
    addLog("error", "runtime.load_failed", { message: error.message || String(error) });
  }
});

el.relaunchBtn.addEventListener("click", async () => {
  if (!state.manifest) {
    setStatus("Load a manifest before relaunching.", true);
    return;
  }
  try {
    buildLaunchContext();
    renderContext();
    renderTranscript();
    launchFrame();
  } catch (error) {
    setStatus(error.message || "Failed to relaunch app.", true);
  }
});

el.clearSessionBtn.addEventListener("click", async () => {
  if (!state.manifest) {
    setStatus("Load a manifest before clearing session state.", true);
    return;
  }
  clearSavedSession();
  buildLaunchContext();
  renderContext();
  renderTranscript();
  launchFrame();
  setStatus(`Cleared persisted session for ${state.manifest.name}.`);
});

renderLog();
loadAndLaunch(DEFAULT_MANIFEST_URL).catch((error) => {
  setStatus(error.message || "Failed to start runtime.", true);
  addLog("error", "runtime.bootstrap_failed", { message: error.message || String(error) });
});
