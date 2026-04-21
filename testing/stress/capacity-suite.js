#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFile, spawn } = require("node:child_process");
const { recoverStageServices, waitForStageRecovery } = require("./lib/suite-recovery");

const repoRoot = path.resolve(__dirname, "..", "..");

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
    const key = String(rawFlag)
      .replace(/^-+/, "")
      .replace(/-([a-z])/g, (_, chr) => chr.toUpperCase());
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

function sanitizeSegment(value) {
  return String(value || "suite")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 80) || "suite";
}

function timestampSegment(startedAt) {
  return new Date(startedAt)
    .toISOString()
    .replace(/:/g, "-")
    .replace(/\.\d{3}Z$/, "Z");
}

function ensureDir(targetPath) {
  fs.mkdirSync(targetPath, { recursive: true });
  return targetPath;
}

function buildConfig(args) {
  const profile = String(args.profile || process.env.OHMF_STRESS_PROFILE || "worst-case")
    .trim()
    .toLowerCase();
  if (!["worst-case", "normal"].includes(profile)) {
    throw new Error(`unsupported profile "${profile}"`);
  }

  const totalClients = Math.max(
    2,
    integerOption(args.totalClients ?? process.env.OHMF_STRESS_TOTAL_CLIENTS, profile === "normal" ? 10000 : 5000)
  );
  const uniqueUserRatio = Math.max(
    0.01,
    Math.min(1, numberOption(args.uniqueUserRatio ?? process.env.OHMF_STRESS_UNIQUE_USER_RATIO, 0.75))
  );
  const logicalUsers = Math.max(2, Math.min(totalClients, Math.ceil(totalClients * uniqueUserRatio)));
  const activeConversations = Math.max(
    1,
    Math.min(
      logicalUsers - 1,
      integerOption(
        args.activeConversations ?? process.env.OHMF_STRESS_ACTIVE_CONVERSATIONS,
        profile === "normal"
          ? Math.min(500, Math.max(100, Math.floor(logicalUsers * 0.05)))
          : Math.min(750, Math.max(250, Math.floor(logicalUsers * 0.1)))
      )
    )
  );

  const startedAt = Date.now();
  const runLabel = String(
    args.runLabel
      || process.env.OHMF_STRESS_RUN_LABEL
      || `${profile}-${totalClients}-75pct`
  ).trim() || `${profile}-${totalClients}`;
  const suiteDir = ensureDir(
    path.resolve(
      repoRoot,
      "testing",
      "stress",
      "reports",
      `${timestampSegment(startedAt)}-capacity-${sanitizeSegment(runLabel)}`
    )
  );

  return {
    startedAt,
    suiteDir,
    profile,
    runLabel,
    baseURL: String(
      args.baseURL
      || process.env.OHMF_STRESS_BASE_URL
      || process.env.OHMF_API_BASE_URL
      || "http://127.0.0.1:18080"
    ).replace(/\/+$/, ""),
    totalClients,
    uniqueUserRatio,
    logicalUsers,
    activeConversations,
    connectBatchSize: Math.max(
      1,
      integerOption(args.connectBatchSize ?? process.env.OHMF_STRESS_CONNECT_BATCH_SIZE, 100)
    ),
    connectTimeoutMs: Math.max(
      250,
      integerOption(args.connectTimeoutMs ?? process.env.OHMF_STRESS_CONNECT_TIMEOUT_MS, 15000)
    ),
    connectHoldMs: Math.max(0, integerOption(args.connectHoldMs ?? process.env.OHMF_STRESS_CONNECT_HOLD_MS, profile === "normal" ? 2000 : 1000)),
    stageTimeoutMs: Math.max(
      60000,
      integerOption(args.stageTimeoutMs ?? process.env.OHMF_STRESS_STAGE_TIMEOUT_MS, profile === "normal" ? 1800000 : 1200000)
    ),
    failureDownMs: Math.max(
      1000,
      integerOption(args.failureDownMs ?? process.env.OHMF_STRESS_FAILURE_DOWN_MS, 8000)
    ),
    failureDelayMs: Math.max(
      0,
      integerOption(args.failureDelayMs ?? process.env.OHMF_STRESS_FAILURE_DELAY_MS, 2000)
    ),
    metricsUrls: [
      ...csvList(process.env.OHMF_STRESS_METRICS_URLS),
      ...(args.metricsUrls || []),
    ],
    topologyFile: args.topologyFile || process.env.OHMF_STRESS_TOPOLOGY_FILE
      ? path.resolve(repoRoot, String(args.topologyFile || process.env.OHMF_STRESS_TOPOLOGY_FILE))
      : path.join(suiteDir, "topology.json"),
    worstCase: {
      messagesPerStage: Math.max(1, integerOption(args.messagesPerStage ?? process.env.OHMF_STRESS_MESSAGES_PER_STAGE, 600)),
      rate: Math.max(1, numberOption(args.rate ?? process.env.OHMF_STRESS_RATE, 120)),
      sendConcurrency: Math.max(1, integerOption(args.sendConcurrency ?? process.env.OHMF_STRESS_SEND_CONCURRENCY, 8)),
      reconnectStormSize: Math.max(
        1,
        integerOption(
          args.reconnectStormSize ?? process.env.OHMF_STRESS_RECONNECT_STORM_SIZE,
          Math.min(totalClients, Math.max(1000, Math.floor(totalClients * 0.25)))
        )
      ),
      reconnectBatchSize: Math.max(1, integerOption(args.reconnectBatchSize ?? process.env.OHMF_STRESS_RECONNECT_BATCH_SIZE, 250)),
      reconnectBatchIntervalMs: Math.max(0, integerOption(args.reconnectBatchIntervalMs ?? process.env.OHMF_STRESS_RECONNECT_BATCH_INTERVAL_MS, 250)),
      reconnectPauseMs: Math.max(0, integerOption(args.reconnectPauseMs ?? process.env.OHMF_STRESS_RECONNECT_PAUSE_MS, 1000)),
      reconnectHoldMs: Math.max(0, integerOption(args.reconnectHoldMs ?? process.env.OHMF_STRESS_RECONNECT_HOLD_MS, 3000)),
      raceIterations: Math.max(1, integerOption(args.raceIterations ?? process.env.OHMF_STRESS_RACE_ITERATIONS, 10)),
      sendTimeoutMs: Math.max(250, integerOption(args.sendTimeoutMs ?? process.env.OHMF_STRESS_SEND_TIMEOUT_MS, 5000)),
      faultRequestDelayMs: Math.max(0, integerOption(args.faultRequestDelayMs ?? process.env.OHMF_STRESS_FAULT_REQUEST_DELAY_MS, 5500)),
      faultRetryDelayMs: Math.max(0, integerOption(args.faultRetryDelayMs ?? process.env.OHMF_STRESS_FAULT_RETRY_DELAY_MS, 250)),
    },
    normal: {
      messagesPerStage: Math.max(1, integerOption(args.messagesPerStage ?? process.env.OHMF_STRESS_MESSAGES_PER_STAGE, 180)),
      rate: Math.max(0.1, numberOption(args.rate ?? process.env.OHMF_STRESS_RATE, 2.5)),
      sendConcurrency: Math.max(1, integerOption(args.sendConcurrency ?? process.env.OHMF_STRESS_SEND_CONCURRENCY, 3)),
      reconnectStormSize: Math.max(
        1,
        integerOption(
          args.reconnectStormSize ?? process.env.OHMF_STRESS_RECONNECT_STORM_SIZE,
          Math.min(totalClients, Math.max(50, Math.floor(totalClients * 0.05)))
        )
      ),
      reconnectBatchSize: Math.max(1, integerOption(args.reconnectBatchSize ?? process.env.OHMF_STRESS_RECONNECT_BATCH_SIZE, 50)),
      reconnectBatchIntervalMs: Math.max(0, integerOption(args.reconnectBatchIntervalMs ?? process.env.OHMF_STRESS_RECONNECT_BATCH_INTERVAL_MS, 1000)),
      reconnectPauseMs: Math.max(0, integerOption(args.reconnectPauseMs ?? process.env.OHMF_STRESS_RECONNECT_PAUSE_MS, 1000)),
      reconnectHoldMs: Math.max(0, integerOption(args.reconnectHoldMs ?? process.env.OHMF_STRESS_RECONNECT_HOLD_MS, 2000)),
      sendTimeoutMs: Math.max(250, integerOption(args.sendTimeoutMs ?? process.env.OHMF_STRESS_SEND_TIMEOUT_MS, 5000)),
      faultRequestDelayMs: Math.max(0, integerOption(args.faultRequestDelayMs ?? process.env.OHMF_STRESS_FAULT_REQUEST_DELAY_MS, 5500)),
      faultRetryDelayMs: Math.max(0, integerOption(args.faultRetryDelayMs ?? process.env.OHMF_STRESS_FAULT_RETRY_DELAY_MS, 250)),
    },
  };
}

