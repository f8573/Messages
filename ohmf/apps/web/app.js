"use strict";

function normalizeAPIBaseURL(value) {
  const fallback = (window.OHMF_WEB_CONFIG?.api_base_url || "http://localhost:18081").replace(/\/+$/, "");
  if (!value) return fallback;
  try {
    const url = new URL(value);
    const localHosts = new Set(["localhost", "127.0.0.1"]);
    const targetPort = String(window.OHMF_WEB_CONFIG?.api_host_port || "18081");
    if (localHosts.has(url.hostname) && (url.port === "18080" || url.port === "8080")) {
      url.port = targetPort;
      const normalized = url.toString().replace(/\/+$/, "");
      window.localStorage.setItem("ohmf.apiBaseUrl", normalized);
      return normalized;
    }
    return url.toString().replace(/\/+$/, "");
  } catch {
    return fallback;
  }
}

const API_BASE_URL = normalizeAPIBaseURL(window.OHMF_WEB_CONFIG?.api_base_url || window.localStorage.getItem("ohmf.apiBaseUrl") || "http://localhost:18081");
const AUTH_STORAGE_KEY = "ohmf.auth.session.v1";
const STORE_VERSION = 2;
const TRANSPORT_SMS = "SMS";
const TRANSPORT_OHMF = "OHMF";
const CONTENT_TYPE_TEXT = "text";
const CONTENT_TYPE_APP_CARD = "app_card";
const CONTENT_TYPE_APP_EVENT = "app_event";
const SMS_DELIVERY_STATUSES = Object.freeze({
  SENT: "SENT",
  FAIL_SEND: "FAIL_SEND",
});
const OHMF_DELIVERY_STATUSES = Object.freeze({
  SENT: "SENT",
  DELIVERED: "DELIVERED",
  READ: "READ",
  FAIL_DELIVERY: "FAIL_DELIVERY",
  FAIL_SEND: "FAIL_SEND",
});
const MINIAPP_CATALOG = Object.freeze([
  {
    appId: "app.ohmf.counter-lab",
    manifestUrl: "./miniapps/counter/manifest.json",
    title: "Counter Lab",
    summary: "Shared state demo with projected messages.",
  },
]);

const state = {
  auth: null,
  challengeId: "",
  query: "",
  activeThreadId: null,
  threads: [],
  typingDraft: "",
  miniapp: {
    drawerOpen: false,
    selectedAppId: "",
    manifest: null,
    launchContext: null,
    sessionState: null,
    grantedPermissions: new Set(),
    frameWindow: null,
    channelId: "",
    sessionMode: "idle",
    consentRequired: false,
    lastShareError: "",
  },
};
let liveSyncInFlight = false;
let eventStreamAbort = null;
let eventStreamReconnectTimer = 0;
let refreshAuthInFlight = null;
let eventStreamDisabled = false;
let realtimeSocket = null;
let realtimeReconnectTimer = 0;

const el = {
  authShell: document.getElementById("auth-shell"),
  appShell: document.getElementById("app-shell"),
  authStatus: document.getElementById("auth-status"),
  phoneStartForm: document.getElementById("phone-start-form"),
  phoneVerifyForm: document.getElementById("phone-verify-form"),
  countryCodeSelect: document.getElementById("country-code-select"),
  phoneInput: document.getElementById("phone-input"),
  phoneE164Preview: document.getElementById("phone-e164-preview"),
  otpInput: document.getElementById("otp-input"),
  threadList: document.getElementById("thread-list"),
  messageList: document.getElementById("message-list"),
  searchInput: document.getElementById("search-input"),
  title: document.getElementById("chat-title"),
  subtitle: document.getElementById("chat-subtitle"),
  composer: document.getElementById("composer"),
  composerInput: document.getElementById("composer-input"),
  emptyState: document.getElementById("empty-state"),
  backBtn: document.getElementById("back-btn"),
  newChatBtn: document.getElementById("new-chat-btn"),
  logoutBtn: document.getElementById("logout-btn"),
  newChatForm: document.getElementById("new-chat-form"),
  newCountryCodeSelect: document.getElementById("new-country-code-select"),
  newPhoneInput: document.getElementById("new-phone-input"),
  nicknameBtn: document.getElementById("nickname-btn"),
  blockBtn: document.getElementById("block-btn"),
  closeThreadBtn: document.getElementById("close-thread-btn"),
  attachBtn: document.getElementById("attach-btn"),
  miniappLauncher: document.getElementById("miniapp-launcher"),
  miniappCloseBtn: document.getElementById("miniapp-close-btn"),
  miniappPicker: document.getElementById("miniapp-picker"),
  miniappCounterCard: document.getElementById("miniapp-counter-card"),
  miniappStage: document.getElementById("miniapp-stage"),
  miniappPreviewTitle: document.getElementById("miniapp-preview-title"),
  miniappPreviewSubtitle: document.getElementById("miniapp-preview-subtitle"),
  miniappPermissions: document.getElementById("miniapp-permissions"),
  miniappContextCopy: document.getElementById("miniapp-context-copy"),
  miniappLaunchMode: document.getElementById("miniapp-launch-mode"),
  miniappShareBtn: document.getElementById("miniapp-share-btn"),
  miniappOpenBtn: document.getElementById("miniapp-open-btn"),
  miniappResetBtn: document.getElementById("miniapp-reset-btn"),
  miniappFrame: document.getElementById("miniapp-frame"),
};

function nowISO() {
  return new Date().toISOString();
}

function formatShortTime(value) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function sanitizeText(value, limit = 1000) {
  return String(value || "")
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .trim()
    .slice(0, limit);
}

function onlyDigits(value, limit = 32) {
  return String(value || "").replace(/\D/g, "").slice(0, limit);
}

function formatPhoneLocal(value) {
  const digits = onlyDigits(value, 10);
  if (digits.length <= 3) return digits ? `(${digits}` : "";
  if (digits.length <= 6) return `(${digits.slice(0, 3)})-${digits.slice(3)}`;
  return `(${digits.slice(0, 3)})-${digits.slice(3, 6)}-${digits.slice(6)}`;
}

function toE164(countryCode, localValue) {
  const prefix = String(countryCode || "").trim();
  const digits = onlyDigits(localValue, 15);
  if (!/^\+\d{1,4}$/.test(prefix)) return "";
  const raw = `${prefix}${digits}`;
  if (!/^\+\d{8,15}$/.test(raw)) return "";
  return raw;
}

function makeIdempotencyKey(prefix = "msg") {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

async function uploadMediaFile(file) {
  if (!file) throw new Error("no file");
  // Initialize upload
  const init = await apiRequest(`/v1/media/uploads`, {
    method: "POST",
    body: JSON.stringify({ items: [{ mime_type: file.type, size_bytes: file.size, kind: "media" }] }),
  });
  const uploadUrl = init?.upload_url;
  const uploadId = init?.upload_id;
  if (!uploadUrl) throw new Error("no upload url returned");

  // Perform upload (placeholder, assumes PUT to upload_url)
  await fetch(uploadUrl, { method: "PUT", body: file });

  return { upload_id: uploadId, upload_url: uploadUrl };
}

function initials(name) {
  return sanitizeText(name, 24)
    .split(/\s+/)
    .map((part) => part[0] || "")
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

function authStoreSet(session) {
  state.auth = session;
  window.sessionStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(session));
}

function authStoreClear() {
  state.auth = null;
  window.sessionStorage.removeItem(AUTH_STORAGE_KEY);
}

function authStoreLoad() {
  const raw = window.sessionStorage.getItem(AUTH_STORAGE_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || !parsed.accessToken || !parsed.refreshToken || !parsed.userId) return null;
    return parsed;
  } catch {
    return null;
  }
}

function conversationStoreKey() {
  return `ohmf.conversations.${state.auth?.userId || "anon"}.v${STORE_VERSION}`;
}

function saveConversationStore() {
  if (!state.auth?.userId) return;
  window.localStorage.setItem(
    conversationStoreKey(),
    JSON.stringify({
      version: STORE_VERSION,
      savedAt: nowISO(),
      threads: state.threads,
    })
  );
}

function loadConversationStore() {
  const raw = window.localStorage.getItem(conversationStoreKey());
  if (!raw) return;
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || !Array.isArray(parsed.threads)) return;
    state.threads = parsed.threads.map((thread) => ({
      id: sanitizeText(thread.id, 80),
      kind: sanitizeText(thread.kind, 24) || "dm",
      title: sanitizeText(thread.title, 80) || "Conversation",
      subtitle: sanitizeText(thread.subtitle, 120),
      nickname: sanitizeText(thread.nickname, 80),
      updatedAt: thread.updatedAt || nowISO(),
      blocked: Boolean(thread.blocked),
      closed: Boolean(thread.closed),
      externalPhones: Array.isArray(thread.externalPhones) ? thread.externalPhones.map((p) => sanitizeText(p, 32)) : [],
      participants: Array.isArray(thread.participants) ? thread.participants.map((p) => sanitizeText(p, 80)) : [],
      messages: Array.isArray(thread.messages)
        ? thread.messages.map((message) => {
            const transport = normalizeTransport(message.transport);
            return {
              ...message,
              transport,
              status: normalizeDeliveryStatus(transport, message.status),
            };
          })
        : [],
      loadedMessages: Boolean(thread.loadedMessages),
    }));
  } catch {
    state.threads = [];
  }
}

