"use strict";

const fs = require("node:fs");
const path = require("node:path");

function ensureDir(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
  return targetPath;
}

function sanitizeSegment(value) {
  return String(value || "run")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 80) || "run";
}

function timestampSegment(startedAt) {
  return new Date(startedAt)
    .toISOString()
    .replace(/:/g, "-")
    .replace(/\.\d{3}Z$/, "Z");
}

function createRunDirectory(baseDir, scenario, runLabel, startedAt) {
  const dir = path.resolve(baseDir, `${timestampSegment(startedAt)}-${sanitizeSegment(scenario)}-${sanitizeSegment(runLabel)}`);
  ensureDir(dir);
  return dir;
}

function buildMarkdownSummary(summary) {
  const lines = [
    "# OHMF Stress Run",
    "",
    `- Scenario: \`${summary.scenario}\``,
    `- Run label: \`${summary.run_label}\``,
    `- Commit: \`${summary.commit_sha || "unknown"}\``,
    `- Base URL: \`${summary.base_url}\``,
    `- WebSocket mode: \`${summary.ws_version}\``,
    `- Started: ${summary.started_at || "unknown"}`,
    `- Completed: ${summary.completed_at || "unknown"}`,
    `- Duration: ${summary.duration_ms ?? "unknown"} ms`,
    "",
    "## Results",
    "",
    `- Messages requested: ${summary.messages_requested}`,
    `- Messages accepted: ${summary.messages_accepted}`,
    `- Queued accepts: ${summary.queued_accepts}`,
    `- Messages persisted: ${summary.messages_persisted}`,
    `- Expected deliveries: ${summary.expected_deliveries}`,
    `- Successful deliveries: ${summary.successful_deliveries}`,
    `- Realtime deliveries: ${summary.realtime_deliveries}`,
    `- Sync recoveries: ${summary.sync_recoveries}`,
    `- Duplicate receipts: ${summary.duplicate_receipts}`,
    `- Lost deliveries: ${summary.lost_deliveries}`,
    `- Unpersisted messages: ${summary.unpersisted_messages}`,
    `- Ordering violations: ${summary.ordering_violations}`,
    `- Send failures: ${summary.send_failures}`,
    `- Client errors: ${summary.client_errors}`,
    "",
    "## Latency",
    "",
    `- Accept p95: ${summary.accept_latency_ms.p95 ?? "n/a"} ms`,
    `- Delivery p95: ${summary.delivery_latency_ms.p95 ?? "n/a"} ms`,
    "",
  ];

  if (summary.missing_receipts?.length) {
    lines.push("## Missing Receipts", "");
    for (const item of summary.missing_receipts.slice(0, 20)) {
      lines.push(`- ${item.messageId} -> ${item.userId}/${item.deviceId}`);
    }
    lines.push("");
  }

  return `${lines.join("\n")}\n`;
}

function writeRunArtifacts(runDir, payload) {
  ensureDir(runDir);
  fs.writeFileSync(
    path.join(runDir, "config.json"),
    `${JSON.stringify(payload.config, null, 2)}\n`,
    "utf8"
  );
  fs.writeFileSync(
    path.join(runDir, "summary.json"),
    `${JSON.stringify(payload.summary, null, 2)}\n`,
    "utf8"
  );
  fs.writeFileSync(
    path.join(runDir, "summary.md"),
    buildMarkdownSummary(payload.summary),
    "utf8"
  );
  fs.writeFileSync(
    path.join(runDir, "messages.json"),
    `${JSON.stringify(payload.messages, null, 2)}\n`,
    "utf8"
  );
  fs.writeFileSync(
    path.join(runDir, "topology.json"),
    `${JSON.stringify(payload.topology, null, 2)}\n`,
    "utf8"
  );
  if (payload.metrics && payload.metrics.length) {
    fs.writeFileSync(
      path.join(runDir, "metrics.json"),
      `${JSON.stringify(payload.metrics, null, 2)}\n`,
      "utf8"
    );
  }
}

module.exports = {
  createRunDirectory,
  ensureDir,
  writeRunArtifacts,
};