function printUsage() {
  console.log("Usage: node ./testing/stress/capacity-suite.js [options]");
  console.log("");
  console.log("Options:");
  console.log("  --profile worst-case|normal");
  console.log("  --base-url <url>");
  console.log("  --total-clients <count>");
  console.log("  --unique-user-ratio <decimal>");
  console.log("  --active-conversations <count>");
  console.log("  --connect-batch-size <count>");
  console.log("  --connect-timeout-ms <milliseconds>");
  console.log("  --messages-per-stage <count>");
  console.log("  --rate <messages-per-second>");
  console.log("  --send-concurrency <count>");
  console.log("  --reconnect-storm-size <count>");
  console.log("  --reconnect-batch-size <count>");
  console.log("  --reconnect-batch-interval-ms <milliseconds>");
  console.log("  --reconnect-pause-ms <milliseconds>");
  console.log("  --failure-down-ms <milliseconds>");
  console.log("  --failure-delay-ms <milliseconds>");
  console.log("  --send-timeout-ms <milliseconds>");
  console.log("  --fault-request-delay-ms <milliseconds>");
  console.log("  --fault-retry-delay-ms <milliseconds>");
  console.log("  --race-iterations <count>");
  console.log("  --topology-file <path>");
  console.log("  --metrics-url <absolute-url> (repeatable)");
  console.log("  --run-label <label>");
}