function setAuthStatus(message, isError = false) {
  el.authStatus.textContent = sanitizeText(message, 200);
  el.authStatus.classList.toggle("error", Boolean(isError));
}

async function apiRequest(path, options = {}, allowRetry = true) {
  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");
  if (state.auth?.accessToken) headers.set("Authorization", `Bearer ${state.auth.accessToken}`);

  const response = await fetch(`${API_BASE_URL}${path}`, { ...options, headers, credentials: "omit" });

  if (response.status === 401 && allowRetry && state.auth?.refreshToken) {
    const refreshed = await refreshAuthTokens();
    if (refreshed) return apiRequest(path, options, false);
  }

  const text = await response.text();
  let payload = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { message: text };
    }
  }
  if (!response.ok) {
    const error = new Error(payload?.message || "Request failed");
    error.status = response.status;
    error.code = payload?.code || `http_${response.status}`;
    throw error;
  }
  return payload;
}

async function refreshAuthTokens() {
  if (!state.auth?.refreshToken) return false;
  if (refreshAuthInFlight) return refreshAuthInFlight;

  const sessionAtStart = state.auth;
  refreshAuthInFlight = (async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/v1/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: sessionAtStart.refreshToken }),
        credentials: "omit",
      });
      if (!response.ok) return false;
      const json = await response.json();
      const tokens = json?.tokens;
      if (!tokens?.access_token || !tokens?.refresh_token) return false;
      if (!state.auth || state.auth.userId !== sessionAtStart.userId) return false;
      authStoreSet({
        ...state.auth,
        accessToken: tokens.access_token,
        refreshToken: tokens.refresh_token,
      });
      return true;
    } catch {
      return false;
    } finally {
      refreshAuthInFlight = null;
    }
  })();

  return refreshAuthInFlight;
}

function cloneJson(value) {
  return value === undefined ? null : JSON.parse(JSON.stringify(value));
}

