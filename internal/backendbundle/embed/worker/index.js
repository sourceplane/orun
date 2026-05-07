var __defProp = Object.defineProperty;
var __name = (target, value) => __defProp(target, "name", { value, configurable: true });

// ../../packages/coordinator/dist/coordinator.js
var HEARTBEAT_TIMEOUT_MS = 3e5;
var SWEEP_INTERVAL_MS = HEARTBEAT_TIMEOUT_MS;
function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" }
  });
}
__name(jsonResponse, "jsonResponse");
function errorResponse(error, code, status) {
  return jsonResponse({ error, code }, status);
}
__name(errorResponse, "errorResponse");
var RunCoordinator = class {
  state;
  env;
  runState = null;
  constructor(state, env) {
    this.state = state;
    this.env = env;
  }
  async loadState() {
    if (this.runState !== null)
      return this.runState;
    this.runState = await this.state.storage.get("runState") ?? null;
    return this.runState;
  }
  async persistState() {
    if (this.runState) {
      await this.state.storage.put("runState", this.runState);
    }
  }
  async scheduleSweep() {
    await this.state.storage.setAlarm(Date.now() + SWEEP_INTERVAL_MS);
  }
  async alarm() {
    const state = await this.loadState();
    if (!state) {
      await this.state.storage.deleteAll();
      return;
    }
    const now = Date.now();
    let swept = false;
    for (const job of Object.values(state.jobs)) {
      if (job.status !== "running")
        continue;
      const heartbeatAge = job.heartbeatAt ? now - new Date(job.heartbeatAt).getTime() : Infinity;
      if (heartbeatAge > HEARTBEAT_TIMEOUT_MS) {
        job.status = "failed";
        job.lastError = "runner heartbeat timeout";
        if (!job.finishedAt) {
          job.finishedAt = (/* @__PURE__ */ new Date()).toISOString();
        }
        swept = true;
      }
    }
    if (swept) {
      const allJobs = Object.values(state.jobs);
      const allSuccess = allJobs.every((j) => j.status === "success");
      const anyFailed = allJobs.some((j) => j.status === "failed");
      if (allSuccess) {
        state.status = "completed";
      } else if (anyFailed) {
        state.status = "failed";
      }
      state.updatedAt = (/* @__PURE__ */ new Date()).toISOString();
      await this.persistState();
    }
    const isActive = state.status === "running" || Object.values(state.jobs).some((j) => j.status === "pending" || j.status === "running");
    if (isActive) {
      await this.scheduleSweep();
    } else {
      await this.state.storage.deleteAll();
      this.runState = null;
    }
  }
  async fetch(request) {
    const url = new URL(request.url);
    const method = request.method;
    const path = url.pathname;
    try {
      if (method === "POST" && path === "/init") {
        return this.handleInit(request);
      }
      const jobMatch = path.match(/^\/jobs\/([^/]+)\/(claim|update|heartbeat|status)$/);
      if (jobMatch) {
        const jobId = decodeURIComponent(jobMatch[1]);
        const action = jobMatch[2];
        if (action === "claim" && method === "POST")
          return this.handleClaim(jobId, request);
        if (action === "update" && method === "POST")
          return this.handleUpdate(jobId, request);
        if (action === "heartbeat" && method === "POST")
          return this.handleHeartbeat(jobId, request);
        if (action === "status" && method === "GET")
          return this.handleJobStatus(jobId);
        return errorResponse("Method not allowed", "INVALID_REQUEST", 400);
      }
      if (method === "GET" && path === "/runnable") {
        return this.handleRunnable();
      }
      if (method === "GET" && path === "/state") {
        return this.handleState();
      }
      if (method === "POST" && path === "/cancel") {
        return this.handleCancel();
      }
      return errorResponse("Not found", "NOT_FOUND", 404);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Internal server error";
      return errorResponse(message, "INTERNAL_ERROR", 500);
    }
  }
  async handleInit(request) {
    let body;
    try {
      body = await request.json();
    } catch {
      return errorResponse("Invalid JSON body", "INVALID_REQUEST", 400);
    }
    const { plan, runId, namespaceId, namespaceSlug } = body;
    if (!plan || !runId || !namespaceId) {
      return errorResponse("Missing required fields: plan, runId, namespaceId", "INVALID_REQUEST", 400);
    }
    if (!Array.isArray(plan.jobs)) {
      return errorResponse("plan.jobs must be an array", "INVALID_REQUEST", 400);
    }
    const jobIds = /* @__PURE__ */ new Set();
    for (const job of plan.jobs) {
      if (!job.jobId || typeof job.jobId !== "string") {
        return errorResponse("Every plan job must have a non-empty jobId", "INVALID_REQUEST", 400);
      }
      if (jobIds.has(job.jobId)) {
        return errorResponse(`Duplicate jobId: ${job.jobId}`, "INVALID_REQUEST", 400);
      }
      jobIds.add(job.jobId);
    }
    for (const job of plan.jobs) {
      if (job.deps) {
        for (const dep of job.deps) {
          if (!jobIds.has(dep)) {
            return errorResponse(`Dependency "${dep}" in job "${job.jobId}" does not exist in plan`, "INVALID_REQUEST", 400);
          }
        }
      }
    }
    const existing = await this.loadState();
    if (existing) {
      if (existing.runId === runId) {
        return jsonResponse({ ok: true, alreadyExists: true });
      }
      return errorResponse(`Coordinator already initialized for runId: ${existing.runId}`, "CONFLICT", 409);
    }
    const now = (/* @__PURE__ */ new Date()).toISOString();
    const jobs = {};
    for (const pj of plan.jobs) {
      jobs[pj.jobId] = {
        jobId: pj.jobId,
        component: pj.component,
        status: "pending",
        deps: pj.deps ?? [],
        runnerId: null,
        startedAt: null,
        finishedAt: null,
        lastError: null,
        heartbeatAt: null
      };
    }
    this.runState = {
      runId,
      namespaceId,
      status: "running",
      plan,
      jobs,
      createdAt: now,
      updatedAt: now
    };
    await this.persistState();
    await this.scheduleSweep();
    return jsonResponse({ ok: true, alreadyExists: false });
  }
  async handleClaim(jobId, request) {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    let body;
    try {
      body = await request.json();
    } catch {
      return errorResponse("Invalid JSON body", "INVALID_REQUEST", 400);
    }
    const { runnerId } = body;
    if (!runnerId || typeof runnerId !== "string") {
      return errorResponse("Missing runnerId", "INVALID_REQUEST", 400);
    }
    const job = state.jobs[jobId];
    if (!job) {
      return errorResponse(`Job not found: ${jobId}`, "NOT_FOUND", 404);
    }
    if (job.status === "pending") {
      const nowMs = Date.now();
      let swept = false;
      for (const dep of job.deps) {
        const depJob = state.jobs[dep];
        if (depJob.status === "running") {
          const heartbeatAge = depJob.heartbeatAt ? nowMs - new Date(depJob.heartbeatAt).getTime() : Infinity;
          if (heartbeatAge > HEARTBEAT_TIMEOUT_MS) {
            depJob.status = "failed";
            depJob.lastError = "runner heartbeat timeout";
            if (!depJob.finishedAt) {
              depJob.finishedAt = (/* @__PURE__ */ new Date()).toISOString();
            }
            swept = true;
          }
        }
      }
      if (swept) {
        const allJobs = Object.values(state.jobs);
        if (allJobs.some((j) => j.status === "failed")) {
          state.status = "failed";
        }
        state.updatedAt = (/* @__PURE__ */ new Date()).toISOString();
        await this.persistState();
      }
      for (const dep of job.deps) {
        const depJob = state.jobs[dep];
        if (depJob.status === "failed") {
          return jsonResponse({
            claimed: false,
            currentStatus: "pending",
            depsBlocked: true
          });
        }
      }
      for (const dep of job.deps) {
        const depJob = state.jobs[dep];
        if (depJob.status !== "success") {
          return jsonResponse({
            claimed: false,
            currentStatus: "pending",
            depsWaiting: true
          });
        }
      }
      const now = (/* @__PURE__ */ new Date()).toISOString();
      job.status = "running";
      job.runnerId = runnerId;
      job.startedAt = now;
      job.heartbeatAt = now;
      state.updatedAt = now;
      await this.persistState();
      return jsonResponse({ claimed: true });
    }
    if (job.status === "running") {
      const now = Date.now();
      const heartbeatAge = job.heartbeatAt ? now - new Date(job.heartbeatAt).getTime() : Infinity;
      if (heartbeatAge > HEARTBEAT_TIMEOUT_MS) {
        const nowIso = (/* @__PURE__ */ new Date()).toISOString();
        job.runnerId = runnerId;
        job.heartbeatAt = nowIso;
        state.updatedAt = nowIso;
        await this.persistState();
        return jsonResponse({
          claimed: true,
          takeover: true
        });
      }
      return jsonResponse({
        claimed: false,
        currentStatus: "running"
      });
    }
    return jsonResponse({
      claimed: false,
      currentStatus: job.status
    });
  }
  async handleUpdate(jobId, request) {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    let body;
    try {
      body = await request.json();
    } catch {
      return errorResponse("Invalid JSON body", "INVALID_REQUEST", 400);
    }
    const { runnerId, status, error } = body;
    if (!runnerId || typeof runnerId !== "string") {
      return errorResponse("Missing runnerId", "INVALID_REQUEST", 400);
    }
    if (status !== "success" && status !== "failed") {
      return errorResponse("status must be 'success' or 'failed'", "INVALID_REQUEST", 400);
    }
    const job = state.jobs[jobId];
    if (!job) {
      return errorResponse(`Job not found: ${jobId}`, "NOT_FOUND", 404);
    }
    if (job.status !== "running") {
      return errorResponse(`Job is not running (current: ${job.status})`, "INVALID_REQUEST", 400);
    }
    if (job.runnerId !== runnerId) {
      return errorResponse("Runner does not own this job", "INVALID_REQUEST", 400);
    }
    const now = (/* @__PURE__ */ new Date()).toISOString();
    job.status = status;
    job.finishedAt = now;
    job.lastError = error ?? null;
    state.updatedAt = now;
    const allJobs = Object.values(state.jobs);
    const allSuccess = allJobs.every((j) => j.status === "success");
    const anyFailed = allJobs.some((j) => j.status === "failed");
    if (allSuccess) {
      state.status = "completed";
    } else if (anyFailed) {
      state.status = "failed";
    }
    await this.persistState();
    return jsonResponse({ ok: true });
  }
  async handleHeartbeat(jobId, request) {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    let body;
    try {
      body = await request.json();
    } catch {
      return errorResponse("Invalid JSON body", "INVALID_REQUEST", 400);
    }
    const { runnerId } = body;
    if (!runnerId || typeof runnerId !== "string") {
      return errorResponse("Missing runnerId", "INVALID_REQUEST", 400);
    }
    const job = state.jobs[jobId];
    if (!job) {
      return errorResponse(`Job not found: ${jobId}`, "NOT_FOUND", 404);
    }
    if (job.runnerId !== runnerId || job.status !== "running") {
      return jsonResponse({ ok: false, abort: true });
    }
    const now = (/* @__PURE__ */ new Date()).toISOString();
    job.heartbeatAt = now;
    state.updatedAt = now;
    await this.persistState();
    return jsonResponse({ ok: true });
  }
  async handleJobStatus(jobId) {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    const job = state.jobs[jobId];
    if (!job) {
      return errorResponse(`Job not found: ${jobId}`, "NOT_FOUND", 404);
    }
    return jsonResponse({
      jobId: job.jobId,
      component: job.component,
      status: job.status,
      deps: job.deps,
      runnerId: job.runnerId,
      startedAt: job.startedAt,
      finishedAt: job.finishedAt,
      lastError: job.lastError,
      heartbeatAt: job.heartbeatAt
    });
  }
  async handleRunnable() {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    const runnableJobs = [];
    for (const job of Object.values(state.jobs)) {
      if (job.status !== "pending")
        continue;
      const allDepsSatisfied = job.deps.every((dep) => state.jobs[dep].status === "success");
      if (allDepsSatisfied) {
        runnableJobs.push(job.jobId);
      }
    }
    return jsonResponse({ jobs: runnableJobs });
  }
  async handleState() {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    return jsonResponse(state);
  }
  async handleCancel() {
    const state = await this.loadState();
    if (!state) {
      return errorResponse("Coordinator not initialized", "NOT_FOUND", 404);
    }
    const now = (/* @__PURE__ */ new Date()).toISOString();
    for (const job of Object.values(state.jobs)) {
      if (job.status === "pending" || job.status === "running") {
        job.status = "failed";
        job.lastError = "cancelled";
        if (!job.finishedAt) {
          job.finishedAt = now;
        }
      }
    }
    state.status = "cancelled";
    state.updatedAt = now;
    await this.persistState();
    return jsonResponse({ ok: true });
  }
};
__name(RunCoordinator, "RunCoordinator");

// src/auth/errors.ts
var STATUS_MAP = {
  UNAUTHORIZED: 401,
  FORBIDDEN: 403,
  NOT_FOUND: 404,
  CONFLICT: 409,
  RATE_LIMITED: 429,
  INVALID_REQUEST: 400,
  INTERNAL_ERROR: 500
};
function statusForCode(code) {
  return STATUS_MAP[code] ?? 500;
}
__name(statusForCode, "statusForCode");
var OrunError = class extends Error {
  constructor(code, message, httpStatus = statusForCode(code)) {
    super(message);
    this.code = code;
    this.httpStatus = httpStatus;
  }
};
__name(OrunError, "OrunError");

// src/http.ts
var CORS_HEADERS = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, POST, DELETE, OPTIONS",
  "Access-Control-Allow-Headers": "Authorization, Content-Type, X-Orun-Deploy-Token, X-GitHub-Access-Token"
};
function corsHeaders() {
  return { ...CORS_HEADERS };
}
__name(corsHeaders, "corsHeaders");
function json(body, status = 200, extra) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...CORS_HEADERS, ...extra }
  });
}
__name(json, "json");
function errorJson(code, message, status, extra) {
  return json({ error: message, code }, status, extra);
}
__name(errorJson, "errorJson");
function handleOptions() {
  return new Response(null, { status: 204, headers: CORS_HEADERS });
}
__name(handleOptions, "handleOptions");
function handleError(err) {
  if (err instanceof OrunError) {
    return errorJson(err.code, err.message, err.httpStatus);
  }
  return errorJson("INTERNAL_ERROR", "Internal server error", 500);
}
__name(handleError, "handleError");

