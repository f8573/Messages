#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const {
  blockUser,
  buildPhone,
  createDirectConversation,
  createUserWithDevices,
  listConversationMessages,
  poll,
  refreshAuthTokens,
  requestText,
  sendTextMessage,
  sleep,
  unblockUser,
} = require("./lib/api");
const { CorrectnessTracker } = require("./lib/correctness-tracker");
const { createFaultProxy } = require("./lib/fault-proxy");
const { RealtimeClient } = require("./lib/realtime-client");
const { createRunDirectory, ensureDir, writeRunArtifacts } = require("./lib/reporting");

const repoRoot = path.resolve(__dirname, "..", "..");

function flagNameToKey(flagName) {
  return String(flagName || "")
    .replace(/^-+/, "")
    .replace(/-([a-z])/g, (_, chr) => chr.toUpperCase());
}

function parseArgs(argv) {
  const parsed = {
    metricsUrls: [],
  };
  for (let index = 0; index < argv.length; index += 1) {
    const value = argv[index];
    if (value === "--help" || value === "-h") {
      parsed.help = true;
      continue;
    }
    if (!value.startsWith("--")) {
      continue;
    }
    const [rawFlag, inlineValue] = value.split(/=(.*)/s, 2);
    const key = flagNameToKey(rawFlag);
    if (typeof inlineValue === "string") {
      if (key === "metricsUrl") {
        parsed.metricsUrls.push(inlineValue);
      } else {
        parsed[key] = inlineValue;
      }
      continue;
    }
    const next = argv[index + 1];
    if (!next || next.startsWith("--")) {
      parsed[key] = true;
      continue;
    }
    if (key === "metricsUrl") {
      parsed.metricsUrls.push(next);
    } else {
      parsed[key] = next;
    }
    index += 1;
  }
  return parsed;
}

function numberOption(value, fallback) {
  if (value === undefined || value === null || value === "") {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    throw new Error(`invalid numeric value: ${value}`);
  }
  return parsed;
}

function integerOption(value, fallback) {
  return Math.trunc(numberOption(value, fallback));
}