function execFileAsync(file, args, options = {}) {
  return new Promise((resolve, reject) => {
    execFile(file, args, options, (error, stdout, stderr) => {
      if (error) {
        error.stdout = stdout;
        error.stderr = stderr;
        reject(error);
        return;
      }
      resolve({ stdout, stderr });
    });
  });
}

async function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, Math.max(0, ms)));
}

async function waitForContainerRunning(containerName, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const { stdout } = await execFileAsync("docker", [
        "inspect",
        "-f",
        "{{.State.Running}}",
        containerName,
      ], {
        cwd: repoRoot,
        encoding: "utf8",
      });
      if (stdout.trim() === "true") {
        return;
      }
    } catch {
      // Poll until the timeout.
    }
    await sleep(1000);
  }
  throw new Error(`container ${containerName} did not become running within ${timeoutMs}ms`);
}

async function injectContainerOutage(containerName, downMs) {
  await execFileAsync("docker", ["stop", containerName], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  await sleep(downMs);
  await execFileAsync("docker", ["start", containerName], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  await waitForContainerRunning(containerName, 30000);
  await sleep(2000);
}

function buildCommonArgs(config, runLabel) {
  const args = [
    ".\\testing\\stress\\run.js",
    "--base-url", config.baseURL,
    "--total-clients", String(config.totalClients),
    "--unique-user-ratio", String(config.uniqueUserRatio),
    "--topology-file", config.topologyFile,
    "--connect-batch-size", String(config.connectBatchSize),
    "--connect-timeout-ms", String(config.connectTimeoutMs),
    "--run-label", runLabel,
  ];
  for (const metricsUrl of config.metricsUrls) {
    args.push("--metrics-url", metricsUrl);
  }
  return args;
}

function profileStages(config) {
  if (config.profile === "normal") {
    const normal = config.normal;
    return [
      {
        label: "connect",
        scenario: "connect",
        args: [
          ["--hold-ms", config.connectHoldMs],
        ],
      },
      {
        label: "messages-steady-a",
        scenario: "throughput",
        args: [
          ["--messages", normal.messagesPerStage],
          ["--rate", normal.rate],
          ["--send-concurrency", normal.sendConcurrency],
          ["--active-conversations", config.activeConversations],
        ],
      },
      {
        label: "reconnect-storm-light",
        scenario: "reconnect-storm",
        args: [
          ["--reconnect-storm-size", normal.reconnectStormSize],
          ["--reconnect-batch-size", normal.reconnectBatchSize],
          ["--reconnect-batch-interval-ms", normal.reconnectBatchIntervalMs],
          ["--reconnect-pause-ms", normal.reconnectPauseMs],
          ["--hold-ms", normal.reconnectHoldMs],
        ],
      },
      {
        label: "messages-steady-b",
        scenario: "throughput",
        args: [
          ["--messages", normal.messagesPerStage],
          ["--rate", normal.rate],
          ["--send-concurrency", normal.sendConcurrency],
          ["--active-conversations", config.activeConversations],
        ],
      },
      {
        label: "high-latency-link-rare",
        scenario: "high-latency-link",
        args: [
          ["--active-conversations", 1],
          ["--send-timeout-ms", normal.sendTimeoutMs],
          ["--fault-request-delay-ms", normal.faultRequestDelayMs],
          ["--fault-retry-delay-ms", normal.faultRetryDelayMs],
        ],
      },
    ];
  }

  const worst = config.worstCase;
  return [
    {
      label: "connect",
      scenario: "connect",
      args: [
        ["--hold-ms", config.connectHoldMs],
      ],
    },
    {
      label: "messages-heavy-a",
      scenario: "throughput",
      args: [
        ["--messages", worst.messagesPerStage],
        ["--rate", worst.rate],
        ["--send-concurrency", worst.sendConcurrency],
        ["--active-conversations", config.activeConversations],
      ],
    },
    {
      label: "reconnect-storm-a",
      scenario: "reconnect-storm",
      args: [
        ["--reconnect-storm-size", worst.reconnectStormSize],
        ["--reconnect-batch-size", worst.reconnectBatchSize],
        ["--reconnect-batch-interval-ms", worst.reconnectBatchIntervalMs],
        ["--reconnect-pause-ms", worst.reconnectPauseMs],
        ["--hold-ms", worst.reconnectHoldMs],
      ],
    },
    {
      label: "messages-delivery-outage",
      scenario: "throughput",
      args: [
        ["--messages", worst.messagesPerStage],
        ["--rate", worst.rate],
        ["--send-concurrency", worst.sendConcurrency],
        ["--active-conversations", config.activeConversations],
      ],
      failureContainer: "ohmf-delivery-processor",
    },
    {
      label: "send-abort",
      scenario: "send-abort",
      args: [
        ["--active-conversations", 1],
        ["--send-timeout-ms", worst.sendTimeoutMs],
        ["--fault-retry-delay-ms", worst.faultRetryDelayMs],
      ],
    },
    {
      label: "high-latency-link",
      scenario: "high-latency-link",
      args: [
        ["--active-conversations", 1],
        ["--send-timeout-ms", worst.sendTimeoutMs],
        ["--fault-request-delay-ms", worst.faultRequestDelayMs],
        ["--fault-retry-delay-ms", worst.faultRetryDelayMs],
      ],
    },
    {
      label: "block-race",
      scenario: "block-race",
      args: [
        ["--active-conversations", 1],
        ["--race-iterations", worst.raceIterations],
        ["--send-timeout-ms", worst.sendTimeoutMs],
      ],
    },
    {
      label: "reconnect-storm-b",
      scenario: "reconnect-storm",
      args: [
        ["--reconnect-storm-size", worst.reconnectStormSize],
        ["--reconnect-batch-size", worst.reconnectBatchSize],
        ["--reconnect-batch-interval-ms", worst.reconnectBatchIntervalMs],
        ["--reconnect-pause-ms", worst.reconnectPauseMs],
        ["--hold-ms", worst.reconnectHoldMs],
      ],
    },
    {
      label: "messages-persist-outage",
      scenario: "throughput",
      args: [
        ["--messages", worst.messagesPerStage],
        ["--rate", worst.rate],
        ["--send-concurrency", worst.sendConcurrency],
        ["--active-conversations", config.activeConversations],
      ],
      failureContainer: "ohmf-messages-processor",
    },
  ];
}

async function runStressStage(config, stage) {
  const args = buildCommonArgs(config, `${config.runLabel}-${stage.label}`);
  args.push("--scenario", stage.scenario);
  for (const [flag, value] of stage.args) {
    args.push(flag, String(value));
  }

  let injectorPromise = null;
  const startedAt = Date.now();
  const stageTimeoutMs = Number.isFinite(stage.timeoutMs) ? Number(stage.timeoutMs) : config.stageTimeoutMs;
  const child = spawn("node", args, {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });

  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => {
    stdout += chunk.toString();
  });
  child.stderr.on("data", (chunk) => {
    stderr += chunk.toString();
  });

  if (stage.failureContainer) {
    injectorPromise = (async () => {
      await sleep(config.failureDelayMs);
      await injectContainerOutage(stage.failureContainer, config.failureDownMs);
    })();
  }

  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    child.kill("SIGKILL");
  }, stageTimeoutMs);

  const exit = await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("close", (code, signal) => resolve({ code, signal }));
  });
  clearTimeout(timer);
  if (injectorPromise) {
    await injectorPromise;
  }

  let parsed = null;
  const trimmed = stdout.trim();
  if (trimmed) {
    parsed = JSON.parse(trimmed);
  }
  const summaryPath = parsed?.run_dir ? path.join(parsed.run_dir, "summary.json") : "";
  const summary = summaryPath && fs.existsSync(summaryPath)
    ? JSON.parse(fs.readFileSync(summaryPath, "utf8"))
    : null;
  if (timedOut) {
    stderr = `${stderr.trim()}\nstage timed out after ${stageTimeoutMs}ms`.trim();
  }

  return {
    label: stage.label,
    scenario: stage.scenario,
    failure_container: stage.failureContainer || null,
    started_at: new Date(startedAt).toISOString(),
    completed_at: new Date().toISOString(),
    exit_code: exit.code,
    exit_signal: exit.signal,
    timed_out: timedOut,
    stdout: trimmed,
    stderr: stderr.trim(),
    run_dir: parsed?.run_dir || "",
    summary,
  };
}

