"use strict";

const http = require("node:http");

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, Math.max(0, ms)));
}

function normalizeBaseURL(baseURL) {
  return String(baseURL || "").replace(/\/+$/, "");
}

function filterHeaders(headers = {}) {
  const output = {};
  for (const [key, value] of Object.entries(headers)) {
    if (value === undefined || value === null) {
      continue;
    }
    const lower = key.toLowerCase();
    if (["host", "connection", "content-length", "transfer-encoding"].includes(lower)) {
      continue;
    }
    output[key] = value;
  }
  return output;
}

function readRequestBody(request) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    request.on("data", (chunk) => {
      chunks.push(chunk);
    });
    request.on("end", () => {
      resolve(Buffer.concat(chunks));
    });
    request.on("error", reject);
  });
}

function copyResponseHeaders(headers) {
  const copied = {};
  headers.forEach((value, key) => {
    if (["connection", "content-length", "transfer-encoding"].includes(key.toLowerCase())) {
      return;
    }
    copied[key] = value;
  });
  return copied;
}

async function createFaultProxy(options = {}) {
  const targetBaseURL = normalizeBaseURL(options.targetBaseURL);
  if (!targetBaseURL) {
    throw new Error("targetBaseURL is required");
  }

  const targetPath = options.targetPath || "/v1/messages";
  const stats = {
    matched_requests: 0,
    forwarded_requests: 0,
    dropped_before_forward: 0,
    dropped_after_forward: 0,
  };
  let matchedRequestCount = 0;
  let forwardedMatchCount = 0;

  const server = http.createServer(async (request, response) => {
    const requestURL = new URL(request.url || "/", targetBaseURL);
    const matchesTarget = requestURL.pathname === targetPath;

    try {
      const body = await readRequestBody(request);
      if (matchesTarget) {
        matchedRequestCount += 1;
        stats.matched_requests += 1;
        if (options.requestDelayMs > 0) {
          await sleep(options.requestDelayMs);
        }
        if (matchedRequestCount <= Number(options.dropBeforeForwardCount || 0)) {
          stats.dropped_before_forward += 1;
          request.socket.destroy();
          response.destroy();
          return;
        }
      }

      const upstream = await fetch(requestURL, {
        method: request.method,
        headers: filterHeaders(request.headers),
        body: body.length > 0 ? body : undefined,
      });
      const responseBody = Buffer.from(await upstream.arrayBuffer());

      if (matchesTarget) {
        stats.forwarded_requests += 1;
        forwardedMatchCount += 1;
        if (options.responseDelayMs > 0) {
          await sleep(options.responseDelayMs);
        }
        if (forwardedMatchCount <= Number(options.dropAfterForwardCount || 0)) {
          stats.dropped_after_forward += 1;
          request.socket.destroy();
          response.destroy();
          return;
        }
      }

      response.writeHead(upstream.status, copyResponseHeaders(upstream.headers));
      response.end(responseBody);
    } catch (error) {
      if (!response.headersSent && !response.destroyed) {
        response.statusCode = 502;
        response.end(error.message || String(error));
        return;
      }
      response.destroy(error);
    }
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });

  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("fault proxy failed to bind to a local TCP port");
  }

  return {
    baseURL: `http://127.0.0.1:${address.port}`,
    stats,
    async close() {
      await new Promise((resolve, reject) => {
        server.close((error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    },
  };
}

module.exports = {
  createFaultProxy,
};