function randomId(prefix) {
  if (window.crypto && typeof window.crypto.randomUUID === "function") {
    return `${prefix}_${window.crypto.randomUUID().replace(/-/g, "")}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

function getMiniappCatalogEntry(appId = state.miniapp.selectedAppId) {
  return MINIAPP_CATALOG.find((item) => item.appId === appId) || null;
}

function miniappSupportReason(thread = getActiveThread()) {
  if (!thread) return "Select a saved OHMF conversation to launch an app.";
  if (thread.closed) return "Reopen or choose an active conversation to launch an app.";
  if (thread.kind === "draft_phone") return "Save the conversation before sending an app.";
  return "";
}

function activeThreadSupportsMiniapps(thread = getActiveThread()) {
  return miniappSupportReason(thread) === "";
}

function rewriteLocalDevEntrypoint(rawUrl) {
  const url = new URL(rawUrl, window.location.href);
  const localHosts = new Set(["localhost", "127.0.0.1"]);
  if (localHosts.has(url.hostname) && localHosts.has(window.location.hostname) && url.port !== window.location.port) {
    url.protocol = window.location.protocol;
    url.host = `${window.location.hostname}:${window.location.port}`;
  }
  return url.toString();
}

function normalizeMiniappSessionState(raw) {
  if (!raw || typeof raw !== "object") {
    return {
      stateVersion: 1,
      stateSnapshot: {},
      storage: {},
      sharedConversationStorage: {},
      transcript: [],
    };
  }
  return {
    stateVersion: Number(raw.state_version || raw.stateVersion || 1) || 1,
    stateSnapshot: raw.snapshot || raw.stateSnapshot || {},
    storage: raw.session_storage || raw.storage || {},
    sharedConversationStorage: raw.shared_conversation_storage || raw.sharedConversationStorage || {},
    transcript: Array.isArray(raw.projected_messages || raw.transcript) ? (raw.projected_messages || raw.transcript) : [],
  };
}

async function fetchMiniappManifest(manifestUrl) {
  const response = await fetch(manifestUrl, { cache: "no-store" });
  if (!response.ok) throw new Error(`Manifest request failed with ${response.status}`);
  const manifest = await response.json();
  if (!manifest?.app_id || !manifest?.entrypoint?.url) throw new Error("invalid_manifest");
  manifest.entrypoint.url = rewriteLocalDevEntrypoint(manifest.entrypoint.url);
  return manifest;
}

async function ensureMiniappManifestRegistered(manifest) {
  try {
    return await apiRequest(`/v1/apps/${encodeURIComponent(manifest.app_id)}`, { method: "GET" });
  } catch (error) {
    if (error.status !== 404) throw error;
  }
  await apiRequest("/v1/apps/register", { method: "POST", body: JSON.stringify({ manifest }) });
  return apiRequest(`/v1/apps/${encodeURIComponent(manifest.app_id)}`, { method: "GET" });
}

async function loadMiniappManifestByAppId(appId) {
  const response = await apiRequest(`/v1/apps/${encodeURIComponent(appId)}`, { method: "GET" });
  const manifest = response?.manifest;
  if (!manifest?.app_id || !manifest?.entrypoint?.url) throw new Error("invalid_manifest");
  manifest.entrypoint.url = rewriteLocalDevEntrypoint(manifest.entrypoint.url);
  return manifest;
}

function buildMiniappViewer(thread) {
  return {
    user_id: state.auth?.userId || "",
    role: "PLAYER",
    display_name: thread?.title || state.auth?.phoneE164 || state.auth?.userId || "You",
  };
}

function buildMiniappParticipants(thread) {
  const ids = Array.isArray(thread?.participants) ? thread.participants : [];
  const viewer = buildMiniappViewer(thread);
  const participants = [{ ...viewer }];
  for (const id of ids) {
    if (!id || id === viewer.user_id) continue;
    participants.push({ user_id: id, role: "PLAYER", display_name: `User ${id.slice(0, 8)}` });
  }
  if (participants.length === 1 && thread?.externalPhones?.[0]) {
    participants.push({ user_id: `phone:${thread.externalPhones[0]}`, role: "PLAYER", display_name: thread.externalPhones[0] });
  }
  return participants;
}

function applyMiniappSessionRecord(record, manifest) {
  state.miniapp.sessionMode = "gateway";
  state.miniapp.manifest = manifest;
  state.miniapp.sessionState = normalizeMiniappSessionState({
    state_version: record?.state_version,
    snapshot: record?.state?.snapshot,
    session_storage: record?.state?.session_storage,
    shared_conversation_storage: record?.state?.shared_conversation_storage,
    projected_messages: record?.state?.projected_messages,
  });
  state.miniapp.launchContext = record?.launch_context || null;
  state.miniapp.consentRequired = Boolean(record?.consent_required || record?.launch_context?.consent_required);
  state.miniapp.grantedPermissions = new Set(
    Array.isArray(record?.capabilities_granted) && record.capabilities_granted.length ? record.capabilities_granted : (manifest.permissions || [])
  );
  state.miniapp.lastShareError = "";
}

async function ensureMiniappSession() {
  const thread = getActiveThread();
  if (!activeThreadSupportsMiniapps(thread)) throw new Error("Select a saved conversation first.");
  const entry = getMiniappCatalogEntry();
  if (!entry) throw new Error("Select an app first.");
  const manifest = state.miniapp.manifest || await fetchMiniappManifest(entry.manifestUrl);
  state.miniapp.manifest = manifest;
  await ensureMiniappManifestRegistered(manifest);
  const record = await apiRequest("/v1/apps/sessions", {
    method: "POST",
    body: JSON.stringify({
      app_id: manifest.app_id,
      conversation_id: thread.id,
      viewer: buildMiniappViewer(thread),
      participants: buildMiniappParticipants(thread),
      capabilities_granted: Array.from(state.miniapp.grantedPermissions),
      state_snapshot: cloneJson(state.miniapp.sessionState?.stateSnapshot || {}),
      resume_existing: true,
    }),
  });
  applyMiniappSessionRecord(record, manifest);
}

async function fetchMiniappSession(sessionId) {
  const record = await apiRequest(`/v1/apps/sessions/${encodeURIComponent(sessionId)}`, { method: "GET" });
  const appId = sanitizeText(record?.app_id, 120);
  if (!appId) throw new Error("invalid_session");
  const manifest = state.miniapp.manifest?.app_id === appId ? state.miniapp.manifest : await loadMiniappManifestByAppId(appId);
  state.miniapp.selectedAppId = appId;
  applyMiniappSessionRecord(record, manifest);
  return record;
}

async function joinMiniappSession(sessionId) {
  const record = await apiRequest(`/v1/apps/sessions/${encodeURIComponent(sessionId)}/join`, {
    method: "POST",
    body: JSON.stringify({ capabilities_granted: Array.from(state.miniapp.grantedPermissions) }),
  });
  const appId = sanitizeText(record?.app_id, 120);
  const manifest = state.miniapp.manifest?.app_id === appId ? state.miniapp.manifest : await loadMiniappManifestByAppId(appId);
  applyMiniappSessionRecord(record, manifest);
  return record;
}

async function persistMiniappSession(version, eventName, eventBody) {
  if (!state.miniapp.launchContext?.app_session_id) return 0;
  const payload = await apiRequest(`/v1/apps/sessions/${encodeURIComponent(state.miniapp.launchContext.app_session_id)}/snapshot`, {
    method: "POST",
    body: JSON.stringify({
      state: {
        snapshot: cloneJson(state.miniapp.sessionState?.stateSnapshot || {}),
        session_storage: cloneJson(state.miniapp.sessionState?.storage || {}),
        shared_conversation_storage: cloneJson(state.miniapp.sessionState?.sharedConversationStorage || {}),
        projected_messages: cloneJson(state.miniapp.sessionState?.transcript || []),
      },
      state_version: version,
      capabilities_granted: Array.from(state.miniapp.grantedPermissions),
    }),
  });
  if (eventName) {
    await apiRequest(`/v1/apps/sessions/${encodeURIComponent(state.miniapp.launchContext.app_session_id)}/events`, {
      method: "POST",
      body: JSON.stringify({ event_name: eventName, body: eventBody || {} }),
    });
  }
  return Number(payload?.state_version || version || 1);
}

async function shareMiniappToConversation() {
  const thread = getActiveThread();
  if (!activeThreadSupportsMiniapps(thread)) {
    state.miniapp.lastShareError = miniappSupportReason(thread);
    renderMiniappLauncher();
    return;
  }
  const entry = getMiniappCatalogEntry();
  if (!entry) return;
  const manifest = state.miniapp.manifest || await fetchMiniappManifest(entry.manifestUrl);
  state.miniapp.manifest = manifest;
  state.miniapp.lastShareError = "";
  let payload;
  try {
    payload = await apiRequest("/v1/apps/shares", {
      method: "POST",
      body: JSON.stringify({
        conversation_id: thread.id,
        app_id: manifest.app_id,
        capabilities_granted: Array.from(state.miniapp.grantedPermissions),
        state_snapshot: cloneJson(state.miniapp.sessionState?.stateSnapshot || {}),
        resume_existing: true,
      }),
    });
  } catch (error) {
    if (error.status === 409 && error.code === "miniapp_unsupported") {
      state.miniapp.lastShareError = "This conversation is not mini-app capable yet. Every participant needs an OHMF device with mini-app support.";
      renderMiniappLauncher();
      return;
    }
    throw error;
  }
  if (payload?.message) {
    upsertThreadMessage(thread.id, mapMessage(payload.message));
  }
  applyMiniappSessionRecord(payload, manifest);
  renderAll();
  await openEmbeddedMiniapp();
}

async function openMiniappCard(message) {
  const sessionId = sanitizeText(message?.content?.app_session_id, 120);
  if (!sessionId) return;
  state.miniapp.drawerOpen = true;
  const record = await fetchMiniappSession(sessionId);
  renderMiniappLauncher();
  if (record?.joinable !== false && !state.miniapp.consentRequired) {
    await openEmbeddedMiniapp();
  }
}

function appendProjectedMiniappMessage(text, contentType = "app_event", content = null) {
  const thread = getActiveThread();
  if (!thread) return;
  const message = {
    id: randomId("appmsg"),
    direction: "out",
    text: sanitizeText(text, 280),
    createdAt: nowISO(),
    serverOrder: Number.MAX_SAFE_INTEGER - Date.now(),
    status: OHMF_DELIVERY_STATUSES.SENT,
    statusUpdatedAt: nowISO(),
    transport: TRANSPORT_OHMF,
    reactions: {},
    editedAt: "",
    deleted: false,
    contentType,
    content,
  };
  upsertThread({ ...thread, messages: [...(thread.messages || []), message], updatedAt: message.createdAt });
  saveConversationStore();
  renderAll();
}

function buildMiniappFrameURL() {
  const url = new URL(state.miniapp.manifest.entrypoint.url, window.location.href);
  state.miniapp.channelId = randomId("chan");
  url.searchParams.set("channel", state.miniapp.channelId);
  url.searchParams.set("parent_origin", window.location.origin);
  url.searchParams.set("app_id", state.miniapp.manifest.app_id);
  return url.toString();
}

function summarizeMiniappMessage(params) {
  const explicit = sanitizeText(params?.text, 220);
  if (explicit) return explicit;
  const eventName = sanitizeText(params?.content?.event_name, 80);
  if (eventName) return `${state.miniapp.manifest?.name || "App"}: ${eventName}`;
  return `${state.miniapp.manifest?.name || "App"} posted an update.`;
}

function requireMiniappPermission(permission) {
  if (state.miniapp.grantedPermissions.has(permission)) return;
  const error = new Error(`Permission required: ${permission}`);
  error.code = "permission_denied";
  throw error;
}

async function handleMiniappBridgeCall(message) {
  const method = sanitizeText(message.method, 120);
  switch (method) {
    case "host.getLaunchContext":
      return cloneJson(state.miniapp.launchContext);
    case "conversation.readContext":
      requireMiniappPermission("conversation.read_context");
      return {
        conversation_id: state.miniapp.launchContext?.conversation_id,
        title: getActiveThread()?.title || "Conversation",
        recent_messages: cloneJson((getActiveThread()?.messages || []).slice(-6).map((item) => ({
          author: item.direction === "out" ? "You" : getActiveThread()?.title || "Participant",
          text: item.text,
          createdAt: item.createdAt,
        }))),
      };
    case "conversation.sendMessage": {
      requireMiniappPermission("conversation.send_message");
      const text = summarizeMiniappMessage(message.params);
      appendProjectedMiniappMessage(text, sanitizeText(message.params?.content_type, 60) || "app_event", message.params?.content || null);
      state.miniapp.sessionState.transcript.push({ author: state.miniapp.manifest.name, text, createdAt: nowISO() });
      state.miniapp.sessionState.stateVersion += 1;
      const persistedVersion = await persistMiniappSession(state.miniapp.sessionState.stateVersion, "MESSAGE_PROJECTED", {
        text,
        content_type: sanitizeText(message.params?.content_type, 60) || "app_event",
      });
      state.miniapp.launchContext.state_version = persistedVersion;
      return { message_id: randomId("msg"), state_version: persistedVersion };
    }
    case "participants.readBasic":
      requireMiniappPermission("participants.read_basic");
      return { participants: cloneJson(state.miniapp.launchContext?.participants || []) };
    case "storage.session.get": {
      requireMiniappPermission("storage.session");
      const key = sanitizeText(message.params?.key, 80);
      return { key, value: cloneJson(state.miniapp.sessionState.storage[key]) };
    }
    case "storage.session.set": {
      requireMiniappPermission("storage.session");
      const key = sanitizeText(message.params?.key, 80);
      state.miniapp.sessionState.storage[key] = cloneJson(message.params?.value);
      state.miniapp.sessionState.stateVersion += 1;
      const persistedVersion = await persistMiniappSession(state.miniapp.sessionState.stateVersion, "SESSION_STORAGE_UPDATED", { key });
      state.miniapp.launchContext.state_version = persistedVersion;
      return { key, value: cloneJson(state.miniapp.sessionState.storage[key]), state_version: persistedVersion };
    }
    case "session.updateState": {
      requireMiniappPermission("realtime.session");
      const next = cloneJson(message.params || {});
      state.miniapp.sessionState.stateSnapshot = {
        ...(state.miniapp.sessionState.stateSnapshot || {}),
        ...next,
        updated_at: nowISO(),
      };
      state.miniapp.sessionState.stateVersion += 1;
      const persistedVersion = await persistMiniappSession(state.miniapp.sessionState.stateVersion, "STATE_UPDATED", { delta: next });
      state.miniapp.launchContext.state_snapshot = cloneJson(state.miniapp.sessionState.stateSnapshot);
      state.miniapp.launchContext.state_version = persistedVersion;
      return { state_version: persistedVersion, state_snapshot: cloneJson(state.miniapp.sessionState.stateSnapshot) };
    }
    default: {
      const error = new Error(`Unknown bridge method: ${method}`);
      error.code = "method_not_found";
      throw error;
    }
  }
}

function sendMiniappBridgeResponse(targetWindow, requestId, ok, result, error) {
  targetWindow.postMessage(
    {
      bridge_version: "1.0",
      channel: state.miniapp.channelId,
      request_id: requestId,
      ok,
      result: ok ? cloneJson(result) : undefined,
      error: ok ? undefined : error,
    },
    "*"
  );
}

function pickTitle(conversation) {
  if (conversation.nickname) return conversation.nickname;
  if (conversation.externalPhones?.length) return conversation.externalPhones[0];
  const others = (conversation.participants || []).filter((id) => id !== state.auth?.userId);
  return others.length ? `User ${others[0].slice(0, 8)}` : `Conversation ${conversation.id.slice(0, 8)}`;
}

function pickSubtitle(conversation) {
  if (conversation.blocked) return "Blocked";
  if (conversation.kind === "phone") return "Phone conversation (OTT preferred)";
  const others = (conversation.participants || []).filter((id) => id !== state.auth?.userId).length;
  return others <= 0 ? "Only you" : `${others} participant${others > 1 ? "s" : ""}`;
}

function mapConversation(item) {
  const kind = item.type === "PHONE_DM" || (Array.isArray(item.external_phones) && item.external_phones.length > 0) ? "phone" : "dm";
  const thread = {
    id: sanitizeText(item.conversation_id, 80),
    kind,
    title: "",
    subtitle: "",
    nickname: "",
    updatedAt: item.updated_at || nowISO(),
    blocked: false,
    closed: false,
    participants: Array.isArray(item.participants) ? item.participants.map((v) => sanitizeText(v, 80)) : [],
    externalPhones: Array.isArray(item.external_phones) ? item.external_phones.map((v) => sanitizeText(v, 32)) : [],
    messages: [],
    loadedMessages: false,
  };
  thread.title = pickTitle(thread);
  thread.subtitle = pickSubtitle(thread);
  return thread;
}

function mapMessage(item) {
  const transport = normalizeTransport(item.transport);
  const status = normalizeDeliveryStatus(
    transport,
    item.status || (item.sender_user_id === state.auth?.userId ? OHMF_DELIVERY_STATUSES.SENT : "")
  );
  const contentType = sanitizeText(item?.content_type, 40) || CONTENT_TYPE_TEXT;
  const content = item?.content && typeof item.content === "object" ? item.content : {};
  const fallbackText = contentType === CONTENT_TYPE_APP_CARD
    ? sanitizeText(content.title || "Shared app", 1000)
    : sanitizeText(item?.content?.text || JSON.stringify(content || {}), 1000);
  return {
    id: sanitizeText(item?.message_id, 80),
    direction: item.sender_user_id === state.auth?.userId ? "out" : "in",
    text: fallbackText,
    createdAt: item.created_at || nowISO(),
    sentAt: item.sent_at || item.created_at || nowISO(),
    deliveredAt: item.delivered_at || "",
    readAt: item.read_at || "",
    serverOrder: Number(item.server_order || 0),
    status,
    statusUpdatedAt: item.status_updated_at || item.read_at || item.delivered_at || item.sent_at || item.created_at || nowISO(),
    transport,
    reactions: {},
    editedAt: "",
    deleted: false,
    contentType,
    content,
  };
}

function isAppCardMessage(message) {
  return sanitizeText(message?.contentType, 40) === CONTENT_TYPE_APP_CARD;
}

function upsertThreadMessage(threadId, nextMessage) {
  const thread = getThreadById(threadId);
  if (!thread) return;
  const nextMessages = [...(thread.messages || []).filter((message) => message.id !== nextMessage.id), nextMessage].sort(
    (a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
  );
  upsertThread({ ...thread, messages: nextMessages, updatedAt: nextMessage.createdAt || nowISO() });
  saveConversationStore();
}

function normalizeTransport(value) {
  return sanitizeText(value, 24) === TRANSPORT_SMS ? TRANSPORT_SMS : TRANSPORT_OHMF;
}

function legacyStatusToCurrent(value, transport) {
  const status = sanitizeText(value, 40).toUpperCase();
  if (!status) return "";
  if (status === "FAILED") return "FAIL_SEND";
  if (status === "PENDING") return "SENT";
  if (status.startsWith("SENT (SMS")) return "SENT";
  if (status === "SENT (SMS FALLBACK)") return "SENT";
  if (transport === TRANSPORT_SMS && status === "DELIVERED") return "SENT";
  if (transport === TRANSPORT_SMS && status === "READ") return "SENT";
  return status;
}

function normalizeDeliveryStatus(transport, status) {
  const resolvedTransport = normalizeTransport(transport);
  const normalized = legacyStatusToCurrent(status, resolvedTransport);
  if (!normalized) return "";
  const allowed = resolvedTransport === TRANSPORT_SMS ? SMS_DELIVERY_STATUSES : OHMF_DELIVERY_STATUSES;
  return Object.values(allowed).includes(normalized) ? normalized : resolvedTransport === TRANSPORT_SMS ? SMS_DELIVERY_STATUSES.SENT : OHMF_DELIVERY_STATUSES.SENT;
}

function deliveryIndicatorLabel(message) {
  if (normalizeTransport(message.transport) !== TRANSPORT_OHMF || message.direction !== "out") return "";
  const status = normalizeDeliveryStatus(message.transport, message.status);
  switch (status) {
    case OHMF_DELIVERY_STATUSES.SENT:
      return message.sentAt ? `Sent ${formatShortTime(message.sentAt)}` : "Sent";
    case OHMF_DELIVERY_STATUSES.DELIVERED:
      return message.deliveredAt ? `Delivered ${formatShortTime(message.deliveredAt)}` : "Delivered";
    case OHMF_DELIVERY_STATUSES.READ:
      return message.readAt ? `Read ${formatShortTime(message.readAt)}` : "Read";
    case OHMF_DELIVERY_STATUSES.FAIL_DELIVERY:
      return "Failed delivery";
    case OHMF_DELIVERY_STATUSES.FAIL_SEND:
      return "Failed to send";
    default:
      return "";
  }
}

function threadSort(a, b) {
  return new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime();
}

function getThreadById(id) {
  return state.threads.find((thread) => thread.id === id) || null;
}

function getActiveThread() {
  return getThreadById(state.activeThreadId);
}

function visibleThreads() {
  const q = sanitizeText(state.query, 120).toLowerCase();
  return [...state.threads]
    .filter((thread) => !thread.closed)
    .filter((thread) => {
      if (!q) return true;
      const combined = `${thread.title} ${thread.subtitle} ${(thread.messages || []).map((m) => m.text).join(" ")}`.toLowerCase();
      return combined.includes(q);
    })
    .sort(threadSort);
}

function upsertThread(thread) {
  thread.title = pickTitle(thread);
  thread.subtitle = pickSubtitle(thread);
  const idx = state.threads.findIndex((item) => item.id === thread.id);
  if (idx === -1) state.threads.push(thread);
  else state.threads[idx] = { ...state.threads[idx], ...thread };
  state.threads.sort(threadSort);
}

async function loadConversationsFromApi() {
  const payload = await apiRequest("/v1/conversations", { method: "GET" });
  const items = Array.isArray(payload?.items) ? payload.items : [];
  for (const item of items) {
    const mapped = mapConversation(item);
    const existing = getThreadById(mapped.id);
    if (existing) {
      upsertThread({ ...mapped, messages: existing.messages, loadedMessages: existing.loadedMessages, nickname: existing.nickname, blocked: existing.blocked, closed: existing.closed });
    } else {
      upsertThread(mapped);
    }
  }
  if (!state.activeThreadId && visibleThreads().length > 0) state.activeThreadId = visibleThreads()[0].id;
  saveConversationStore();
}

async function loadMessagesForThread(threadId) {
  const thread = getThreadById(threadId);
  if (!thread || thread.kind === "draft_phone") return;
  const payload = await apiRequest(`/v1/conversations/${encodeURIComponent(threadId)}/messages`, { method: "GET" });
  const items = Array.isArray(payload?.items) ? payload.items : [];
  const messages = items.map(mapMessage);
  upsertThread({ ...thread, messages, loadedMessages: true, updatedAt: messages.length ? messages[messages.length - 1].createdAt : thread.updatedAt });
  const refreshedThread = getThreadById(threadId) || thread;
  await markConversationRead(refreshedThread);
  saveConversationStore();
}

async function refreshLiveState() {
  if (!state.auth || liveSyncInFlight) return;
  liveSyncInFlight = true;
  try {
    await loadConversationsFromApi();
    const active = getActiveThread();
    if (active && active.kind !== "draft_phone") {
      await loadMessagesForThread(active.id);
    }
    renderAll();
  } catch (error) {
    console.error(error);
  } finally {
    liveSyncInFlight = false;
  }
}

function stopEventStream() {
  if (eventStreamReconnectTimer) {
    window.clearTimeout(eventStreamReconnectTimer);
    eventStreamReconnectTimer = 0;
  }
  if (eventStreamAbort) {
    eventStreamAbort.abort();
    eventStreamAbort = null;
  }
}

function scheduleEventStreamReconnect(delayMs = 1500) {
  if (!state.auth || eventStreamReconnectTimer || eventStreamDisabled) return;
  eventStreamReconnectTimer = window.setTimeout(() => {
    eventStreamReconnectTimer = 0;
    void startEventStream();
  }, delayMs);
}

function handleSSEEvent(name, rawData) {
  if (name === "message_created" || name === "delivery_update") {
    try {
      handleRealtimeEvent(name, rawData ? JSON.parse(rawData) : null);
    } catch (error) {
      console.error(error);
    }
    return;
  }
  if (name === "sync_required") {
    void refreshLiveState();
  }
}

async function startEventStream() {
  if (!state.auth || eventStreamAbort || eventStreamDisabled) return;
  const controller = new AbortController();
  eventStreamAbort = controller;
  try {
    const response = await fetch(`${API_BASE_URL}/v1/events/stream`, {
      method: "GET",
      headers: {
        Accept: "text/event-stream",
        Authorization: `Bearer ${state.auth.accessToken}`,
      },
      signal: controller.signal,
      credentials: "omit",
      cache: "no-store",
    });
    if (response.status === 401 && state.auth?.refreshToken) {
      const refreshed = await refreshAuthTokens();
      if (refreshed) {
        eventStreamAbort = null;
        scheduleEventStreamReconnect(200);
        return;
      }
    }
    if (response.status >= 500) {
      eventStreamDisabled = true;
      console.warn("Event stream unavailable; realtime updates disabled for this session.");
      return;
    }
    if (!response.ok || !response.body) {
      throw new Error(`stream_http_${response.status}`);
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let eventName = "";
    let dataLines = [];

    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split(/\r?\n/);
      buffer = lines.pop() || "";

      for (const line of lines) {
        if (!line) {
          const payload = dataLines.join("\n");
          handleSSEEvent(eventName || "message", payload);
          eventName = "";
          dataLines = [];
          continue;
        }
        if (line.startsWith(":")) continue;
        if (line.startsWith("event:")) {
          eventName = line.slice(6).trim();
          continue;
        }
        if (line.startsWith("data:")) {
          dataLines.push(line.slice(5).trim());
        }
      }
    }
    if (!controller.signal.aborted) {
      scheduleEventStreamReconnect();
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      console.error(error);
      if (!eventStreamDisabled) {
        scheduleEventStreamReconnect();
      }
    }
  } finally {
    if (eventStreamAbort === controller) {
      eventStreamAbort = null;
    }
  }
}

async function ensureMessagesLoaded(threadId) {
  const thread = getThreadById(threadId);
  if (!thread || thread.kind === "draft_phone" || thread.loadedMessages) return;
  await loadMessagesForThread(threadId);
}

async function waitForOutgoingStatus(threadId, messageId, currentStatus, attempts = 8, delayMs = 900) {
  if (!threadId || !messageId) return;
  const baseline = normalizeDeliveryStatus(TRANSPORT_OHMF, currentStatus);
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    await loadMessagesForThread(threadId);
    const thread = getThreadById(threadId);
    const message = (thread?.messages || []).find((item) => item.id === messageId);
    const nextStatus = normalizeDeliveryStatus(message?.transport || TRANSPORT_OHMF, message?.status || "");
    if (nextStatus && nextStatus !== baseline && nextStatus !== OHMF_DELIVERY_STATUSES.SENT) {
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, delayMs));
  }
}

function stopRealtimeSocket() {
  if (realtimeReconnectTimer) {
    window.clearTimeout(realtimeReconnectTimer);
    realtimeReconnectTimer = 0;
  }
  if (realtimeSocket) {
    realtimeSocket.close();
    realtimeSocket = null;
  }
}

function scheduleRealtimeReconnect(delayMs = 1200) {
  if (!state.auth || realtimeReconnectTimer || realtimeSocket) return;
  realtimeReconnectTimer = window.setTimeout(() => {
    realtimeReconnectTimer = 0;
    startRealtimeSocket();
  }, delayMs);
}

function applyDeliveryUpdate(payload) {
  const conversationId = sanitizeText(payload?.conversation_id, 80);
  const status = normalizeDeliveryStatus(TRANSPORT_OHMF, payload?.status || "");
  if (!conversationId || !status) return;
  if (status === OHMF_DELIVERY_STATUSES.READ) {
    const through = Number(payload?.through_server_order || 0);
    const thread = getThreadById(conversationId);
    if (!thread) return;
    const nextMessages = (thread.messages || []).map((message) => {
      if (message.direction !== "out" || Number(message.serverOrder || 0) > through) return message;
      return {
        ...message,
        status: OHMF_DELIVERY_STATUSES.READ,
        readAt: payload?.status_updated_at || nowISO(),
        statusUpdatedAt: payload?.status_updated_at || nowISO(),
      };
    });
    upsertThread({ ...thread, messages: nextMessages, updatedAt: payload?.status_updated_at || thread.updatedAt });
    saveConversationStore();
    renderAll();
    return;
  }

  const messageId = sanitizeText(payload?.message_id, 80);
  if (!messageId) return;
  patchMessage(conversationId, messageId, {
    status,
    transport: TRANSPORT_OHMF,
    deliveredAt: payload?.status_updated_at || nowISO(),
    statusUpdatedAt: payload?.status_updated_at || nowISO(),
  });
  renderAll();
}

function handleRealtimeEvent(eventName, payload) {
  if (eventName === "message_created") {
    applyIncomingMessage(payload);
    return;
  }
  if (eventName === "delivery_update") {
    applyDeliveryUpdate(payload);
  }
}

function applyIncomingMessage(payload) {
  const conversationId = sanitizeText(payload?.conversation_id, 80);
  if (!conversationId) return;
  const nextMessage = mapMessage(payload);
  const existingThread = getThreadById(conversationId);
  if (existingThread) {
    upsertThreadMessage(conversationId, nextMessage);
    if (state.activeThreadId === conversationId) {
      void loadMessagesForThread(conversationId);
    } else {
      renderAll();
    }
    return;
  }
  void (async () => {
    await loadConversationsFromApi();
    await loadMessagesForThread(conversationId);
    renderAll();
  })();
}

function startRealtimeSocket() {
  if (!state.auth || realtimeSocket) return;
  const wsURL = new URL(API_BASE_URL.replace(/^http/i, "ws") + "/v1/ws");
  wsURL.searchParams.set("access_token", state.auth.accessToken);
  const socket = new window.WebSocket(wsURL.toString());
  realtimeSocket = socket;

  socket.addEventListener("message", (event) => {
    try {
      const message = JSON.parse(event.data);
      handleRealtimeEvent(sanitizeText(message?.event, 80), message?.data);
    } catch (error) {
      console.error(error);
    }
  });

  socket.addEventListener("close", () => {
    if (realtimeSocket === socket) {
      realtimeSocket = null;
      scheduleRealtimeReconnect();
    }
  });

  socket.addEventListener("error", () => {
    socket.close();
  });
}

async function markConversationRead(thread) {
  const lastIncoming = [...(thread.messages || [])].reverse().find((msg) => msg.direction === "in" && msg.serverOrder > 0);
  if (!lastIncoming) return;
  try {
    await apiRequest(`/v1/conversations/${encodeURIComponent(thread.id)}/read`, {
      method: "POST",
      body: JSON.stringify({ through_server_order: lastIncoming.serverOrder }),
    });
  } catch {
    // Best effort.
  }
}

function buildThreadItem(thread) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `thread-item${thread.id === state.activeThreadId ? " active" : ""}`;

  const avatar = document.createElement("div");
  avatar.className = "avatar";
  avatar.textContent = initials(thread.title);

  const body = document.createElement("div");
  const meta = document.createElement("div");
  meta.className = "thread-meta";

  const name = document.createElement("p");
  name.className = "thread-name";
  name.textContent = thread.title;

  const time = document.createElement("p");
  time.className = "thread-time";
  time.textContent = formatShortTime(thread.updatedAt);

  const preview = document.createElement("p");
  preview.className = "thread-preview";
  const last = thread.messages?.[thread.messages.length - 1];
  preview.textContent = last ? (last.deleted ? "Message deleted" : last.text) : "No messages yet";

  meta.append(name, time);
  body.append(meta, preview);
  button.append(avatar, body);
  button.addEventListener("click", async () => {
    state.activeThreadId = thread.id;
    renderAll();
    openMobileThread();
    try {
      await ensureMessagesLoaded(thread.id);
      renderAll();
    } catch (error) {
      console.error(error);
    }
  });
  return button;
}

function renderThreadList() {
  el.threadList.replaceChildren();
  for (const thread of visibleThreads()) {
    const li = document.createElement("li");
    li.appendChild(buildThreadItem(thread));
    el.threadList.appendChild(li);
  }
}

function isNonSMSMessage(message) {
  return normalizeTransport(message.transport) !== TRANSPORT_SMS;
}

function addReaction(threadId, messageId) {
  const emoji = sanitizeText(window.prompt("Reaction emoji", "👍"), 4);
  if (!emoji) return;
  const thread = getThreadById(threadId);
  if (!thread) return;
  const nextMessages = (thread.messages || []).map((msg) => {
    if (msg.id !== messageId) return msg;
    const reactions = { ...(msg.reactions || {}) };
    reactions[emoji] = Number(reactions[emoji] || 0) + 1;
    return { ...msg, reactions };
  });
  upsertThread({ ...thread, messages: nextMessages });
  saveConversationStore();
  renderAll();
}

function editMessage(threadId, messageId) {
  const thread = getThreadById(threadId);
  const msg = thread?.messages?.find((m) => m.id === messageId);
  if (!thread || !msg || msg.deleted) return;
  const nextText = sanitizeText(window.prompt("Edit message", msg.text), 1000);
  if (!nextText) return;
  const nextMessages = thread.messages.map((m) => (m.id === messageId ? { ...m, text: nextText, editedAt: nowISO() } : m));
  upsertThread({ ...thread, messages: nextMessages, updatedAt: nowISO() });
  saveConversationStore();
  renderAll();
}

function deleteMessage(threadId, messageId) {
  const thread = getThreadById(threadId);
  if (!thread) return;
  const nextMessages = thread.messages.map((m) => (m.id === messageId ? { ...m, deleted: true, text: "Message deleted", editedAt: nowISO() } : m));
  upsertThread({ ...thread, messages: nextMessages, updatedAt: nowISO() });
  saveConversationStore();
  renderAll();
}

function scrollMessageListToBottom() {
  el.messageList.scrollTop = el.messageList.scrollHeight;
}

function closeMiniappLauncher() {
  state.miniapp.drawerOpen = false;
  state.miniapp.frameWindow = null;
  state.miniapp.consentRequired = false;
  state.miniapp.lastShareError = "";
  el.miniappFrame.src = "about:blank";
}

function renderMiniappLauncher() {
  const thread = getActiveThread();
  const supported = activeThreadSupportsMiniapps(thread);
  const supportReason = miniappSupportReason(thread);
  const joinable = state.miniapp.launchContext?.joinable !== false;
  el.attachBtn.disabled = false;
  el.attachBtn.title = supported ? "Open apps and attachments" : supportReason;
  el.miniappLauncher.classList.toggle("hidden", !state.miniapp.drawerOpen);
  el.miniappStage.classList.toggle("hidden", !state.miniapp.selectedAppId);
  el.miniappCounterCard.classList.toggle("active", state.miniapp.selectedAppId === "app.ohmf.counter-lab");

  if (!state.miniapp.drawerOpen) {
    el.miniappContextCopy.textContent = supported
      ? "Open the attachment menu to choose an app."
      : supportReason;
    el.miniappLaunchMode.textContent = "Ready";
    el.miniappShareBtn.disabled = true;
    return;
  }

  const manifest = state.miniapp.manifest;
  el.miniappPreviewTitle.textContent = manifest?.name || "App Preview";
  el.miniappPreviewSubtitle.textContent = thread
    ? `Prepare ${manifest?.name || "the app"} for ${thread.title}.`
    : "Choose launch settings, then open the app.";
  el.miniappLaunchMode.textContent = state.miniapp.launchContext?.app_session_id
    ? (!joinable ? "Session ended" : (state.miniapp.consentRequired ? "Consent required" : "Attached to thread"))
    : "Needs launch";
  el.miniappContextCopy.textContent = state.miniapp.lastShareError || (supported
    ? `App sessions attach to ${thread.title}. Permissions can be adjusted before launch.`
    : supportReason);

  el.miniappPermissions.replaceChildren();
  const permissions = Array.isArray(manifest?.permissions) ? manifest.permissions : [];
  for (const permission of permissions) {
    const item = document.createElement("label");
    item.className = "miniapp-permission";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = state.miniapp.grantedPermissions.has(permission);
    input.disabled = !supported || !joinable;
    input.addEventListener("change", () => {
      if (input.checked) state.miniapp.grantedPermissions.add(permission);
      else state.miniapp.grantedPermissions.delete(permission);
    });
    const copy = document.createElement("span");
    copy.textContent = permission;
    item.append(input, copy);
    el.miniappPermissions.append(item);
  }
  el.miniappShareBtn.disabled = !supported || !manifest || !joinable;
  el.miniappShareBtn.textContent = state.miniapp.launchContext?.app_session_id ? "Send Again" : "Send App";
  el.miniappOpenBtn.disabled = !supported || !manifest || !joinable;
  el.miniappOpenBtn.textContent = !joinable ? "Session Ended" : (state.miniapp.consentRequired ? "Join & Open" : (state.miniapp.launchContext?.app_session_id ? "Resume App" : "Open App"));
  el.miniappResetBtn.disabled = !state.miniapp.launchContext?.app_session_id;
}

function buildAppCardBubble(message) {
  const bubble = document.createElement("article");
  bubble.className = `bubble ${message.direction} app-card-bubble`;
  const joinable = message.content?.joinable !== false;

  const title = document.createElement("strong");
  title.className = "app-card-title";
  title.textContent = sanitizeText(message.content?.title || message.text || "Shared app", 120);

  const summary = document.createElement("p");
  summary.className = "app-card-summary";
  summary.textContent = sanitizeText(message.content?.summary || "Open this shared app in the conversation.", 180);

  bubble.append(title, summary);

  const cta = document.createElement("button");
  cta.type = "button";
  cta.className = "secondary-btn compact app-card-open-btn";
  cta.textContent = joinable ? sanitizeText(message.content?.cta_label || "Open", 32) : "Ended";
  cta.disabled = !joinable;
  cta.addEventListener("click", async () => {
    try {
      await openMiniappCard(message);
    } catch (error) {
      console.error(error);
      state.miniapp.lastShareError = sanitizeText(error.message || "Unable to open app card.", 180);
      renderMiniappLauncher();
    }
  });
  bubble.appendChild(cta);

  return bubble;
}

function renderMessages() {
  const thread = getActiveThread();
  const blocked = Boolean(thread?.blocked);
  el.nicknameBtn.disabled = !thread;
  el.blockBtn.disabled = !thread;
  el.closeThreadBtn.disabled = !thread;
  el.blockBtn.textContent = blocked ? "Unblock" : "Block";

  if (!thread) {
    el.title.textContent = "Select a conversation";
    el.subtitle.textContent = "No active thread";
    el.composerInput.disabled = true;
    closeMiniappLauncher();
    renderMiniappLauncher();
    el.messageList.replaceChildren(el.emptyState);
    return;
  }

  el.title.textContent = thread.title;
  el.subtitle.textContent = thread.subtitle;
  el.composerInput.disabled = blocked;
  el.composerInput.placeholder = blocked ? "Unblock this user to send messages" : "Message";
  renderMiniappLauncher();
  el.messageList.replaceChildren();

  for (const message of thread.messages || []) {
    const wrap = document.createElement("div");
    wrap.className = `bubble-wrap ${message.direction}`;

    const bubble = isAppCardMessage(message)
      ? buildAppCardBubble(message)
      : (() => {
          const plainBubble = document.createElement("article");
          plainBubble.className = `bubble ${message.direction}`;
          plainBubble.textContent = message.deleted ? "Message deleted" : message.text;
          return plainBubble;
        })();

    const meta = document.createElement("p");
    meta.className = `bubble-meta ${message.direction}`;
    const stamp = formatShortTime(message.createdAt);
    const edited = message.editedAt ? "edited" : "";
    const leftMeta = document.createElement("span");
    leftMeta.textContent = stamp;
    meta.appendChild(leftMeta);

    if (message.direction === "out") {
      const normalizedStatus = normalizeDeliveryStatus(message.transport, message.status);
      if (normalizedStatus) {
        const statusNode = document.createElement("span");
        statusNode.className = `delivery-status status-${normalizedStatus.toLowerCase().replace(/_/g, "-")}`;
        const ohmfLabel = deliveryIndicatorLabel(message);
        statusNode.textContent = ohmfLabel || normalizedStatus;
        meta.appendChild(statusNode);
      }
    }

    if (edited) {
      const editedNode = document.createElement("span");
      editedNode.textContent = edited;
      meta.appendChild(editedNode);
    }

    wrap.append(bubble, meta);

    const nonSMS = isNonSMSMessage(message);
    if (nonSMS) {
      const reactionKeys = Object.keys(message.reactions || {});
      if (reactionKeys.length) {
        const reactionMeta = document.createElement("p");
        reactionMeta.className = `bubble-meta ${message.direction}`;
        reactionMeta.textContent = reactionKeys.map((k) => `${k} ${message.reactions[k]}`).join("  ");
        wrap.appendChild(reactionMeta);
      }

      const actions = document.createElement("div");
      actions.className = "bubble-actions";

      const reactBtn = document.createElement("button");
      reactBtn.type = "button";
      reactBtn.textContent = "React";
      reactBtn.addEventListener("click", () => addReaction(thread.id, message.id));
      actions.appendChild(reactBtn);

      if (message.direction === "out" && !message.deleted && !isAppCardMessage(message)) {
        const editBtn = document.createElement("button");
        editBtn.type = "button";
        editBtn.textContent = "Edit";
        editBtn.addEventListener("click", () => editMessage(thread.id, message.id));
        actions.appendChild(editBtn);

        const deleteBtn = document.createElement("button");
        deleteBtn.type = "button";
        deleteBtn.textContent = "Delete";
        deleteBtn.addEventListener("click", () => deleteMessage(thread.id, message.id));
        actions.appendChild(deleteBtn);
      }

      wrap.appendChild(actions);
    }

    el.messageList.appendChild(wrap);
  }

  if (state.typingDraft && !thread.blocked) {
    const typingWrap = document.createElement("div");
    typingWrap.className = "bubble-wrap out";
    const typingBubble = document.createElement("article");
    typingBubble.className = "bubble out typing";
    typingBubble.textContent = "Typing...";
    typingWrap.appendChild(typingBubble);
    el.messageList.appendChild(typingWrap);
  }

  requestAnimationFrame(scrollMessageListToBottom);
}

function renderAll() {
  renderThreadList();
  renderMessages();
}

function openMobileThread() {
  if (window.matchMedia("(max-width: 880px)").matches) el.appShell.classList.add("mobile-chat-open");
}

function closeMobileThread() {
  el.appShell.classList.remove("mobile-chat-open");
}

function showAppShell() {
  el.authShell.classList.add("hidden");
  el.appShell.classList.remove("hidden");
}

function showAuthShell() {
  el.appShell.classList.add("hidden");
  el.authShell.classList.remove("hidden");
}

function pushPendingMessage(threadId, text, transport = TRANSPORT_OHMF) {
  const thread = getThreadById(threadId);
  if (!thread) return null;
  const normalizedTransport = normalizeTransport(transport);
  const status = normalizedTransport === TRANSPORT_SMS ? SMS_DELIVERY_STATUSES.SENT : OHMF_DELIVERY_STATUSES.SENT;
  const pending = {
    id: `tmp-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    direction: "out",
    text,
    createdAt: nowISO(),
    status,
    statusUpdatedAt: nowISO(),
    transport: normalizedTransport,
    serverOrder: 0,
    reactions: {},
    editedAt: "",
    deleted: false,
  };
  upsertThread({ ...thread, messages: [...(thread.messages || []), pending], updatedAt: pending.createdAt });
  saveConversationStore();
  return pending.id;
}