function buildMarkdownSummary(config, results) {
  const lines = [
    "# OHMF Capacity Suite",
    "",
    `- Profile: \`${config.profile}\``,
    `- Run label: \`${config.runLabel}\``,
    `- Base URL: \`${config.baseURL}\``,
    `- Total clients: ${config.totalClients}`,
    `- Logical users: ${config.logicalUsers}`,
    `- Unique user ratio: ${config.uniqueUserRatio}`,
    `- Active conversations: ${config.activeConversations}`,
    `- Topology file: \`${config.topologyFile}\``,
    "",
    "## Stages",
    "",
  ];

  for (const result of results) {
    const summary = result.summary;
    lines.push(`- ${result.label}: exit ${result.exit_code}${result.timed_out ? " (timed out)" : ""}`);
    if (summary) {
      lines.push(`  connected_devices=${summary.connected_devices}, client_errors=${summary.client_errors}, messages_accepted=${summary.messages_accepted}, successful_deliveries=${summary.successful_deliveries}, lost_deliveries=${summary.lost_deliveries}`);
    }
    if (result.failure_container) {
      lines.push(`  injected_failure=${result.failure_container}`);
    }
  }
  lines.push("");
  return `${lines.join("\n")}\n`;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    printUsage();
    return;
  }

  const config = buildConfig(args);
  const stages = profileStages(config);
  const results = [];

  for (const stage of stages) {
    const result = await runStressStage(config, stage);
    results.push(result);
    if (result.exit_code !== 0 || result.timed_out) {
      break;
    }
    if (results.length < stages.length) {
      await waitForStageRecovery(config, stage);
      await recoverStageServices(config, stage);
    }
  }

  const output = {
    profile: config.profile,
    run_label: config.runLabel,
    started_at: new Date(config.startedAt).toISOString(),
    completed_at: new Date().toISOString(),
    base_url: config.baseURL,
    total_clients: config.totalClients,
    logical_users: config.logicalUsers,
    unique_user_ratio: config.uniqueUserRatio,
    active_conversations: config.activeConversations,
    topology_file: config.topologyFile,
    stages: results,
  };

  fs.writeFileSync(
    path.join(config.suiteDir, "capacity-suite-summary.json"),
    `${JSON.stringify(output, null, 2)}\n`,
    "utf8"
  );
  fs.writeFileSync(
    path.join(config.suiteDir, "capacity-suite-summary.md"),
    buildMarkdownSummary(config, results),
    "utf8"
  );

  console.log(JSON.stringify({
    suite_dir: config.suiteDir,
    profile: config.profile,
    topology_file: config.topologyFile,
    stages: results.map((result) => ({
      label: result.label,
      scenario: result.scenario,
      failure_container: result.failure_container,
      exit_code: result.exit_code,
      exit_signal: result.exit_signal,
      timed_out: result.timed_out,
      run_dir: result.run_dir,
      connected_devices: result.summary?.connected_devices ?? null,
      client_errors: result.summary?.client_errors ?? null,
      messages_accepted: result.summary?.messages_accepted ?? null,
      successful_deliveries: result.summary?.successful_deliveries ?? null,
      lost_deliveries: result.summary?.lost_deliveries ?? null,
    })),
  }, null, 2));

  if (results.some((result) => result.exit_code !== 0 || result.timed_out)) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(error.message || error);
  process.exitCode = 1;
});
