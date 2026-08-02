// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

/** Client for the kive hook runtime API (KV store, demands, semaphores). */

const RUNTIME_API_DEFAULT_PORT = "8080";
const ROUTE_STORE_KEYS = "/kv";
const ROUTE_STORE_KEYS_LIST = "/kv/keys";
const ROUTE_STORE_SECRET = "/kv/secret";
const ROUTE_DEMANDS = "/demands";
const ROUTE_SEMAPHORE_ACQUIRE = "/semaphore/acquire";
const ROUTE_SEMAPHORE_RELEASE = "/semaphore/release";
const ROUTE_SEMAPHORE_STATUS = "/semaphore/status";
const ROUTE_HTTP = "/http";

export function allocationId(): string | undefined {
  return process.env.ALLOCATION_ID;
}

export function allocationIp(): string | undefined {
  return process.env.ALLOCATION_IP;
}

export function allocationIndex(): string | undefined {
  return process.env.ALLOCATION_INDEX;
}

export function isAllocationDisabled(): boolean {
  return process.env.DISABLED === "1";
}

/**
 * True on the first allocation of the first batch.
 * Prefer BATCH_ALLOCATIONS[0] over ALLOCATION_INDEX=0: rollout order can put
 * catalog index 0 in a later batch, which would otherwise make no worker one-shot.
 */
export function isOneShot(): boolean {
  if ((process.env.BATCH_INDEX ?? "0") !== "0") {
    return false;
  }
  const batch = (process.env.BATCH_ALLOCATIONS ?? "")
    .split(",")
    .map((ip) => ip.trim())
    .filter(Boolean);
  if (batch.length) {
    return (process.env.ALLOCATION_IP ?? "").trim() === batch[0];
  }
  return (process.env.ALLOCATION_INDEX ?? "0") === "0";
}

export function hookEvent(): string | undefined {
  return process.env.EVENT;
}

export function hookName(): string | undefined {
  return process.env.HOOK;
}

export function jobName(): string | undefined {
  return process.env.JOB;
}

function runtimeApiBaseUrl(): string {
  const host = process.env.HOOK_API_HOST ?? "127.0.0.1";
  const port = process.env.HOOK_API_PORT ?? RUNTIME_API_DEFAULT_PORT;
  return `http://${host}:${port}`;
}

function runtimeRequestHeaders(): Record<string, string> {
  const headers: Record<string, string> = {
    "X-ALLOCATION-ID": allocationId() ?? "",
    HOOK: hookName() ?? "",
    EVENT: hookEvent() ?? "",
    "Content-Type": "application/json",
    Accept: "application/json",
  };
  const token = process.env.HOOK_API_TOKEN;
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

// Fetch forbids bodies on GET/HEAD. The runtime API requires JSON bodies on
// GET /kv and GET /kv/keys (and DELETE), so use node:http (Bun/Node) instead.
async function runtimeRequest(
  method: string,
  route: string,
  body?: unknown,
  timeoutMs = 120_000,
): Promise<Response> {
  const http = await import("node:http");
  const payload = body === undefined ? undefined : JSON.stringify(body);
  const url = new URL(`${runtimeApiBaseUrl()}${route}`);
  const headers = { ...runtimeRequestHeaders() };
  if (payload !== undefined) {
    headers["Content-Length"] = String(Buffer.byteLength(payload));
  }

  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        protocol: url.protocol,
        hostname: url.hostname,
        port: url.port,
        path: `${url.pathname}${url.search}`,
        method,
        headers,
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => {
          const buf = Buffer.concat(chunks);
          const headerInit: Record<string, string> = {};
          for (const [k, v] of Object.entries(res.headers)) {
            if (v === undefined) continue;
            headerInit[k] = Array.isArray(v) ? v.join(", ") : v;
          }
          resolve(new Response(buf, { status: res.statusCode ?? 0, headers: headerInit }));
        });
      },
    );
    req.setTimeout(timeoutMs, () => {
      req.destroy(new Error(`runtime api timeout after ${timeoutMs}ms`));
    });
    req.on("error", reject);
    if (payload !== undefined) {
      req.write(payload);
    }
    req.end();
  });
}

export async function getStoreValue(namespace: string, key: string): Promise<Response> {
  // Body on GET is required by the runtime API (matches Python/Ruby/Bash SDKs).
  return runtimeRequest("GET", ROUTE_STORE_KEYS, { namespace, key });
}