function patchMessage(threadId, messageId, patch) {
  const thread = getThreadById(threadId);
  if (!thread) return;
  const nextMessages = (thread.messages || []).map((message) => {
    if (message.id !== messageId) return message;
    const merged = { ...message, ...patch };
    const transport = normalizeTransport(merged.transport);
    return {
      ...merged,
      transport,
      status: normalizeDeliveryStatus(transport, merged.status),
      statusUpdatedAt: patch.status ? (patch.statusUpdatedAt || nowISO()) : merged.statusUpdatedAt,
    };
  });
  upsertThread({ ...thread, messages: nextMessages, updatedAt: patch.createdAt || thread.updatedAt });
  saveConversationStore();
}

async function createOrGetPhoneConversation(phone) {
  const payload = await apiRequest("/v1/conversations/phone", {
    method: "POST",
    body: JSON.stringify({ phone_e164: phone }),
  });
  const mapped = mapConversation(payload);
  const existing = state.threads.find((t) => t.id === mapped.id);
  if (existing) {
    upsertThread({ ...existing, ...mapped });
  } else {
    upsertThread(mapped);
  }
  return mapped.id;
}

function moveDraftToConversation(draftId, conversationId, phone) {
  const draft = getThreadById(draftId);
  const existing = getThreadById(conversationId);
  const carried = draft?.messages || [];
  const merged = [...(existing?.messages || []), ...carried];
  state.threads = state.threads.filter((t) => t.id !== draftId);
  upsertThread({
    ...(existing || {}),
    id: conversationId,
    kind: "phone",
    title: existing?.title || phone,
    subtitle: existing?.subtitle || "Phone conversation (OTT preferred)",
    externalPhones: [phone],
    participants: existing?.participants || [state.auth.userId],
    messages: merged,
    loadedMessages: Boolean(existing?.loadedMessages),
    updatedAt: nowISO(),
  });
  state.activeThreadId = conversationId;
}

