"use strict";

function normalizeBaseURL(baseURL) {
  return String(baseURL || "").replace(/\/+$/, "");
}

async function parseResponse(response) {
  const text = await response.text();
  if (!text) {
    return {};
  }
  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

function buildPhone(runID, suffix) {
  const digits = String(runID || "").replace(/\D/g, "");
  const core = digits.slice(-4).padStart(4, "0");
  const tail = String(suffix).padStart(3, "0").slice(-3);
  return `+1555${core}${tail}`;
}

function makeHeaders(bearerToken = "", extra = {}) {
  const headers = { ...extra };
  if (bearerToken) {
    headers.Authorization = `Bearer ${bearerToken}`;
  }
  return headers;
}

async function requestJSON(baseURL, path, options = {}) {
  const url = `${normalizeBaseURL(baseURL)}${path}`;
  const response = await fetch(url, options);
  const body = await parseResponse(response);
  if (!response.ok) {
    const message = body?.message || body?.code || JSON.stringify(body);
    const error = new Error(`request_failed ${response.status} ${path} ${message}`);
    error.status = response.status;
    error.body = body;
    error.url = url;
    throw error;
  }
  return body;
}

async function requestText(url, options = {}) {
  const response = await fetch(url, options);
  const body = await response.text();
  if (!response.ok) {
    const error = new Error(`request_failed ${response.status} ${url}`);
    error.status = response.status;
    error.body = body;
    error.url = url;
    throw error;
  }
  return body;
}

async function postJSON(baseURL, path, body, bearerToken = "") {
  return requestJSON(baseURL, path, {
    method: "POST",
    headers: makeHeaders(bearerToken, {
      "Content-Type": "application/json",
    }),
    body: JSON.stringify(body),
  });
}

async function getJSON(baseURL, path, bearerToken = "") {
  return requestJSON(baseURL, path, {
    method: "GET",
    headers: makeHeaders(bearerToken),
  });
}

async function createVerifiedDevice(baseURL, phoneE164, deviceName) {
  const start = await postJSON(baseURL, "/v1/auth/phone/start", {
    phone_e164: phoneE164,
    channel: "SMS",
  });
  const verify = await postJSON(baseURL, "/v1/auth/phone/verify", {
    challenge_id: start.challenge_id,
    otp_code: "123456",
    device: {
      platform: "WEB",
      device_name: deviceName,
      capabilities: ["MINI_APPS", "WEB_PUSH_V1"],
    },
  });
  return {
    userId: verify?.user?.user_id || "",
    accessToken: verify?.tokens?.access_token || "",
    refreshToken: verify?.tokens?.refresh_token || "",
    deviceId: verify?.device?.device_id || "",
    phoneE164,
    raw: verify,
  };
}

async function createUserWithDevices(baseURL, phoneE164, deviceCount, label) {
  const devices = [];
  let userId = "";
  for (let index = 0; index < deviceCount; index += 1) {
    const device = await createVerifiedDevice(
      baseURL,
      phoneE164,
      `${label} device ${index + 1}`
    );
    if (!device.userId) {
      throw new Error(`missing user id while provisioning ${label}`);
    }
    if (!userId) {
      userId = device.userId;
    } else if (userId !== device.userId) {
      throw new Error(`phone ${phoneE164} resolved to multiple user ids`);
    }
    devices.push({
      ...device,
      label: `${label}-device-${index + 1}`,
    });
  }
  return {
    label,
    userId,
    phoneE164,
    devices,
  };
}

async function createDirectConversation(baseURL, accessToken, participantUserID) {
  const response = await postJSON(baseURL, "/v1/conversations", {
    type: "DM",
    participants: [participantUserID],
  }, accessToken);
  return {
    conversationId: response?.conversation_id || "",
    raw: response,
  };
}

async function sendTextMessage(baseURL, accessToken, conversationID, text, idempotencyKey) {
  return postJSON(baseURL, "/v1/messages", {
    conversation_id: conversationID,
    idempotency_key: idempotencyKey,
    content_type: "text",
    content: { text },
  }, accessToken);
}

async function listConversationMessages(baseURL, accessToken, conversationID) {
  return getJSON(baseURL, `/v1/conversations/${conversationID}/messages`, accessToken);
}

async function listMessageDeliveries(baseURL, accessToken, messageID) {
  return getJSON(baseURL, `/v1/messages/${messageID}/deliveries`, accessToken);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, Math.max(0, ms)));
}

async function poll(check, options = {}) {
  const timeoutMs = Number.isFinite(options.timeoutMs) ? options.timeoutMs : 5000;
  const intervalMs = Number.isFinite(options.intervalMs) ? options.intervalMs : 250;
  const description = options.description || "condition";
  const startedAt = Date.now();
  let lastError = null;

  while (Date.now() - startedAt <= timeoutMs) {
    try {
      const value = await check();
      if (value) {
        return value;
      }
      lastError = null;
    } catch (error) {
      lastError = error;
    }
    await sleep(intervalMs);
  }

  if (lastError) {
    throw new Error(`${description}: ${lastError.message || lastError}`);
  }
  throw new Error(`timed out waiting for ${description} after ${timeoutMs}ms`);
}

module.exports = {
  buildPhone,
  createDirectConversation,
  createUserWithDevices,
  createVerifiedDevice,
  listConversationMessages,
  listMessageDeliveries,
  poll,
  requestText,
  sendTextMessage,
  sleep,
};