// src/rate-limit.ts
var WINDOW_MS = 1e3;
var MAX_TOKENS = 300;
var REFILL_RATE = 30;
var RateLimitCounter = class {
  constructor(state, env) {
    this.state = state;
    this.env = env;
  }
  tokens = MAX_TOKENS;
  lastRefill = Date.now();
  async fetch(request) {
    const now = Date.now();
    const elapsed = now - this.lastRefill;
    const refillCount = Math.floor(elapsed / WINDOW_MS) * REFILL_RATE;
    if (refillCount > 0) {
      this.tokens = Math.min(MAX_TOKENS, this.tokens + refillCount);
      this.lastRefill = now;
    }
    if (this.tokens <= 0) {
      return new Response(
        JSON.stringify({ remaining: 0, limited: true }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    }
    this.tokens--;
    return new Response(
      JSON.stringify({ remaining: this.tokens, limited: false }),
      { status: 200, headers: { "Content-Type": "application/json" } }
    );
  }
};
__name(RateLimitCounter, "RateLimitCounter");
async function checkRateLimit(env, namespaceId) {
  const id = env.RATE_LIMITER.idFromName(namespaceId);
  const stub = env.RATE_LIMITER.get(id);
  const resp = await stub.fetch(new Request("https://rate-limiter.local/check"));
  const data = await resp.json();
  if (data.limited) {
    return errorJson("RATE_LIMITED", "Rate limit exceeded", 429, {
      "Retry-After": "1",
      "X-RateLimit-Limit": String(MAX_TOKENS),
      "X-RateLimit-Remaining": "0"
    });
  }
  return null;
}
__name(checkRateLimit, "checkRateLimit");

// src/auth/base64url.ts
var CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
function base64urlEncode(data) {
  let result = "";
  for (let i = 0; i < data.length; i += 3) {
    const a = data[i];
    const b = data[i + 1] ?? 0;
    const c = data[i + 2] ?? 0;
    const triplet = a << 16 | b << 8 | c;
    result += CHARS[triplet >> 18 & 63];
    result += CHARS[triplet >> 12 & 63];
    result += i + 1 < data.length ? CHARS[triplet >> 6 & 63] : "";
    result += i + 2 < data.length ? CHARS[triplet & 63] : "";
  }
  return result.replace(/\+/g, "-").replace(/\//g, "_");
}
__name(base64urlEncode, "base64urlEncode");
function base64urlDecode(str) {
  const base64 = str.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64 + "=".repeat((4 - base64.length % 4) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
__name(base64urlDecode, "base64urlDecode");
function base64urlEncodeString(str) {
  return base64urlEncode(new TextEncoder().encode(str));
}
__name(base64urlEncodeString, "base64urlEncodeString");
function base64urlDecodeString(b64url) {
  return new TextDecoder().decode(base64urlDecode(b64url));
}
__name(base64urlDecodeString, "base64urlDecodeString");

// src/auth/jwt.ts
function decodeJwt(token) {
  const segments = token.split(".");
  if (segments.length !== 3) {
    throw new OrunError("UNAUTHORIZED", "Malformed JWT: expected 3 segments");
  }
  let header;
  try {
    header = JSON.parse(base64urlDecodeString(segments[0]));
  } catch {
    throw new OrunError("UNAUTHORIZED", "Malformed JWT header");
  }
  let payload;
  try {
    payload = JSON.parse(base64urlDecodeString(segments[1]));
  } catch {
    throw new OrunError("UNAUTHORIZED", "Malformed JWT payload");
  }
  const signatureBytes = base64urlDecode(segments[2]);
  const signingInput = segments[0] + "." + segments[1];
  return { header, payload, signatureBytes, signingInput };
}
__name(decodeJwt, "decodeJwt");
async function signHmac(signingInput, secret) {
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"]
  );
  const sig = await crypto.subtle.sign(
    "HMAC",
    key,
    new TextEncoder().encode(signingInput)
  );
  return new Uint8Array(sig);
}
__name(signHmac, "signHmac");
async function verifyHmac(signingInput, signatureBytes, secret) {
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["verify"]
  );
  return crypto.subtle.verify(
    "HMAC",
    key,
    signatureBytes,
    new TextEncoder().encode(signingInput)
  );
}
__name(verifyHmac, "verifyHmac");
async function buildSignedHmacJwt(payload, secret) {
  const header = { alg: "HS256", typ: "JWT" };
  const h = base64urlEncodeString(JSON.stringify(header));
  const p = base64urlEncodeString(JSON.stringify(payload));
  const signingInput = `${h}.${p}`;
  const sig = await signHmac(signingInput, secret);
  return `${signingInput}.${base64urlEncode(sig)}`;
}
__name(buildSignedHmacJwt, "buildSignedHmacJwt");

// src/auth/oidc.ts
var JWKS_TTL_MS = 15 * 60 * 1e3;
var EXPECTED_ISSUER = "https://token.actions.githubusercontent.com";
var REQUIRED_CLAIMS = [
  "repository",
  "repository_id",
  "repository_owner",
  "repository_owner_id",
  "actor"
];
var cache = /* @__PURE__ */ new Map();
async function fetchJwks(jwksUrl, nowMs = Date.now()) {
  const cached = cache.get(jwksUrl);
  if (cached && cached.expiresAt > nowMs) {
    return cached.value;
  }
  const resp = await fetch(jwksUrl);
  if (!resp.ok) {
    throw new OrunError("UNAUTHORIZED", "Failed to fetch JWKS");
  }
  const jwks = await resp.json();
  cache.set(jwksUrl, { value: jwks, expiresAt: nowMs + JWKS_TTL_MS });
  return jwks;
}
__name(fetchJwks, "fetchJwks");
function findKey(jwks, kid) {
  const key = jwks.keys.find((k) => k.kid === kid);
  if (!key) {
    throw new OrunError("UNAUTHORIZED", "Unknown key ID");
  }
  return key;
}
__name(findKey, "findKey");
async function importRsaKey(jwk) {
  return crypto.subtle.importKey(
    "jwk",
    jwk,
    { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
    false,
    ["verify"]
  );
}
__name(importRsaKey, "importRsaKey");
function looksLikeOIDC(token) {
  try {
    const parts = token.split(".");
    if (parts.length !== 3)
      return false;
    const payload = JSON.parse(base64urlDecodeString(parts[1]));
    return payload.iss === EXPECTED_ISSUER;
  } catch {
    return false;
  }
}
__name(looksLikeOIDC, "looksLikeOIDC");
async function verifyOIDCToken(token, env) {
  const { header, payload, signatureBytes, signingInput } = decodeJwt(token);
  if (header.alg !== "RS256") {
    throw new OrunError("UNAUTHORIZED", "Unsupported algorithm");
  }
  if (!header.kid) {
    throw new OrunError("UNAUTHORIZED", "Missing key ID");
  }
  const jwks = await fetchJwks(env.GITHUB_JWKS_URL);
  const jwk = findKey(jwks, header.kid);
  const key = await importRsaKey(jwk);
  const valid = await crypto.subtle.verify(
    "RSASSA-PKCS1-v1_5",
    key,
    signatureBytes,
    new TextEncoder().encode(signingInput)
  );
  if (!valid) {
    throw new OrunError("UNAUTHORIZED", "Invalid OIDC signature");
  }
  if (payload.iss !== EXPECTED_ISSUER) {
    throw new OrunError("UNAUTHORIZED", "Invalid issuer");
  }
  const aud = payload.aud;
  if (Array.isArray(aud)) {
    if (!aud.includes(env.GITHUB_OIDC_AUDIENCE)) {
      throw new OrunError("UNAUTHORIZED", "Invalid audience");
    }
  } else if (aud !== env.GITHUB_OIDC_AUDIENCE) {
    throw new OrunError("UNAUTHORIZED", "Invalid audience");
  }
  const now = Math.floor(Date.now() / 1e3);
  if (typeof payload.exp !== "number" || payload.exp <= now) {
    throw new OrunError("UNAUTHORIZED", "Token expired");
  }
  if (typeof payload.iat !== "number" || payload.iat > now + 60) {
    throw new OrunError("UNAUTHORIZED", "Token not yet valid");
  }
  for (const claim of REQUIRED_CLAIMS) {
    if (!payload[claim] || typeof payload[claim] !== "string") {
      throw new OrunError("UNAUTHORIZED", `Missing required claim: ${claim}`);
    }
  }
  return {
    repository: payload.repository,
    repository_id: payload.repository_id,
    repository_owner: payload.repository_owner,
    repository_owner_id: payload.repository_owner_id,
    actor: payload.actor,
    aud: typeof aud === "string" ? aud : aud[0],
    iss: payload.iss,
    exp: payload.exp,
    iat: payload.iat
  };
}
__name(verifyOIDCToken, "verifyOIDCToken");
function extractNamespaceFromOIDC(claims) {
  return {
    namespaceId: claims.repository_id,
    namespaceSlug: claims.repository
  };
}
__name(extractNamespaceFromOIDC, "extractNamespaceFromOIDC");

// src/auth/session.ts
var DEFAULT_TTL_SECONDS = 3600;
async function issueSessionToken(claims, secret, ttlSeconds = DEFAULT_TTL_SECONDS) {
  if (!secret) {
    throw new OrunError("INTERNAL_ERROR", "Session secret not configured");
  }
  const now = Math.floor(Date.now() / 1e3);
  const payload = {
    ...claims,
    iat: now,
    exp: now + ttlSeconds
  };
  return buildSignedHmacJwt(payload, secret);
}
__name(issueSessionToken, "issueSessionToken");
async function verifySessionToken(token, secret) {
  if (!secret) {
    throw new OrunError("UNAUTHORIZED", "Session secret not configured");
  }
  const { header, payload, signatureBytes, signingInput } = decodeJwt(token);
  if (header.alg === "none") {
    throw new OrunError("UNAUTHORIZED", "Unsigned tokens not accepted");
  }
  if (header.alg !== "HS256") {
    throw new OrunError("UNAUTHORIZED", "Unsupported algorithm");
  }
  const valid = await verifyHmac(signingInput, signatureBytes, secret);
  if (!valid) {
    throw new OrunError("UNAUTHORIZED", "Invalid session signature");
  }
  const now = Math.floor(Date.now() / 1e3);
  if (typeof payload.exp !== "number" || payload.exp <= now) {
    throw new OrunError("UNAUTHORIZED", "Session token expired");
  }
  if (!payload.sub || typeof payload.sub !== "string") {
    throw new OrunError("UNAUTHORIZED", "Missing subject claim");
  }
  if (!Array.isArray(payload.allowedNamespaceIds) || !payload.allowedNamespaceIds.every((id) => typeof id === "string")) {
    throw new OrunError("UNAUTHORIZED", "Invalid allowedNamespaceIds claim");
  }
  return {
    sub: payload.sub,
    allowedNamespaceIds: payload.allowedNamespaceIds,
    sessionKind: payload.sessionKind ?? void 0,
    tokenUse: payload.tokenUse ?? void 0,
    githubUserId: typeof payload.githubUserId === "string" ? payload.githubUserId : void 0,
    exp: payload.exp,
    iat: payload.iat
  };
}
__name(verifySessionToken, "verifySessionToken");

// src/auth/namespace.ts
async function upsertNamespaceSlug(db, namespace, now) {
  const ts = (now ?? /* @__PURE__ */ new Date()).toISOString();
  await db.prepare(
    `INSERT INTO namespaces (namespace_id, namespace_slug, last_seen_at)
       VALUES (?1, ?2, ?3)
       ON CONFLICT(namespace_id) DO UPDATE SET
         namespace_slug = excluded.namespace_slug,
         last_seen_at = excluded.last_seen_at`
  ).bind(namespace.namespaceId, namespace.namespaceSlug, ts).run();
}
__name(upsertNamespaceSlug, "upsertNamespaceSlug");

// src/auth/github-oauth.ts
var GITHUB_AUTHORIZE_URL = "https://github.com/login/oauth/authorize";
var GITHUB_TOKEN_URL = "https://github.com/login/oauth/access_token";
var GITHUB_API_BASE = "https://api.github.com";
var OAUTH_STATE_TTL_SECONDS = 600;
var USER_AGENT = "orun-backend-auth";
async function buildSignedState(secret, returnTo, client) {
  const nonce = base64urlEncode(crypto.getRandomValues(new Uint8Array(16)));
  const exp = Math.floor(Date.now() / 1e3) + OAUTH_STATE_TTL_SECONDS;
  const payload = { nonce, exp };
  if (returnTo) {
    payload.returnTo = returnTo;
  }
  if (client) {
    payload.client = client;
  }
  const data = base64urlEncodeString(JSON.stringify(payload));
  const sig = await signHmac(data, secret);
  return `${data}.${base64urlEncode(sig)}`;
}
__name(buildSignedState, "buildSignedState");
async function verifySignedState(state, secret) {
  const dotIdx = state.lastIndexOf(".");
  if (dotIdx === -1) {
    throw new OrunError("INVALID_REQUEST", "Invalid OAuth state");
  }
  const data = state.slice(0, dotIdx);
  const sigB64 = state.slice(dotIdx + 1);
  const sigBytes = base64urlDecode(sigB64);
  const valid = await verifyHmac(data, sigBytes, secret);
  if (!valid) {
    throw new OrunError("INVALID_REQUEST", "Invalid OAuth state signature");
  }
  let payload;
  try {
    payload = JSON.parse(base64urlDecodeString(data));
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid OAuth state");
  }
  if (typeof payload.exp !== "number" || payload.exp <= Math.floor(Date.now() / 1e3)) {
    throw new OrunError("INVALID_REQUEST", "OAuth state expired");
  }
  return payload;
}
__name(verifySignedState, "verifySignedState");
function requireSecret(env, name) {
  const val = env[name];
  if (!val) {
    throw new OrunError("INTERNAL_ERROR", `${name} not configured`);
  }
  return val;
}
__name(requireSecret, "requireSecret");
function buildCallbackUrl(request, env) {
  if (env.ORUN_PUBLIC_URL) {
    return `${env.ORUN_PUBLIC_URL}/v1/auth/github/callback`;
  }
  const url = new URL(request.url);
  return `${url.origin}/v1/auth/github/callback`;
}
__name(buildCallbackUrl, "buildCallbackUrl");
function isLoopbackUrl(url) {
  return url.protocol === "http:" && (url.hostname === "127.0.0.1" || url.hostname === "localhost");
}
__name(isLoopbackUrl, "isLoopbackUrl");
function validateReturnTo(returnTo, env, request, client) {
  let parsed;
  try {
    parsed = new URL(returnTo);
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid returnTo URL");
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
    throw new OrunError("INVALID_REQUEST", "Invalid returnTo URL");
  }
  if (client === "cli") {
    if (!isLoopbackUrl(parsed)) {
      throw new OrunError("INVALID_REQUEST", "CLI returnTo must be a loopback URL (127.0.0.1 or localhost)");
    }
    return returnTo;
  }
  if (env.ORUN_DASHBOARD_URL) {
    const dashboardOrigin = new URL(env.ORUN_DASHBOARD_URL).origin;
    if (parsed.origin !== dashboardOrigin) {
      throw new OrunError("INVALID_REQUEST", "returnTo origin not allowed");
    }
  } else {
    const requestOrigin = new URL(request.url).origin;
    if (parsed.origin !== requestOrigin) {
      throw new OrunError("INVALID_REQUEST", "returnTo origin not allowed");
    }
  }
  return returnTo;
}
__name(validateReturnTo, "validateReturnTo");
async function buildGitHubOAuthRedirect(request, env) {
  const clientId = requireSecret(env, "GITHUB_CLIENT_ID");
  const sessionSecret = requireSecret(env, "ORUN_SESSION_SECRET");
  const url = new URL(request.url);
  const returnToParam = url.searchParams.get("returnTo");
  const clientParam = url.searchParams.get("client");
  const client = clientParam === "cli" ? "cli" : void 0;
  let returnTo;
  if (returnToParam) {
    returnTo = validateReturnTo(returnToParam, env, request, client);
  }
  const state = await buildSignedState(sessionSecret, returnTo, client);
  const redirectUri = buildCallbackUrl(request, env);
  const params = new URLSearchParams({
    client_id: clientId,
    redirect_uri: redirectUri,
    scope: "read:user,read:org",
    state
  });
  return Response.redirect(`${GITHUB_AUTHORIZE_URL}?${params.toString()}`, 302);
}
__name(buildGitHubOAuthRedirect, "buildGitHubOAuthRedirect");
async function exchangeCodeForToken(code, env, redirectUri) {
  const clientId = requireSecret(env, "GITHUB_CLIENT_ID");
  const clientSecret = requireSecret(env, "GITHUB_CLIENT_SECRET");
  const resp = await fetch(GITHUB_TOKEN_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json"
    },
    body: JSON.stringify({
      client_id: clientId,
      client_secret: clientSecret,
      code,
      redirect_uri: redirectUri
    })
  });
  if (!resp.ok) {
    throw new OrunError("UNAUTHORIZED", "Failed to exchange OAuth code");
  }
  const data = await resp.json();
  if (!data.access_token || typeof data.access_token !== "string") {
    throw new OrunError("UNAUTHORIZED", "GitHub OAuth token exchange failed");
  }
  return data.access_token;
}
__name(exchangeCodeForToken, "exchangeCodeForToken");
async function fetchGitHubUser(accessToken) {
  const resp = await fetch(`${GITHUB_API_BASE}/user`, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      Accept: "application/vnd.github+json",
      "User-Agent": USER_AGENT
    }
  });
  if (!resp.ok) {
    throw new OrunError("UNAUTHORIZED", "Failed to fetch GitHub user");
  }
  const data = await resp.json();
  return { login: data.login, id: data.id };
}
__name(fetchGitHubUser, "fetchGitHubUser");
async function fetchAllPages(url, accessToken) {
  const results = [];
  let nextUrl = url;
  while (nextUrl) {
    const resp = await fetch(nextUrl, {
      headers: {
        Authorization: `Bearer ${accessToken}`,
        Accept: "application/vnd.github+json",
        "User-Agent": USER_AGENT
      }
    });
    if (!resp.ok)
      break;
    const page = await resp.json();
    results.push(...page);
    const link = resp.headers.get("Link");
    nextUrl = parseLinkNext(link);
  }
  return results;
}
__name(fetchAllPages, "fetchAllPages");
function parseLinkNext(link) {
  if (!link)
    return null;
  const match = link.match(/<([^>]+)>;\s*rel="next"/);
  return match ? match[1] : null;
}
__name(parseLinkNext, "parseLinkNext");
async function fetchAdminRepos(accessToken) {
  const repos = await fetchAllPages(
    `${GITHUB_API_BASE}/user/repos?type=all&per_page=100`,
    accessToken
  );
  return repos.filter((r) => r.permissions?.admin).map((r) => ({ id: String(r.id), slug: r.full_name }));
}
__name(fetchAdminRepos, "fetchAdminRepos");
async function fetchOrgAdminRepos(accessToken) {
  const memberships = await fetchAllPages(
    `${GITHUB_API_BASE}/user/memberships/orgs?per_page=100`,
    accessToken
  );
  const adminOrgs = memberships.filter((m) => m.role === "admin");
  const items = [];
  for (const org of adminOrgs) {
    const repos = await fetchAllPages(
      `${GITHUB_API_BASE}/orgs/${org.organization.login}/repos?type=all&per_page=100`,
      accessToken
    );
    for (const r of repos) {
      items.push({ id: String(r.id), slug: r.full_name });
    }
  }
  return items;
}
__name(fetchOrgAdminRepos, "fetchOrgAdminRepos");
async function generateRefreshToken() {
  const raw = base64urlEncode(crypto.getRandomValues(new Uint8Array(32)));
  const hashBytes = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(raw));
  const hash = base64urlEncode(new Uint8Array(hashBytes));
  return { raw, hash };
}
__name(generateRefreshToken, "generateRefreshToken");
async function hashRefreshToken(raw) {
  const hashBytes = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(raw));
  return base64urlEncode(new Uint8Array(hashBytes));
}
__name(hashRefreshToken, "hashRefreshToken");
async function handleGitHubOAuthCallback(request, env) {
  const sessionSecret = requireSecret(env, "ORUN_SESSION_SECRET");
  const url = new URL(request.url);
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  if (!code) {
    throw new OrunError("INVALID_REQUEST", "Missing OAuth code");
  }
  if (!state) {
    throw new OrunError("INVALID_REQUEST", "Missing OAuth state");
  }
  const statePayload = await verifySignedState(state, sessionSecret);
  const isCli = statePayload.client === "cli";
  const redirectUri = buildCallbackUrl(request, env);
  const accessToken = await exchangeCodeForToken(code, env, redirectUri);
  const user = await fetchGitHubUser(accessToken);
  const [repoItems, orgRepoItems] = await Promise.all([
    fetchAdminRepos(accessToken),
    fetchOrgAdminRepos(accessToken)
  ]);
  const seen = /* @__PURE__ */ new Map();
  for (const r of [...repoItems, ...orgRepoItems]) {
    if (!seen.has(r.id))
      seen.set(r.id, r.slug);
  }
  const namespaceSlugs = Array.from(seen.entries()).map(([id, slug]) => ({ id, slug }));
  const allowedNamespaceIds = namespaceSlugs.map((r) => r.id);
  const sessionKind = isCli ? "cli" : "dashboard";
  const sessionToken = await issueSessionToken(
    { sub: user.login, allowedNamespaceIds, sessionKind, tokenUse: "access", githubUserId: String(user.id) },
    sessionSecret
  );
  const result = {
    sessionToken,
    sessionKind,
    githubLogin: user.login,
    githubUserId: String(user.id),
    allowedNamespaceIds,
    namespaceSlugs,
    returnTo: statePayload.returnTo
  };
  if (isCli) {
    const { raw, hash } = await generateRefreshToken();
    const refreshExpiresAt = new Date(Date.now() + 30 * 24 * 60 * 60 * 1e3).toISOString();
    result.refreshToken = raw;
    result.refreshExpiresAt = refreshExpiresAt;
    result._refreshTokenHash = hash;
  }
  return result;
}
__name(handleGitHubOAuthCallback, "handleGitHubOAuthCallback");