async function sendOTT(conversationId, text) {
  return apiRequest("/v1/messages", {
    method: "POST",
    body: JSON.stringify({
      conversation_id: conversationId,
      idempotency_key: makeIdempotencyKey("conv"),
      content_type: "text",
      content: { text },
    }),
  });
}

async function sendSMS(phone, text) {
  return apiRequest("/v1/messages/phone", {
    method: "POST",
    body: JSON.stringify({
      phone_e164: phone,
      idempotency_key: makeIdempotencyKey("phone"),
      content_type: "text",
      content: { text },
    }),
  });
}

async function sendInConversation(thread, text) {
  const pendingId = pushPendingMessage(thread.id, text, TRANSPORT_OHMF);
  renderAll();
  try {
    const payload = await sendOTT(thread.id, text);
    const finalMessageId = sanitizeText(payload.message_id, 80) || pendingId;
    patchMessage(thread.id, pendingId, {
      id: finalMessageId,
      serverOrder: Number(payload.server_order || 0),
      status: OHMF_DELIVERY_STATUSES.SENT,
      transport: TRANSPORT_OHMF,
      createdAt: nowISO(),
      statusUpdatedAt: nowISO(),
    });
    await waitForOutgoingStatus(thread.id, finalMessageId, OHMF_DELIVERY_STATUSES.SENT);
  } catch (err) {
    const phone = thread.externalPhones?.[0];
    if (thread.kind !== "phone" || !phone) {
      patchMessage(thread.id, pendingId, { status: OHMF_DELIVERY_STATUSES.FAIL_SEND });
      throw err;
    }

    try {
      const smsPayload = await sendSMS(phone, text);
      patchMessage(thread.id, pendingId, {
        id: sanitizeText(smsPayload.message_id, 80) || pendingId,
        serverOrder: Number(smsPayload.server_order || 0),
        status: SMS_DELIVERY_STATUSES.SENT,
        transport: TRANSPORT_SMS,
        createdAt: nowISO(),
        statusUpdatedAt: nowISO(),
      });
      await loadMessagesForThread(thread.id);
    } catch (smsErr) {
      patchMessage(thread.id, pendingId, { status: SMS_DELIVERY_STATUSES.FAIL_SEND, transport: TRANSPORT_SMS });
      throw smsErr;
    }
  }
}

