#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const net = require("node:net");

function parseArgs(argv) {
  const parsed = {};
  for (let index = 0; index < argv.length; index += 1) {
    const value = argv[index];
    if (!value.startsWith("--")) {
      continue;
    }
    const [rawFlag, inlineValue] = value.split(/=(.*)/s, 2);
    const key = String(rawFlag)
      .replace(/^-+/, "")
      .replace(/-([a-z])/g, (_, chr) => chr.toUpperCase());
    if (typeof inlineValue === "string") {
      parsed[key] = inlineValue;
      continue;
    }
    const next = argv[index + 1];
    if (!next || next.startsWith("--")) {
      parsed[key] = true;
      continue;
    }
    parsed[key] = next;
    index += 1;
  }
  return parsed;
}

function numberOption(value, fallback) {
  if (value === undefined || value === null || value === "") {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    throw new Error(`invalid numeric value: ${value}`);
  }
  return parsed;
}

function writeStateFile(stateFile, state) {
  if (!stateFile) {
    return;
  }
  fs.writeFileSync(stateFile, `${JSON.stringify(state, null, 2)}\n`, "utf8");
}

function normalizeListenHost(value) {
  return String(value || "127.0.0.1").trim() || "127.0.0.1";
}

function targetFromBaseURL(baseURL) {
  const targetURL = new URL(String(baseURL || ""));
  if (!["http:", "https:"].includes(targetURL.protocol)) {
    throw new Error(`unsupported target protocol: ${targetURL.protocol}`);
  }
  return {
    url: targetURL,
    host: targetURL.hostname,
    port: Number(targetURL.port || (targetURL.protocol === "https:" ? 443 : 80)),
  };
}

function safeDestroy(socket) {
  if (socket && !socket.destroyed) {
    socket.destroy();
  }
}

function relayWithDelay(source, destination, delayMs, stats, counterPrefix) {
  source.on("data", (chunk) => {
    stats[`${counterPrefix}_chunks`] += 1;
    stats[`${counterPrefix}_bytes`] += chunk.length;
    const payload = Buffer.from(chunk);
    if (delayMs <= 0) {
      if (!destination.destroyed) {
        destination.write(payload);
      }
      return;
    }
    setTimeout(() => {
      if (!destination.destroyed) {
        destination.write(payload);
      }
    }, delayMs);
  });
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!args.targetBaseUrl) {
    throw new Error("--target-base-url is required");
  }

  const target = targetFromBaseURL(args.targetBaseUrl);
  const listenHost = normalizeListenHost(args.listenHost);
  const listenPort = Math.trunc(numberOption(args.listenPort, 0));
  const clientToServerDelayMs = numberOption(args.clientToServerDelayMs, 0);
  const serverToClientDelayMs = numberOption(args.serverToClientDelayMs, clientToServerDelayMs);
  const stateFile = args.stateFile ? String(args.stateFile) : "";
  const verbose = Boolean(args.verbose);

  const stats = {
    accepted_connections: 0,
    closed_connections: 0,
    client_to_server_chunks: 0,
    client_to_server_bytes: 0,
    server_to_client_chunks: 0,
    server_to_client_bytes: 0,
    upstream_connect_errors: 0,
  };

  const server = net.createServer((clientSocket) => {
    stats.accepted_connections += 1;
    clientSocket.setNoDelay(true);

    const upstreamSocket = net.createConnection({
      host: target.host,
      port: target.port,
    });
    upstreamSocket.setNoDelay(true);

    let closed = false;
    const closePair = () => {
      if (closed) {
        return;
      }
      closed = true;
      stats.closed_connections += 1;
      safeDestroy(clientSocket);
      safeDestroy(upstreamSocket);
    };

    relayWithDelay(clientSocket, upstreamSocket, clientToServerDelayMs, stats, "client_to_server");
    relayWithDelay(upstreamSocket, clientSocket, serverToClientDelayMs, stats, "server_to_client");

    clientSocket.on("error", closePair);
    upstreamSocket.on("error", (error) => {
      stats.upstream_connect_errors += 1;
      if (verbose) {
        console.error(error.message || String(error));
      }
      closePair();
    });
    clientSocket.on("close", closePair);
    upstreamSocket.on("close", closePair);
  });

  server.on("error", (error) => {
    console.error(error.message || String(error));
    process.exitCode = 1;
  });

  server.listen(listenPort, listenHost, () => {
    const address = server.address();
    if (!address || typeof address === "string") {
      throw new Error("proxy failed to bind");
    }
    const state = {
      pid: process.pid,
      listen_host: listenHost,
      listen_port: address.port,
      base_url: `http://${listenHost}:${address.port}`,
      target_base_url: target.url.toString(),
      target_host: target.host,
      target_port: target.port,
      client_to_server_delay_ms: clientToServerDelayMs,
      server_to_client_delay_ms: serverToClientDelayMs,
      stats,
    };
    writeStateFile(stateFile, state);
    process.stdout.write(`${JSON.stringify(state, null, 2)}\n`);
  });

  const shutdown = () => {
    server.close(() => {
      process.exit(0);
    });
    setTimeout(() => {
      process.exit(0);
    }, 2000).unref();
  };

  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

try {
  main();
} catch (error) {
  console.error(error.message || String(error));
  process.exitCode = 1;
}