// src/auth/index.ts
async function authenticate(request, env, ctx) {
  const deployToken = request.headers.get("X-Orun-Deploy-Token");
  if (deployToken) {
    if (!env.ORUN_DEPLOY_TOKEN) {
      throw new OrunError("UNAUTHORIZED", "Deploy token not configured");
    }
    if (deployToken !== env.ORUN_DEPLOY_TOKEN) {
      throw new OrunError("UNAUTHORIZED", "Invalid deploy token");
    }
    return { type: "deploy", namespace: null, allowedNamespaceIds: ["*"], actor: "system" };
  }
  const auth = request.headers.get("Authorization");
  if (!auth?.startsWith("Bearer ")) {
    throw new OrunError("UNAUTHORIZED", "Missing authorization header");
  }
  const token = auth.slice(7);
  if (looksLikeOIDC(token)) {
    const claims2 = await verifyOIDCToken(token, env);
    const namespace = extractNamespaceFromOIDC(claims2);
    const upsertPromise = upsertNamespaceSlug(env.DB, namespace);
    if (ctx?.waitUntil) {
      ctx.waitUntil(upsertPromise);
    } else {
      await upsertPromise;
    }
    return {
      type: "oidc",
      namespace,
      allowedNamespaceIds: [namespace.namespaceId],
      actor: claims2.actor
    };
  }
  if (!env.ORUN_SESSION_SECRET) {
    throw new OrunError("UNAUTHORIZED", "Session secret not configured");
  }
  const claims = await verifySessionToken(token, env.ORUN_SESSION_SECRET);
  return {
    type: "session",
    sessionKind: claims.sessionKind === "cli" ? "cli" : "dashboard",
    namespace: null,
    allowedNamespaceIds: claims.allowedNamespaceIds,
    actor: claims.sub,
    githubUserId: claims.githubUserId
  };
}
__name(authenticate, "authenticate");

// src/auth/device-flow.ts
var GITHUB_DEVICE_CODE_URL = "https://github.com/login/device/code";
var GITHUB_TOKEN_URL2 = "https://github.com/login/oauth/access_token";
var GITHUB_API_BASE2 = "https://api.github.com";
var DEVICE_SCOPE = "read:user,read:org";
var USER_AGENT2 = "orun-backend-auth";
function requireSecret2(env, name) {
  const val = env[name];
  if (!val) {
    throw new OrunError("INTERNAL_ERROR", `${name} not configured`);
  }
  return val;
}
__name(requireSecret2, "requireSecret");
async function startDeviceFlow(env) {
  const clientId = requireSecret2(env, "GITHUB_CLIENT_ID");
  const resp = await fetch(GITHUB_DEVICE_CODE_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      "User-Agent": USER_AGENT2
    },
    body: JSON.stringify({ client_id: clientId, scope: DEVICE_SCOPE })
  });
  if (!resp.ok) {
    throw new OrunError("INTERNAL_ERROR", "Failed to start device flow");
  }
  const data = await resp.json();
  if (!data.device_code || !data.user_code || !data.verification_uri) {
    throw new OrunError("INTERNAL_ERROR", "Invalid device flow response from GitHub");
  }
  return {
    deviceCode: data.device_code,
    userCode: data.user_code,
    verificationUri: data.verification_uri,
    verificationUriComplete: data.verification_uri_complete ?? data.verification_uri,
    expiresIn: data.expires_in ?? 900,
    interval: data.interval ?? 5
  };
}
__name(startDeviceFlow, "startDeviceFlow");
async function fetchAllPages2(url, accessToken) {
  const results = [];
  let nextUrl = url;
  while (nextUrl) {
    const resp = await fetch(nextUrl, {
      headers: {
        Authorization: `Bearer ${accessToken}`,
        Accept: "application/vnd.github+json",
        "User-Agent": USER_AGENT2
      }
    });
    if (!resp.ok)
      break;
    const page = await resp.json();
    results.push(...page);
    const link = resp.headers.get("Link");
    nextUrl = parseLinkNext2(link);
  }
  return results;
}
__name(fetchAllPages2, "fetchAllPages");
function parseLinkNext2(link) {
  if (!link)
    return null;
  const match = link.match(/<([^>]+)>;\s*rel="next"/);
  return match ? match[1] : null;
}
__name(parseLinkNext2, "parseLinkNext");
async function fetchAdminRepos2(accessToken) {
  const repos = await fetchAllPages2(
    `${GITHUB_API_BASE2}/user/repos?type=all&per_page=100`,
    accessToken
  );
  return repos.filter((r) => r.permissions?.admin).map((r) => ({ id: String(r.id), slug: r.full_name }));
}
__name(fetchAdminRepos2, "fetchAdminRepos");
async function fetchOrgAdminRepos2(accessToken) {
  const memberships = await fetchAllPages2(
    `${GITHUB_API_BASE2}/user/memberships/orgs?per_page=100`,
    accessToken
  );
  const adminOrgs = memberships.filter((m) => m.role === "admin");
  const items = [];
  for (const org of adminOrgs) {
    const repos = await fetchAllPages2(
      `${GITHUB_API_BASE2}/orgs/${org.organization.login}/repos?type=all&per_page=100`,
      accessToken
    );
    for (const r of repos)
      items.push({ id: String(r.id), slug: r.full_name });
  }
  return items;
}
__name(fetchOrgAdminRepos2, "fetchOrgAdminRepos");
async function pollDeviceFlow(deviceCode, env) {
  const clientId = requireSecret2(env, "GITHUB_CLIENT_ID");
  const sessionSecret = requireSecret2(env, "ORUN_SESSION_SECRET");
  const resp = await fetch(GITHUB_TOKEN_URL2, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      "User-Agent": USER_AGENT2
    },
    body: JSON.stringify({
      client_id: clientId,
      device_code: deviceCode,
      grant_type: "urn:ietf:params:oauth:grant-type:device_code"
    })
  });
  if (!resp.ok) {
    throw new OrunError("INTERNAL_ERROR", "Failed to poll device flow");
  }
  const data = await resp.json();
  if (data.error) {
    const errorCode = data.error;
    if (errorCode === "authorization_pending") {
      return { status: "pending", interval: 5 };
    }
    if (errorCode === "slow_down") {
      const interval = typeof data.interval === "number" ? data.interval : 10;
      throw new OrunError("RATE_LIMITED", `slow_down: retry after ${interval}s`);
    }
    if (errorCode === "expired_token") {
      throw new OrunError("UNAUTHORIZED", "Device code expired");
    }
    if (errorCode === "access_denied") {
      throw new OrunError("FORBIDDEN", "Device authorization denied");
    }
    throw new OrunError("INVALID_REQUEST", `GitHub device flow error: ${errorCode}`);
  }
  if (!data.access_token || typeof data.access_token !== "string") {
    throw new OrunError("INTERNAL_ERROR", "Missing access token in device flow response");
  }
  const githubAccessToken = data.access_token;
  const userResp = await fetch(`${GITHUB_API_BASE2}/user`, {
    headers: {
      Authorization: `Bearer ${githubAccessToken}`,
      Accept: "application/vnd.github+json",
      "User-Agent": USER_AGENT2
    }
  });
  if (!userResp.ok) {
    throw new OrunError("UNAUTHORIZED", "Failed to fetch GitHub user after device flow");
  }
  const user = await userResp.json();
  const [repoItems, orgRepoItems] = await Promise.all([
    fetchAdminRepos2(githubAccessToken),
    fetchOrgAdminRepos2(githubAccessToken)
  ]);
  const seen = /* @__PURE__ */ new Map();
  for (const r of [...repoItems, ...orgRepoItems]) {
    if (!seen.has(r.id))
      seen.set(r.id, r.slug);
  }
  const namespaceSlugs = Array.from(seen.entries()).map(([id, slug]) => ({ id, slug }));
  const allowedNamespaceIds = namespaceSlugs.map((r) => r.id);
  const accessToken = await issueSessionToken(
    { sub: user.login, allowedNamespaceIds, sessionKind: "cli", tokenUse: "access", githubUserId: String(user.id) },
    sessionSecret
  );
  const expiresAt = new Date(Date.now() + 3600 * 1e3).toISOString();
  const refreshExpiresAt = new Date(Date.now() + 30 * 24 * 60 * 60 * 1e3).toISOString();
  const { raw: refreshToken, hash: _refreshTokenHash } = await generateRefreshToken();
  return {
    accessToken,
    expiresAt,
    refreshToken,
    refreshExpiresAt,
    githubLogin: user.login,
    githubUserId: String(user.id),
    allowedNamespaceIds,
    namespaceSlugs,
    _refreshTokenHash
  };
}
__name(pollDeviceFlow, "pollDeviceFlow");

// ../../packages/types/src/paths.ts
function runLogPath(namespaceId, runId, jobId) {
  return `${namespaceId}/runs/${runId}/logs/${jobId}.log`;
}
__name(runLogPath, "runLogPath");
function planPath(namespaceId, checksum) {
  return `${namespaceId}/plans/${checksum}.json`;
}
__name(planPath, "planPath");
function coordinatorKey(namespaceId, runId) {
  return `${namespaceId}:${runId}`;
}
__name(coordinatorKey, "coordinatorKey");
function catalogEnvelopePath(namespaceId, uploadId) {
  return `${namespaceId}/catalog/uploads/${uploadId}/catalog-sync-envelope.json`;
}
__name(catalogEnvelopePath, "catalogEnvelopePath");
function catalogComponentStatePath(namespaceId, commitSha, componentName) {
  return `${namespaceId}/catalog/commits/${commitSha}/components/${componentName}.json`;
}
__name(catalogComponentStatePath, "catalogComponentStatePath");

// ../../packages/storage/dist/r2.js
var R2Storage = class {
  bucket;
  constructor(bucket) {
    this.bucket = bucket;
  }
  async writeLog(namespaceId, runId, jobId, content, options) {
    const key = runLogPath(namespaceId, runId, jobId);
    const putOptions = {
      httpMetadata: { contentType: "text/plain; charset=utf-8" }
    };
    if (options?.expiresAt) {
      const isoString = options.expiresAt instanceof Date ? options.expiresAt.toISOString() : options.expiresAt;
      putOptions.customMetadata = { "expires-at": isoString };
    }
    await this.bucket.put(key, content, putOptions);
    return key;
  }
  async readLog(namespaceId, runId, jobId) {
    const key = runLogPath(namespaceId, runId, jobId);
    return this.bucket.get(key);
  }
  async savePlan(namespaceId, plan) {
    const key = planPath(namespaceId, plan.checksum);
    await this.bucket.put(key, JSON.stringify(plan), {
      httpMetadata: { contentType: "application/json; charset=utf-8" }
    });
    return key;
  }
  async getPlan(namespaceId, checksum) {
    const key = planPath(namespaceId, checksum);
    const obj = await this.bucket.get(key);
    if (!obj)
      return null;
    return await obj.json();
  }
  async listRunLogs(namespaceId, runId) {
    const prefix = `${namespaceId}/runs/${runId}/logs/`;
    const keys = [];
    let cursor;
    do {
      const listed = await this.bucket.list({ prefix, cursor });
      for (const obj of listed.objects) {
        keys.push(obj.key);
      }
      cursor = listed.truncated ? listed.cursor : void 0;
    } while (cursor);
    return keys;
  }
  async deleteRun(namespaceId, runId) {
    const prefix = `${namespaceId}/runs/${runId}/`;
    let cursor;
    do {
      const listed = await this.bucket.list({ prefix, cursor });
      const keysToDelete = listed.objects.map((obj) => obj.key);
      if (keysToDelete.length > 0) {
        await this.bucket.delete(keysToDelete);
      }
      cursor = listed.truncated ? listed.cursor : void 0;
    } while (cursor);
  }
  async writeCatalogEnvelope(namespaceId, uploadId, envelope) {
    const key = catalogEnvelopePath(namespaceId, uploadId);
    await this.bucket.put(key, JSON.stringify(envelope), {
      httpMetadata: { contentType: "application/json; charset=utf-8" }
    });
    return key;
  }
  async writeCatalogComponentState(namespaceId, commitSha, componentName, state) {
    const key = catalogComponentStatePath(namespaceId, commitSha, componentName);
    await this.bucket.put(key, JSON.stringify(state), {
      httpMetadata: { contentType: "application/json; charset=utf-8" }
    });
    return key;
  }
};
__name(R2Storage, "R2Storage");