function ensureDraftPhoneThread(phone) {
  const existing = state.threads.find((thread) => thread.kind === "draft_phone" && thread.externalPhones?.[0] === phone);
  if (existing) {
    state.activeThreadId = existing.id;
    return existing;
  }
  const draft = {
    id: `draft:${phone}`,
    kind: "draft_phone",
    title: phone,
    subtitle: "New phone conversation",
    nickname: "",
    blocked: false,
    closed: false,
    updatedAt: nowISO(),
    participants: [state.auth?.userId || ""],
    externalPhones: [phone],
    messages: [],
    loadedMessages: true,
  };
  upsertThread(draft);
  state.activeThreadId = draft.id;
  saveConversationStore();
  return draft;
}

async function sendInDraftPhoneConversation(thread, text) {
  const phone = thread.externalPhones?.[0] || "";
  const pendingId = pushPendingMessage(thread.id, text, TRANSPORT_OHMF);
  renderAll();
  try {
    const conversationId = await createOrGetPhoneConversation(phone);
    moveDraftToConversation(thread.id, conversationId, phone);
    const payload = await sendOTT(conversationId, text);
    const finalMessageId = sanitizeText(payload.message_id, 80) || pendingId;
    patchMessage(conversationId, pendingId, {
      id: finalMessageId,
      serverOrder: Number(payload.server_order || 0),
      status: OHMF_DELIVERY_STATUSES.SENT,
      transport: TRANSPORT_OHMF,
      createdAt: nowISO(),
      statusUpdatedAt: nowISO(),
    });
    await waitForOutgoingStatus(conversationId, finalMessageId, OHMF_DELIVERY_STATUSES.SENT);
  } catch {
    try {
      const smsPayload = await sendSMS(phone, text);
      const conversationId = sanitizeText(smsPayload.conversation_id, 80);
      if (conversationId) moveDraftToConversation(thread.id, conversationId, phone);
      const targetThreadId = conversationId || thread.id;
      patchMessage(targetThreadId, pendingId, {
        id: sanitizeText(smsPayload.message_id, 80) || pendingId,
        serverOrder: Number(smsPayload.server_order || 0),
        status: SMS_DELIVERY_STATUSES.SENT,
        transport: TRANSPORT_SMS,
        createdAt: nowISO(),
        statusUpdatedAt: nowISO(),
      });
      if (conversationId) await loadMessagesForThread(conversationId);
    } catch (error) {
      patchMessage(thread.id, pendingId, { status: SMS_DELIVERY_STATUSES.FAIL_SEND, transport: TRANSPORT_SMS });
      throw error;
    }
  }
}

