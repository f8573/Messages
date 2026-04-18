"use strict";

function deviceKey(userId, deviceId) {
  return `${userId}:${deviceId || "unknown"}`;
}

function percentile(values, ratio) {
  if (!values.length) {
    return null;
  }
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * ratio) - 1));
  return Number(sorted[index].toFixed(2));
}

function summarizeSeries(values) {
  if (!values.length) {
    return {
      count: 0,
      min: null,
      max: null,
      avg: null,
      p50: null,
      p95: null,
      p99: null,
    };
  }
  const total = values.reduce((sum, value) => sum + value, 0);
  return {
    count: values.length,
    min: Number(Math.min(...values).toFixed(2)),
    max: Number(Math.max(...values).toFixed(2)),
    avg: Number((total / values.length).toFixed(2)),
    p50: percentile(values, 0.5),
    p95: percentile(values, 0.95),
    p99: percentile(values, 0.99),
  };
}

class CorrectnessTracker {
  constructor(options = {}) {
    this.scenario = options.scenario || "unknown";
    this.wsVersion = options.wsVersion || "v1";
    this.messages = new Map();
    this.sendFailures = [];
    this.clientErrors = [];
    this.deliveryUpdates = [];
    this.unexpectedReceipts = [];
    this.orderingViolations = [];
    this.deviceOrdering = new Map();
  }

  registerAccepted(message) {
    const expectedRecipients = new Map();
    for (const recipient of message.expectedRecipientDevices || []) {
      expectedRecipients.set(deviceKey(recipient.userId, recipient.deviceId), {
        userId: recipient.userId,
        deviceId: recipient.deviceId,
        label: recipient.label || "",
      });
    }

    const record = {
      messageId: message.messageId,
      conversationId: message.conversationId,
      senderUserId: message.senderUserId,
      senderDeviceId: message.senderDeviceId,
      text: message.text,
      sendStartedAt: message.sendStartedAt,
      acceptedAt: message.acceptedAt,
      response: message.response || {},
      queued: Boolean(message.response?.queued),
      serverOrder: Number(message.response?.server_order || message.serverOrder || 0),
      expectedRecipients,
      actualReceipts: new Map(),
      persisted: false,
      persistedAt: null,
      persistedServerOrder: null,
      duplicateCount: 0,
      unexpectedReceiptCount: 0,
    };
    this.messages.set(record.messageId, record);
    return record;
  }

  noteSendFailure(failure) {
    this.sendFailures.push({
      at: Date.now(),
      ...failure,
      error: failure?.error?.message || String(failure?.error || "unknown send failure"),
    });
  }

  noteClientError(label, error) {
    this.clientErrors.push({
      at: Date.now(),
      label,
      error: error?.message || String(error),
    });
  }

  noteDeliveryUpdate(update) {
    this.deliveryUpdates.push({
      at: Date.now(),
      ...update,
    });
  }

  notePersisted(messageId, details = {}) {
    const record = this.messages.get(messageId);
    if (!record) {
      return;
    }
    record.persisted = true;
    record.persistedAt = Number.isFinite(details.observedAt) ? details.observedAt : Date.now();
    record.persistedServerOrder = Number.isFinite(details.serverOrder)
      ? Number(details.serverOrder)
      : record.serverOrder;
  }

  noteReceipt(messageId, receipt = {}) {
    const record = this.messages.get(messageId);
    const normalized = {
      userId: receipt.userId,
      deviceId: receipt.deviceId,
      deviceKey: deviceKey(receipt.userId, receipt.deviceId),
      conversationId: receipt.conversationId || "",
      observedAt: Number.isFinite(receipt.observedAt) ? receipt.observedAt : Date.now(),
      source: receipt.source || "realtime",
      serverOrder: Number.isFinite(receipt.serverOrder) ? Number(receipt.serverOrder) : 0,
      userEventId: Number.isFinite(receipt.userEventId) ? Number(receipt.userEventId) : null,
      wsVersion: receipt.wsVersion || this.wsVersion,
    };

    if (!record) {
      this.unexpectedReceipts.push({
        messageId,
        reason: "unknown_message",
        ...normalized,
      });
      return;
    }

    if (record.actualReceipts.has(normalized.deviceKey)) {
      record.duplicateCount += 1;
      return;
    }

    record.actualReceipts.set(normalized.deviceKey, normalized);
    if (!record.expectedRecipients.has(normalized.deviceKey)) {
      record.unexpectedReceiptCount += 1;
      this.unexpectedReceipts.push({
        messageId,
        reason: "unexpected_device",
        ...normalized,
      });
      return;
    }

    const orderKey = `${normalized.deviceKey}|${record.conversationId}`;
    const prior = this.deviceOrdering.get(orderKey);
    const orderValue = normalized.serverOrder || record.serverOrder || 0;
    if (prior && orderValue > 0 && orderValue < prior.serverOrder) {
      this.orderingViolations.push({
        messageId,
        deviceKey: normalized.deviceKey,
        conversationId: record.conversationId,
        priorMessageId: prior.messageId,
        priorServerOrder: prior.serverOrder,
        serverOrder: orderValue,
      });
    }
    if (!prior || orderValue >= prior.serverOrder) {
      this.deviceOrdering.set(orderKey, {
        messageId,
        serverOrder: orderValue,
      });
    }
  }