// ../../packages/storage/dist/d1.js
var D1Index = class {
  db;
  constructor(db) {
    this.db = db;
  }
  async upsertNamespace(namespace) {
    await this.db.prepare(`INSERT INTO namespaces (namespace_id, namespace_slug, last_seen_at)
         VALUES (?1, ?2, ?3)
         ON CONFLICT(namespace_id) DO UPDATE SET
           namespace_slug = excluded.namespace_slug,
           last_seen_at = excluded.last_seen_at`).bind(namespace.namespaceId, namespace.namespaceSlug, (/* @__PURE__ */ new Date()).toISOString()).run();
  }
  async createRun(run) {
    await this.upsertNamespace(run.namespace);
    await this.db.prepare(`INSERT INTO runs (run_id, namespace_id, status, plan_checksum, trigger_type, actor, dry_run, created_at, updated_at, finished_at, job_total, job_done, job_failed, expires_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14)
         ON CONFLICT(namespace_id, run_id) DO UPDATE SET
           status = excluded.status,
           updated_at = excluded.updated_at`).bind(run.runId, run.namespace.namespaceId, run.status, run.planChecksum, run.triggerType, run.actor, run.dryRun ? 1 : 0, run.createdAt, run.updatedAt, run.finishedAt, run.jobTotal, run.jobDone, run.jobFailed, run.expiresAt).run();
  }
  async updateRun(namespaceId, runId, update) {
    const setClauses = [];
    const values = [];
    let paramIdx = 1;
    if (update.status !== void 0) {
      setClauses.push(`status = ?${paramIdx++}`);
      values.push(update.status);
    }
    if (update.jobDone !== void 0) {
      setClauses.push(`job_done = ?${paramIdx++}`);
      values.push(update.jobDone);
    }
    if (update.jobFailed !== void 0) {
      setClauses.push(`job_failed = ?${paramIdx++}`);
      values.push(update.jobFailed);
    }
    if (update.finishedAt !== void 0) {
      setClauses.push(`finished_at = ?${paramIdx++}`);
      values.push(update.finishedAt);
    }
    if (update.updatedAt !== void 0) {
      setClauses.push(`updated_at = ?${paramIdx++}`);
      values.push(update.updatedAt);
    }
    if (setClauses.length === 0)
      return;
    const sql = `UPDATE runs SET ${setClauses.join(", ")} WHERE namespace_id = ?${paramIdx++} AND run_id = ?${paramIdx}`;
    values.push(namespaceId, runId);
    await this.db.prepare(sql).bind(...values).run();
  }
  async listRuns(namespaceIds, limit = 50, offset = 0) {
    if (namespaceIds.length === 0)
      return [];
    const placeholders = namespaceIds.map((_, i) => `?${i + 1}`).join(", ");
    const sql = `SELECT r.*, n.namespace_slug
      FROM runs r
      JOIN namespaces n ON n.namespace_id = r.namespace_id
      WHERE r.namespace_id IN (${placeholders})
      ORDER BY r.created_at DESC
      LIMIT ?${namespaceIds.length + 1} OFFSET ?${namespaceIds.length + 2}`;
    const result = await this.db.prepare(sql).bind(...namespaceIds, limit, offset).all();
    return (result.results ?? []).map(rowToRun);
  }
  async getRun(namespaceId, runId) {
    const result = await this.db.prepare(`SELECT r.*, n.namespace_slug
         FROM runs r
         JOIN namespaces n ON n.namespace_id = r.namespace_id
         WHERE r.namespace_id = ?1 AND r.run_id = ?2`).bind(namespaceId, runId).first();
    if (!result)
      return null;
    return rowToRun(result);
  }
  async upsertJob(job) {
    await this.db.prepare(`INSERT INTO jobs (job_id, run_id, namespace_id, component, status, runner_id, started_at, finished_at, log_ref)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
         ON CONFLICT(namespace_id, run_id, job_id) DO UPDATE SET
           status = excluded.status,
           runner_id = excluded.runner_id,
           started_at = excluded.started_at,
           finished_at = excluded.finished_at,
           log_ref = excluded.log_ref`).bind(job.jobId, job.runId, job.namespaceId, job.component, job.status, job.runnerId, job.startedAt, job.finishedAt, job.logRef).run();
  }
  async listJobs(namespaceId, runId) {
    const result = await this.db.prepare(`SELECT * FROM jobs WHERE namespace_id = ?1 AND run_id = ?2`).bind(namespaceId, runId).all();
    return (result.results ?? []).map(rowToJob);
  }
  async deleteExpiredRuns(now) {
    const isoNow = now instanceof Date ? now.toISOString() : now ?? (/* @__PURE__ */ new Date()).toISOString();
    await this.db.prepare(`DELETE FROM jobs WHERE EXISTS (
           SELECT 1 FROM runs
           WHERE runs.namespace_id = jobs.namespace_id
             AND runs.run_id = jobs.run_id
             AND runs.expires_at <= ?1
         )`).bind(isoNow).run();
    const result = await this.db.prepare(`DELETE FROM runs WHERE expires_at <= ?1`).bind(isoNow).run();
    return result.meta?.changes ?? 0;
  }
  async createCliSession(input) {
    const now = (/* @__PURE__ */ new Date()).toISOString();
    await this.db.prepare(`INSERT INTO cli_sessions (session_id, account_id, github_login, refresh_token_hash, allowed_namespace_ids_json, created_at, expires_at, user_agent, device_label)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`).bind(input.sessionId, input.accountId, input.githubLogin, input.refreshTokenHash, JSON.stringify(input.allowedNamespaceIds), now, input.expiresAt, input.userAgent ?? null, input.deviceLabel ?? null).run();
    return {
      sessionId: input.sessionId,
      accountId: input.accountId,
      githubLogin: input.githubLogin,
      allowedNamespaceIds: input.allowedNamespaceIds,
      createdAt: now,
      lastUsedAt: null,
      expiresAt: input.expiresAt,
      revokedAt: null,
      userAgent: input.userAgent ?? null,
      deviceLabel: input.deviceLabel ?? null
    };
  }
  async getCliSessionByRefreshHash(refreshTokenHash) {
    const row = await this.db.prepare(`SELECT session_id, account_id, github_login, allowed_namespace_ids_json, created_at, last_used_at, expires_at, revoked_at, user_agent, device_label
         FROM cli_sessions WHERE refresh_token_hash = ?1`).bind(refreshTokenHash).first();
    if (!row)
      return null;
    return rowToCliSession(row);
  }
  async markCliSessionUsed(sessionId, usedAt) {
    await this.db.prepare(`UPDATE cli_sessions SET last_used_at = ?1 WHERE session_id = ?2`).bind(usedAt, sessionId).run();
  }
  async revokeCliSession(sessionId, revokedAt) {
    await this.db.prepare(`UPDATE cli_sessions SET revoked_at = ?1 WHERE session_id = ?2`).bind(revokedAt, sessionId).run();
  }
  async recordCatalogUpload(input) {
    const existing = await this.db.prepare(`SELECT upload_id, created_at, component_count FROM catalog_uploads WHERE upload_id = ?1`).bind(input.uploadId).first();
    if (existing) {
      return {
        uploadId: existing.upload_id,
        acceptedAt: existing.created_at,
        componentCount: existing.component_count
      };
    }
    await this.db.prepare(`INSERT INTO catalog_uploads (upload_id, namespace_id, repo_id, repo_full_name, commit_sha, branch, workflow_run_id, workflow_ref, pr_number, envelope_ref, component_count, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12)`).bind(input.uploadId, input.namespaceId, input.repoId, input.repoFullName, input.commitSha, input.branch ?? null, input.workflowRunId ?? null, input.workflowRef ?? null, input.prNumber ?? null, input.envelopeRef, input.componentCount, input.createdAt).run();
    return { uploadId: input.uploadId, acceptedAt: input.createdAt, componentCount: input.componentCount };
  }
  async uploadExists(uploadId) {
    const row = await this.db.prepare(`SELECT upload_id FROM catalog_uploads WHERE upload_id = ?1`).bind(uploadId).first();
    return row !== null;
  }
  async upsertCatalogComponent(input) {
    await this.db.prepare(`INSERT INTO catalog_components (component_id, namespace_id, repo_id, repo_full_name, name, title, description, type, owner, system, lifecycle, repo_path, tags_json, environments_json, latest_plan_id, latest_plan_checksum, latest_commit_sha, latest_status, current_state_ref, first_seen_at, last_seen_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, ?20, ?21)
         ON CONFLICT(component_id) DO UPDATE SET
           name = excluded.name,
           title = excluded.title,
           description = excluded.description,
           type = excluded.type,
           owner = excluded.owner,
           system = excluded.system,
           lifecycle = excluded.lifecycle,
           repo_path = excluded.repo_path,
           tags_json = excluded.tags_json,
           environments_json = excluded.environments_json,
           latest_plan_id = excluded.latest_plan_id,
           latest_plan_checksum = excluded.latest_plan_checksum,
           latest_commit_sha = excluded.latest_commit_sha,
           latest_status = excluded.latest_status,
           current_state_ref = excluded.current_state_ref,
           last_seen_at = excluded.last_seen_at`).bind(input.componentId, input.namespaceId, input.repoId, input.repoFullName, input.name, input.title ?? null, input.description ?? null, input.type, input.owner ?? null, input.system ?? null, input.lifecycle ?? null, input.repoPath, JSON.stringify(input.tags), JSON.stringify(input.environments), input.latestPlanId ?? null, input.latestPlanChecksum ?? null, input.latestCommitSha, input.latestStatus, input.currentStateRef, input.firstSeenAt, input.lastSeenAt).run();
  }
  async getCatalogComponentRow(componentId) {
    return this.db.prepare(`SELECT * FROM catalog_components WHERE component_id = ?1`).bind(componentId).first();
  }
  async replaceCatalogRelations(componentId, relations) {
    await this.db.prepare(`DELETE FROM catalog_component_relations WHERE source_component_id = ?1`).bind(componentId).run();
    for (const rel of relations) {
      await this.db.prepare(`INSERT INTO catalog_component_relations (relation_id, source_component_id, relation_type, target_kind, target_ref, environment, job_id, last_seen_at)
           VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
           ON CONFLICT(relation_id) DO UPDATE SET last_seen_at = excluded.last_seen_at`).bind(rel.relationId, componentId, rel.relationType, rel.targetKind, rel.targetRef, rel.environment ?? null, rel.jobId ?? null, rel.lastSeenAt).run();
    }
  }
  async appendCatalogComponentEvent(input) {
    await this.db.prepare(`INSERT INTO catalog_component_events (event_id, component_id, namespace_id, upload_id, event_type, commit_sha, pr_number, summary, payload_ref, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)
         ON CONFLICT(event_id) DO NOTHING`).bind(input.eventId, input.componentId, input.namespaceId, input.uploadId, input.eventType, input.commitSha, input.prNumber ?? null, input.summary ?? null, input.payloadRef ?? null, input.createdAt).run();
  }
  async listCatalogComponents(filter) {
    if (filter.visibleNamespaceIds.length === 0)
      return { components: [], total: 0 };
    const limit = Math.min(filter.limit ?? 50, 100);
    const offset = filter.offset ?? 0;
    const conditions = [];
    const params = [];
    let idx = 1;
    const nsPH = filter.visibleNamespaceIds.map(() => `?${idx++}`).join(", ");
    params.push(...filter.visibleNamespaceIds);
    conditions.push(`cc.namespace_id IN (${nsPH})`);
    if (filter.repoId) {
      conditions.push(`cc.repo_id = ?${idx++}`);
      params.push(filter.repoId);
    }
    if (filter.type) {
      conditions.push(`cc.type = ?${idx++}`);
      params.push(filter.type);
    }
    if (filter.owner) {
      conditions.push(`cc.owner = ?${idx++}`);
      params.push(filter.owner);
    }
    if (filter.system) {
      conditions.push(`cc.system = ?${idx++}`);
      params.push(filter.system);
    }
    if (filter.status) {
      conditions.push(`cc.latest_status = ?${idx++}`);
      params.push(filter.status);
    }
    if (filter.tag) {
      conditions.push(`cc.tags_json LIKE ?${idx++}`);
      params.push(`%${filter.tag}%`);
    }
    if (filter.q) {
      conditions.push(`(cc.name LIKE ?${idx} OR cc.title LIKE ?${idx} OR cc.owner LIKE ?${idx} OR cc.system LIKE ?${idx} OR cc.repo_full_name LIKE ?${idx} OR cc.tags_json LIKE ?${idx})`);
      params.push(`%${filter.q}%`);
      idx++;
    }
    const where = conditions.length > 0 ? `WHERE ${conditions.join(" AND ")}` : "";
    const countSql = `SELECT COUNT(*) as total FROM catalog_components cc JOIN namespaces n ON n.namespace_id = cc.namespace_id ${where}`;
    const countRow = await this.db.prepare(countSql).bind(...params).first();
    const total = countRow?.total ?? 0;
    const dataSql = `SELECT cc.*, n.namespace_slug FROM catalog_components cc JOIN namespaces n ON n.namespace_id = cc.namespace_id ${where} ORDER BY cc.last_seen_at DESC LIMIT ?${idx++} OFFSET ?${idx}`;
    params.push(limit, offset);
    const result = await this.db.prepare(dataSql).bind(...params).all();
    const components = (result.results ?? []).map(rowToCatalogSummary);
    return { components, total };
  }
  async getCatalogComponent(visibleNamespaceIds, componentId) {
    if (visibleNamespaceIds.length === 0)
      return null;
    const nsPH = visibleNamespaceIds.map((_, i) => `?${i + 2}`).join(", ");
    const row = await this.db.prepare(`SELECT cc.*, n.namespace_slug FROM catalog_components cc JOIN namespaces n ON n.namespace_id = cc.namespace_id WHERE cc.component_id = ?1 AND cc.namespace_id IN (${nsPH})`).bind(componentId, ...visibleNamespaceIds).first();
    if (!row)
      return null;
    const relResult = await this.db.prepare(`SELECT * FROM catalog_component_relations WHERE source_component_id = ?1`).bind(componentId).all();
    const relations = (relResult.results ?? []).map(rowToRelation);
    return { ...rowToCatalogSummary(row), relations };
  }
  async listCatalogComponentEvents(visibleNamespaceIds, componentId) {
    if (visibleNamespaceIds.length === 0)
      return [];
    const nsPH = visibleNamespaceIds.map((_, i) => `?${i + 2}`).join(", ");
    const result = await this.db.prepare(`SELECT e.*, n.namespace_slug FROM catalog_component_events e JOIN namespaces n ON n.namespace_id = e.namespace_id WHERE e.component_id = ?1 AND e.namespace_id IN (${nsPH}) ORDER BY e.created_at DESC LIMIT 100`).bind(componentId, ...visibleNamespaceIds).all();
    return (result.results ?? []).map(rowToCatalogEvent);
  }
  async listCatalogComponentRelations(visibleNamespaceIds, componentId) {
    if (visibleNamespaceIds.length === 0)
      return { outgoing: [], incoming: [] };
    const nsPH = visibleNamespaceIds.map((_, i) => `?${i + 2}`).join(", ");
    const outResult = await this.db.prepare(`SELECT r.* FROM catalog_component_relations r JOIN catalog_components cc ON cc.component_id = r.source_component_id WHERE r.source_component_id = ?1 AND cc.namespace_id IN (${nsPH})`).bind(componentId, ...visibleNamespaceIds).all();
    const inResult = await this.db.prepare(`SELECT r.*, cc.name as source_name FROM catalog_component_relations r JOIN catalog_components cc ON cc.component_id = r.source_component_id WHERE r.target_ref = (SELECT component_id FROM catalog_components WHERE component_id = ?1) AND cc.namespace_id IN (${nsPH})`).bind(componentId, ...visibleNamespaceIds).all();
    const outgoing = (outResult.results ?? []).map(rowToRelation);
    const incoming = (inResult.results ?? []).map((row) => ({
      ...rowToRelation(row),
      sourceComponentId: row.source_component_id,
      sourceName: row.source_name
    }));
    return { outgoing, incoming };
  }
  async listCatalogComponentRecentRuns(visibleNamespaceIds, componentName, limit = 10) {
    if (visibleNamespaceIds.length === 0)
      return [];
    const nsPH = visibleNamespaceIds.map((_, i) => `?${i + 2}`).join(", ");
    const result = await this.db.prepare(`SELECT r.*, n.namespace_slug FROM runs r JOIN namespaces n ON n.namespace_id = r.namespace_id WHERE r.namespace_id IN (${nsPH}) AND EXISTS (SELECT 1 FROM jobs j WHERE j.run_id = r.run_id AND j.namespace_id = r.namespace_id AND j.component = ?1) ORDER BY r.created_at DESC LIMIT ?${visibleNamespaceIds.length + 2}`).bind(componentName, ...visibleNamespaceIds, limit).all();
    return (result.results ?? []).map(rowToRun);
  }
};
__name(D1Index, "D1Index");
function rowToRun(row) {
  return {
    runId: row.run_id,
    namespace: {
      namespaceId: row.namespace_id,
      namespaceSlug: row.namespace_slug ?? ""
    },
    status: row.status,
    planChecksum: row.plan_checksum ?? "",
    triggerType: row.trigger_type ?? "ci",
    actor: row.actor ?? null,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    finishedAt: row.finished_at ?? null,
    jobTotal: row.job_total,
    jobDone: row.job_done,
    jobFailed: row.job_failed,
    dryRun: row.dry_run === 1,
    expiresAt: row.expires_at
  };
}
__name(rowToRun, "rowToRun");
function rowToJob(row) {
  return {
    jobId: row.job_id,
    runId: row.run_id,
    component: row.component,
    status: row.status,
    deps: [],
    runnerId: row.runner_id ?? null,
    startedAt: row.started_at ?? null,
    finishedAt: row.finished_at ?? null,
    lastError: null,
    heartbeatAt: null,
    logRef: row.log_ref ?? null
  };
}
__name(rowToJob, "rowToJob");
function rowToCliSession(row) {
  return {
    sessionId: row.session_id,
    accountId: row.account_id,
    githubLogin: row.github_login,
    allowedNamespaceIds: JSON.parse(row.allowed_namespace_ids_json),
    createdAt: row.created_at,
    lastUsedAt: row.last_used_at ?? null,
    expiresAt: row.expires_at,
    revokedAt: row.revoked_at ?? null,
    userAgent: row.user_agent ?? null,
    deviceLabel: row.device_label ?? null
  };
}
__name(rowToCliSession, "rowToCliSession");
function rowToCatalogSummary(row) {
  return {
    componentId: row.component_id,
    namespace: {
      namespaceId: row.namespace_id,
      namespaceSlug: row.namespace_slug ?? ""
    },
    repoId: row.repo_id,
    repoFullName: row.repo_full_name,
    name: row.name,
    title: row.title ?? void 0,
    description: row.description ?? void 0,
    type: row.type,
    owner: row.owner ?? void 0,
    system: row.system ?? void 0,
    lifecycle: row.lifecycle ?? void 0,
    repoPath: row.repo_path,
    tags: JSON.parse(row.tags_json ?? "[]"),
    environments: JSON.parse(row.environments_json ?? "[]"),
    latestPlanId: row.latest_plan_id ?? void 0,
    latestPlanChecksum: row.latest_plan_checksum ?? void 0,
    latestCommitSha: row.latest_commit_sha,
    latestStatus: row.latest_status ?? "unknown",
    currentStateRef: row.current_state_ref,
    firstSeenAt: row.first_seen_at,
    lastSeenAt: row.last_seen_at
  };
}
__name(rowToCatalogSummary, "rowToCatalogSummary");
function rowToRelation(row) {
  return {
    relationType: row.relation_type,
    targetKind: row.target_kind,
    targetRef: row.target_ref,
    environment: row.environment ?? void 0,
    jobId: row.job_id ?? void 0
  };
}
__name(rowToRelation, "rowToRelation");
function rowToCatalogEvent(row) {
  return {
    eventId: row.event_id,
    componentId: row.component_id,
    namespace: {
      namespaceId: row.namespace_id,
      namespaceSlug: row.namespace_slug ?? ""
    },
    uploadId: row.upload_id,
    eventType: row.event_type,
    commitSha: row.commit_sha,
    prNumber: row.pr_number ?? void 0,
    summary: row.summary ?? void 0,
    payloadRef: row.payload_ref ?? void 0,
    createdAt: row.created_at
  };
}
__name(rowToCatalogEvent, "rowToCatalogEvent");