async function handleComposerSend(text) {
  const thread = getActiveThread();
  if (!thread || thread.blocked) return;
  if (thread.kind === "draft_phone") await sendInDraftPhoneConversation(thread, text);
  else await sendInConversation(thread, text);
}

function updatePhonePreview() {
  el.phoneInput.value = formatPhoneLocal(el.phoneInput.value);
  const preview = toE164(el.countryCodeSelect.value, el.phoneInput.value);
  el.phoneE164Preview.textContent = preview ? `Will send OTP to ${preview}` : "Enter at least 8 digits including country code";
}

function updateNewPhoneFormat() {
  el.newPhoneInput.value = formatPhoneLocal(el.newPhoneInput.value);
}

async function startPhoneAuth(event) {
  event.preventDefault();
  const phone = toE164(el.countryCodeSelect.value, el.phoneInput.value);
  if (!phone) {
    setAuthStatus("Enter a valid phone number.", true);
    return;
  }
  setAuthStatus("Requesting OTP...");
  try {
    const payload = await apiRequest("/v1/auth/phone/start", {
      method: "POST",
      body: JSON.stringify({ phone_e164: phone, channel: "SMS" }),
    });
    state.challengeId = sanitizeText(payload.challenge_id, 80);
    el.phoneVerifyForm.classList.remove("hidden");
    setAuthStatus("OTP sent. Enter code to continue.");
  } catch (error) {
    setAuthStatus(`OTP start failed: ${error.message}`, true);
  }
}

