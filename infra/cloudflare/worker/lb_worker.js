const KV_KEY_PREFIX = "replicas:";
const KV_LIST_PAGE_SIZE = 1000;
const DIRECTORY_TTL_SECONDS = 30;
const DIRECTORY_PATH = "/__lb/directory";
const FAILOVER_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

let _memory = { at: 0, replicas: [] };

const FNV64_OFFSET = 0xcbf29ce484222325n;
const FNV64_PRIME = 0x00000100000001b3n;
const MASK64 = 0xffffffffffffffffn;

function fnv1a64Bytes(bytes) {
  let h = FNV64_OFFSET;
  for (let i = 0; i < bytes.length; i++) {
    h = ((h ^ BigInt(bytes[i])) * FNV64_PRIME) & MASK64;
  }
  return h;
}

const _enc = new TextEncoder();

function score(key, replicaId) {
  const a = _enc.encode(key);
  const b = _enc.encode(replicaId);
  const buf = new Uint8Array(a.length + 1 + b.length);
  buf.set(a, 0);
  buf[a.length] = 0;
  buf.set(b, a.length + 1);

  let x = fnv1a64Bytes(buf);
  x = ((x ^ (x >> 30n)) * 0xbf58476d1ce4e5b9n) & MASK64;
  x = ((x ^ (x >> 27n)) * 0x94d049bb133111ebn) & MASK64;
  x = (x ^ (x >> 31n)) & MASK64;
  return x;
}

function pick(workspaceId, replicas) {
  if (!replicas.length) return null;
  if (replicas.length === 1) return replicas[0];
  if (!workspaceId) {
    return replicas[Math.floor(Math.random() * replicas.length)];
  }
  const key = `ws:${workspaceId}`;
  let best = null;
  let bestScore = -1n;
  for (const r of replicas) {
    const s = score(key, r.id);
    if (s > bestScore || (s === bestScore && (best === null || r.id < best.id))) {
      bestScore = s;
      best = r;
    }
  }
  return best;
}

function pickRanked(workspaceId, replicas, n) {
  if (!workspaceId) {
    const shuffled = [...replicas];
    for (let i = shuffled.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
    }
    return shuffled.slice(0, n);
  }
  const key = `ws:${workspaceId}`;
  const scored = replicas.map(r => ({ r, s: score(key, r.id) }));
  scored.sort((a, b) => {
    if (a.s !== b.s) return a.s > b.s ? -1 : 1;
    return a.r.id < b.r.id ? -1 : 1;
  });
  return scored.slice(0, n).map(x => x.r);
}

function validReplicas(values) {
  const replicas = [];
  for (const v of Array.isArray(values) ? values : []) {
    if (v && typeof v.id === "string" && typeof v.url === "string") {
      replicas.push(v);
    }
  }
  replicas.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  return replicas;
}

async function readDirectoryFromKV(env) {
  const keys = [];
  let cursor;
  for (; ;) {
    const page = await env.REPLICAS_KV.list({
      prefix: KV_KEY_PREFIX,
      limit: KV_LIST_PAGE_SIZE,
      cursor,
    });
    keys.push(...page.keys);
    if (page.list_complete || !page.cursor) break;
    cursor = page.cursor;
  }

  const values = await Promise.all(
    keys.map(k => env.REPLICAS_KV.get(k.name, { type: "json" }))
  );
  return validReplicas(values);
}

function directoryKey(url) {
  return new Request(new URL(DIRECTORY_PATH, url.origin).toString());
}

async function readSharedDirectory(key) {
  try {
    const cached = await caches.default.match(key);
    if (!cached) return null;
    const shared = await cached.json();
    const replicas = validReplicas(shared && shared.replicas);
    if (!replicas.length || typeof shared.at !== "number") return null;
    return { at: shared.at, replicas };
  } catch (_) {
    return null;
  }
}

function shareDirectory(key, directory) {
  return caches.default.put(key, new Response(JSON.stringify(directory), {
    headers: {
      "content-type": "application/json",
      "cache-control": `max-age=${DIRECTORY_TTL_SECONDS}`,
    },
  })).catch(() => {});
}

async function getReplicas(env, url, ctx) {
  const now = Date.now();
  if (now - _memory.at < DIRECTORY_TTL_SECONDS * 1000 && _memory.replicas.length > 0) {
    return _memory.replicas;
  }

  const key = directoryKey(url);
  const shared = await readSharedDirectory(key);
  if (shared) {
    _memory = shared;
    return shared.replicas;
  }

  const replicas = await readDirectoryFromKV(env);
  if (replicas.length > 0) {
    _memory = { at: now, replicas };
    ctx.waitUntil(shareDirectory(key, _memory));
  }
  return replicas;
}