  getMessage(messageId) {
    return this.messages.get(messageId) || null;
  }

  getMessageRecords() {
    return [...this.messages.values()].map((record) => ({
      message_id: record.messageId,
      conversation_id: record.conversationId,
      sender_user_id: record.senderUserId,
      sender_device_id: record.senderDeviceId,
      text: record.text,
      send_started_at_ms: record.sendStartedAt,
      accepted_at_ms: record.acceptedAt,
      accepted_latency_ms: record.acceptedAt - record.sendStartedAt,
      persisted: record.persisted,
      persisted_at_ms: record.persistedAt,
      persisted_server_order: record.persistedServerOrder,
      expected_recipient_devices: [...record.expectedRecipients.values()],
      actual_receipts: [...record.actualReceipts.values()],
      duplicate_count: record.duplicateCount,
      unexpected_receipt_count: record.unexpectedReceiptCount,
    }));
  }

  getMissingReceipts() {
    const missing = [];
    for (const record of this.messages.values()) {
      for (const [expectedKey, recipient] of record.expectedRecipients.entries()) {
        if (!record.actualReceipts.has(expectedKey)) {
          missing.push({
            messageId: record.messageId,
            conversationId: record.conversationId,
            userId: recipient.userId,
            deviceId: recipient.deviceId,
            label: recipient.label,
          });
        }
      }
    }
    return missing;
  }

  hasAllPersisted() {
    return [...this.messages.values()].every((record) => record.persisted);
  }

  hasAllExpectedReceipts() {
    return this.getMissingReceipts().length === 0;
  }

  buildSummary(details = {}) {
    const records = [...this.messages.values()];
    const expectedDeliveries = records.reduce((sum, record) => sum + record.expectedRecipients.size, 0);
    let successfulDeliveries = 0;
    let realtimeDeliveries = 0;
    let syncRecoveries = 0;
    let duplicateCount = 0;
    let unpersistedMessages = 0;
    let queuedAccepts = 0;
    const acceptLatency = [];
    const deliveryLatency = [];

    for (const record of records) {
      acceptLatency.push(record.acceptedAt - record.sendStartedAt);
      if (record.queued) {
        queuedAccepts += 1;
      }
      if (!record.persisted) {
        unpersistedMessages += 1;
      }
      duplicateCount += record.duplicateCount;

      let latestExpectedReceipt = null;
      let matchedExpectedReceipts = 0;
      for (const expectedKey of record.expectedRecipients.keys()) {
        const receipt = record.actualReceipts.get(expectedKey);
        if (!receipt) {
          continue;
        }
        matchedExpectedReceipts += 1;
        successfulDeliveries += 1;
        if (receipt.source === "sync") {
          syncRecoveries += 1;
        } else {
          realtimeDeliveries += 1;
        }
        if (!latestExpectedReceipt || receipt.observedAt > latestExpectedReceipt.observedAt) {
          latestExpectedReceipt = receipt;
        }
      }
      if (latestExpectedReceipt && record.expectedRecipients.size > 0 && matchedExpectedReceipts === record.expectedRecipients.size) {
        deliveryLatency.push(latestExpectedReceipt.observedAt - record.sendStartedAt);
      }
    }

    return {
      scenario: this.scenario,
      ws_version: this.wsVersion,
      started_at: details.startedAt ? new Date(details.startedAt).toISOString() : null,
      completed_at: details.completedAt ? new Date(details.completedAt).toISOString() : null,
      duration_ms: Number.isFinite(details.durationMs) ? details.durationMs : null,
      base_url: details.baseURL || "",
      run_label: details.runLabel || "",
      commit_sha: details.commitSHA || "",
      connected_devices: Number.isFinite(details.connectedDevices) ? details.connectedDevices : null,
      logical_users: Number.isFinite(details.logicalUsers) ? details.logicalUsers : null,
      messages_requested: records.length + this.sendFailures.length,
      messages_accepted: records.length,
      queued_accepts: queuedAccepts,
      messages_persisted: records.length - unpersistedMessages,
      expected_deliveries: expectedDeliveries,
      successful_deliveries: successfulDeliveries,
      realtime_deliveries: realtimeDeliveries,
      sync_recoveries: syncRecoveries,
      duplicate_receipts: duplicateCount,
      lost_deliveries: expectedDeliveries - successfulDeliveries,
      unpersisted_messages: unpersistedMessages,
      unexpected_receipts: this.unexpectedReceipts.length,
      ordering_violations: this.orderingViolations.length,
      send_failures: this.sendFailures.length,
      client_errors: this.clientErrors.length,
      accept_latency_ms: summarizeSeries(acceptLatency),
      delivery_latency_ms: summarizeSeries(deliveryLatency),
      missing_receipts: this.getMissingReceipts(),
      send_failure_details: this.sendFailures,
      client_error_details: this.clientErrors,
      ordering_violation_details: this.orderingViolations,
    };
  }
}

module.exports = {
  CorrectnessTracker,
  deviceKey,
};