// src/handlers/accounts.ts
async function upsertBulkNamespaceSlugs(db, slugs, now) {
  const ts = now ?? (/* @__PURE__ */ new Date()).toISOString();
  for (const { id, slug } of slugs) {
    await db.prepare(
      `INSERT INTO namespaces (namespace_id, namespace_slug, last_seen_at)
         VALUES (?1, ?2, ?3)
         ON CONFLICT(namespace_id) DO UPDATE SET
           namespace_slug = excluded.namespace_slug,
           last_seen_at = excluded.last_seen_at`
    ).bind(id, slug, ts).run();
  }
}
__name(upsertBulkNamespaceSlugs, "upsertBulkNamespaceSlugs");
async function upsertAccountRepoCache(db, accountId, repos, now) {
  const ts = now ?? (/* @__PURE__ */ new Date()).toISOString();
  for (const { id, slug } of repos) {
    await db.prepare(
      `INSERT INTO account_repo_cache (account_id, repo_id, repo_full_name, last_seen_at)
         VALUES (?1, ?2, ?3, ?4)
         ON CONFLICT(account_id, repo_id) DO UPDATE SET
           repo_full_name = excluded.repo_full_name,
           last_seen_at = excluded.last_seen_at`
    ).bind(accountId, id, slug, ts).run();
  }
}
__name(upsertAccountRepoCache, "upsertAccountRepoCache");
async function lookupRepoInAccountCache(db, accountId, repoFullName) {
  return db.prepare(
    `SELECT account_id, repo_id, repo_full_name, last_seen_at
       FROM account_repo_cache
       WHERE account_id = ?1 AND repo_full_name = ?2`
  ).bind(accountId, repoFullName).first();
}
__name(lookupRepoInAccountCache, "lookupRepoInAccountCache");
async function lookupRepoInAccountCacheByRepoId(db, accountId, repoId) {
  return db.prepare(
    `SELECT account_id, repo_id, repo_full_name, last_seen_at
       FROM account_repo_cache
       WHERE account_id = ?1 AND repo_id = ?2`
  ).bind(accountId, repoId).first();
}
__name(lookupRepoInAccountCacheByRepoId, "lookupRepoInAccountCacheByRepoId");
async function getOrCreateAccount(db, githubLogin, now, githubUserId) {
  const ts = now ?? (/* @__PURE__ */ new Date()).toISOString();
  const accountId = crypto.randomUUID();
  await db.prepare(
    `INSERT INTO accounts (account_id, github_login, created_at)
       VALUES (?1, ?2, ?3)
       ON CONFLICT(github_login) DO NOTHING`
  ).bind(accountId, githubLogin, ts).run();
  if (githubUserId) {
    await db.prepare(
      `UPDATE accounts SET github_user_id = ?1 WHERE github_login = ?2`
    ).bind(githubUserId, githubLogin).run();
  }
  const row = await db.prepare("SELECT account_id, github_login, github_user_id, created_at FROM accounts WHERE github_login = ?1").bind(githubLogin).first();
  if (!row)
    throw new OrunError("INTERNAL_ERROR", "Failed to create account");
  return row;
}
__name(getOrCreateAccount, "getOrCreateAccount");
async function getAccountByLogin(db, githubLogin) {
  return db.prepare("SELECT account_id, github_login, github_user_id, created_at FROM accounts WHERE github_login = ?1").bind(githubLogin).first();
}
__name(getAccountByLogin, "getAccountByLogin");
async function listLinkedRepos(db, accountId) {
  const result = await db.prepare(
    `SELECT n.namespace_id, n.namespace_slug, ar.linked_at
       FROM account_repos ar
       JOIN namespaces n ON n.namespace_id = ar.namespace_id
       WHERE ar.account_id = ?1
       ORDER BY ar.linked_at DESC`
  ).bind(accountId).all();
  return result.results ?? [];
}
__name(listLinkedRepos, "listLinkedRepos");
async function linkRepo(db, accountId, namespaceId, namespaceSlug, linkedBy, now, namespaceKind) {
  const ts = now ?? (/* @__PURE__ */ new Date()).toISOString();
  const kind = namespaceKind ?? "repo";
  await db.prepare(
    `INSERT INTO namespaces (namespace_id, namespace_slug, namespace_kind, last_seen_at)
       VALUES (?1, ?2, ?3, ?4)
       ON CONFLICT(namespace_id) DO UPDATE SET
         namespace_slug = excluded.namespace_slug,
         last_seen_at = excluded.last_seen_at`
  ).bind(namespaceId, namespaceSlug, kind, ts).run();
  await db.prepare(
    `INSERT INTO account_repos (account_id, namespace_id, linked_by, linked_at)
       VALUES (?1, ?2, ?3, ?4)
       ON CONFLICT(account_id, namespace_id) DO NOTHING`
  ).bind(accountId, namespaceId, linkedBy, ts).run();
  const row = await db.prepare(
    `SELECT n.namespace_id, n.namespace_slug, ar.linked_at
       FROM account_repos ar
       JOIN namespaces n ON n.namespace_id = ar.namespace_id
       WHERE ar.account_id = ?1 AND ar.namespace_id = ?2`
  ).bind(accountId, namespaceId).first();
  if (!row)
    throw new OrunError("INTERNAL_ERROR", "Failed to link repo");
  return row;
}
__name(linkRepo, "linkRepo");
async function unlinkRepo(db, accountId, namespaceId) {
  await db.prepare("DELETE FROM account_repos WHERE account_id = ?1 AND namespace_id = ?2").bind(accountId, namespaceId).run();
}
__name(unlinkRepo, "unlinkRepo");
async function resolveSessionNamespaceIds(authCtx, db) {
  const base = [...authCtx.allowedNamespaceIds];
  if (authCtx.type !== "session")
    return base;
  const account = await getAccountByLogin(db, authCtx.actor);
  if (!account)
    return base;
  const linked = await db.prepare("SELECT namespace_id FROM account_repos WHERE account_id = ?1").bind(account.account_id).all();
  const linkedIds = (linked.results ?? []).map((r) => r.namespace_id);
  const seen = new Set(base);
  for (const id of linkedIds) {
    if (!seen.has(id)) {
      base.push(id);
      seen.add(id);
    }
  }
  return base;
}
__name(resolveSessionNamespaceIds, "resolveSessionNamespaceIds");
function validateRepoFullName(repoFullName) {
  if (!repoFullName || typeof repoFullName !== "string") {
    throw new OrunError("INVALID_REQUEST", "repoFullName is required");
  }
  const parts = repoFullName.split("/");
  if (parts.length !== 2) {
    throw new OrunError("INVALID_REQUEST", "repoFullName must be owner/repo");
  }
  const [owner, repo] = parts;
  if (!owner || !repo) {
    throw new OrunError("INVALID_REQUEST", "repoFullName must have non-empty owner and repo");
  }
  if (owner.includes("..") || repo.includes("..")) {
    throw new OrunError("INVALID_REQUEST", "Invalid repoFullName");
  }
  return { owner, repo };
}
__name(validateRepoFullName, "validateRepoFullName");
async function verifyRepoAdminAccess(githubLogin, repoFullName, githubAccessToken, fetchImpl = fetch) {
  if (!githubAccessToken) {
    throw new OrunError("UNAUTHORIZED", "GitHub access token is required");
  }
  const { owner, repo } = validateRepoFullName(repoFullName);
  const encodedOwner = encodeURIComponent(owner);
  const encodedRepo = encodeURIComponent(repo);
  const repoResp = await fetchImpl(
    `https://api.github.com/repos/${encodedOwner}/${encodedRepo}`,
    {
      headers: {
        Authorization: `Bearer ${githubAccessToken}`,
        Accept: "application/vnd.github+json",
        "User-Agent": "orun-backend-account-linking"
      }
    }
  );
  if (repoResp.status === 404) {
    throw new OrunError("NOT_FOUND", "Repository not found");
  }
  if (!repoResp.ok) {
    throw new OrunError("INTERNAL_ERROR", "GitHub API error");
  }
  const repoData = await repoResp.json();
  if (repoData.permissions?.admin === true) {
    return { namespaceId: String(repoData.id), namespaceSlug: repoData.full_name };
  }
  const orgResp = await fetchImpl(
    `https://api.github.com/orgs/${encodeURIComponent(repoData.owner.login)}/memberships/${encodeURIComponent(githubLogin)}`,
    {
      headers: {
        Authorization: `Bearer ${githubAccessToken}`,
        Accept: "application/vnd.github+json",
        "User-Agent": "orun-backend-account-linking"
      }
    }
  );
  if (orgResp.ok) {
    const orgData = await orgResp.json();
    if (orgData.role === "admin") {
      return { namespaceId: String(repoData.id), namespaceSlug: repoData.full_name };
    }
  }
  throw new OrunError("FORBIDDEN", "Admin access required to link repository");
}
__name(verifyRepoAdminAccess, "verifyRepoAdminAccess");
async function handleCreateAccount(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const account = await getOrCreateAccount(rc.env.DB, rc.authCtx.actor);
  return json({
    accountId: account.account_id,
    githubLogin: account.github_login,
    createdAt: account.created_at
  });
}
__name(handleCreateAccount, "handleCreateAccount");
async function handleGetAccount(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const account = await getAccountByLogin(rc.env.DB, rc.authCtx.actor);
  if (!account) {
    throw new OrunError("NOT_FOUND", "Account not found");
  }
  return json({
    accountId: account.account_id,
    githubLogin: account.github_login,
    createdAt: account.created_at
  });
}
__name(handleGetAccount, "handleGetAccount");
async function handleLinkRepo(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const githubAccessToken = rc.request.headers.get("X-GitHub-Access-Token");
  if (!githubAccessToken) {
    throw new OrunError("UNAUTHORIZED", "GitHub access token is required");
  }
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const repoFullName = body.repoFullName;
  if (!repoFullName || typeof repoFullName !== "string") {
    throw new OrunError("INVALID_REQUEST", "repoFullName is required");
  }
  const verified = await verifyRepoAdminAccess(
    rc.authCtx.actor,
    repoFullName,
    githubAccessToken
  );
  const account = await getOrCreateAccount(rc.env.DB, rc.authCtx.actor);
  const link = await linkRepo(
    rc.env.DB,
    account.account_id,
    verified.namespaceId,
    verified.namespaceSlug,
    rc.authCtx.actor
  );
  return json({
    namespaceId: link.namespace_id,
    namespaceSlug: link.namespace_slug,
    linkedAt: link.linked_at
  });
}
__name(handleLinkRepo, "handleLinkRepo");
async function handleListLinkedRepos(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const account = await getAccountByLogin(rc.env.DB, rc.authCtx.actor);
  if (!account) {
    return json({ repos: [] });
  }
  const repos = await listLinkedRepos(rc.env.DB, account.account_id);
  return json({
    repos: repos.map((r) => ({
      namespaceId: r.namespace_id,
      namespaceSlug: r.namespace_slug,
      linkedAt: r.linked_at
    }))
  });
}
__name(handleListLinkedRepos, "handleListLinkedRepos");
async function handleUnlinkRepo(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const { namespaceId } = rc.params;
  const account = await getAccountByLogin(rc.env.DB, rc.authCtx.actor);
  if (account) {
    await unlinkRepo(rc.env.DB, account.account_id, namespaceId);
  }
  return json({ ok: true });
}
__name(handleUnlinkRepo, "handleUnlinkRepo");
function deriveLocalNamespaceId(githubUserId, repoId) {
  return `local:user:${githubUserId}:repo:${repoId}`;
}
__name(deriveLocalNamespaceId, "deriveLocalNamespaceId");
function deriveLocalNamespaceSlug(githubLogin, repoFullName) {
  return `local:${githubLogin}/${repoFullName}`;
}
__name(deriveLocalNamespaceSlug, "deriveLocalNamespaceSlug");
async function handleLinkRepoFromSession(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  if (rc.authCtx.sessionKind !== "cli") {
    throw new OrunError("FORBIDDEN", "CLI session required for this endpoint");
  }
  const githubUserId = rc.authCtx.githubUserId;
  if (!githubUserId) {
    throw new OrunError(
      "FORBIDDEN",
      "CLI session is missing GitHub user ID. Re-run `orun auth login` to obtain a refreshed token."
    );
  }
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const rawRepoFullName = body.repoFullName;
  if (!rawRepoFullName || typeof rawRepoFullName !== "string") {
    throw new OrunError("INVALID_REQUEST", "repoFullName is required");
  }
  validateRepoFullName(rawRepoFullName);
  const account = await getOrCreateAccount(rc.env.DB, rc.authCtx.actor);
  const cached = await lookupRepoInAccountCache(rc.env.DB, account.account_id, rawRepoFullName);
  if (!cached) {
    throw new OrunError(
      "NOT_FOUND",
      `Repository ${rawRepoFullName} is not in your repo cache. Re-run \`orun auth login\` to refresh your repo list, or check that your GitHub account has access to this repository.`
    );
  }
  const namespaceId = deriveLocalNamespaceId(githubUserId, cached.repo_id);
  const namespaceSlug = deriveLocalNamespaceSlug(rc.authCtx.actor, rawRepoFullName);
  const link = await linkRepo(
    rc.env.DB,
    account.account_id,
    namespaceId,
    namespaceSlug,
    rc.authCtx.actor,
    void 0,
    "local"
  );
  return json({
    namespaceKind: "local",
    namespaceId: link.namespace_id,
    namespaceSlug: link.namespace_slug,
    repoId: cached.repo_id,
    repoFullName: rawRepoFullName,
    linkedAt: link.linked_at
  });
}
__name(handleLinkRepoFromSession, "handleLinkRepoFromSession");