function getWorkspaceId(req, url) {
  const qp = url.searchParams.get("workspace") || url.searchParams.get("workspace_id");
  if (qp) return qp;

  const hdr = req.headers.get("x-workspace-id");
  if (hdr) return hdr;

  const auth = req.headers.get("authorization");
  if (auth && auth.startsWith("Bearer ")) {
    try {
      const parts = auth.slice(7).split(".");
      if (parts.length >= 2) {
        const payload = parts[1].replace(/-/g, "+").replace(/_/g, "/");
        const padded = payload + "=".repeat((4 - (payload.length % 4)) % 4);
        const json = JSON.parse(atob(padded));
        if (json.workspace_id) return String(json.workspace_id);
      }
    } catch (_) { }
  }
  return "";
}

async function forward(req, target, env, workspaceId) {
  const incoming = new URL(req.url);
  const upstream = new URL(target.url);
  upstream.pathname = incoming.pathname;
  upstream.search = incoming.search;

  const headers = new Headers(req.headers);
  headers.set("x-origin-lb-secret", env.LB_SECRET);
  headers.set("x-origin-replica", target.id);
  if (workspaceId) headers.set("x-origin-workspace", workspaceId);

  const cfIP = req.headers.get("cf-connecting-ip");
  if (cfIP) {
    headers.set("x-real-ip", cfIP);
    const xff = req.headers.get("x-forwarded-for");
    headers.set("x-forwarded-for", xff ? `${xff}, ${cfIP}` : cfIP);
  }
  headers.set("x-forwarded-proto", incoming.protocol.replace(":", ""));
  headers.set("x-forwarded-host", incoming.host);

  const upstreamReq = new Request(upstream.toString(), req);
  return fetch(upstreamReq, { headers, redirect: "manual" });
}

async function forwardWithMaybeFailover(req, replicas, env, workspaceId) {
  const canFailover = FAILOVER_METHODS.has(req.method) && replicas.length > 1;
  const targets = canFailover ? pickRanked(workspaceId, replicas, 2) : [pick(workspaceId, replicas)];

  let lastErr;
  for (let i = 0; i < targets.length; i++) {
    const target = targets[i];
    if (!target) continue;
    try {
      const resp = await forward(req, target, env, workspaceId);
      if (i + 1 < targets.length && (resp.status === 502 || resp.status === 503 || resp.status === 504)) {
        continue;
      }
      return { resp, target };
    } catch (err) {
      lastErr = err;
    }
  }
  throw lastErr || new Error("all replicas failed");
}

export default {
  async fetch(req, env, ctx) {
    const url = new URL(req.url);

    if (!env.REPLICAS_KV || !env.LB_SECRET) {
      return new Response("load balancer is not configured", { status: 503 });
    }

    if (url.pathname === "/__lb/health") {
      return new Response(JSON.stringify({ ok: true, ts: Date.now() }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }

    if (url.pathname === DIRECTORY_PATH) {
      return new Response("not found", { status: 404 });
    }

    if (url.pathname === "/__lb/replicas") {
      if (!env.LB_DEBUG_TOKEN || req.headers.get("x-debug-token") !== env.LB_DEBUG_TOKEN) {
        return new Response("forbidden", { status: 403 });
      }
      const replicas = await getReplicas(env, url, ctx);
      return new Response(JSON.stringify({
        replicas,
        cached_at: _memory.at,
        cache_age_ms: Date.now() - _memory.at,
      }, null, 2), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }

    const workspaceId = getWorkspaceId(req, url);
    const replicas = await getReplicas(env, url, ctx);
    if (replicas.length === 0) {
      return new Response("no replicas available", { status: 503 });
    }

    let resp, target;
    try {
      ({ resp, target } = await forwardWithMaybeFailover(req, replicas, env, workspaceId));
    } catch (err) {
      return new Response(`upstream error: ${err && err.message ? err.message : err}`, {
        status: 502,
      });
    }

    if (resp.status === 101) return resp;

    const out = new Response(resp.body, resp);
    out.headers.set("x-origin-replica", target.id);
    if (workspaceId && req.headers.get("x-origin-debug") === "1") {
      out.headers.set("x-origin-workspace", workspaceId);
    }
    return out;
  },
};