async function verifyPhoneAuth(event) {
  event.preventDefault();
  const otp = sanitizeText(el.otpInput.value, 8);
  if (!state.challengeId || otp.length < 4) {
    setAuthStatus("Challenge and OTP are required.", true);
    return;
  }
  setAuthStatus("Verifying...");
  try {
    const payload = await apiRequest("/v1/auth/phone/verify", {
      method: "POST",
      body: JSON.stringify({
        challenge_id: state.challengeId,
        otp_code: otp,
        device: { platform: "WEB", device_name: "OHMF Web", capabilities: ["MINI_APPS"] },
      }),
    });
    const user = payload?.user || {};
    const tokens = payload?.tokens || {};
    if (!tokens.access_token || !tokens.refresh_token || !user.user_id) throw new Error("invalid_auth_response");
    authStoreSet({
      userId: sanitizeText(user.user_id, 80),
      phoneE164: sanitizeText(user.primary_phone_e164, 32),
      accessToken: sanitizeText(tokens.access_token, 3000),
      refreshToken: sanitizeText(tokens.refresh_token, 3000),
    });
    state.challengeId = "";
    el.phoneVerifyForm.classList.add("hidden");
    el.phoneStartForm.reset();
    el.phoneVerifyForm.reset();
    updatePhonePreview();
    await bootAfterAuth();
  } catch (error) {
    setAuthStatus(`Verify failed: ${error.message}`, true);
  }
}

async function bootAfterAuth() {
  state.query = "";
  state.activeThreadId = null;
  state.threads = [];
  eventStreamDisabled = false;
  loadConversationStore();
  showAppShell();
  renderAll();
  try {
    await loadConversationsFromApi();
    if (state.activeThreadId) await ensureMessagesLoaded(state.activeThreadId);
    stopEventStream();
    stopRealtimeSocket();
    void startEventStream();
    startRealtimeSocket();
    renderAll();
  } catch (error) {
    console.error(error);
  }
}

async function selectMiniapp(appId) {
  const entry = getMiniappCatalogEntry(appId);
  if (!entry) return;
  state.miniapp.selectedAppId = appId;
  state.miniapp.launchContext = null;
  state.miniapp.sessionState = normalizeMiniappSessionState(null);
  state.miniapp.frameWindow = null;
  state.miniapp.consentRequired = false;
  state.miniapp.lastShareError = "";
  el.miniappFrame.src = "about:blank";
  try {
    const manifest = await fetchMiniappManifest(entry.manifestUrl);
    state.miniapp.manifest = manifest;
    state.miniapp.grantedPermissions = new Set(Array.isArray(manifest.permissions) ? manifest.permissions : []);
  } catch (error) {
    console.error(error);
  }
  renderMiniappLauncher();
}

async function openEmbeddedMiniapp() {
  try {
    if (state.miniapp.launchContext?.joinable === false) {
      throw new Error("This shared app session has ended.");
    }
    if (state.miniapp.launchContext?.app_session_id) {
      if (state.miniapp.consentRequired) {
        await joinMiniappSession(state.miniapp.launchContext.app_session_id);
      } else {
        await fetchMiniappSession(state.miniapp.launchContext.app_session_id);
      }
    } else {
      await ensureMiniappSession();
    }
    el.miniappFrame.setAttribute("sandbox", "allow-scripts allow-same-origin");
    el.miniappFrame.src = buildMiniappFrameURL();
    renderMiniappLauncher();
  } catch (error) {
    console.error(error);
    state.miniapp.lastShareError = sanitizeText(error.message || "Unable to open app.", 180);
    renderMiniappLauncher();
  }
}

async function resetEmbeddedMiniappSession() {
  if (!state.miniapp.launchContext?.app_session_id) return;
  try {
    await apiRequest(`/v1/apps/sessions/${encodeURIComponent(state.miniapp.launchContext.app_session_id)}`, { method: "DELETE" });
  } catch (error) {
    console.error(error);
  }
  state.miniapp.launchContext = null;
  state.miniapp.sessionState = normalizeMiniappSessionState(null);
  state.miniapp.frameWindow = null;
  state.miniapp.consentRequired = false;
  state.miniapp.lastShareError = "";
  el.miniappFrame.src = "about:blank";
  renderMiniappLauncher();
}

function logout() {
  stopEventStream();
  stopRealtimeSocket();
  authStoreClear();
  state.threads = [];
  state.activeThreadId = null;
  state.query = "";
  state.typingDraft = "";
  state.miniapp.drawerOpen = false;
  state.miniapp.selectedAppId = "";
  state.miniapp.manifest = null;
  state.miniapp.launchContext = null;
  state.miniapp.sessionState = null;
  state.miniapp.frameWindow = null;
  state.miniapp.consentRequired = false;
  state.miniapp.lastShareError = "";
  showAuthShell();
  setAuthStatus("Signed out.");
  renderAll();
}

el.searchInput.addEventListener("input", (event) => {
  state.query = sanitizeText(event.target.value, 120);
  renderThreadList();
});

el.attachBtn.addEventListener("click", async () => {
  state.miniapp.drawerOpen = !state.miniapp.drawerOpen;
  if (state.miniapp.drawerOpen && !state.miniapp.selectedAppId) {
    await selectMiniapp("app.ohmf.counter-lab");
  }
  renderMiniappLauncher();
});

el.miniappCloseBtn.addEventListener("click", () => {
  closeMiniappLauncher();
  renderMiniappLauncher();
});

el.miniappCounterCard.addEventListener("click", async () => {
  await selectMiniapp("app.ohmf.counter-lab");
});

el.miniappShareBtn.addEventListener("click", async () => {
  try {
    await shareMiniappToConversation();
  } catch (error) {
    console.error(error);
    state.miniapp.lastShareError = sanitizeText(error.message || "Unable to share app.", 180);
    renderMiniappLauncher();
  }
});

el.miniappOpenBtn.addEventListener("click", async () => {
  await openEmbeddedMiniapp();
});

el.miniappResetBtn.addEventListener("click", async () => {
  await resetEmbeddedMiniappSession();
});

el.composerInput.addEventListener("input", () => {
  state.typingDraft = sanitizeText(el.composerInput.value, 1000);
  renderMessages();
});

el.composer.addEventListener("submit", async (event) => {
  event.preventDefault();
  const text = sanitizeText(el.composerInput.value, 1000);
  if (!text) return;
  el.composerInput.value = "";
  state.typingDraft = "";
  renderMessages();
  try {
    await handleComposerSend(text);
  } catch (error) {
    console.error(error);
  }
  renderAll();
});

el.backBtn.addEventListener("click", () => closeMobileThread());

el.newChatBtn.addEventListener("click", () => {
  el.newChatForm.classList.toggle("hidden");
  if (!el.newChatForm.classList.contains("hidden")) el.newPhoneInput.focus();
});

el.newChatForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const phone = toE164(el.newCountryCodeSelect.value, el.newPhoneInput.value);
  if (!phone) {
    el.newPhoneInput.focus();
    return;
  }
  ensureDraftPhoneThread(phone);
  el.newChatForm.classList.add("hidden");
  el.newPhoneInput.value = "";
  renderAll();
  openMobileThread();
});

el.logoutBtn.addEventListener("click", async () => {
  try {
    await apiRequest("/v1/auth/logout", { method: "POST", body: JSON.stringify({}) });
  } catch {
    // Best effort logout.
  }
  logout();
});

el.nicknameBtn.addEventListener("click", () => {
  const thread = getActiveThread();
  if (!thread) return;
  const nickname = sanitizeText(window.prompt("Set a nickname for this user", thread.nickname || thread.title), 80);
  if (!nickname) return;
  upsertThread({ ...thread, nickname });
  saveConversationStore();
  renderAll();
});

el.blockBtn.addEventListener("click", () => {
  const thread = getActiveThread();
  if (!thread) return;
  upsertThread({ ...thread, blocked: !thread.blocked });
  saveConversationStore();
  renderAll();
});

el.closeThreadBtn.addEventListener("click", () => {
  const thread = getActiveThread();
  if (!thread) return;
  upsertThread({ ...thread, closed: true });
  const remaining = visibleThreads();
  state.activeThreadId = remaining.length ? remaining[0].id : null;
  saveConversationStore();
  renderAll();
});

el.phoneStartForm.addEventListener("submit", startPhoneAuth);
el.phoneVerifyForm.addEventListener("submit", verifyPhoneAuth);
el.phoneInput.addEventListener("input", updatePhonePreview);
el.countryCodeSelect.addEventListener("change", updatePhonePreview);
el.newPhoneInput.addEventListener("input", updateNewPhoneFormat);

window.addEventListener("resize", () => {
  if (!window.matchMedia("(max-width: 880px)").matches) closeMobileThread();
});

window.addEventListener("message", async (event) => {
  if (event.source !== el.miniappFrame.contentWindow) return;
  if (!state.miniapp.manifest?.entrypoint?.url) return;
  const expectedOrigin = new URL(state.miniapp.manifest.entrypoint.url, window.location.href).origin;
  if (event.origin !== expectedOrigin) return;

  const message = event.data;
  if (!message || typeof message !== "object" || message.channel !== state.miniapp.channelId) return;
  state.miniapp.frameWindow = event.source;
  const requestId = sanitizeText(message.request_id, 80);
  if (!requestId) return;

  try {
    const result = await handleMiniappBridgeCall(message);
    sendMiniappBridgeResponse(event.source, requestId, true, result);
  } catch (error) {
    sendMiniappBridgeResponse(event.source, requestId, false, null, {
      code: sanitizeText(error.code || "bridge_error", 80),
      message: sanitizeText(error.message || "Bridge call failed", 220),
    });
  }
});

async function init() {
  updatePhonePreview();
  const session = authStoreLoad();
  if (session) {
    state.auth = session;
    await bootAfterAuth();
    return;
  }
  showAuthShell();
  setAuthStatus("");
  renderAll();
}

init();