// src/handlers/auth.ts
var CLI_REFRESH_TTL_SECONDS = 30 * 24 * 60 * 60;
async function handleAuthGitHub(rc) {
  return buildGitHubOAuthRedirect(rc.request, rc.env);
}
__name(handleAuthGitHub, "handleAuthGitHub");
async function handleAuthGitHubCallback(rc) {
  const result = await handleGitHubOAuthCallback(rc.request, rc.env);
  if (result.namespaceSlugs && result.namespaceSlugs.length > 0) {
    await upsertBulkNamespaceSlugs(rc.env.DB, result.namespaceSlugs);
  }
  if (result.sessionKind === "cli" && result.refreshToken && result._refreshTokenHash) {
    const account = await getOrCreateAccount(rc.env.DB, result.githubLogin, void 0, result.githubUserId);
    const db = new D1Index(rc.env.DB);
    const expiresAt = result.refreshExpiresAt ?? new Date(Date.now() + CLI_REFRESH_TTL_SECONDS * 1e3).toISOString();
    await db.createCliSession({
      sessionId: crypto.randomUUID(),
      accountId: account.account_id,
      githubLogin: result.githubLogin,
      refreshTokenHash: result._refreshTokenHash,
      allowedNamespaceIds: result.allowedNamespaceIds,
      expiresAt,
      userAgent: rc.request.headers.get("User-Agent") ?? void 0
    });
    await upsertAccountRepoCache(rc.env.DB, account.account_id, result.namespaceSlugs);
  }
  if (result.returnTo) {
    const fragmentParams = {
      sessionToken: result.sessionToken,
      githubLogin: result.githubLogin,
      allowedNamespaceIds: JSON.stringify(result.allowedNamespaceIds)
    };
    if (result.sessionKind === "cli" && result.refreshToken) {
      fragmentParams.refreshToken = result.refreshToken;
      fragmentParams.refreshExpiresAt = result.refreshExpiresAt ?? "";
    }
    const fragment = new URLSearchParams(fragmentParams).toString();
    return new Response(null, {
      status: 302,
      headers: { Location: `${result.returnTo}#${fragment}` }
    });
  }
  const responseBody = {
    sessionToken: result.sessionToken,
    sessionKind: result.sessionKind,
    githubLogin: result.githubLogin,
    allowedNamespaceIds: result.allowedNamespaceIds
  };
  if (result.sessionKind === "cli" && result.refreshToken) {
    responseBody.refreshToken = result.refreshToken;
    responseBody.refreshExpiresAt = result.refreshExpiresAt;
  }
  return json(responseBody);
}
__name(handleAuthGitHubCallback, "handleAuthGitHubCallback");
async function handleCliDeviceStart(rc) {
  const deviceInfo = await startDeviceFlow(rc.env);
  return json(deviceInfo);
}
__name(handleCliDeviceStart, "handleCliDeviceStart");
async function handleCliDevicePoll(rc) {
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const deviceCode = body.deviceCode;
  if (!deviceCode || typeof deviceCode !== "string") {
    throw new OrunError("INVALID_REQUEST", "Missing deviceCode");
  }
  const pollResult = await pollDeviceFlow(deviceCode, rc.env);
  if ("status" in pollResult && pollResult.status === "pending") {
    return json(pollResult, 202);
  }
  const successResult = pollResult;
  if (successResult.namespaceSlugs && successResult.namespaceSlugs.length > 0) {
    await upsertBulkNamespaceSlugs(rc.env.DB, successResult.namespaceSlugs);
  }
  const account = await getOrCreateAccount(rc.env.DB, successResult.githubLogin, void 0, successResult.githubUserId);
  const db = new D1Index(rc.env.DB);
  const expiresAt = successResult.refreshExpiresAt;
  await db.createCliSession({
    sessionId: crypto.randomUUID(),
    accountId: account.account_id,
    githubLogin: successResult.githubLogin,
    refreshTokenHash: successResult._refreshTokenHash,
    allowedNamespaceIds: successResult.allowedNamespaceIds,
    expiresAt,
    userAgent: rc.request.headers.get("User-Agent") ?? void 0
  });
  await upsertAccountRepoCache(rc.env.DB, account.account_id, successResult.namespaceSlugs);
  return json({
    accessToken: successResult.accessToken,
    expiresAt: successResult.expiresAt,
    refreshToken: successResult.refreshToken,
    refreshExpiresAt: successResult.refreshExpiresAt,
    githubLogin: successResult.githubLogin,
    allowedNamespaceIds: successResult.allowedNamespaceIds
  });
}
__name(handleCliDevicePoll, "handleCliDevicePoll");
async function handleCliToken(rc) {
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const refreshToken = body.refreshToken;
  if (!refreshToken || typeof refreshToken !== "string") {
    throw new OrunError("INVALID_REQUEST", "Missing refreshToken");
  }
  const refreshHash = await hashRefreshToken(refreshToken);
  const db = new D1Index(rc.env.DB);
  const session = await db.getCliSessionByRefreshHash(refreshHash);
  if (!session) {
    throw new OrunError("UNAUTHORIZED", "Invalid refresh token");
  }
  const now = (/* @__PURE__ */ new Date()).toISOString();
  if (session.revokedAt) {
    throw new OrunError("UNAUTHORIZED", "Refresh token has been revoked");
  }
  if (session.expiresAt <= now) {
    throw new OrunError("UNAUTHORIZED", "Refresh token expired");
  }
  const sessionSecret = rc.env.ORUN_SESSION_SECRET;
  if (!sessionSecret) {
    throw new OrunError("INTERNAL_ERROR", "Session secret not configured");
  }
  await db.markCliSessionUsed(session.sessionId, now);
  const accountRow = await getAccountByLogin(rc.env.DB, session.githubLogin);
  const githubUserId = accountRow?.github_user_id ?? void 0;
  const accessToken = await issueSessionToken(
    {
      sub: session.githubLogin,
      allowedNamespaceIds: session.allowedNamespaceIds,
      sessionKind: "cli",
      tokenUse: "access",
      ...githubUserId ? { githubUserId } : {}
    },
    sessionSecret
  );
  const expiresAt = new Date(Date.now() + 3600 * 1e3).toISOString();
  return json({
    accessToken,
    expiresAt,
    githubLogin: session.githubLogin,
    allowedNamespaceIds: session.allowedNamespaceIds
  });
}
__name(handleCliToken, "handleCliToken");
async function handleCliLogout(rc) {
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const refreshToken = body.refreshToken;
  if (!refreshToken || typeof refreshToken !== "string") {
    throw new OrunError("INVALID_REQUEST", "Missing refreshToken");
  }
  const refreshHash = await hashRefreshToken(refreshToken);
  const db = new D1Index(rc.env.DB);
  const session = await db.getCliSessionByRefreshHash(refreshHash);
  if (session && !session.revokedAt) {
    await db.revokeCliSession(session.sessionId, (/* @__PURE__ */ new Date()).toISOString());
  }
  return json({ ok: true });
}
__name(handleCliLogout, "handleCliLogout");

// src/coordinator.ts
function getCoordinator(env, namespaceId, runId) {
  const key = coordinatorKey(namespaceId, runId);
  const id = env.COORDINATOR.idFromName(key);
  return env.COORDINATOR.get(id);
}
__name(getCoordinator, "getCoordinator");
async function coordinatorFetch(stub, path, init) {
  return stub.fetch(new Request(`https://coordinator.local${path}`, init));
}
__name(coordinatorFetch, "coordinatorFetch");

// src/handlers/runs.ts
function requireLocalNamespacePrefix(authCtx) {
  const githubUserId = authCtx.githubUserId;
  if (!githubUserId) {
    throw new OrunError(
      "FORBIDDEN",
      "CLI session is missing GitHub user ID. Re-run `orun auth login` to obtain a refreshed token."
    );
  }
  return `local:user:${githubUserId}:repo:`;
}
__name(requireLocalNamespacePrefix, "requireLocalNamespacePrefix");
async function handleCreateRun(rc) {
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const plan = body.plan;
  if (!plan || !plan.checksum || !Array.isArray(plan.jobs)) {
    throw new OrunError("INVALID_REQUEST", "Missing or invalid plan");
  }
  let runId = body.runId;
  if (runId !== void 0) {
    if (typeof runId !== "string" || runId.length === 0) {
      throw new OrunError("INVALID_REQUEST", "runId must be a non-empty string");
    }
  } else {
    runId = crypto.randomUUID();
  }
  let namespaceId;
  let namespaceSlug;
  if (rc.authCtx.type === "oidc") {
    namespaceId = rc.authCtx.namespace.namespaceId;
    namespaceSlug = rc.authCtx.namespace.namespaceSlug;
    if (body.namespaceId && body.namespaceId !== namespaceId) {
      throw new OrunError("FORBIDDEN", "Namespace mismatch");
    }
  } else if (rc.authCtx.type === "session") {
    if (rc.authCtx.sessionKind !== "cli") {
      throw new OrunError("FORBIDDEN", "Dashboard sessions may not create runs");
    }
    const localPrefix = requireLocalNamespacePrefix(rc.authCtx);
    const githubUserId = rc.authCtx.githubUserId;
    const repoFullName = body.repoFullName;
    const bodyNs = body.namespaceId;
    if (bodyNs && !bodyNs.startsWith("local:")) {
      throw new OrunError(
        "FORBIDDEN",
        "Session tokens may not create runs under canonical repo namespaces. Use an OIDC token for CI/workload runs."
      );
    }
    const account = await getOrCreateAccount(rc.env.DB, rc.authCtx.actor);
    if (repoFullName) {
      const cached = await lookupRepoInAccountCache(rc.env.DB, account.account_id, repoFullName);
      if (!cached) {
        throw new OrunError(
          "NOT_FOUND",
          `Repository ${repoFullName} is not in your repo cache. Re-run \`orun auth login\` to refresh.`
        );
      }
      namespaceId = `${localPrefix}${cached.repo_id}`;
      namespaceSlug = `local:${rc.authCtx.actor}/${repoFullName}`;
      if (bodyNs && bodyNs !== namespaceId) {
        throw new OrunError("FORBIDDEN", "Namespace mismatch");
      }
    } else if (bodyNs) {
      if (!bodyNs.startsWith(localPrefix)) {
        throw new OrunError(
          "FORBIDDEN",
          "Namespace does not belong to this CLI session. Re-run `orun auth login` and `orun cloud link`."
        );
      }
      const repoId = bodyNs.slice(localPrefix.length);
      const cached = await lookupRepoInAccountCacheByRepoId(rc.env.DB, account.account_id, repoId);
      if (!cached) {
        throw new OrunError(
          "NOT_FOUND",
          "Local namespace not found in your repo cache. Re-run `orun auth login`."
        );
      }
      namespaceId = bodyNs;
      namespaceSlug = `local:${rc.authCtx.actor}/${cached.repo_full_name}`;
    } else {
      throw new OrunError(
        "INVALID_REQUEST",
        "CLI session requires repoFullName or a local namespaceId (local:user:...) to create a run."
      );
    }
  } else {
    throw new OrunError("FORBIDDEN", "Deploy token not accepted");
  }
  const stub = getCoordinator(rc.env, namespaceId, runId);
  const initResp = await coordinatorFetch(stub, "/init", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ plan, runId, namespaceId, namespaceSlug })
  });
  const initData = await initResp.json();
  if (!initResp.ok) {
    if (initResp.status === 409) {
      throw new OrunError("CONFLICT", initData.error ?? "Run already exists with different state");
    }
    throw new OrunError("INTERNAL_ERROR", initData.error ?? "Coordinator init failed");
  }
  if (initData.alreadyExists) {
    const stateResp = await coordinatorFetch(stub, "/state");
    if (stateResp.ok) {
      const state = await stateResp.json();
      if (state.plan.checksum !== plan.checksum) {
        throw new OrunError("CONFLICT", "Run exists with different plan checksum");
      }
    } else {
      throw new OrunError("INTERNAL_ERROR", "Cannot verify existing run state");
    }
    return json({ runId, status: "running", createdAt: (/* @__PURE__ */ new Date()).toISOString() }, 200);
  }
  const now = (/* @__PURE__ */ new Date()).toISOString();
  const expiresAt = new Date(Date.now() + 24 * 60 * 60 * 1e3).toISOString();
  const triggerType = body.triggerType ?? "ci";
  const actor = body.actor ?? rc.authCtx.actor;
  const dryRun = Boolean(body.dryRun);
  const db = new D1Index(rc.env.DB);
  const r2 = new R2Storage(rc.env.STORAGE);
  const mirrorPromise = (async () => {
    await db.createRun({
      runId,
      namespace: { namespaceId, namespaceSlug },
      status: "running",
      planChecksum: plan.checksum,
      triggerType,
      actor,
      createdAt: now,
      updatedAt: now,
      finishedAt: null,
      jobTotal: plan.jobs.length,
      jobDone: 0,
      jobFailed: 0,
      dryRun,
      expiresAt
    });
    for (const pj of plan.jobs) {
      await db.upsertJob({
        jobId: pj.jobId,
        runId,
        namespaceId,
        component: pj.component,
        status: "pending",
        runnerId: null,
        startedAt: null,
        finishedAt: null,
        logRef: null
      });
    }
    await r2.savePlan(namespaceId, plan);
  })();
  rc.ctx.waitUntil(mirrorPromise);
  return json({ runId, status: "running", createdAt: now }, 201);
}
__name(handleCreateRun, "handleCreateRun");
async function handleListRuns(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const url = new URL(rc.request.url);
  const limit = Math.min(Math.max(parseInt(url.searchParams.get("limit") ?? "50", 10) || 50, 1), 100);
  const offset = Math.max(parseInt(url.searchParams.get("offset") ?? "0", 10) || 0, 0);
  const resolved = await resolveSessionNamespaceIds(rc.authCtx, rc.env.DB);
  const db = new D1Index(rc.env.DB);
  const runs = await db.listRuns(resolved, limit, offset);
  return json({ runs });
}
__name(handleListRuns, "handleListRuns");
async function handleGetRun(rc) {
  const { runId } = rc.params;
  if (rc.authCtx.type === "oidc") {
    const namespaceId = rc.authCtx.namespace.namespaceId;
    const stub = getCoordinator(rc.env, namespaceId, runId);
    const stateResp = await coordinatorFetch(stub, "/state");
    if (stateResp.ok) {
      const state = await stateResp.json();
      return json({ run: coordinatorStateToRun(state) });
    }
    const db = new D1Index(rc.env.DB);
    const run = await db.getRun(namespaceId, runId);
    if (run)
      return json({ run });
    throw new OrunError("NOT_FOUND", "Run not found");
  }
  if (rc.authCtx.type === "session") {
    const resolved = await resolveSessionNamespaceIds(rc.authCtx, rc.env.DB);
    const db = new D1Index(rc.env.DB);
    for (const nsId of resolved) {
      const run = await db.getRun(nsId, runId);
      if (run)
        return json({ run });
    }
    throw new OrunError("NOT_FOUND", "Run not found");
  }
  throw new OrunError("FORBIDDEN", "Access denied");
}
__name(handleGetRun, "handleGetRun");
function coordinatorStateToRun(state) {
  const jobs = Object.values(state.jobs);
  return {
    runId: state.runId,
    namespace: { namespaceId: state.namespaceId, namespaceSlug: "" },
    status: state.status === "cancelled" ? "cancelled" : state.status,
    planChecksum: state.plan.checksum,
    createdAt: state.createdAt,
    updatedAt: state.updatedAt,
    jobTotal: jobs.length,
    jobDone: jobs.filter((j) => j.status === "success").length,
    jobFailed: jobs.filter((j) => j.status === "failed").length
  };
}
__name(coordinatorStateToRun, "coordinatorStateToRun");

