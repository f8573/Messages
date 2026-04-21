"use strict";

const path = require("node:path");
const { execFile } = require("node:child_process");
const { poll, requestText, sleep } = require("./api");

const repoRoot = path.resolve(__dirname, "..", "..", "..");

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

function escapeRegex(value) {
  return String(value || "").replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function parseLabels(rawLabels) {
  const labels = {};
  for (const match of String(rawLabels || "").matchAll(/([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"/g)) {
    labels[match[1]] = match[2];
  }
  return labels;
}

function parseMetricSamples(metricsBody, metricName) {
  const pattern = new RegExp(
    `^${escapeRegex(metricName)}(?:\\{([^}]*)\\})?\\s+([0-9.eE+-]+)$`
  );
  const samples = [];
  for (const line of String(metricsBody || "").split(/\r?\n/)) {
    const match = line.match(pattern);
    if (!match) {
      continue;
    }
    const value = Number(match[2]);
    if (!Number.isFinite(value)) {
      continue;
    }
    samples.push({
      labels: parseLabels(match[1] || ""),
      value,
    });
  }
  return samples;
}

function sumInflightRequests(metricsBody) {
  return parseMetricSamples(metricsBody, "ohmf_gateway_http_requests_in_flight")
    .filter((sample) => {
      const route = sample.labels.route || "";
      return route !== "/metrics" && route !== "/healthz";
    })
    .reduce((sum, sample) => sum + sample.value, 0);
}

function activeWebsocketConnections(metricsBody) {
  const sample = parseMetricSamples(metricsBody, "ohmf_gateway_ws_connections_active")[0];
  return sample ? sample.value : 0;
}

function recoveryGraceMs(stage) {
  if (Number.isFinite(stage?.recoveryGraceMs)) {
    return Number(stage.recoveryGraceMs);
  }
  if (stage?.failureContainer) {
    return 30000;
  }
  if (stage?.scenario === "reconnect-storm") {
    return 10000;
  }
  return 2000;
}

function recoveryTimeoutMs(stage) {
  if (Number.isFinite(stage?.recoveryTimeoutMs)) {
    return Number(stage.recoveryTimeoutMs);
  }
  if (stage?.failureContainer) {
    return 120000;
  }
  return 60000;
}

function metricsURL(config) {
  if (Array.isArray(config?.metricsUrls) && config.metricsUrls.length > 0) {
    return config.metricsUrls[0];
  }
  return `${String(config?.baseURL || "").replace(/\/+$/, "")}/metrics`;
}

function healthURL(config) {
  return `${String(config?.baseURL || "").replace(/\/+$/, "")}/healthz`;
}

async function waitForStageRecovery(config, stage) {
  const healthz = healthURL(config);
  const metrics = metricsURL(config);

  await poll(async () => {
    const health = String(await requestText(healthz)).trim().toLowerCase();
    if (health !== "ok") {
      return null;
    }

    let body = "";
    try {
      body = await requestText(metrics);
    } catch {
      return {
        health: "ok",
        wsActive: 0,
        inflightRequests: 0,
      };
    }

    const wsActive = activeWebsocketConnections(body);
    const inflightRequests = sumInflightRequests(body);
    if (wsActive > 0 || inflightRequests > 0) {
      return null;
    }
    return {
      health: "ok",
      wsActive,
      inflightRequests,
    };
  }, {
    timeoutMs: recoveryTimeoutMs(stage),
    intervalMs: 1000,
    description: `${stage?.label || stage?.scenario || "stage"} recovery`,
  });

  const graceMs = recoveryGraceMs(stage);
  if (graceMs > 0) {
    await sleep(graceMs);
  }
}

function shouldRestartGateway(stage) {
  if (stage?.restartGatewayAfter === false) {
    return false;
  }
  if (stage?.restartGatewayAfter === true) {
    return true;
  }
  return Boolean(stage?.failureContainer || stage?.scenario === "reconnect-storm");
}

async function localContainerAvailable(containerName) {
  try {
    await execFileAsync("docker", ["inspect", containerName], {
      cwd: repoRoot,
      encoding: "utf8",
    });
    return true;
  } catch {
    return false;
  }
}

async function waitForContainerRunning(containerName, timeoutMs = 60000) {
  await poll(async () => {
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
      return stdout.trim() === "true";
    } catch {
      return false;
    }
  }, {
    timeoutMs,
    intervalMs: 1000,
    description: `${containerName} running`,
  });
}

async function restartContainer(containerName) {
  if (!(await localContainerAvailable(containerName))) {
    return false;
  }

  await execFileAsync("docker", ["restart", containerName], {
    cwd: repoRoot,
    encoding: "utf8",
  });

  await waitForContainerRunning(containerName, 60000);
  return true;
}

async function recoverStageServices(config, stage) {
  const restarted = [];

  if (stage?.failureContainer) {
    const restartedFailureContainer = await restartContainer(stage.failureContainer);
    if (restartedFailureContainer) {
      restarted.push(stage.failureContainer);
      await sleep(5000);
    }
  }

  if (!shouldRestartGateway(stage)) {
    return restarted;
  }
  const restartedGateway = await restartContainer("ohmf-api");
  if (!restartedGateway) {
    return restarted;
  }
  restarted.push("ohmf-api");

  await poll(async () => {
    const health = String(await requestText(healthURL(config))).trim().toLowerCase();
    return health === "ok";
  }, {
    timeoutMs: 60000,
    intervalMs: 1000,
    description: `${stage?.label || stage?.scenario || "stage"} gateway restart`,
  });

  await sleep(2000);
  return restarted;
}

module.exports = {
  recoverStageServices,
  waitForStageRecovery,
};
