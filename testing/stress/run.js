#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const {
  buildPhone,
  createDirectConversation,
  createUserWithDevices,
  listConversationMessages,
  poll,
  requestText,
  sendTextMessage,
  sleep,
} = require("./lib/api");
const { CorrectnessTracker } = require("./lib/correctness-tracker");
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
  const scenario = String(
    args.scenario || process.env.OHMF_STRESS_SCENARIO || "smoke"
  ).trim().toLowerCase();

  const defaultMessages = scenario === "throughput"
    ? 0
    : scenario === "reconnect"
      ? 12
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
    users: Math.max(2, integerOption(args.users ?? process.env.OHMF_STRESS_USERS, scenario === "throughput" ? 4 : 2)),
    devicesPerUser: Math.max(1, integerOption(args.devicesPerUser ?? process.env.OHMF_STRESS_DEVICES_PER_USER, 2)),
    messages: Math.max(0, integerOption(args.messages ?? process.env.OHMF_STRESS_MESSAGES, defaultMessages)),
    rate: Math.max(0, numberOption(args.rate ?? process.env.OHMF_STRESS_RATE, scenario === "throughput" ? 10 : 2)),
    durationMs: Math.max(0, integerOption(args.durationMs ?? process.env.OHMF_STRESS_DURATION_MS, scenario === "throughput" ? 30000 : 0)),
    settleTimeoutMs: Math.max(1000, integerOption(args.settleTimeoutMs ?? process.env.OHMF_STRESS_SETTLE_TIMEOUT_MS, 20000)),
    persistPollIntervalMs: Math.max(50, integerOption(args.persistPollIntervalMs ?? process.env.OHMF_STRESS_PERSIST_POLL_INTERVAL_MS, 500)),
    receiptPollIntervalMs: Math.max(25, integerOption(args.receiptPollIntervalMs ?? process.env.OHMF_STRESS_RECEIPT_POLL_INTERVAL_MS, 100)),
    connectDelayMs: Math.max(0, integerOption(args.connectDelayMs ?? process.env.OHMF_STRESS_CONNECT_DELAY_MS, 50)),
    reconnectAfter: Math.max(1, integerOption(args.reconnectAfter ?? process.env.OHMF_STRESS_RECONNECT_AFTER, 4)),
    offlineMessages: Math.max(1, integerOption(args.offlineMessages ?? process.env.OHMF_STRESS_OFFLINE_MESSAGES, 4)),
    postReconnectMessages: Math.max(0, integerOption(args.postReconnectMessages ?? process.env.OHMF_STRESS_POST_RECONNECT_MESSAGES, 4)),
    reportDir: path.resolve(
      repoRoot,
      String(args.reportDir || process.env.OHMF_STRESS_REPORT_DIR || path.join("testing", "stress", "reports"))
    ),
    runLabel: String(args.runLabel || process.env.OHMF_STRESS_RUN_LABEL || scenario).trim(),
    metricsUrls: [
      ...csvList(process.env.OHMF_STRESS_METRICS_URLS),
      ...(args.metricsUrls || []),
    ],
    dryRun: Boolean(args.dryRun || process.env.OHMF_STRESS_DRY_RUN === "1"),
  };

  if (!["smoke", "throughput", "reconnect"].includes(config.scenario)) {
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

  return config;
}

function printUsage() {
  console.log("Usage: node ./testing/stress/run.js [options]");
  console.log("");
  console.log("Options:");
  console.log("  --scenario smoke|throughput|reconnect");
  console.log("  --base-url <url>");
  console.log("  --ws-version v1|v2");
  console.log("  --users <count>");
  console.log("  --devices-per-user <count>");
  console.log("  --messages <count>");
  console.log("  --rate <messages-per-second>");
  console.log("  --duration-ms <milliseconds>");
  console.log("  --settle-timeout-ms <milliseconds>");
  console.log("  --connect-delay-ms <milliseconds>");
  console.log("  --metrics-url <absolute-url> (repeatable)");
  console.log("  --report-dir <path>");
  console.log("  --run-label <label>");
  console.log("  --dry-run");
  console.log("");
  console.log("Environment:");
  console.log("  OHMF_STRESS_BASE_URL, OHMF_STRESS_SCENARIO, OHMF_STRESS_WS_VERSION,");
  console.log("  OHMF_STRESS_USERS, OHMF_STRESS_DEVICES_PER_USER, OHMF_STRESS_MESSAGES,");
  console.log("  OHMF_STRESS_RATE, OHMF_STRESS_DURATION_MS, OHMF_STRESS_SETTLE_TIMEOUT_MS,");
  console.log("  OHMF_STRESS_CONNECT_DELAY_MS, OHMF_STRESS_METRICS_URLS, OHMF_STRESS_REPORT_DIR.");
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
    if (!details.intentional) {
      tracker.noteClientError(
        client.label,
        new Error(`socket closed unexpectedly (${details.code} ${details.reason})`)
      );
    }
  });
}

