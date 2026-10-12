import { afterEach, beforeEach, expect, setSystemTime, test } from "bun:test";

const API = "https://api.vozkoia.com";
const REPLICA = { id: "11111111-1111-1111-1111-111111111111", url: "https://replica-1.internal" };

let isolates = 0;
let originalFetch;
let originalCaches;
let origin;

async function workerCopy() {
  isolates += 1;
  return (await import(`./lb_worker.js?copy=${isolates}`)).default;
}

function dataCenterCache() {
  const entries = new Map();
  const stats = { puts: 0 };
  return {
    stats,
    async match(request) {
      const entry = entries.get(request.url);
      if (!entry || entry.expiresAt <= Date.now()) return undefined;
      return entry.response.clone();
    },
    async put(request, response) {
      stats.puts += 1;
      const maxAge = Number(/max-age=(\d+)/.exec(response.headers.get("cache-control") ?? "")?.[1] ?? 0);
      entries.set(request.url, { response: response.clone(), expiresAt: Date.now() + maxAge * 1000 });
    },
  };
}

function directory(replicas) {
  const keys = new Map(replicas.map(r => [`replicas:${r.id}`, r]));
  const calls = { list: 0, get: 0 };
  return {
    calls,
    async list({ prefix }) {
      calls.list += 1;
      return { keys: [...keys.keys()].filter(k => k.startsWith(prefix)).map(name => ({ name })), list_complete: true };
    },
    async get(name) {
      calls.get += 1;
      return keys.get(name) ?? null;
    },
  };
}

function executionContext() {
  const pending = [];
  return {
    waitUntil(promise) {
      pending.push(promise);
    },
    settle() {
      return Promise.all(pending);
    },
  };
}

async function call(worker, env, path = "/conversations", init = {}) {
  const ctx = executionContext();
  const response = await worker.fetch(new Request(`${API}${path}`, init), env, ctx);
  await ctx.settle();
  return response;
}

beforeEach(() => {
  originalFetch = globalThis.fetch;
  originalCaches = globalThis.caches;
  globalThis.caches = { default: dataCenterCache() };
  origin = { requests: [], fail: false };
  globalThis.fetch = async (request, init) => {
    origin.requests.push({ url: request.url, headers: new Headers(init?.headers) });
    if (origin.fail) throw new Error("connection refused");
    return new Response("ok", { status: 200 });
  };
  setSystemTime(new Date("2026-10-10T12:00:00Z"));
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.caches = originalCaches;
  setSystemTime();
});

test("copies of the worker in one data center share a single directory read", async () => {
  const kv = directory([REPLICA]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };

  const first = await call(await workerCopy(), env);
  const second = await call(await workerCopy(), env);
  const third = await call(await workerCopy(), env);

  expect([first.status, second.status, third.status]).toEqual([200, 200, 200]);
  expect(kv.calls.list).toBe(1);
  expect(globalThis.caches.default.stats.puts).toBe(1);
});

test("the directory is read from KV again once the shared window has passed", async () => {
  const kv = directory([REPLICA]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };

  await call(await workerCopy(), env);
  setSystemTime(new Date("2026-10-10T12:00:31Z"));
  await call(await workerCopy(), env);

  expect(kv.calls.list).toBe(2);
});

test("a copy reuses its own directory without asking the data center cache", async () => {
  const kv = directory([REPLICA]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };
  const worker = await workerCopy();

  await call(worker, env);
  let lookups = 0;
  const shared = globalThis.caches.default;
  globalThis.caches = { default: { ...shared, match: (request) => { lookups += 1; return shared.match(request); } } };
  await call(worker, env);

  expect(lookups).toBe(0);
  expect(kv.calls.list).toBe(1);
});

test("an unreachable origin does not send every following request back to KV", async () => {
  const kv = directory([REPLICA]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };
  const worker = await workerCopy();
  origin.fail = true;

  const responses = [await call(worker, env), await call(worker, env), await call(await workerCopy(), env)];

  expect(responses.map(r => r.status)).toEqual([502, 502, 502]);
  expect(kv.calls.list).toBe(1);
});

test("an empty directory is refused and never shared", async () => {
  const kv = directory([]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };

  const first = await call(await workerCopy(), env);
  const second = await call(await workerCopy(), env);

  expect([first.status, second.status]).toEqual([503, 503]);
  expect(globalThis.caches.default.stats.puts).toBe(0);
  expect(kv.calls.list).toBe(2);
});

test("the shared directory path is never forwarded or served to clients", async () => {
  const kv = directory([REPLICA]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };
  const worker = await workerCopy();
  await call(worker, env);
  const forwardedBefore = origin.requests.length;

  const response = await call(worker, env, "/__lb/directory");

  expect(response.status).toBe(404);
  expect(origin.requests.length).toBe(forwardedBefore);
});

test("a shared directory with malformed entries keeps only valid replicas", async () => {
  const kv = directory([REPLICA]);
  const env = { REPLICAS_KV: kv, LB_SECRET: "secret" };
  await globalThis.caches.default.put(
    new Request(`${API}/__lb/directory`),
    new Response(JSON.stringify({ at: Date.now(), replicas: [{ id: 7 }, REPLICA] }), { headers: { "cache-control": "max-age=30" } }),
  );

  const response = await call(await workerCopy(), env);

  expect(response.status).toBe(200);
  expect(kv.calls.list).toBe(0);
  expect(origin.requests.at(-1).url).toBe(`${REPLICA.url}/conversations`);
});

test("forwarded requests keep the path, query and origin secret and name the workspace", async () => {
  const env = { REPLICAS_KV: directory([REPLICA]), LB_SECRET: "secret" };

  await call(await workerCopy(), env, "/leads?page=2", { headers: { "x-workspace-id": "ws-1" } });

  const forwarded = origin.requests.at(-1);
  expect(forwarded.url).toBe(`${REPLICA.url}/leads?page=2`);
  expect(forwarded.headers.get("x-origin-lb-secret")).toBe("secret");
  expect(forwarded.headers.get("x-origin-workspace")).toBe("ws-1");
});

test("the health check answers without touching KV", async () => {
  const kv = directory([REPLICA]);

  const response = await call(await workerCopy(), { REPLICAS_KV: kv, LB_SECRET: "secret" }, "/__lb/health");

  expect(response.status).toBe(200);
  expect(kv.calls.list).toBe(0);
});
