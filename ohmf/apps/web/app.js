"use strict";

const API_BASE_URL = (window.localStorage.getItem("ohmf.apiBaseUrl") || "http://localhost:18080").replace(/\/+$/, "");
const AUTH_STORAGE_KEY = "ohmf.auth.session.v1";
const STORE_VERSION = 1;

const state = {
  auth: null,
  challengeId: "",
  query: "",
  activeThreadId: null,
  threads: [],
};

const el = {
  authShell: document.getElementById("auth-shell"),
  appShell: document.getElementById("app-shell"),
  authStatus: document.getElementById("auth-status"),
  phoneStartForm: document.getElementById("phone-start-form"),
  phoneVerifyForm: document.getElementById("phone-verify-form"),
  phoneInput: document.getElementById("phone-input"),
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
  newPhoneInput: document.getElementById("new-phone-input"),
};

function nowISO() {
  return new Date().toISOString();
}

function formatShortTime(value) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function sanitizeText(value, limit = 1000) {
  return String(value || "")
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .trim()
    .slice(0, limit);
}

function normalizePhone(value) {
  const compact = String(value || "").replace(/[^\d+]/g, "");
  if (!/^\+\d{8,15}$/.test(compact)) {
    return "";
  }
  return compact;
}

function makeIdempotencyKey(prefix = "msg") {
  const rand = Math.random().toString(36).slice(2, 10);
  return `${prefix}-${Date.now()}-${rand}`;
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
    if (!parsed || !parsed.accessToken || !parsed.refreshToken || !parsed.userId) {
      return null;
    }
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
  const data = {
    version: STORE_VERSION,
    savedAt: nowISO(),
    threads: state.threads.map((thread) => ({
      id: thread.id,
      kind: thread.kind,
      title: thread.title,
      subtitle: thread.subtitle,
      updatedAt: thread.updatedAt,
      externalPhones: thread.externalPhones || [],
      participants: thread.participants || [],
      messages: thread.messages || [],
      loadedMessages: Boolean(thread.loadedMessages),
    })),
  };
  window.localStorage.setItem(conversationStoreKey(), JSON.stringify(data));
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
      updatedAt: thread.updatedAt || nowISO(),
      externalPhones: Array.isArray(thread.externalPhones) ? thread.externalPhones.map((p) => sanitizeText(p, 32)) : [],
      participants: Array.isArray(thread.participants) ? thread.participants.map((p) => sanitizeText(p, 80)) : [],
      messages: Array.isArray(thread.messages) ? thread.messages : [],
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
  const url = `${API_BASE_URL}${path}`;
  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");
  if (state.auth?.accessToken) {
    headers.set("Authorization", `Bearer ${state.auth.accessToken}`);
  }
  const response = await fetch(url, { ...options, headers, credentials: "omit" });

  if (response.status === 401 && allowRetry && state.auth?.refreshToken) {
    const refreshed = await refreshAuthTokens();
    if (refreshed) {
      return apiRequest(path, options, false);
    }
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
    const code = payload?.code || `http_${response.status}`;
    const message = payload?.message || "Request failed";
    const error = new Error(message);
    error.code = code;
    error.status = response.status;
    throw error;
  }
  return payload;
}