export async function putRolloutOrder(order: string | string[]): Promise<Response> {
  const job = jobName();
  const value = Array.isArray(order) ? order.map(String).filter(Boolean).join(",") : order;
  return runtimeRequest("PUT", ROUTE_STORE_KEYS, {
    namespace: `kive/job/${job}`,
    key: "rollout_order",
    value,
  });
}

export async function getRolloutOrder(): Promise<Response> {
  const job = jobName();
  return getStoreValue(`kive/job/${job}`, "rollout_order");
}

export async function putJobVariable(key: string, value: string, ttl = 0): Promise<Response> {
  const job = jobName();
  const body: Record<string, string | number> = {
    namespace: `vars/job/${job}`,
    key,
    value,
  };
  if (ttl > 0) {
    body.ttl = ttl;
  }
  return runtimeRequest("PUT", ROUTE_STORE_KEYS, body);
}

export async function putJobSecret(key: string, value: string, ttl = 0): Promise<Response> {
  const job = jobName();
  const body: Record<string, string | number> = {
    namespace: `secrets/job/${job}`,
    key,
    value,
  };
  if (ttl > 0) {
    body.ttl = ttl;
  }
  return runtimeRequest("PUT", ROUTE_STORE_SECRET, body);
}

export async function listJobKeys(namespace?: string): Promise<Response> {
  const body: Record<string, string> = {};
  if (namespace) {
    body.namespace = namespace;
  }
  return runtimeRequest("GET", ROUTE_STORE_KEYS_LIST, body);
}

export async function deleteJobVariable(key: string): Promise<Response> {
  const job = jobName();
  return runtimeRequest("DELETE", ROUTE_STORE_KEYS, {
    namespace: `vars/job/${job}`,
    key,
  });
}

export async function deleteJobSecret(key: string): Promise<Response> {
  const job = jobName();
  return runtimeRequest("DELETE", ROUTE_STORE_SECRET, {
    namespace: `secrets/job/${job}`,
    key,
  });
}

export async function listHookDemands(): Promise<Response> {
  return runtimeRequest("GET", ROUTE_DEMANDS);
}

export async function acquireSemaphore(
  name: string,
  capacity = 1,
  timeoutSeconds = 600,
): Promise<Response> {
  return runtimeRequest(
    "POST",
    ROUTE_SEMAPHORE_ACQUIRE,
    { name, capacity, timeout_seconds: timeoutSeconds },
    (timeoutSeconds + 30) * 1000,
  );
}

export async function releaseSemaphore(name: string): Promise<Response> {
  return runtimeRequest("POST", ROUTE_SEMAPHORE_RELEASE, { name });
}

export async function semaphoreStatus(name: string): Promise<Response> {
  return runtimeRequest("GET", `${ROUTE_SEMAPHORE_STATUS}?name=${encodeURIComponent(name)}`);
}

/** Outbound HTTP(S) via POST /http (worker-IP allowlist + bucket CA). */
export async function httpRequest(
  method: string,
  url: string,
  options?: {
    headers?: Record<string, string>;
    body?: string;
    timeoutSeconds?: number;
    tls?: {
      ca?: string;
      server_name?: string;
      insecure_skip_verify?: boolean;
    };
  },
): Promise<{
  status_code: number;
  headers?: Record<string, string[]>;
  body: string;
  truncated: boolean;
  error?: string;
}> {
  const timeoutSeconds = options?.timeoutSeconds ?? 30;
  const resp = await runtimeRequest(
    "POST",
    ROUTE_HTTP,
    {
      method,
      url,
      headers: options?.headers ?? {},
      body: options?.body ?? "",
      timeout_seconds: timeoutSeconds,
      tls: options?.tls ?? {},
    },
    (timeoutSeconds + 30) * 1000,
  );
  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(`httpRequest failed: ${resp.status} ${text}`);
  }
  return resp.json();
}

// Legacy aliases for older hook scripts.
export const getAllocationId = allocationId;
export const getAllocationIp = allocationIp;
export const getAllocationIndex = allocationIndex;
export const getEvent = hookEvent;
export const getHook = hookName;
export const getJob = jobName;
export const kvGet = getStoreValue;
export const kvPut = putJobVariable;
export const kvPutSecret = putJobSecret;
export const getDemands = listHookDemands;