function csvList(value) {
  return String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function buildConfig(args) {
  let scenario = String(
    args.scenario || process.env.OHMF_STRESS_SCENARIO || "smoke"
  ).trim().toLowerCase();
  if (scenario === "send-timeout") {
    scenario = "high-latency-link";
  }
  const uniqueUserRatio = numberOption(
    args.uniqueUserRatio ?? process.env.OHMF_STRESS_UNIQUE_USER_RATIO,
    0
  );
  const requestedUsers = Math.max(
    2,
    integerOption(args.users ?? process.env.OHMF_STRESS_USERS, scenario === "throughput" ? 4 : 2)
  );
  const devicesPerUser = Math.max(
    1,
    integerOption(args.devicesPerUser ?? process.env.OHMF_STRESS_DEVICES_PER_USER, 2)
  );
  const requestedTotalClients = Math.max(
    0,
    integerOption(args.totalClients ?? process.env.OHMF_STRESS_TOTAL_CLIENTS, 0)
  );
  const totalClients = requestedTotalClients > 0
    ? requestedTotalClients
    : requestedUsers * devicesPerUser;
  const derivedUsers = uniqueUserRatio > 0
    ? Math.max(2, Math.min(totalClients, Math.ceil(totalClients * uniqueUserRatio)))
    : requestedUsers;

  const defaultMessages = scenario === "throughput"
    ? 0
    : scenario === "reconnect"
      ? 12
      : scenario === "connect" || scenario === "reconnect-storm"
        ? 0
        : scenario === "send-abort" || scenario === "high-latency-link" || scenario === "block-race"
          ? 0
        : 8;

  const config = {
    scenario,
    baseURL: String(
      args.baseURL
      || process.env.OHMF_STRESS_BASE_URL
      || process.env.OHMF_API_BASE_URL
      || "http://127.0.0.1:18080"
    ).replace(/\/+$/, ""),
    wsVersion: String(
      args.wsVersion || process.env.OHMF_STRESS_WS_VERSION || "v1"
    ).trim().toLowerCase(),
    users: derivedUsers,
    devicesPerUser,
    totalClients,
    uniqueUserRatio,
    activeConversations: Math.max(
      0,
      integerOption(args.activeConversations ?? process.env.OHMF_STRESS_ACTIVE_CONVERSATIONS, 0)
    ),
    messages: Math.max(0, integerOption(args.messages ?? process.env.OHMF_STRESS_MESSAGES, defaultMessages)),
    rate: Math.max(0, numberOption(args.rate ?? process.env.OHMF_STRESS_RATE, scenario === "throughput" ? 10 : 2)),
    durationMs: Math.max(0, integerOption(args.durationMs ?? process.env.OHMF_STRESS_DURATION_MS, scenario === "throughput" ? 30000 : 0)),
    holdMs: Math.max(0, integerOption(args.holdMs ?? process.env.OHMF_STRESS_HOLD_MS, scenario === "connect" ? 15000 : scenario === "reconnect-storm" ? 5000 : 0)),
    connectTimeoutMs: Math.max(250, integerOption(args.connectTimeoutMs ?? process.env.OHMF_STRESS_CONNECT_TIMEOUT_MS, 10000)),
    sendTimeoutMs: Math.max(250, integerOption(args.sendTimeoutMs ?? process.env.OHMF_STRESS_SEND_TIMEOUT_MS, 30000)),
    readTimeoutMs: Math.max(250, integerOption(args.readTimeoutMs ?? process.env.OHMF_STRESS_READ_TIMEOUT_MS, 5000)),
    refreshTimeoutMs: Math.max(250, integerOption(args.refreshTimeoutMs ?? process.env.OHMF_STRESS_REFRESH_TIMEOUT_MS, 5000)),
    sendConcurrency: Math.max(1, integerOption(args.sendConcurrency ?? process.env.OHMF_STRESS_SEND_CONCURRENCY, 1)),
    raceIterations: Math.max(1, integerOption(args.raceIterations ?? process.env.OHMF_STRESS_RACE_ITERATIONS, 5)),
    connectBatchSize: Math.max(1, integerOption(args.connectBatchSize ?? process.env.OHMF_STRESS_CONNECT_BATCH_SIZE, 1)),
    settleTimeoutMs: Math.max(1000, integerOption(args.settleTimeoutMs ?? process.env.OHMF_STRESS_SETTLE_TIMEOUT_MS, 20000)),
    persistPollIntervalMs: Math.max(50, integerOption(args.persistPollIntervalMs ?? process.env.OHMF_STRESS_PERSIST_POLL_INTERVAL_MS, 500)),
    receiptPollIntervalMs: Math.max(25, integerOption(args.receiptPollIntervalMs ?? process.env.OHMF_STRESS_RECEIPT_POLL_INTERVAL_MS, 100)),
    connectDelayMs: Math.max(0, integerOption(args.connectDelayMs ?? process.env.OHMF_STRESS_CONNECT_DELAY_MS, 50)),
    reconnectAfter: Math.max(1, integerOption(args.reconnectAfter ?? process.env.OHMF_STRESS_RECONNECT_AFTER, 4)),
    offlineMessages: Math.max(1, integerOption(args.offlineMessages ?? process.env.OHMF_STRESS_OFFLINE_MESSAGES, 4)),
    postReconnectMessages: Math.max(0, integerOption(args.postReconnectMessages ?? process.env.OHMF_STRESS_POST_RECONNECT_MESSAGES, 4)),
    reconnectStormSize: Math.max(0, integerOption(args.reconnectStormSize ?? process.env.OHMF_STRESS_RECONNECT_STORM_SIZE, 0)),
    reconnectBatchSize: Math.max(1, integerOption(args.reconnectBatchSize ?? process.env.OHMF_STRESS_RECONNECT_BATCH_SIZE, 50)),
    reconnectBatchIntervalMs: Math.max(0, integerOption(args.reconnectBatchIntervalMs ?? process.env.OHMF_STRESS_RECONNECT_BATCH_INTERVAL_MS, 250)),
    reconnectPauseMs: Math.max(0, integerOption(args.reconnectPauseMs ?? process.env.OHMF_STRESS_RECONNECT_PAUSE_MS, 1000)),
    faultRequestDelayMs: Math.max(0, integerOption(args.faultRequestDelayMs ?? process.env.OHMF_STRESS_FAULT_REQUEST_DELAY_MS, 0)),
    faultResponseDelayMs: Math.max(0, integerOption(args.faultResponseDelayMs ?? process.env.OHMF_STRESS_FAULT_RESPONSE_DELAY_MS, 0)),
    faultRetryDelayMs: Math.max(0, integerOption(args.faultRetryDelayMs ?? process.env.OHMF_STRESS_FAULT_RETRY_DELAY_MS, 250)),
    reportDir: path.resolve(
      repoRoot,
      String(args.reportDir || process.env.OHMF_STRESS_REPORT_DIR || path.join("testing", "stress", "reports"))
    ),
    topologyFile: args.topologyFile || process.env.OHMF_STRESS_TOPOLOGY_FILE
      ? path.resolve(repoRoot, String(args.topologyFile || process.env.OHMF_STRESS_TOPOLOGY_FILE))
      : "",
    topologyUserOffset: Math.max(0, integerOption(args.topologyUserOffset ?? process.env.OHMF_STRESS_TOPOLOGY_USER_OFFSET, 0)),
    runLabel: String(args.runLabel || process.env.OHMF_STRESS_RUN_LABEL || scenario).trim(),
    metricsUrls: [
      ...csvList(process.env.OHMF_STRESS_METRICS_URLS),
      ...(args.metricsUrls || []),
    ],
    dryRun: Boolean(args.dryRun || process.env.OHMF_STRESS_DRY_RUN === "1"),
  };

  if (![
    "smoke",
    "throughput",
    "reconnect",
    "connect",
    "reconnect-storm",
    "send-abort",
    "high-latency-link",
    "block-race",
  ].includes(config.scenario)) {
    throw new Error(`unsupported scenario "${config.scenario}"`);
  }
  if (!["v1", "v2"].includes(config.wsVersion)) {
    throw new Error(`unsupported ws version "${config.wsVersion}"`);
  }
  if (config.scenario === "reconnect" && config.devicesPerUser < 2) {
    throw new Error("reconnect scenario requires --devices-per-user >= 2");
  }
  if (config.scenario === "throughput" && config.messages === 0 && config.durationMs === 0) {
    config.durationMs = 30000;
  }
  if (config.scenario === "throughput" && config.messages === 0) {
    config.messages = Math.max(1, Math.ceil((config.durationMs / 1000) * config.rate));
  }
  if (config.scenario === "high-latency-link" && config.faultRequestDelayMs === 0) {
    config.faultRequestDelayMs = config.sendTimeoutMs + 500;
  }

  return config;
}

function printUsage() {
  console.log("Usage: node ./testing/stress/run.js [options]");
  console.log("");
  console.log("Options:");
  console.log("  --scenario smoke|throughput|reconnect|connect|reconnect-storm|send-abort|high-latency-link|block-race");
  console.log("  --base-url <url>");
  console.log("  --ws-version v1|v2");
  console.log("  --users <count>");
  console.log("  --devices-per-user <count>");
  console.log("  --total-clients <count>");
  console.log("  --unique-user-ratio <decimal>");
  console.log("  --active-conversations <count>");
  console.log("  --messages <count>");
  console.log("  --rate <messages-per-second>");
  console.log("  --duration-ms <milliseconds>");
  console.log("  --hold-ms <milliseconds>");
  console.log("  --connect-timeout-ms <milliseconds>");
  console.log("  --send-timeout-ms <milliseconds>");
  console.log("  --read-timeout-ms <milliseconds>");
  console.log("  --refresh-timeout-ms <milliseconds>");
  console.log("  --send-concurrency <count>");
  console.log("  --race-iterations <count>");
  console.log("  --reconnect-storm-size <count>");
  console.log("  --reconnect-batch-size <count>");
  console.log("  --reconnect-batch-interval-ms <milliseconds>");
  console.log("  --reconnect-pause-ms <milliseconds>");
  console.log("  --fault-request-delay-ms <milliseconds>");
  console.log("  --fault-response-delay-ms <milliseconds>");
  console.log("  --fault-retry-delay-ms <milliseconds>");
  console.log("  --settle-timeout-ms <milliseconds>");
  console.log("  --connect-delay-ms <milliseconds>");
  console.log("  --connect-batch-size <devices>");
  console.log("  --topology-file <path>");
  console.log("  --topology-user-offset <count>");
  console.log("  --metrics-url <absolute-url> (repeatable)");
  console.log("  --report-dir <path>");
  console.log("  --run-label <label>");
  console.log("  --dry-run");
  console.log("");
  console.log("Environment:");
  console.log("  OHMF_STRESS_BASE_URL, OHMF_STRESS_SCENARIO, OHMF_STRESS_WS_VERSION,");
  console.log("  OHMF_STRESS_USERS, OHMF_STRESS_DEVICES_PER_USER, OHMF_STRESS_TOTAL_CLIENTS,");
  console.log("  OHMF_STRESS_UNIQUE_USER_RATIO, OHMF_STRESS_ACTIVE_CONVERSATIONS, OHMF_STRESS_MESSAGES,");
  console.log("  OHMF_STRESS_RATE, OHMF_STRESS_DURATION_MS, OHMF_STRESS_HOLD_MS, OHMF_STRESS_CONNECT_TIMEOUT_MS,");
  console.log("  OHMF_STRESS_SEND_TIMEOUT_MS, OHMF_STRESS_READ_TIMEOUT_MS, OHMF_STRESS_SEND_CONCURRENCY,");
  console.log("  OHMF_STRESS_RACE_ITERATIONS, OHMF_STRESS_FAULT_REQUEST_DELAY_MS,");
  console.log("  OHMF_STRESS_FAULT_RESPONSE_DELAY_MS, OHMF_STRESS_FAULT_RETRY_DELAY_MS,");
  console.log("  OHMF_STRESS_REFRESH_TIMEOUT_MS,");
  console.log("  OHMF_STRESS_RECONNECT_STORM_SIZE, OHMF_STRESS_RECONNECT_BATCH_SIZE,");
  console.log("  OHMF_STRESS_RECONNECT_BATCH_INTERVAL_MS, OHMF_STRESS_RECONNECT_PAUSE_MS,");
  console.log("  OHMF_STRESS_SETTLE_TIMEOUT_MS, OHMF_STRESS_CONNECT_DELAY_MS,");
  console.log("  OHMF_STRESS_CONNECT_BATCH_SIZE, OHMF_STRESS_TOPOLOGY_FILE, OHMF_STRESS_TOPOLOGY_USER_OFFSET,");
  console.log("  OHMF_STRESS_METRICS_URLS, OHMF_STRESS_REPORT_DIR.");
}

function currentCommitSHA() {
  try {
    return execFileSync("git", ["rev-parse", "--short", "HEAD"], {
      cwd: repoRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "unknown";
  }
}

function serializeReusableUsers(users) {
  return users.map((user) => ({
    label: user.label,
    user_id: user.userId,
    phone_e164: user.phoneE164,
    devices: user.devices.map((device) => ({
      label: device.label,
      device_id: device.deviceId,
      access_token: device.accessToken,
      refresh_token: device.refreshToken,
    })),
  }));
}

function buildDeviceCounts(config) {
  if (config.totalClients > 0 && config.uniqueUserRatio > 0) {
    const counts = Array.from({ length: config.users }, () => 1);
    let remaining = Math.max(0, config.totalClients - config.users);
    if (remaining === 0) {
      return counts;
    }

    const maxPerPass = counts.length;
    while (remaining > 0) {
      const pass = Math.min(remaining, maxPerPass);
      for (let index = 0; index < pass; index += 1) {
        const target = Math.min(
          counts.length - 1,
          Math.floor(((index + 0.5) * counts.length) / pass)
        );
        counts[target] += 1;
      }
      remaining -= pass;
    }
    return counts;
  }

  return Array.from({ length: config.users }, () => config.devicesPerUser);
}

function selectRecipientIndexes(totalRecipients, limit) {
  if (totalRecipients <= 0) {
    return [];
  }
  const capped = limit > 0 ? Math.min(totalRecipients, limit) : totalRecipients;
  if (capped >= totalRecipients) {
    return Array.from({ length: totalRecipients }, (_, index) => index);
  }

  const selected = [];
  const seen = new Set();
  for (let index = 0; index < capped; index += 1) {
    const candidate = Math.min(
      totalRecipients - 1,
      Math.floor(((index + 0.5) * totalRecipients) / capped)
    );
    if (seen.has(candidate)) {
      continue;
    }
    seen.add(candidate);
    selected.push(candidate);
  }
  return selected;
}

function loadReusableUsers(topologyFile) {
  const raw = fs.readFileSync(topologyFile, "utf8");
  const payload = JSON.parse(raw);
  const users = Array.isArray(payload?.users) ? payload.users : [];
  if (!users.length) {
    throw new Error(`topology file ${topologyFile} does not contain any users`);
  }
  return users.map((user, userIndex) => ({
    label: String(user.label || `stress-user-${userIndex + 1}`),
    userId: String(user.user_id || ""),
    phoneE164: String(user.phone_e164 || ""),
    devices: (Array.isArray(user.devices) ? user.devices : []).map((device, deviceIndex) => ({
      label: String(device.label || `device-${deviceIndex + 1}`),
      deviceId: String(device.device_id || ""),
      accessToken: String(device.access_token || ""),
      refreshToken: String(device.refresh_token || ""),
      phoneE164: String(user.phone_e164 || ""),
    })),
  }));
}

function fitReusableUsersToConfig(config, users) {
  const targetCounts = buildDeviceCounts(config);
  const start = Math.max(0, Math.min(config.topologyUserOffset, users.length));
  const selected = [];
  let cursor = start;

  for (const requiredDevices of targetCounts) {
    while (cursor < users.length && users[cursor].devices.length < requiredDevices) {
      cursor += 1;
    }
    if (cursor >= users.length) {
      throw new Error(
        `topology file does not have enough users with ${requiredDevices}+ devices after offset ${start}`
      );
    }
    const user = users[cursor];
    selected.push({
      ...user,
      devices: user.devices.slice(0, requiredDevices),
    });
    cursor += 1;
  }

  return selected;
}

function writeReusableUsers(topologyFile, users) {
  if (!topologyFile) {
    return;
  }
  ensureDir(path.dirname(topologyFile));
  fs.writeFileSync(
    topologyFile,
    `${JSON.stringify({ users: serializeReusableUsers(users) }, null, 2)}\n`,
    "utf8"
  );
}

async function captureMetrics(label, config, runDir) {
  if (!config.metricsUrls.length) {
    return [];
  }

  const metricsDir = ensureDir(path.join(runDir, "metrics"));
  const snapshots = [];
  for (let index = 0; index < config.metricsUrls.length; index += 1) {
    const url = config.metricsUrls[index];
    const fileName = `${label}-${index + 1}.prom`;
    const targetPath = path.join(metricsDir, fileName);
    try {
      const body = await requestText(url);
      fs.writeFileSync(targetPath, body, "utf8");
      snapshots.push({
        label,
        url,
        file: targetPath,
        bytes: Buffer.byteLength(body),
      });
    } catch (error) {
      snapshots.push({
        label,
        url,
        error: error.message || String(error),
      });
    }
  }
  return snapshots;
}

async function refreshReusableUsers(config, users) {
  const targets = users.flatMap((user) => user.devices.map((device) => ({
    user,
    device,
  })));

  for (let index = 0; index < targets.length; index += config.connectBatchSize) {
    const batch = targets.slice(index, index + config.connectBatchSize);
    const refreshed = await Promise.all(
      batch.map(async ({ user, device }) => {
        if (!device.refreshToken) {
          throw new Error(`missing refresh token for ${user.label}/${device.label}`);
        }
        const tokens = await refreshAuthTokens(
          config.baseURL,
          device.refreshToken,
          {
            timeoutMs: config.refreshTimeoutMs,
          }
        );
        if (!tokens.accessToken || !tokens.refreshToken) {
          throw new Error(`refresh returned incomplete tokens for ${user.label}/${device.label}`);
        }
        return {
          device,
          tokens,
        };
      })
    );

    for (const item of refreshed) {
      item.device.accessToken = item.tokens.accessToken;
      item.device.refreshToken = item.tokens.refreshToken;
    }
  }
}

function bindClient(client, tracker) {
  client.on("message-created", (message) => {
    if (!message?.message_id) {
      return;
    }
    tracker.noteReceipt(message.message_id, {
      userId: client.userId,
      deviceId: client.deviceId,
      conversationId: message.conversation_id,
      observedAt: Date.now(),
      serverOrder: Number(message.server_order || 0),
      source: message.source || "realtime",
      userEventId: message.userEventId,
      wsVersion: message.wsVersion || client.wsVersion,
    });
  });

  client.on("delivery-update", (update) => {
    tracker.noteDeliveryUpdate({
      clientUserId: client.userId,
      clientDeviceId: client.deviceId,
      ...update,
    });
  });

  client.on("transport-error", (error) => {
    tracker.noteClientError(client.label, error);
  });

  client.on("error-frame", (payload) => {
    tracker.noteClientError(client.label, new Error(`ws error frame ${JSON.stringify(payload)}`));
  });

  client.on("closed", (details) => {
    if (!details.intentional && !details.handshakeFailure) {
      tracker.noteClientError(
        client.label,
        new Error(`socket closed unexpectedly (${details.code} ${details.reason})`)
      );
    }
  });
}

function buildRealtimeClient(config, tracker, user, device) {
  const client = new RealtimeClient({
    baseURL: config.baseURL,
    accessToken: device.accessToken,
    deviceId: device.deviceId,
    userId: user.userId,
    label: `${user.label}/${device.label}`,
    wsVersion: config.wsVersion,
    autoAck: config.wsVersion === "v2",
  });
  bindClient(client, tracker);
  return client;
}

async function connectDeviceClient(config, tracker, user, device) {
  const client = buildRealtimeClient(config, tracker, user, device);
  try {
    await client.connect({
      timeoutMs: config.connectTimeoutMs,
    });
    device.client = client;
    return client;
  } catch (error) {
    tracker.noteClientError(client.label, error);
    return null;
  }
}

async function ensureConversations(config, users) {
  const hub = users[0];
  const conversations = [];
  if (config.scenario === "connect" || config.scenario === "reconnect-storm") {
    return {
      hubUserId: hub.userId,
      conversations,
    };
  }

  if (config.scenario === "throughput") {
    const conversationLimit = config.activeConversations > 0
      ? Math.min(config.activeConversations, Math.floor(users.length / 2))
      : Math.floor(users.length / 2);
    for (let index = 0; index < conversationLimit; index += 1) {
      const sender = users[index * 2];
      const recipient = users[(index * 2) + 1];
      const conversation = await createDirectConversation(
        config.baseURL,
        sender.devices[0].accessToken,
        recipient.userId
      );
      if (!conversation.conversationId) {
        throw new Error(`failed to create conversation for ${sender.label} and ${recipient.label}`);
      }
      conversations.push({
        label: `${sender.label}-to-${recipient.label}`,
        conversationId: conversation.conversationId,
        participants: [sender, recipient],
        senderIndex: index % 2,
        sentMessageIds: [],
      });
    }

    return {
      hubUserId: conversations[0]?.participants?.[0]?.userId || hub.userId,
      conversations,
    };
  }

  for (const recipientIndex of selectRecipientIndexes(users.length - 1, config.activeConversations)) {
    const recipient = users[recipientIndex + 1];
    const conversation = await createDirectConversation(
      config.baseURL,
      hub.devices[0].accessToken,
      recipient.userId
    );
    if (!conversation.conversationId) {
      throw new Error(`failed to create conversation for ${hub.label} and ${recipient.label}`);
    }
    conversations.push({
      label: `${hub.label}-to-${recipient.label}`,
      conversationId: conversation.conversationId,
      participants: [hub, recipient],
      senderIndex: conversations.length % 2,
      sentMessageIds: [],
    });
  }

  return {
    hubUserId: hub.userId,
    conversations,
  };
}

async function provisionTopology(config, tracker, runID) {
  let users = [];
  if (config.topologyFile && fs.existsSync(config.topologyFile)) {
    const reusableUsers = loadReusableUsers(config.topologyFile);
    users = fitReusableUsersToConfig(config, reusableUsers);
    await refreshReusableUsers(config, users);
    writeReusableUsers(config.topologyFile, reusableUsers);
  } else {
    const deviceCounts = buildDeviceCounts(config);
    for (let index = 0; index < config.users; index += 1) {
      const user = await createUserWithDevices(
        config.baseURL,
        buildPhone(runID, index + 1),
        deviceCounts[index],
        `stress-user-${index + 1}`
      );
      users.push(user);
    }
    writeReusableUsers(config.topologyFile, users);
  }

  const { hubUserId, conversations } = await ensureConversations(config, users);

  const targets = users.flatMap((user) => user.devices.map((device) => ({ user, device })));
  const clients = [];
  for (let index = 0; index < targets.length; index += config.connectBatchSize) {
    const batch = targets.slice(index, index + config.connectBatchSize);
    const connected = await Promise.all(
      batch.map(async ({ user, device }) => ({
        user,
        device,
        client: await connectDeviceClient(config, tracker, user, device),
      }))
    );

    for (const { user, device, client } of connected) {
      if (client) {
        clients.push(client);
      } else if (config.scenario !== "connect" && config.scenario !== "reconnect-storm") {
        throw new Error(`failed to connect ${user.label}/${device.label}`);
      }
    }

    if (config.connectDelayMs > 0 && index + config.connectBatchSize < targets.length) {
      await sleep(config.connectDelayMs);
    }
  }

  return {
    users,
    hubUserId,
    conversations,
    clients,
  };
}

function activeRecipientDevices(conversation, senderUserId) {
  return conversation.participants
    .filter((user) => user.userId !== senderUserId)
    .flatMap((user) => user.devices.map((device) => ({
      userId: user.userId,
      deviceId: device.deviceId,
      label: `${user.label}/${device.label}`,
    })));
}

function nextSender(conversation) {
  const sender = conversation.participants[conversation.senderIndex % conversation.participants.length];
  conversation.senderIndex = (conversation.senderIndex + 1) % conversation.participants.length;
  return sender;
}

async function sendTrackedMessage(config, tracker, conversation, sender, ordinal) {
  const senderDevice = sender.devices[0];
  const sendStartedAt = Date.now();
  const text = `${config.runLabel} ${config.scenario} message ${ordinal}`;
  const idempotencyKey = `${config.runLabel}-${config.scenario}-${ordinal}-${sendStartedAt}`;

  try {
    const response = await sendTextMessage(
      config.baseURL,
      senderDevice.accessToken,
      conversation.conversationId,
      text,
      idempotencyKey,
      {
        timeoutMs: config.sendTimeoutMs,
      }
    );

    tracker.registerAccepted({
      messageId: response.message_id,
      conversationId: conversation.conversationId,
      senderUserId: sender.userId,
      senderDeviceId: senderDevice.deviceId,
      expectedRecipientDevices: activeRecipientDevices(conversation, sender.userId),
      sendStartedAt,
      acceptedAt: Date.now(),
      response,
      text,
    });

    conversation.sentMessageIds.push(response.message_id);
    return response.message_id;
  } catch (error) {
    tracker.noteSendFailure({
      conversationId: conversation.conversationId,
      senderUserId: sender.userId,
      senderDeviceId: senderDevice.deviceId,
      ordinal,
      error,
    });
    return null;
  }
}

function pushSentMessageId(conversation, messageId) {
  if (!messageId) {
    return;
  }
  if (!conversation.sentMessageIds.includes(messageId)) {
    conversation.sentMessageIds.push(messageId);
  }
}

function buildScenarioText(config, label) {
  return `${config.runLabel} ${config.scenario} ${label} ${Date.now()}`;
}

function isBlockedError(error) {
  return Number(error?.status) === 403 && /blocked/i.test(String(error?.message || ""));
}

function isExpectedClientFault(error) {
  if (!error) {
    return false;
  }
  if (error.code === "REQUEST_TIMEOUT") {
    return true;
  }
  const message = String(error.message || error);
  return /request_timeout|fetch failed|socket hang up|aborted|network|econnreset/i.test(message);
}

async function listConversationMessagesByText(config, accessToken, conversationID, text) {
  const payload = await listConversationMessages(
    config.baseURL,
    accessToken,
    conversationID,
    {
      timeoutMs: config.readTimeoutMs,
    }
  );
  const items = Array.isArray(payload?.items) ? payload.items : [];
  return items.filter((item) => item?.content?.text === text);
}

async function waitForMessagesByText(config, conversation, accessToken, text, description) {
  return poll(async () => {
    const matches = await listConversationMessagesByText(
      config,
      accessToken,
      conversation.conversationId,
      text
    );
    return matches.length > 0 ? matches : null;
  }, {
    timeoutMs: config.settleTimeoutMs,
    intervalMs: config.persistPollIntervalMs,
    description,
  });
}

function registerScenarioAcceptedMessage(config, tracker, conversation, sender, text, message, sendStartedAt, response = {}) {
  const senderDevice = sender.devices[0];
  const messageId = message?.message_id || response?.message_id || "";
  if (!messageId) {
    throw new Error(`missing message id while registering scenario message for ${conversation.label}`);
  }
  const acceptedAt = Date.now();
  if (!tracker.getMessage(messageId)) {
    tracker.registerAccepted({
      messageId,
      conversationId: conversation.conversationId,
      senderUserId: sender.userId,
      senderDeviceId: senderDevice.deviceId,
      expectedRecipientDevices: activeRecipientDevices(conversation, sender.userId),
      sendStartedAt,
      acceptedAt,
      response: {
        ...response,
        message_id: messageId,
        server_order: Number(message?.server_order || response?.server_order || 0),
        queued: Boolean(response?.queued),
      },
      text,
    });
  }
  pushSentMessageId(conversation, messageId);
  tracker.notePersisted(messageId, {
    observedAt: acceptedAt,
    serverOrder: Number(message?.server_order || response?.server_order || 0),
  });
  return messageId;
}

async function waitForStableReplay(config, senderDevice, conversationID, text, idempotencyKey, description) {
  return poll(async () => {
    const response = await sendTextMessage(
      config.baseURL,
      senderDevice.accessToken,
      conversationID,
      text,
      idempotencyKey,
      {
        timeoutMs: config.sendTimeoutMs,
      }
    );
    if (response?.queued) {
      return null;
    }
    return response;
  }, {
    timeoutMs: config.settleTimeoutMs,
    intervalMs: config.persistPollIntervalMs,
    description,
  });
}

async function clearBlockState(config, sender, recipient, scenarioFailures, label) {
  try {
    await unblockUser(config.baseURL, sender.devices[0].accessToken, recipient.userId, {
      timeoutMs: config.sendTimeoutMs,
    });
  } catch (error) {
    scenarioFailures.push(`${label}: sender unblock failed: ${error.message || error}`);
  }
  try {
    await unblockUser(config.baseURL, recipient.devices[0].accessToken, sender.userId, {
      timeoutMs: config.sendTimeoutMs,
    });
  } catch (error) {
    scenarioFailures.push(`${label}: recipient unblock failed: ${error.message || error}`);
  }
}

function buildMessagePlan(conversations, messageCount) {
  const plan = [];
  for (let index = 0; index < messageCount; index += 1) {
    const conversation = conversations[index % conversations.length];
    const sender = nextSender(conversation);
    plan.push({
      conversation,
      sender,
      ordinal: index + 1,
    });
  }
  return plan;
}

async function executePlan(config, tracker, conversations, messageCount) {
  const startedAt = Date.now();
  const plan = buildMessagePlan(conversations, messageCount);
  let nextIndex = 0;

  async function worker() {
    while (true) {
      const currentIndex = nextIndex;
      if (currentIndex >= plan.length) {
        return;
      }
      nextIndex += 1;
      const step = plan[currentIndex];
      if (config.rate > 0) {
        const targetAt = startedAt + Math.round((1000 / config.rate) * currentIndex);
        const delay = targetAt - Date.now();
        if (delay > 0) {
          await sleep(delay);
        }
      }
      await sendTrackedMessage(
        config,
        tracker,
        step.conversation,
        step.sender,
        step.ordinal
      );
    }
  }

  const workerCount = Math.min(config.sendConcurrency, Math.max(1, plan.length));
  await Promise.all(Array.from({ length: workerCount }, () => worker()));
}

async function waitForPersistence(config, tracker, topology, warnings) {
  for (const conversation of topology.conversations) {
    if (!conversation.sentMessageIds.length) {
      continue;
    }
    const accessToken = conversation.participants[0].devices[0].accessToken;
    try {
      await poll(async () => {
        const payload = await listConversationMessages(
          config.baseURL,
          accessToken,
          conversation.conversationId,
          {
            timeoutMs: config.readTimeoutMs,
          }
        );
        const items = Array.isArray(payload?.items) ? payload.items : [];
        const byId = new Map(items.map((item) => [item.message_id, item]));
        for (const messageId of conversation.sentMessageIds) {
          const stored = byId.get(messageId);
          if (stored) {
            tracker.notePersisted(messageId, {
              observedAt: Date.now(),
              serverOrder: Number(stored.server_order || 0),
            });
          }
        }
        return conversation.sentMessageIds.every((messageId) => tracker.getMessage(messageId)?.persisted);
      }, {
        timeoutMs: config.settleTimeoutMs,
        intervalMs: config.persistPollIntervalMs,
        description: `persistence for ${conversation.label}`,
      });
    } catch (error) {
      warnings.push(error.message || String(error));
    }
  }
}

async function waitForReceipts(config, tracker, warnings) {
  try {
    await poll(
      () => tracker.hasAllExpectedReceipts(),
      {
        timeoutMs: config.settleTimeoutMs,
        intervalMs: config.receiptPollIntervalMs,
        description: "all expected device receipts",
      }
    );
  } catch (error) {
    warnings.push(error.message || String(error));
  }
}

async function runSmokeScenario(config, tracker, topology) {
  await executePlan(config, tracker, topology.conversations, config.messages);
}

async function runThroughputScenario(config, tracker, topology) {
  await executePlan(config, tracker, topology.conversations, config.messages);
}

async function runConnectScenario(config) {
  if (config.holdMs > 0) {
    await sleep(config.holdMs);
  }
}

async function runSendAbortScenario(config, tracker, topology, scenarioFailures, scenarioDetails) {
  const conversation = topology.conversations[0];
  if (!conversation) {
    throw new Error("send-abort scenario requires at least one conversation");
  }
  const sender = conversation.participants[0];
  const senderDevice = sender.devices[0];
  const text = buildScenarioText(config, "send-abort");
  const idempotencyKey = `${config.runLabel}-${config.scenario}-send-abort-${Date.now()}`;
  const sendStartedAt = Date.now();
  const detail = {};
  const proxy = await createFaultProxy({
    targetBaseURL: config.baseURL,
    dropAfterForwardCount: 1,
    responseDelayMs: config.faultResponseDelayMs,
  });

  try {
    detail.proxy_base_url = proxy.baseURL;
    try {
      detail.initial_response = await sendTextMessage(
        proxy.baseURL,
        senderDevice.accessToken,
        conversation.conversationId,
        text,
        idempotencyKey,
        {
          timeoutMs: config.sendTimeoutMs,
        }
      );
      scenarioFailures.push("send-abort: initial request unexpectedly succeeded");
    } catch (error) {
      detail.initial_error = error.message || String(error);
      if (!isExpectedClientFault(error)) {
        scenarioFailures.push(`send-abort: unexpected initial failure: ${detail.initial_error}`);
      }
    }
  } finally {
    detail.proxy_stats = { ...proxy.stats };
    await proxy.close();
  }

  if (config.faultRetryDelayMs > 0) {
    await sleep(config.faultRetryDelayMs);
  }

  let retryResponse = null;
  try {
    retryResponse = await sendTextMessage(
      config.baseURL,
      senderDevice.accessToken,
      conversation.conversationId,
      text,
      idempotencyKey,
      {
        timeoutMs: config.sendTimeoutMs,
      }
    );
    detail.retry_response = retryResponse;
  } catch (error) {
    detail.retry_error = error.message || String(error);
    scenarioFailures.push(`send-abort: retry failed: ${detail.retry_error}`);
    scenarioDetails.send_abort = detail;
    return;
  }

  let matches = [];
  try {
    matches = await waitForMessagesByText(
      config,
      conversation,
      senderDevice.accessToken,
      text,
      "send-abort persisted message"
    );
    detail.persisted_match_count = matches.length;
  } catch (error) {
    detail.persistence_error = error.message || String(error);
    scenarioFailures.push(`send-abort: failed to observe persisted message: ${detail.persistence_error}`);
    scenarioDetails.send_abort = detail;
    return;
  }

  if (matches.length !== 1) {
    scenarioFailures.push(`send-abort: expected exactly one persisted message, found ${matches.length}`);
  }
  const finalMessage = matches[0];
  detail.final_message_id = finalMessage?.message_id || "";
  registerScenarioAcceptedMessage(
    config,
    tracker,
    conversation,
    sender,
    text,
    finalMessage,
    sendStartedAt,
    retryResponse
  );

  try {
    detail.replay_response = await waitForStableReplay(
      config,
      senderDevice,
      conversation.conversationId,
      text,
      idempotencyKey,
      "send-abort stable replay"
    );
    if (detail.replay_response?.message_id !== finalMessage?.message_id) {
      scenarioFailures.push(
        `send-abort: replay returned ${detail.replay_response?.message_id || "unknown"} instead of ${finalMessage?.message_id || "unknown"}`
      );
    }
  } catch (error) {
    detail.replay_error = error.message || String(error);
    scenarioFailures.push(`send-abort: stable replay failed: ${detail.replay_error}`);
  }

  const finalMatches = await listConversationMessagesByText(
    config,
    senderDevice.accessToken,
    conversation.conversationId,
    text
  );
  detail.final_match_count = finalMatches.length;
  if (finalMatches.length !== 1) {
    scenarioFailures.push(`send-abort: final persisted message count was ${finalMatches.length}, expected 1`);
  }

  scenarioDetails.send_abort = detail;
}

async function runHighLatencyLinkScenario(config, tracker, topology, scenarioFailures, scenarioDetails) {
  const conversation = topology.conversations[0];
  if (!conversation) {
    throw new Error("high-latency-link scenario requires at least one conversation");
  }
  const sender = conversation.participants[0];
  const senderDevice = sender.devices[0];
  const text = buildScenarioText(config, "high-latency-link");
  const idempotencyKey = `${config.runLabel}-${config.scenario}-high-latency-${Date.now()}`;
  const sendStartedAt = Date.now();
  const detail = {};
  const proxy = await createFaultProxy({
    targetBaseURL: config.baseURL,
    requestDelayMs: config.faultRequestDelayMs,
    dropBeforeForwardCount: 1,
  });

  try {
    detail.proxy_base_url = proxy.baseURL;
    try {
      detail.initial_response = await sendTextMessage(
        proxy.baseURL,
        senderDevice.accessToken,
        conversation.conversationId,
        text,
        idempotencyKey,
        {
          timeoutMs: config.sendTimeoutMs,
        }
      );
      scenarioFailures.push("high-latency-link: initial request unexpectedly succeeded");
    } catch (error) {
      detail.initial_error = error.message || String(error);
      if (!isExpectedClientFault(error)) {
        scenarioFailures.push(`high-latency-link: unexpected initial failure: ${detail.initial_error}`);
      }
    }
  } finally {
    detail.proxy_stats = { ...proxy.stats };
    await proxy.close();
  }

  if (config.faultRetryDelayMs > 0) {
    await sleep(config.faultRetryDelayMs);
  }

  let retryResponse = null;
  try {
    retryResponse = await sendTextMessage(
      config.baseURL,
      senderDevice.accessToken,
      conversation.conversationId,
      text,
      idempotencyKey,
      {
        timeoutMs: config.sendTimeoutMs,
      }
    );
    detail.retry_response = retryResponse;
  } catch (error) {
    detail.retry_error = error.message || String(error);
    scenarioFailures.push(`high-latency-link: retry failed: ${detail.retry_error}`);
    scenarioDetails.high_latency_link = detail;
    return;
  }

  let matches = [];
  try {
    matches = await waitForMessagesByText(
      config,
      conversation,
      senderDevice.accessToken,
      text,
      "high-latency-link persisted message"
    );
    detail.persisted_match_count = matches.length;
  } catch (error) {
    detail.persistence_error = error.message || String(error);
    scenarioFailures.push(`high-latency-link: failed to observe persisted message: ${detail.persistence_error}`);
    scenarioDetails.high_latency_link = detail;
    return;
  }

  if (matches.length !== 1) {
    scenarioFailures.push(`high-latency-link: expected exactly one persisted message, found ${matches.length}`);
  }
  const finalMessage = matches[0];
  detail.final_message_id = finalMessage?.message_id || "";
  registerScenarioAcceptedMessage(
    config,
    tracker,
    conversation,
    sender,
    text,
    finalMessage,
    sendStartedAt,
    retryResponse
  );

  try {
    detail.replay_response = await waitForStableReplay(
      config,
      senderDevice,
      conversation.conversationId,
      text,
      idempotencyKey,
      "high-latency-link stable replay"
    );
    if (detail.replay_response?.message_id !== finalMessage?.message_id) {
      scenarioFailures.push(
        `high-latency-link: replay returned ${detail.replay_response?.message_id || "unknown"} instead of ${finalMessage?.message_id || "unknown"}`
      );
    }
  } catch (error) {
    detail.replay_error = error.message || String(error);
    scenarioFailures.push(`high-latency-link: stable replay failed: ${detail.replay_error}`);
  }

  const finalMatches = await listConversationMessagesByText(
    config,
    senderDevice.accessToken,
    conversation.conversationId,
    text
  );
  detail.final_match_count = finalMatches.length;
  if (finalMatches.length !== 1) {
    scenarioFailures.push(`high-latency-link: final persisted message count was ${finalMatches.length}, expected 1`);
  }

  scenarioDetails.high_latency_link = detail;
}

async function runBlockRaceScenario(config, tracker, topology, scenarioFailures, scenarioDetails) {
  const conversation = topology.conversations[0];
  if (!conversation || conversation.participants.length < 2) {
    throw new Error("block-race scenario requires a two-user conversation");
  }
  const sender = conversation.participants[0];
  const recipient = conversation.participants[1];
  const senderDevice = sender.devices[0];
  const detail = {
    accepted_iterations: 0,
    blocked_iterations: 0,
    iterations: [],
  };

  for (let index = 0; index < config.raceIterations; index += 1) {
    const iteration = index + 1;
    const iterationLabel = `block-race iteration ${iteration}`;
    const sendText = buildScenarioText(config, `block-race-${iteration}`);
    const idempotencyKey = `${config.runLabel}-${config.scenario}-block-race-${iteration}-${Date.now()}`;
    const followupText = `${sendText} followup`;
    const sendStartedAt = Date.now();
    const iterationDetail = {
      iteration,
    };

    await clearBlockState(config, sender, recipient, scenarioFailures, `${iterationLabel} preflight`);

    const [sendResult, blockResult] = await Promise.allSettled([
      sendTextMessage(
        config.baseURL,
        senderDevice.accessToken,
        conversation.conversationId,
        sendText,
        idempotencyKey,
        {
          timeoutMs: config.sendTimeoutMs,
        }
      ),
      blockUser(config.baseURL, senderDevice.accessToken, recipient.userId, {
        timeoutMs: config.sendTimeoutMs,
      }),
    ]);

    if (blockResult.status === "rejected") {
      iterationDetail.block_error = blockResult.reason?.message || String(blockResult.reason);
      scenarioFailures.push(`${iterationLabel}: block request failed: ${iterationDetail.block_error}`);
    } else {
      iterationDetail.blocked = true;
    }

    if (sendResult.status === "fulfilled") {
      detail.accepted_iterations += 1;
      iterationDetail.initial_send = "accepted";
      iterationDetail.initial_response = sendResult.value;
      let matches = [];
      try {
        matches = await waitForMessagesByText(
          config,
          conversation,
          senderDevice.accessToken,
          sendText,
          `${iterationLabel} persisted message`
        );
        iterationDetail.persisted_match_count = matches.length;
      } catch (error) {
        iterationDetail.persistence_error = error.message || String(error);
        scenarioFailures.push(`${iterationLabel}: failed to observe persisted message: ${iterationDetail.persistence_error}`);
      }
      if (matches.length !== 1) {
        scenarioFailures.push(`${iterationLabel}: expected exactly one persisted message, found ${matches.length}`);
      }
      if (matches[0]) {
        registerScenarioAcceptedMessage(
          config,
          tracker,
          conversation,
          sender,
          sendText,
          matches[0],
          sendStartedAt,
          sendResult.value
        );
      }
    } else if (isBlockedError(sendResult.reason)) {
      detail.blocked_iterations += 1;
      iterationDetail.initial_send = "blocked";
      iterationDetail.initial_error = sendResult.reason?.message || String(sendResult.reason);
    } else {
      iterationDetail.initial_send = "unexpected_failure";
      iterationDetail.initial_error = sendResult.reason?.message || String(sendResult.reason);
      scenarioFailures.push(`${iterationLabel}: unexpected initial send outcome: ${iterationDetail.initial_error}`);
    }

    try {
      iterationDetail.followup_response = await sendTextMessage(
        config.baseURL,
        senderDevice.accessToken,
        conversation.conversationId,
        followupText,
        `${idempotencyKey}-followup`,
        {
          timeoutMs: config.sendTimeoutMs,
        }
      );
      scenarioFailures.push(`${iterationLabel}: post-block send unexpectedly succeeded`);
    } catch (error) {
      iterationDetail.followup_error = error.message || String(error);
      if (!isBlockedError(error)) {
        scenarioFailures.push(`${iterationLabel}: unexpected post-block outcome: ${iterationDetail.followup_error}`);
      }
    }

    await clearBlockState(config, sender, recipient, scenarioFailures, `${iterationLabel} cleanup`);
    detail.iterations.push(iterationDetail);
  }

  scenarioDetails.block_race = detail;
}

async function runReconnectStormScenario(config, tracker, topology) {
  const stormClients = topology.clients.slice(
    0,
    config.reconnectStormSize > 0 ? config.reconnectStormSize : topology.clients.length
  );

  await Promise.allSettled(stormClients.map((client) => client.disconnect({
    code: 1000,
    reason: "storm_disconnect",
  })));

  if (config.reconnectPauseMs > 0) {
    await sleep(config.reconnectPauseMs);
  }

  for (let index = 0; index < stormClients.length; index += config.reconnectBatchSize) {
    const batch = stormClients.slice(index, index + config.reconnectBatchSize);
    await Promise.all(batch.map(async (client) => {
      try {
        await client.connect({
          timeoutMs: config.connectTimeoutMs,
        });
      } catch (error) {
        tracker.noteClientError(client.label, error);
      }
    }));
    if (config.reconnectBatchIntervalMs > 0 && index + config.reconnectBatchSize < stormClients.length) {
      await sleep(config.reconnectBatchIntervalMs);
    }
  }

  if (config.holdMs > 0) {
    await sleep(config.holdMs);
  }
}

async function markSyncReceiptsForDevice(config, tracker, conversation, device, messageIDs) {
  if (!messageIDs.length) {
    return;
  }
  const payload = await listConversationMessages(
    config.baseURL,
    device.accessToken,
    conversation.conversationId,
    {
      timeoutMs: config.readTimeoutMs,
    }
  );
  const items = Array.isArray(payload?.items) ? payload.items : [];
  const byId = new Map(items.map((item) => [item.message_id, item]));
  for (const messageID of messageIDs) {
    const item = byId.get(messageID);
    if (!item) {
      continue;
    }
    tracker.noteReceipt(messageID, {
      userId: device.client.userId,
      deviceId: device.deviceId,
      conversationId: conversation.conversationId,
      observedAt: Date.now(),
      serverOrder: Number(item.server_order || 0),
      source: "sync",
      wsVersion: config.wsVersion,
    });
  }
}

async function runReconnectScenario(config, tracker, topology) {
  const conversation = topology.conversations[0];
  const receiver = conversation.participants[1];
  const reconnectDevice = receiver.devices[1];

  await executePlan(config, tracker, [conversation], config.reconnectAfter);
  await reconnectDevice.client.disconnect({
    code: 1000,
    reason: "scenario_disconnect",
  });

  const offlineMessageIDs = [];
  const offlineStart = config.reconnectAfter + 1;
  for (let index = 0; index < config.offlineMessages; index += 1) {
    const sender = nextSender(conversation);
    const messageId = await sendTrackedMessage(
      config,
      tracker,
      conversation,
      sender,
      offlineStart + index
    );
    if (messageId) {
      offlineMessageIDs.push(messageId);
    }
    if (config.rate > 0) {
      await sleep(Math.round(1000 / config.rate));
    }
  }

  await reconnectDevice.client.reconnect({
    timeoutMs: config.connectTimeoutMs,
  });

  if (config.wsVersion === "v1") {
    await markSyncReceiptsForDevice(
      config,
      tracker,
      conversation,
      reconnectDevice,
      offlineMessageIDs
    );
  }

  const postStart = config.reconnectAfter + config.offlineMessages + 1;
  for (let index = 0; index < config.postReconnectMessages; index += 1) {
    const sender = nextSender(conversation);
    await sendTrackedMessage(
      config,
      tracker,
      conversation,
      sender,
      postStart + index
    );
    if (config.rate > 0) {
      await sleep(Math.round(1000 / config.rate));
    }
  }
}

function serializeTopology(topology) {
  return {
    users: topology.users.map((user) => ({
      label: user.label,
      user_id: user.userId,
      phone_e164: user.phoneE164,
      devices: user.devices.map((device) => ({
        label: device.label,
        device_id: device.deviceId,
      })),
    })),
    conversations: topology.conversations.map((conversation) => ({
      label: conversation.label,
      conversation_id: conversation.conversationId,
      participants: conversation.participants.map((user) => ({
        label: user.label,
        user_id: user.userId,
      })),
      sent_message_ids: conversation.sentMessageIds,
    })),
  };
}

function countConnectedClients(topology) {
  return topology?.clients?.filter((client) => client && client.closed === false).length || 0;
}

async function shutdownTopology(topology) {
  if (!topology?.clients?.length) {
    return;
  }
  const batchSize = 250;
  for (let index = 0; index < topology.clients.length; index += batchSize) {
    const batch = topology.clients.slice(index, index + batchSize);
    await Promise.allSettled(batch.map((client) => client.disconnect({
      code: 1000,
      reason: "run_complete",
      timeoutMs: 250,
    })));
  }

  for (const client of topology.clients) {
    if (!client || client.closed) {
      continue;
    }
    client.forceClose();
  }
}

function runPassed(summary) {
  return summary.send_failures === 0
    && summary.unpersisted_messages === 0
    && summary.lost_deliveries === 0
    && summary.duplicate_receipts === 0
    && summary.ordering_violations === 0
    && summary.client_errors === 0
    && (!summary.scenario_failures || summary.scenario_failures.length === 0);
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    printUsage();
    return;
  }

  const config = buildConfig(args);
  if (config.dryRun) {
    console.log(JSON.stringify(config, null, 2));
    return;
  }

  const startedAt = Date.now();
  const runID = String(startedAt);
  const runDir = createRunDirectory(config.reportDir, config.scenario, config.runLabel, startedAt);
  const tracker = new CorrectnessTracker({
    scenario: config.scenario,
    wsVersion: config.wsVersion,
  });
  const warnings = [];
  const scenarioFailures = [];
  const scenarioDetails = {};
  const metrics = [];
  const commitSHA = currentCommitSHA();
  let connectedDevicesAtMeasurement = 0;
  let topology = null;

  try {
    metrics.push(...await captureMetrics("start", config, runDir));
    topology = await provisionTopology(config, tracker, runID);
    connectedDevicesAtMeasurement = countConnectedClients(topology);
    metrics.push(...await captureMetrics("connected", config, runDir));

    if (config.scenario === "smoke") {
      await runSmokeScenario(config, tracker, topology);
    } else if (config.scenario === "throughput") {
      await runThroughputScenario(config, tracker, topology);
    } else if (config.scenario === "connect") {
      await runConnectScenario(config, tracker, topology);
    } else if (config.scenario === "reconnect-storm") {
      await runReconnectStormScenario(config, tracker, topology);
      connectedDevicesAtMeasurement = countConnectedClients(topology);
      metrics.push(...await captureMetrics("reconnected", config, runDir));
    } else if (config.scenario === "send-abort") {
      await runSendAbortScenario(config, tracker, topology, scenarioFailures, scenarioDetails);
    } else if (config.scenario === "high-latency-link") {
      await runHighLatencyLinkScenario(config, tracker, topology, scenarioFailures, scenarioDetails);
    } else if (config.scenario === "block-race") {
      await runBlockRaceScenario(config, tracker, topology, scenarioFailures, scenarioDetails);
    } else {
      await runReconnectScenario(config, tracker, topology);
    }

    connectedDevicesAtMeasurement = countConnectedClients(topology);

    await waitForPersistence(config, tracker, topology, warnings);
    await waitForReceipts(config, tracker, warnings);
  } finally {
    metrics.push(...await captureMetrics("end", config, runDir));
    await shutdownTopology(topology);
  }

  const completedAt = Date.now();
  const summary = tracker.buildSummary({
    startedAt,
    completedAt,
    durationMs: completedAt - startedAt,
    baseURL: config.baseURL,
    runLabel: config.runLabel,
    commitSHA,
    connectedDevices: connectedDevicesAtMeasurement,
    logicalUsers: topology?.users?.length || 0,
  });
  if (warnings.length) {
    summary.validation_warnings = warnings;
  }
  if (scenarioFailures.length) {
    summary.scenario_failures = scenarioFailures;
  }

  writeRunArtifacts(runDir, {
    config,
    summary,
    messages: tracker.getMessageRecords(),
    topology: serializeTopology(topology || { users: [], conversations: [] }),
    metrics,
    details: Object.keys(scenarioDetails).length ? scenarioDetails : null,
  });

  console.log(JSON.stringify({
    run_dir: runDir,
    scenario: summary.scenario,
    messages_accepted: summary.messages_accepted,
    queued_accepts: summary.queued_accepts,
    expected_deliveries: summary.expected_deliveries,
    successful_deliveries: summary.successful_deliveries,
    lost_deliveries: summary.lost_deliveries,
    duplicate_receipts: summary.duplicate_receipts,
    unpersisted_messages: summary.unpersisted_messages,
    ordering_violations: summary.ordering_violations,
    connected_devices: summary.connected_devices,
    client_errors: summary.client_errors,
    scenario_failures: summary.scenario_failures?.length || 0,
    delivery_p95_ms: summary.delivery_latency_ms.p95,
  }, null, 2));

  if (!runPassed(summary)) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error?.stack || error?.message || error);
  if (error?.cause) {
    console.error("Cause:", error.cause?.stack || error.cause?.message || error.cause);
  }
  process.exitCode = 1;
});