async function refreshAuthTokens() {
  if (!state.auth?.refreshToken) return false;
  try {
    const payload = await fetch(`${API_BASE_URL}/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: state.auth.refreshToken }),
      credentials: "omit",
    });
    if (!payload.ok) return false;
    const json = await payload.json();
    const tokens = json?.tokens;
    if (!tokens?.access_token || !tokens?.refresh_token) return false;
    authStoreSet({
      ...state.auth,
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
    });
    return true;
  } catch {
    return false;
  }
}

function pickTitle(conversation) {
  if (conversation.externalPhones?.length) {
    return conversation.externalPhones[0];
  }
  const others = (conversation.participants || []).filter((id) => id !== state.auth?.userId);
  if (others.length) {
    return `User ${others[0].slice(0, 8)}`;
  }
  return `Conversation ${conversation.id.slice(0, 8)}`;
}

function pickSubtitle(conversation) {
  if (conversation.kind === "phone") return "SMS thread";
  const others = (conversation.participants || []).filter((id) => id !== state.auth?.userId).length;
  if (others <= 0) return "Only you";
  return `${others} participant${others > 1 ? "s" : ""}`;
}

function mapConversation(item) {
  const kind = item.type === "PHONE_DM" || (Array.isArray(item.external_phones) && item.external_phones.length > 0) ? "phone" : "dm";
  const thread = {
    id: sanitizeText(item.conversation_id, 80),
    kind,
    title: "",
    subtitle: "",
    updatedAt: item.updated_at || nowISO(),
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
  const text = sanitizeText(item?.content?.text || JSON.stringify(item?.content || {}), 1000);
  const outgoing = item.sender_user_id === state.auth?.userId;
  return {
    id: sanitizeText(item.message_id, 80),
    direction: outgoing ? "out" : "in",
    text,
    createdAt: item.created_at || nowISO(),
    serverOrder: Number(item.server_order || 0),
    status: outgoing ? "SENT" : "",
  };
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
  if (!q) return [...state.threads].sort(threadSort);
  return state.threads
    .filter((thread) => {
      const combined = `${thread.title} ${thread.subtitle} ${(thread.messages || []).map((m) => m.text).join(" ")}`.toLowerCase();
      return combined.includes(q);
    })
    .sort(threadSort);
}

function upsertThread(thread) {
  const idx = state.threads.findIndex((item) => item.id === thread.id);
  if (idx === -1) {
    state.threads.push(thread);
  } else {
    state.threads[idx] = { ...state.threads[idx], ...thread };
  }
  state.threads.sort(threadSort);
}

async function loadConversationsFromApi() {
  const payload = await apiRequest("/v1/conversations", { method: "GET" });
  const items = Array.isArray(payload?.items) ? payload.items : [];
  for (const item of items) {
    const mapped = mapConversation(item);
    const existing = getThreadById(mapped.id);
    if (existing) {
      upsertThread({
        ...mapped,
        messages: existing.messages,
        loadedMessages: existing.loadedMessages,
      });
    } else {
      upsertThread(mapped);
    }
  }
  if (!state.activeThreadId && state.threads.length > 0) {
    state.activeThreadId = state.threads[0].id;
  }
  saveConversationStore();
}

async function loadMessagesForThread(threadId) {
  const thread = getThreadById(threadId);
  if (!thread || thread.kind === "draft_phone") return;
  const payload = await apiRequest(`/v1/conversations/${encodeURIComponent(threadId)}/messages`, { method: "GET" });
  const items = Array.isArray(payload?.items) ? payload.items : [];
  const messages = items.map(mapMessage);
  upsertThread({
    ...thread,
    messages,
    loadedMessages: true,
    updatedAt: messages.length ? messages[messages.length - 1].createdAt : thread.updatedAt,
  });
  saveConversationStore();
}

async function ensureMessagesLoaded(threadId) {
  const thread = getThreadById(threadId);
  if (!thread) return;
  if (thread.kind === "draft_phone") return;
  if (thread.loadedMessages) return;
  await loadMessagesForThread(threadId);
}

function buildThreadItem(thread) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `thread-item${thread.id === state.activeThreadId ? " active" : ""}`;
  button.dataset.threadId = thread.id;

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
  meta.append(name, time);

  const preview = document.createElement("p");
  preview.className = "thread-preview";
  const last = thread.messages?.[thread.messages.length - 1];
  preview.textContent = last ? last.text : "No messages yet";
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
  const threads = visibleThreads();
  for (const thread of threads) {
    const li = document.createElement("li");
    li.appendChild(buildThreadItem(thread));
    el.threadList.appendChild(li);
  }
}

function renderMessages() {
  const thread = getActiveThread();
  if (!thread) {
    el.title.textContent = "Select a conversation";
    el.subtitle.textContent = "No active thread";
    el.composerInput.disabled = true;
    el.messageList.replaceChildren(el.emptyState);
    return;
  }
  el.title.textContent = thread.title;
  el.subtitle.textContent = thread.subtitle;
  el.composerInput.disabled = false;
  el.messageList.replaceChildren();

  for (const message of thread.messages || []) {
    const wrap = document.createElement("div");
    wrap.className = `bubble-wrap ${message.direction}`;

    const bubble = document.createElement("article");
    bubble.className = `bubble ${message.direction}`;
    bubble.textContent = message.text;

    const meta = document.createElement("p");
    meta.className = `bubble-meta ${message.direction}`;
    const stamp = formatShortTime(message.createdAt);
    if (message.direction === "out") {
      meta.textContent = `${stamp} ${message.status || ""}`.trim();
    } else {
      meta.textContent = stamp;
    }

    wrap.append(bubble, meta);
    el.messageList.appendChild(wrap);
  }

  el.messageList.scrollTop = el.messageList.scrollHeight;
}

function renderAll() {
  renderThreadList();
  renderMessages();
}

function openMobileThread() {
  if (window.matchMedia("(max-width: 880px)").matches) {
    el.appShell.classList.add("mobile-chat-open");
  }
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

function pushPendingMessage(threadId, text) {
  const thread = getThreadById(threadId);
  if (!thread) return null;
  const pending = {
    id: `tmp-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    direction: "out",
    text,
    createdAt: nowISO(),
    status: "PENDING",
    serverOrder: 0,
  };
  const nextMessages = [...(thread.messages || []), pending];
  upsertThread({
    ...thread,
    messages: nextMessages,
    updatedAt: pending.createdAt,
  });
  saveConversationStore();
  return pending.id;
}

function patchMessage(threadId, messageId, patch) {
  const thread = getThreadById(threadId);
  if (!thread) return;
  const nextMessages = (thread.messages || []).map((message) => (message.id === messageId ? { ...message, ...patch } : message));
  upsertThread({
    ...thread,
    messages: nextMessages,
    updatedAt: patch.createdAt || thread.updatedAt,
  });
  saveConversationStore();
}

async function sendInConversation(thread, text) {
  const pendingId = pushPendingMessage(thread.id, text);
  renderAll();
  try {
    const payload = await apiRequest("/v1/messages", {
      method: "POST",
      body: JSON.stringify({
        conversation_id: thread.id,
        idempotency_key: makeIdempotencyKey("conv"),
        content_type: "text",
        content: { text },
      }),
    });
    patchMessage(thread.id, pendingId, {
      id: sanitizeText(payload.message_id, 80) || pendingId,
      serverOrder: Number(payload.server_order || 0),
      status: "SENT",
      createdAt: nowISO(),
    });
    await ensureMessagesLoaded(thread.id);
  } catch (error) {
    patchMessage(thread.id, pendingId, { status: "FAILED" });
    throw error;
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
    subtitle: "New SMS conversation",
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
  const pendingId = pushPendingMessage(thread.id, text);
  renderAll();
  try {
    const payload = await apiRequest("/v1/messages/phone", {
      method: "POST",
      body: JSON.stringify({
        phone_e164: phone,
        idempotency_key: makeIdempotencyKey("phone"),
        content_type: "text",
        content: { text },
      }),
    });
    const conversationId = sanitizeText(payload.conversation_id, 80);
    if (!conversationId) throw new Error("missing_conversation_id");
    const oldDraftId = thread.id;
    state.threads = state.threads.filter((item) => item.id !== oldDraftId);
    state.activeThreadId = conversationId;
    upsertThread({
      id: conversationId,
      kind: "phone",
      title: phone,
      subtitle: "SMS thread",
      updatedAt: nowISO(),
      participants: [state.auth.userId],
      externalPhones: [phone],
      messages: [],
      loadedMessages: false,
    });
    saveConversationStore();
    await ensureMessagesLoaded(conversationId);
  } catch (error) {
    patchMessage(thread.id, pendingId, { status: "FAILED" });
    throw error;
  }
}

async function handleComposerSend(text) {
  const thread = getActiveThread();
  if (!thread) return;
  if (thread.kind === "draft_phone") {
    await sendInDraftPhoneConversation(thread, text);
  } else {
    await sendInConversation(thread, text);
  }
}

async function startPhoneAuth(event) {
  event.preventDefault();
  const phone = normalizePhone(el.phoneInput.value);
  if (!phone) {
    setAuthStatus("Enter a valid E.164 phone number, e.g. +15551230001", true);
    return;
  }
  setAuthStatus("Requesting OTP...");
  try {
    const payload = await apiRequest("/v1/auth/phone/start", {
      method: "POST",
      headers: { Authorization: "" },
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
      headers: { Authorization: "" },
      body: JSON.stringify({
        challenge_id: state.challengeId,
        otp_code: otp,
        device: { platform: "WEB", device_name: "OHMF Web" },
      }),
    });
    const user = payload?.user || {};
    const tokens = payload?.tokens || {};
    if (!tokens.access_token || !tokens.refresh_token || !user.user_id) {
      throw new Error("invalid_auth_response");
    }
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
    await bootAfterAuth();
  } catch (error) {
    setAuthStatus(`Verify failed: ${error.message}`, true);
  }
}

async function bootAfterAuth() {
  state.query = "";
  state.activeThreadId = null;
  state.threads = [];
  loadConversationStore();
  showAppShell();
  renderAll();
  try {
    await loadConversationsFromApi();
    if (state.activeThreadId) {
      await ensureMessagesLoaded(state.activeThreadId);
    }
    renderAll();
  } catch (error) {
    console.error(error);
  }
}

function logout() {
  authStoreClear();
  state.threads = [];
  state.activeThreadId = null;
  state.query = "";
  showAuthShell();
  setAuthStatus("Signed out.");
  renderAll();
}

el.searchInput.addEventListener("input", (event) => {
  state.query = sanitizeText(event.target.value, 120);
  renderThreadList();
});

el.composer.addEventListener("submit", async (event) => {
  event.preventDefault();
  const text = sanitizeText(el.composerInput.value, 1000);
  if (!text) return;
  el.composerInput.value = "";
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
  if (!el.newChatForm.classList.contains("hidden")) {
    el.newPhoneInput.focus();
  }
});

el.newChatForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const phone = normalizePhone(el.newPhoneInput.value);
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
    await apiRequest("/v1/auth/logout", {
      method: "POST",
      body: JSON.stringify({}),
    });
  } catch {
    // Best effort logout.
  }
  logout();
});

el.phoneStartForm.addEventListener("submit", startPhoneAuth);
el.phoneVerifyForm.addEventListener("submit", verifyPhoneAuth);

window.addEventListener("resize", () => {
  if (!window.matchMedia("(max-width: 880px)").matches) {
    closeMobileThread();
  }
});

async function init() {
  const session = authStoreLoad();
  if (session) {
    state.auth = session;
    await bootAfterAuth();
  } else {
    showAuthShell();
    setAuthStatus("");
    renderAll();
  }
}

init();