// src/handlers/jobs.ts
function assertMutableAccess(authCtx) {
  if (authCtx.type === "oidc")
    return;
  if (authCtx.type === "session" && authCtx.sessionKind === "cli")
    return;
  throw new OrunError("FORBIDDEN", "Dashboard sessions may not use mutable coordination routes");
}
__name(assertMutableAccess, "assertMutableAccess");
async function resolveNamespaceForMutableAccess(authCtx, env, runId) {
  if (authCtx.type === "oidc") {
    return authCtx.namespace.namespaceId;
  }
  if (authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Access denied");
  }
  const githubUserId = authCtx.githubUserId;
  if (!githubUserId) {
    throw new OrunError(
      "FORBIDDEN",
      "CLI session is missing GitHub user ID. Re-run `orun auth login` to obtain a refreshed token."
    );
  }
  const localPrefix = `local:user:${githubUserId}:repo:`;
  const db = new D1Index(env.DB);
  const resolved = await resolveSessionNamespaceIds(authCtx, env.DB);
  const localNamespaces = resolved.filter((id) => id.startsWith(localPrefix));
  for (const nsId of localNamespaces) {
    const run = await db.getRun(nsId, runId);
    if (run)
      return nsId;
  }
  throw new OrunError("NOT_FOUND", "Run not found or access denied");
}
__name(resolveNamespaceForMutableAccess, "resolveNamespaceForMutableAccess");
async function handleClaimJob(rc) {
  assertMutableAccess(rc.authCtx);
  const { runId, jobId } = rc.params;
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const runnerId = body.runnerId;
  if (!runnerId || typeof runnerId !== "string") {
    throw new OrunError("INVALID_REQUEST", "Missing runnerId");
  }
  const namespaceId = await resolveNamespaceForMutableAccess(rc.authCtx, rc.env, runId);
  const stub = getCoordinator(rc.env, namespaceId, runId);
  const resp = await coordinatorFetch(stub, `/jobs/${encodeURIComponent(jobId)}/claim`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ runnerId })
  });
  const data = await resp.json();
  return json(data, resp.status);
}
__name(handleClaimJob, "handleClaimJob");
async function handleUpdateJob(rc) {
  assertMutableAccess(rc.authCtx);
  const { runId, jobId } = rc.params;
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const runnerId = body.runnerId;
  const status = body.status;
  if (!runnerId || typeof runnerId !== "string") {
    throw new OrunError("INVALID_REQUEST", "Missing runnerId");
  }
  if (status !== "success" && status !== "failed") {
    throw new OrunError("INVALID_REQUEST", "status must be 'success' or 'failed'");
  }
  const db = new D1Index(rc.env.DB);
  const namespaceId = await resolveNamespaceForMutableAccess(rc.authCtx, rc.env, runId);
  const updateBody = { runnerId, status, error: body.error };
  const stub = getCoordinator(rc.env, namespaceId, runId);
  const resp = await coordinatorFetch(stub, `/jobs/${encodeURIComponent(jobId)}/update`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(updateBody)
  });
  const data = await resp.json();
  if (!resp.ok) {
    return json(data, resp.status);
  }
  rc.ctx.waitUntil((async () => {
    const stateResp = await coordinatorFetch(stub, "/state");
    if (!stateResp.ok)
      return;
    const state = await stateResp.json();
    const jobs = Object.values(state.jobs);
    const jobDone = jobs.filter((j) => j.status === "success").length;
    const jobFailed = jobs.filter((j) => j.status === "failed").length;
    const finishedAt = state.status === "completed" || state.status === "failed" ? state.updatedAt : null;
    await db.updateRun(namespaceId, runId, {
      status: state.status === "cancelled" ? "cancelled" : state.status,
      jobDone,
      jobFailed,
      finishedAt,
      updatedAt: state.updatedAt
    });
    const jobState = state.jobs[jobId];
    if (jobState) {
      const existingRow = await rc.env.DB.prepare("SELECT log_ref FROM jobs WHERE namespace_id = ?1 AND run_id = ?2 AND job_id = ?3").bind(namespaceId, runId, jobId).first();
      await db.upsertJob({
        jobId,
        runId,
        namespaceId,
        component: jobState.component,
        status: jobState.status,
        runnerId: jobState.runnerId,
        startedAt: jobState.startedAt,
        finishedAt: jobState.finishedAt,
        logRef: existingRow?.log_ref ?? null
      });
    }
  })());
  return json(data, 200);
}
__name(handleUpdateJob, "handleUpdateJob");
async function handleHeartbeat(rc) {
  assertMutableAccess(rc.authCtx);
  const { runId, jobId } = rc.params;
  let body;
  try {
    body = await rc.request.json();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  const runnerId = body.runnerId;
  if (!runnerId || typeof runnerId !== "string") {
    throw new OrunError("INVALID_REQUEST", "Missing runnerId");
  }
  const namespaceId = await resolveNamespaceForMutableAccess(rc.authCtx, rc.env, runId);
  const stub = getCoordinator(rc.env, namespaceId, runId);
  const resp = await coordinatorFetch(stub, `/jobs/${encodeURIComponent(jobId)}/heartbeat`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ runnerId })
  });
  const data = await resp.json();
  return json(data, resp.status);
}
__name(handleHeartbeat, "handleHeartbeat");
async function handleRunnable(rc) {
  assertMutableAccess(rc.authCtx);
  const { runId } = rc.params;
  const namespaceId = await resolveNamespaceForMutableAccess(rc.authCtx, rc.env, runId);
  const stub = getCoordinator(rc.env, namespaceId, runId);
  const resp = await coordinatorFetch(stub, "/runnable");
  const data = await resp.json();
  return json(data, resp.status);
}
__name(handleRunnable, "handleRunnable");
async function handleListJobs(rc) {
  const { runId } = rc.params;
  if (rc.authCtx.type === "oidc") {
    const namespaceId = rc.authCtx.namespace.namespaceId;
    const stub = getCoordinator(rc.env, namespaceId, runId);
    const stateResp = await coordinatorFetch(stub, "/state");
    if (stateResp.ok) {
      const state = await stateResp.json();
      const jobs2 = Object.values(state.jobs).map(coordinatorJobToPublic);
      return json({ jobs: jobs2 });
    }
    const db = new D1Index(rc.env.DB);
    const jobs = await db.listJobs(namespaceId, runId);
    return json({ jobs });
  }
  if (rc.authCtx.type === "session") {
    const resolved = await resolveSessionNamespaceIds(rc.authCtx, rc.env.DB);
    const db = new D1Index(rc.env.DB);
    for (const nsId of resolved) {
      const run = await db.getRun(nsId, runId);
      if (run) {
        const jobs = await db.listJobs(nsId, runId);
        return json({ jobs });
      }
    }
    throw new OrunError("NOT_FOUND", "Run not found");
  }
  throw new OrunError("FORBIDDEN", "Access denied");
}
__name(handleListJobs, "handleListJobs");
async function handleJobStatus(rc) {
  const { runId, jobId } = rc.params;
  if (rc.authCtx.type === "oidc") {
    const namespaceId = rc.authCtx.namespace.namespaceId;
    const stub = getCoordinator(rc.env, namespaceId, runId);
    const resp = await coordinatorFetch(stub, `/jobs/${encodeURIComponent(jobId)}/status`);
    if (resp.ok) {
      const data = await resp.json();
      return json(data);
    }
    throw new OrunError("NOT_FOUND", "Job not found");
  }
  if (rc.authCtx.type === "session") {
    const resolved = await resolveSessionNamespaceIds(rc.authCtx, rc.env.DB);
    const db = new D1Index(rc.env.DB);
    for (const nsId of resolved) {
      const run = await db.getRun(nsId, runId);
      if (run) {
        const jobs = await db.listJobs(nsId, runId);
        const job = jobs.find((j) => j.jobId === jobId);
        if (job)
          return json(job);
        throw new OrunError("NOT_FOUND", "Job not found");
      }
    }
    throw new OrunError("NOT_FOUND", "Run not found");
  }
  throw new OrunError("FORBIDDEN", "Access denied");
}
__name(handleJobStatus, "handleJobStatus");
function coordinatorJobToPublic(j) {
  return {
    jobId: j.jobId,
    component: j.component,
    status: j.status,
    deps: j.deps,
    runnerId: j.runnerId,
    startedAt: j.startedAt,
    finishedAt: j.finishedAt,
    lastError: j.lastError,
    heartbeatAt: j.heartbeatAt,
    logRef: null
  };
}
__name(coordinatorJobToPublic, "coordinatorJobToPublic");

// src/handlers/logs.ts
function assertMutableAccess2(authCtx) {
  if (authCtx.type === "oidc")
    return;
  if (authCtx.type === "session" && authCtx.sessionKind === "cli")
    return;
  throw new OrunError("FORBIDDEN", "Dashboard sessions may not use mutable coordination routes");
}
__name(assertMutableAccess2, "assertMutableAccess");
async function resolveNamespaceForMutableAccess2(authCtx, env, runId) {
  if (authCtx.type === "oidc") {
    return authCtx.namespace.namespaceId;
  }
  if (authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Access denied");
  }
  const githubUserId = authCtx.githubUserId;
  if (!githubUserId) {
    throw new OrunError(
      "FORBIDDEN",
      "CLI session is missing GitHub user ID. Re-run `orun auth login` to obtain a refreshed token."
    );
  }
  const localPrefix = `local:user:${githubUserId}:repo:`;
  const db = new D1Index(env.DB);
  const resolved = await resolveSessionNamespaceIds(authCtx, env.DB);
  const localNamespaces = resolved.filter((id) => id.startsWith(localPrefix));
  for (const nsId of localNamespaces) {
    const run = await db.getRun(nsId, runId);
    if (run)
      return nsId;
  }
  throw new OrunError("NOT_FOUND", "Run not found or access denied");
}
__name(resolveNamespaceForMutableAccess2, "resolveNamespaceForMutableAccess");
async function handleUploadLog(rc) {
  assertMutableAccess2(rc.authCtx);
  const { runId, jobId } = rc.params;
  const namespaceId = await resolveNamespaceForMutableAccess2(rc.authCtx, rc.env, runId);
  const content = rc.request.body ?? "";
  const r2 = new R2Storage(rc.env.STORAGE);
  const db = new D1Index(rc.env.DB);
  const run = await db.getRun(namespaceId, runId);
  const expiresAt = run?.expiresAt ?? new Date(Date.now() + 24 * 60 * 60 * 1e3).toISOString();
  const logRef = await r2.writeLog(namespaceId, runId, jobId, content, { expiresAt });
  rc.ctx.waitUntil((async () => {
    const updated = await rc.env.DB.prepare("UPDATE jobs SET log_ref = ?1 WHERE namespace_id = ?2 AND run_id = ?3 AND job_id = ?4").bind(logRef, namespaceId, runId, jobId).run();
    if ((updated.meta?.changes ?? 0) === 0) {
      await db.upsertJob({
        jobId,
        runId,
        namespaceId,
        component: "",
        status: "pending",
        runnerId: null,
        startedAt: null,
        finishedAt: null,
        logRef
      });
    }
  })());
  return json({ ok: true, logRef });
}
__name(handleUploadLog, "handleUploadLog");
async function handleGetLog(rc) {
  const { runId, jobId } = rc.params;
  let namespaceId;
  if (rc.authCtx.type === "oidc") {
    namespaceId = rc.authCtx.namespace.namespaceId;
  } else if (rc.authCtx.type === "session") {
    const resolved = await resolveSessionNamespaceIds(rc.authCtx, rc.env.DB);
    const db = new D1Index(rc.env.DB);
    let found = false;
    namespaceId = "";
    for (const nsId of resolved) {
      const run = await db.getRun(nsId, runId);
      if (run) {
        namespaceId = nsId;
        found = true;
        break;
      }
    }
    if (!found)
      throw new OrunError("NOT_FOUND", "Run not found");
  } else {
    throw new OrunError("FORBIDDEN", "Access denied");
  }
  const r2 = new R2Storage(rc.env.STORAGE);
  const obj = await r2.readLog(namespaceId, runId, jobId);
  if (!obj) {
    throw new OrunError("NOT_FOUND", "Log not found");
  }
  return new Response(obj.body, {
    status: 200,
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      ...corsHeaders()
    }
  });
}
__name(handleGetLog, "handleGetLog");