async function provisionTopology(config, tracker, runID) {
  const users = [];
  for (let index = 0; index < config.users; index += 1) {
    const user = await createUserWithDevices(
      config.baseURL,
      buildPhone(runID, index + 1),
      config.devicesPerUser,
      `stress-user-${index + 1}`
    );
    users.push(user);
  }

  const hub = users[0];
  const conversations = [];
  for (let index = 1; index < users.length; index += 1) {
    const recipient = users[index];
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
      senderIndex: 0,
      sentMessageIds: [],
    });
  }

  const clients = [];
  for (const user of users) {
    for (const device of user.devices) {
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
      await client.connect();
      device.client = client;
      clients.push(client);
      if (config.connectDelayMs > 0) {
        await sleep(config.connectDelayMs);
      }
    }
  }

  return {
    users,
    hubUserId: hub.userId,
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
      idempotencyKey
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

async function executePlan(config, tracker, conversations, messageCount) {
  const startedAt = Date.now();
  for (let index = 0; index < messageCount; index += 1) {
    if (config.rate > 0) {
      const targetAt = startedAt + Math.round((1000 / config.rate) * index);
      const delay = targetAt - Date.now();
      if (delay > 0) {
        await sleep(delay);
      }
    }

    const conversation = conversations[index % conversations.length];
    const sender = nextSender(conversation);
    await sendTrackedMessage(config, tracker, conversation, sender, index + 1);
  }
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
          conversation.conversationId
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

async function markSyncReceiptsForDevice(config, tracker, conversation, device, messageIDs) {
  if (!messageIDs.length) {
    return;
  }
  const payload = await listConversationMessages(
    config.baseURL,
    device.accessToken,
    conversation.conversationId
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

  await reconnectDevice.client.reconnect();

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

async function shutdownTopology(topology) {
  if (!topology?.clients?.length) {
    return;
  }
  await Promise.allSettled(topology.clients.map((client) => client.disconnect({
    code: 1000,
    reason: "run_complete",
  })));
}

function runPassed(summary) {
  return summary.send_failures === 0
    && summary.unpersisted_messages === 0
    && summary.lost_deliveries === 0
    && summary.duplicate_receipts === 0
    && summary.ordering_violations === 0
    && summary.client_errors === 0;
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
  const metrics = [];
  const commitSHA = currentCommitSHA();
  let topology = null;

  try {
    metrics.push(...await captureMetrics("start", config, runDir));
    topology = await provisionTopology(config, tracker, runID);

    if (config.scenario === "smoke") {
      await runSmokeScenario(config, tracker, topology);
    } else if (config.scenario === "throughput") {
      await runThroughputScenario(config, tracker, topology);
    } else {
      await runReconnectScenario(config, tracker, topology);
    }

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
    connectedDevices: topology?.clients?.length || 0,
    logicalUsers: topology?.users?.length || 0,
  });
  if (warnings.length) {
    summary.validation_warnings = warnings;
  }

  writeRunArtifacts(runDir, {
    config,
    summary,
    messages: tracker.getMessageRecords(),
    topology: serializeTopology(topology || { users: [], conversations: [] }),
    metrics,
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
    delivery_p95_ms: summary.delivery_latency_ms.p95,
  }, null, 2));

  if (!runPassed(summary)) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error.message || error);
  process.exitCode = 1;
});
