#!/usr/bin/env node

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const schemaPath = path.join(rootDir, "manifest.schema.json");

function usage() {
  console.log(`miniapp-cli

Commands:
  validate <manifest.json>
  sign <manifest.json> --private-key <pem> [--kid <key-id>] [--alg ed25519|rsa-sha256] [--output <path>]
  package <manifest.json> [--out <dir>]
  upload-draft <manifest.json> --api <base-url> --token <bearer-token>
  submit <app-id> <version> --api <base-url> --token <bearer-token>
`);
}

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function writeJSON(filePath, value) {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, `${JSON.stringify(value, null, 2)}\n`);
}

function parseArgs(argv) {
  const positional = [];
  const flags = {};
  for (let i = 0; i < argv.length; i += 1) {
    const part = argv[i];
    if (!part.startsWith("--")) {
      positional.push(part);
      continue;
    }
    const key = part.slice(2);
    const next = argv[i + 1];
    if (!next || next.startsWith("--")) {
      flags[key] = true;
      continue;
    }
    flags[key] = next;
    i += 1;
  }
  return { positional, flags };
}

function isObject(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function canonicalize(value) {
  if (Array.isArray(value)) {
    return value.map(canonicalize);
  }
  if (isObject(value)) {
    return Object.keys(value).sort().reduce((acc, key) => {
      acc[key] = canonicalize(value[key]);
      return acc;
    }, {});
  }
  return value;
}

function validateManifest(manifest) {
  const schema = readJSON(schemaPath);
  const required = new Set(schema.required || []);
  for (const key of required) {
    if (manifest[key] === undefined || manifest[key] === null || manifest[key] === "") {
      throw new Error(`manifest missing required field: ${key}`);
    }
  }
  if (!isObject(manifest.entrypoint) || !manifest.entrypoint.url || !manifest.entrypoint.type) {
    throw new Error("manifest entrypoint must contain type and url");
  }
  if (!Array.isArray(manifest.permissions)) {
    throw new Error("manifest permissions must be an array");
  }
  if (!isObject(manifest.capabilities)) {
    throw new Error("manifest capabilities must be an object");
  }
  if (!isObject(manifest.message_preview) || !manifest.message_preview.url || !manifest.message_preview.type) {
    throw new Error("manifest message_preview must contain type and url");
  }
  return true;
}

function signaturePayload(manifest) {
  const clone = canonicalize({ ...manifest });
  delete clone.signature;
  return Buffer.from(JSON.stringify(clone));
}

function signManifest(manifest, privateKeyPem, algorithm, kid) {
  const payload = signaturePayload(manifest);
  const privateKey = fs.readFileSync(privateKeyPem, "utf8");
  let sig;
  let alg;
  if ((algorithm || "ed25519").toLowerCase() === "ed25519") {
    sig = crypto.sign(null, payload, privateKey).toString("base64");
    alg = "Ed25519";
  } else {
    sig = crypto.sign("RSA-SHA256", payload, privateKey).toString("base64");
    alg = "RS256";
  }
  return {
    ...manifest,
    signature: {
      alg,
      kid: kid || path.basename(privateKeyPem),
      sig,
    },
  };
}

function collectAssetFiles(manifestPath, manifest) {
  const baseDir = path.dirname(path.resolve(manifestPath));
  const files = [];
  const maybeCollect = (urlValue) => {
    if (typeof urlValue !== "string" || !urlValue) return;
    if (/^https?:\/\//i.test(urlValue)) return;
    const localPath = path.resolve(baseDir, urlValue);
    if (!fs.existsSync(localPath) || fs.statSync(localPath).isDirectory()) return;
    const bytes = fs.readFileSync(localPath);
    files.push({
      path: path.relative(baseDir, localPath).replace(/\\/g, "/"),
      sha256: crypto.createHash("sha256").update(bytes).digest("hex"),
      size_bytes: bytes.length,
    });
  };
  maybeCollect(manifest.entrypoint?.url);
  maybeCollect(manifest.message_preview?.url);
  for (const icon of manifest.icons || []) {
    maybeCollect(icon?.url);
  }
  return files;
}

async function apiRequest(baseUrl, route, token, method, body) {
  const response = await fetch(`${baseUrl.replace(/\/+$/, "")}${route}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await response.text();
  const payload = text ? JSON.parse(text) : null;
  if (!response.ok) {
    throw new Error(payload?.message || `${method} ${route} failed with ${response.status}`);
  }
  return payload;
}

async function main() {
  const argv = process.argv.slice(2);
  if (!argv.length || argv[0] === "--help" || argv[0] === "-h") {
    usage();
    process.exit(0);
  }

  const command = argv[0];
  const { positional, flags } = parseArgs(argv.slice(1));

  if (command === "validate") {
    const manifestPath = positional[0];
    if (!manifestPath) throw new Error("validate requires a manifest path");
    const manifest = readJSON(manifestPath);
    validateManifest(manifest);
    console.log(`manifest valid: ${manifest.app_id}@${manifest.version}`);
    return;
  }

  if (command === "sign") {
    const manifestPath = positional[0];
    if (!manifestPath || !flags["private-key"]) throw new Error("sign requires <manifest> and --private-key");
    const manifest = readJSON(manifestPath);
    validateManifest(manifest);
    const signed = signManifest(manifest, flags["private-key"], flags.alg, flags.kid);
    const output = flags.output || manifestPath;
    writeJSON(output, signed);
    console.log(`signed manifest written to ${output}`);
    return;
  }

  if (command === "package") {
    const manifestPath = positional[0];
    if (!manifestPath) throw new Error("package requires a manifest path");
    const manifest = readJSON(manifestPath);
    validateManifest(manifest);
    const outDir = path.resolve(flags.out || path.join(path.dirname(manifestPath), "dist"));
    const files = collectAssetFiles(manifestPath, manifest);
    writeJSON(path.join(outDir, "bundle-metadata.json"), {
      app_id: manifest.app_id,
      version: manifest.version,
      generated_at: new Date().toISOString(),
      files,
    });
    console.log(`bundle metadata written to ${path.join(outDir, "bundle-metadata.json")}`);
    return;
  }

  if (command === "upload-draft") {
    const manifestPath = positional[0];
    if (!manifestPath || !flags.api || !flags.token) throw new Error("upload-draft requires <manifest>, --api, and --token");
    const manifest = readJSON(manifestPath);
    validateManifest(manifest);
    await apiRequest(flags.api, "/v1/publisher/apps", flags.token, "POST", {
      app_id: manifest.app_id,
      name: manifest.name,
    }).catch(() => null);
    const payload = await apiRequest(flags.api, `/v1/publisher/apps/${encodeURIComponent(manifest.app_id)}/releases`, flags.token, "POST", {
      manifest,
    });
    console.log(JSON.stringify(payload, null, 2));
    return;
  }

  if (command === "submit") {
    const appId = positional[0];
    const version = positional[1];
    if (!appId || !version || !flags.api || !flags.token) throw new Error("submit requires <app-id> <version> --api --token");
    const payload = await apiRequest(flags.api, `/v1/publisher/apps/${encodeURIComponent(appId)}/releases/${encodeURIComponent(version)}/submit`, flags.token, "POST");
    console.log(JSON.stringify(payload, null, 2));
    return;
  }

  throw new Error(`unknown command: ${command}`);
}

main().catch((error) => {
  console.error(error.message || String(error));
  process.exit(1);
});