// src/handlers/catalog.ts
var SUPPORTED_SCHEMA_VERSION = "1";
var MAX_BODY_BYTES = 1 * 1024 * 1024;
async function deriveRelationId(parts) {
  const data = new TextEncoder().encode(parts.map((p) => p ?? "").join(""));
  const hash = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(hash)).map((b) => b.toString(16).padStart(2, "0")).join("").slice(0, 32);
}
__name(deriveRelationId, "deriveRelationId");
function validateComponentPath(path) {
  if (!path || path.trim() === "") {
    throw new OrunError("INVALID_REQUEST", `Component path must not be empty`);
  }
  if (path.startsWith("/")) {
    throw new OrunError("INVALID_REQUEST", `Component path must be relative, got: ${path}`);
  }
  const segments = path.split("/");
  if (segments.some((s) => s === "..")) {
    throw new OrunError("INVALID_REQUEST", `Component path must not contain '..' traversal: ${path}`);
  }
}
__name(validateComponentPath, "validateComponentPath");
function deriveLatestStatus(environments) {
  if (!environments || environments.length === 0)
    return "unknown";
  const statuses = environments.map((e) => e.status ?? "unknown");
  if (statuses.some((s) => s === "failing"))
    return "failing";
  if (statuses.every((s) => s === "healthy"))
    return "healthy";
  if (statuses.some((s) => s === "stale"))
    return "stale";
  return "unknown";
}
__name(deriveLatestStatus, "deriveLatestStatus");
async function resolveVisibleCatalogNamespaceIds(authCtx, db) {
  const account = await getAccountByLogin(db, authCtx.actor);
  if (!account)
    return [];
  const result = await db.prepare(
    `SELECT n.namespace_id FROM account_repos ar JOIN namespaces n ON n.namespace_id = ar.namespace_id WHERE ar.account_id = ?1 AND (n.namespace_kind IS NULL OR n.namespace_kind = 'repo')`
  ).bind(account.account_id).all();
  return (result.results ?? []).map((r) => r.namespace_id);
}
__name(resolveVisibleCatalogNamespaceIds, "resolveVisibleCatalogNamespaceIds");
async function handleCatalogSync(rc) {
  if (rc.authCtx.type !== "oidc") {
    throw new OrunError("FORBIDDEN", "Catalog sync requires GitHub Actions OIDC authentication");
  }
  const contentLength = rc.request.headers.get("content-length");
  if (contentLength !== null && parseInt(contentLength, 10) > MAX_BODY_BYTES) {
    throw new OrunError("INVALID_REQUEST", "Request body exceeds 1 MiB limit");
  }
  let rawBody;
  try {
    rawBody = await rc.request.text();
  } catch {
    throw new OrunError("INVALID_REQUEST", "Failed to read request body");
  }
  if (rawBody.length > MAX_BODY_BYTES) {
    throw new OrunError("INVALID_REQUEST", "Request body exceeds 1 MiB limit");
  }
  let envelope;
  try {
    envelope = JSON.parse(rawBody);
  } catch {
    throw new OrunError("INVALID_REQUEST", "Invalid JSON body");
  }
  if (!envelope.uploadId || typeof envelope.uploadId !== "string" || envelope.uploadId.trim() === "") {
    throw new OrunError("INVALID_REQUEST", "uploadId is required");
  }
  if (!envelope.schemaVersion) {
    throw new OrunError("INVALID_REQUEST", "schemaVersion is required");
  }
  if (envelope.schemaVersion !== SUPPORTED_SCHEMA_VERSION) {
    throw new OrunError("INVALID_REQUEST", `Unsupported schemaVersion: ${envelope.schemaVersion}`);
  }
  if (!envelope.source?.repoId || typeof envelope.source.repoId !== "string") {
    throw new OrunError("INVALID_REQUEST", "source.repoId is required");
  }
  if (!envelope.source?.repo || typeof envelope.source.repo !== "string") {
    throw new OrunError("INVALID_REQUEST", "source.repo is required");
  }
  if (!envelope.source?.commit || typeof envelope.source.commit !== "string") {
    throw new OrunError("INVALID_REQUEST", "source.commit is required");
  }
  const oidcRepoId = rc.authCtx.namespace.namespaceId;
  const oidcRepo = rc.authCtx.namespace.namespaceSlug;
  if (envelope.source.repoId !== oidcRepoId) {
    throw new OrunError(
      "FORBIDDEN",
      `OIDC repository_id (${oidcRepoId}) does not match envelope source.repoId (${envelope.source.repoId})`
    );
  }
  if (envelope.source.repo !== oidcRepo) {
    throw new OrunError(
      "FORBIDDEN",
      `OIDC repository (${oidcRepo}) does not match envelope source.repo (${envelope.source.repo})`
    );
  }
  if (!Array.isArray(envelope.components)) {
    throw new OrunError("INVALID_REQUEST", "components must be an array");
  }
  for (const cs of envelope.components) {
    if (typeof cs.component?.path !== "string") {
      throw new OrunError("INVALID_REQUEST", "component.path is required and must be a string");
    }
    validateComponentPath(cs.component.path);
    if (!cs.component?.id || typeof cs.component.id !== "string") {
      throw new OrunError("INVALID_REQUEST", "component.id is required for each component");
    }
    if (!cs.component?.name || typeof cs.component.name !== "string") {
      throw new OrunError("INVALID_REQUEST", "component.name is required for each component");
    }
  }
  const namespaceId = oidcRepoId;
  const db = new D1Index(rc.env.DB);
  const r2 = new R2Storage(rc.env.STORAGE);
  const alreadyExists = await db.uploadExists(envelope.uploadId);
  if (alreadyExists) {
    const existing = await db.recordCatalogUpload({
      uploadId: envelope.uploadId,
      namespaceId,
      repoId: envelope.source.repoId,
      repoFullName: envelope.source.repo,
      commitSha: envelope.source.commit,
      envelopeRef: "",
      componentCount: 0,
      createdAt: (/* @__PURE__ */ new Date()).toISOString()
    });
    return json(existing, 202);
  }
  const normalizePromise = /* @__PURE__ */ __name(async () => {
    await db.upsertNamespace({
      namespaceId,
      namespaceSlug: envelope.source.repo,
      kind: "repo"
    });
    const envelopeRef = await r2.writeCatalogEnvelope(namespaceId, envelope.uploadId, envelope);
    const now = (/* @__PURE__ */ new Date()).toISOString();
    const componentCount = envelope.components?.length ?? 0;
    const uploadInput = {
      uploadId: envelope.uploadId,
      namespaceId,
      repoId: envelope.source.repoId,
      repoFullName: envelope.source.repo,
      commitSha: envelope.source.commit,
      branch: envelope.source.branch,
      workflowRunId: envelope.source.workflowRunId,
      workflowRef: envelope.source.workflowRef,
      prNumber: envelope.source.prNumber,
      envelopeRef,
      componentCount,
      createdAt: now
    };
    await db.recordCatalogUpload(uploadInput);
    for (const cs of envelope.components ?? []) {
      const stateRef = await r2.writeCatalogComponentState(
        namespaceId,
        envelope.source.commit,
        cs.component.name,
        cs
      );
      const existingRow = await db.getCatalogComponentRow(cs.component.id);
      const latestStatus = deriveLatestStatus(cs.environments);
      const upsert = {
        componentId: cs.component.id,
        namespaceId,
        repoId: envelope.source.repoId,
        repoFullName: envelope.source.repo,
        name: cs.component.name,
        title: cs.component.title,
        description: cs.component.description,
        type: cs.component.type,
        owner: cs.component.owner,
        system: cs.component.system,
        lifecycle: cs.component.lifecycle,
        repoPath: cs.component.path,
        tags: cs.component.tags ?? [],
        environments: cs.environments ?? [],
        latestPlanId: cs.plan?.planId,
        latestPlanChecksum: cs.plan?.checksum,
        latestCommitSha: envelope.source.commit,
        latestStatus,
        currentStateRef: stateRef,
        firstSeenAt: existingRow ? existingRow.first_seen_at : now,
        lastSeenAt: now
      };
      await db.upsertCatalogComponent(upsert);
      const relations = [];
      for (const rel of cs.relations ?? []) {
        const relationId = await deriveRelationId([
          cs.component.id,
          rel.relationType,
          rel.targetKind,
          rel.targetRef,
          rel.environment ?? null,
          rel.jobId ?? null
        ]);
        relations.push({
          relationId,
          sourceComponentId: cs.component.id,
          relationType: rel.relationType,
          targetKind: rel.targetKind,
          targetRef: rel.targetRef,
          environment: rel.environment,
          jobId: rel.jobId,
          lastSeenAt: now
        });
      }
      await db.replaceCatalogRelations(cs.component.id, relations);
      let eventType = "synced";
      if (!existingRow) {
        eventType = "created";
      } else if (existingRow.latest_commit_sha !== envelope.source.commit || existingRow.owner !== (cs.component.owner ?? null) || existingRow.type !== cs.component.type || existingRow.system !== (cs.component.system ?? null) || existingRow.lifecycle !== (cs.component.lifecycle ?? null)) {
        eventType = "updated";
      }
      if (cs.source?.prNumber !== void 0) {
        eventType = "pr_changed";
      }
      const eventId = await deriveRelationId([
        cs.component.id,
        envelope.uploadId,
        eventType,
        envelope.source.commit
      ]);
      const eventInput = {
        eventId,
        componentId: cs.component.id,
        namespaceId,
        uploadId: envelope.uploadId,
        eventType,
        commitSha: envelope.source.commit,
        prNumber: cs.source?.prNumber ?? envelope.source.prNumber,
        createdAt: now
      };
      await db.appendCatalogComponentEvent(eventInput);
    }
  }, "normalizePromise");
  rc.ctx.waitUntil(normalizePromise());
  return json(
    {
      uploadId: envelope.uploadId,
      acceptedAt: (/* @__PURE__ */ new Date()).toISOString(),
      componentCount: envelope.components?.length ?? 0
    },
    202
  );
}
__name(handleCatalogSync, "handleCatalogSync");
async function handleListCatalogComponents(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const visibleNamespaceIds = await resolveVisibleCatalogNamespaceIds(rc.authCtx, rc.env.DB);
  const url = new URL(rc.request.url);
  const sp = url.searchParams;
  const filter = {
    visibleNamespaceIds,
    q: sp.get("q") ?? void 0,
    repoId: sp.get("repoId") ?? void 0,
    type: sp.get("type") ?? void 0,
    owner: sp.get("owner") ?? void 0,
    system: sp.get("system") ?? void 0,
    tag: sp.get("tag") ?? void 0,
    status: sp.get("status") ?? void 0,
    limit: sp.has("limit") ? Math.min(parseInt(sp.get("limit"), 10) || 50, 100) : 50,
    offset: sp.has("offset") ? Math.max(parseInt(sp.get("offset"), 10) || 0, 0) : 0
  };
  const db = new D1Index(rc.env.DB);
  const result = await db.listCatalogComponents(filter);
  return json(result);
}
__name(handleListCatalogComponents, "handleListCatalogComponents");
async function handleGetCatalogComponent(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const { componentId } = rc.params;
  const visibleNamespaceIds = await resolveVisibleCatalogNamespaceIds(rc.authCtx, rc.env.DB);
  const db = new D1Index(rc.env.DB);
  const component = await db.getCatalogComponent(visibleNamespaceIds, componentId);
  if (!component) {
    throw new OrunError("NOT_FOUND", "Component not found");
  }
  return json({ component });
}
__name(handleGetCatalogComponent, "handleGetCatalogComponent");
async function handleGetCatalogComponentHistory(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const { componentId } = rc.params;
  const visibleNamespaceIds = await resolveVisibleCatalogNamespaceIds(rc.authCtx, rc.env.DB);
  const db = new D1Index(rc.env.DB);
  const events = await db.listCatalogComponentEvents(visibleNamespaceIds, componentId);
  return json({ events });
}
__name(handleGetCatalogComponentHistory, "handleGetCatalogComponentHistory");
async function handleGetCatalogComponentRuns(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const { componentId } = rc.params;
  const visibleNamespaceIds = await resolveVisibleCatalogNamespaceIds(rc.authCtx, rc.env.DB);
  const db = new D1Index(rc.env.DB);
  const component = await db.getCatalogComponent(visibleNamespaceIds, componentId);
  if (!component) {
    throw new OrunError("NOT_FOUND", "Component not found");
  }
  const runs = await db.listCatalogComponentRecentRuns(visibleNamespaceIds, component.name);
  return json({ runs });
}
__name(handleGetCatalogComponentRuns, "handleGetCatalogComponentRuns");
async function handleGetCatalogComponentDependencies(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const { componentId } = rc.params;
  const visibleNamespaceIds = await resolveVisibleCatalogNamespaceIds(rc.authCtx, rc.env.DB);
  const db = new D1Index(rc.env.DB);
  const relations = await db.listCatalogComponentRelations(visibleNamespaceIds, componentId);
  return json(relations);
}
__name(handleGetCatalogComponentDependencies, "handleGetCatalogComponentDependencies");
async function handleListRepoComponents(rc) {
  if (rc.authCtx.type !== "session") {
    throw new OrunError("FORBIDDEN", "Session authentication required");
  }
  const { repoId } = rc.params;
  const visibleNamespaceIds = await resolveVisibleCatalogNamespaceIds(rc.authCtx, rc.env.DB);
  const url = new URL(rc.request.url);
  const sp = url.searchParams;
  const filter = {
    visibleNamespaceIds,
    repoId,
    q: sp.get("q") ?? void 0,
    type: sp.get("type") ?? void 0,
    owner: sp.get("owner") ?? void 0,
    system: sp.get("system") ?? void 0,
    tag: sp.get("tag") ?? void 0,
    status: sp.get("status") ?? void 0,
    limit: sp.has("limit") ? Math.min(parseInt(sp.get("limit"), 10) || 50, 100) : 50,
    offset: sp.has("offset") ? Math.max(parseInt(sp.get("offset"), 10) || 0, 0) : 0
  };
  const db = new D1Index(rc.env.DB);
  const result = await db.listCatalogComponents(filter);
  return json(result);
}
__name(handleListRepoComponents, "handleListRepoComponents");

// src/router.ts
function route(method, path, handler, auth, rateLimit = true) {
  const paramNames = [];
  const regexStr = path.replace(/:([a-zA-Z]+)/g, (_, name) => {
    paramNames.push(name);
    return "([^/]+)";
  });
  return { method, pattern: new RegExp(`^${regexStr}$`), paramNames, handler, auth, rateLimit };
}
__name(route, "route");
var routes = [
  route("GET", "/v1/auth/github", handleAuthGitHub, "none", false),
  route("GET", "/v1/auth/github/callback", handleAuthGitHubCallback, "none", false),
  route("POST", "/v1/auth/cli/device/start", handleCliDeviceStart, "none", false),
  route("POST", "/v1/auth/cli/device/poll", handleCliDevicePoll, "none", false),
  route("POST", "/v1/auth/cli/token", handleCliToken, "none", false),
  route("POST", "/v1/auth/cli/logout", handleCliLogout, "none", false),
  route("POST", "/v1/accounts", handleCreateAccount, "session", true),
  route("GET", "/v1/accounts/me", handleGetAccount, "session", true),
  route("POST", "/v1/accounts/repos/link", handleLinkRepoFromSession, "session", true),
  route("POST", "/v1/accounts/repos", handleLinkRepo, "session", true),
  route("GET", "/v1/accounts/repos", handleListLinkedRepos, "session", true),
  route("DELETE", "/v1/accounts/repos/:namespaceId", handleUnlinkRepo, "session", true),
  route("POST", "/v1/runs", handleCreateRun, "oidc_or_session", true),
  route("GET", "/v1/runs", handleListRuns, "session", true),
  route("GET", "/v1/runs/:runId", handleGetRun, "oidc_or_session", true),
  route("GET", "/v1/runs/:runId/jobs", handleListJobs, "oidc_or_session", true),
  route("GET", "/v1/runs/:runId/jobs/:jobId/status", handleJobStatus, "oidc_or_session", true),
  route("GET", "/v1/runs/:runId/runnable", handleRunnable, "oidc_or_session", true),
  route("POST", "/v1/runs/:runId/jobs/:jobId/claim", handleClaimJob, "oidc_or_session", true),
  route("POST", "/v1/runs/:runId/jobs/:jobId/update", handleUpdateJob, "oidc_or_session", true),
  route("POST", "/v1/runs/:runId/jobs/:jobId/heartbeat", handleHeartbeat, "oidc_or_session", true),
  route("POST", "/v1/runs/:runId/logs/:jobId", handleUploadLog, "oidc_or_session", true),
  route("GET", "/v1/runs/:runId/logs/:jobId", handleGetLog, "oidc_or_session", true),
  route("POST", "/v1/catalog/sync", handleCatalogSync, "oidc", true),
  route("GET", "/v1/catalog/components", handleListCatalogComponents, "session", true),
  route("GET", "/v1/catalog/components/:componentId", handleGetCatalogComponent, "session", true),
  route("GET", "/v1/catalog/components/:componentId/history", handleGetCatalogComponentHistory, "session", true),
  route("GET", "/v1/catalog/components/:componentId/runs", handleGetCatalogComponentRuns, "session", true),
  route("GET", "/v1/catalog/components/:componentId/dependencies", handleGetCatalogComponentDependencies, "session", true),
  route("GET", "/v1/repos/:repoId/components", handleListRepoComponents, "session", true)
];
async function routeRequest(request, env, ctx) {
  try {
    return await routeRequestInner(request, env, ctx);
  } catch (err) {
    return handleError(err);
  }
}
__name(routeRequest, "routeRequest");
async function routeRequestInner(request, env, ctx) {
  const url = new URL(request.url);
  const path = url.pathname;
  const method = request.method;
  if (method === "OPTIONS") {
    return handleOptions();
  }
  if (path === "/" && method === "GET") {
    return json({ status: "ok", service: "orun-api" });
  }
  for (const r of routes) {
    if (r.method !== method)
      continue;
    const match = r.pattern.exec(path);
    if (!match)
      continue;
    const params = {};
    for (let i = 0; i < r.paramNames.length; i++) {
      params[r.paramNames[i]] = decodeURIComponent(match[i + 1]);
    }
    let authCtx;
    if (r.auth === "none") {
      authCtx = { type: "deploy", namespace: null, allowedNamespaceIds: ["*"], actor: "system" };
    } else {
      authCtx = await authenticate(request, env, ctx);
      if (r.auth === "oidc" && authCtx.type !== "oidc") {
        throw new OrunError("FORBIDDEN", "OIDC authentication required");
      }
      if (r.auth === "session" && authCtx.type !== "session") {
        throw new OrunError("FORBIDDEN", "Session authentication required");
      }
      if (r.auth === "oidc_or_session" && authCtx.type === "deploy") {
        throw new OrunError("FORBIDDEN", "Deploy token not accepted for this endpoint");
      }
    }
    if (r.rateLimit && authCtx.type !== "deploy") {
      const namespaceId = authCtx.type === "oidc" ? authCtx.namespace.namespaceId : authCtx.allowedNamespaceIds[0] ?? authCtx.actor;
      const limitResp = await checkRateLimit(env, namespaceId);
      if (limitResp)
        return limitResp;
    }
    return r.handler({ request, env, ctx, params, authCtx });
  }
  const knownPath = routes.some((r) => r.pattern.test(path));
  if (knownPath) {
    return errorJson("INVALID_REQUEST", "Method not allowed", 405);
  }
  return errorJson("NOT_FOUND", "Not found", 404);
}
__name(routeRequestInner, "routeRequestInner");

// src/scheduled.ts
async function handleScheduled(env, ctx) {
  const now = (/* @__PURE__ */ new Date()).toISOString();
  const db = new D1Index(env.DB);
  const r2 = new R2Storage(env.STORAGE);
  const result = await env.DB.prepare("SELECT namespace_id, run_id FROM runs WHERE expires_at <= ?1").bind(now).all();
  const expiredRuns = result.results ?? [];
  const cleanupPromises = expiredRuns.map(async (row) => {
    try {
      const stub = getCoordinator(env, row.namespace_id, row.run_id);
      await coordinatorFetch(stub, "/cancel", { method: "POST" });
    } catch {
    }
    try {
      await r2.deleteRun(row.namespace_id, row.run_id);
    } catch {
    }
  });
  ctx.waitUntil(Promise.all(cleanupPromises));
  await db.deleteExpiredRuns(now);
}
__name(handleScheduled, "handleScheduled");

// src/index.ts
var src_default = {
  async fetch(request, env, ctx) {
    return routeRequest(request, env, ctx);
  },
  async scheduled(_event, env, ctx) {
    await handleScheduled(env, ctx);
  }
};
export {
  RateLimitCounter,
  RunCoordinator,
  src_default as default
};
//# sourceMappingURL=index.js.map
