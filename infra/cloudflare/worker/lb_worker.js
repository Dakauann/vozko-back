// =====================================================================
// Workspace-sticky API load-balancer Worker
//
// - Reads active replica directory from Cloudflare KV (binding: REPLICAS_KV)
// - Picks one replica per workspaceId via Rendezvous (HRW) hash
// - HRW score MUST match the Go implementation in
//   domain/sip_trunk/hash_ring.go (FNV-1a 64 + '\0' separator +
//   SplitMix64), otherwise the trunk owner the LB picks won't match the
//   trunk owner the cluster reconciler assigned.
// - Forwards request (HTTP + WebSocket), with failover to the 2nd HRW
//   pick on idempotent methods only.
// - Authenticates to origin with shared secret (binding: LB_SECRET)
//
// Bindings required:
//   REPLICAS_KV   KV namespace      (entries: replicas:<id> -> {id,url,...})
//   LB_SECRET     Secret string     (matched by the origin gateway)
//   LB_DEBUG_TOKEN Secret string     Used only to gate /__lb/replicas
//
// Entry shape written by the Go publisher
// (infra/cloudflare/kv_directory.go):
//   {"id":"<uuid>","url":"https://replica-1.example.com",
//    "region":"us-east","updated_at":1730000000}
// =====================================================================

const KV_KEY_PREFIX = "replicas:";
const KV_CACHE_TTL_MS = 30_000;       // refresh directory every 30s
const KV_LIST_PAGE_SIZE = 1000;         // CF KV max per list page
const FAILOVER_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

// Module-scope cache (per-isolate; CF spins many isolates so cache is best-effort).
let _cache = { at: 0, replicas: [] };

// ---------------------------------------------------------------------
// Hashing — must mirror Go score():
//   h = fnv1a64(key + 0x00 + replicaID)
//   h = splitmix64_finalize(h)
// See domain/sip_trunk/hash_ring.go.
// ---------------------------------------------------------------------
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
  buf[a.length] = 0;                 // null separator — MUST match Go
  buf.set(b, a.length + 1);

  let x = fnv1a64Bytes(buf);
  x = ((x ^ (x >> 30n)) * 0xbf58476d1ce4e5b9n) & MASK64;
  x = ((x ^ (x >> 27n)) * 0x94d049bb133111ebn) & MASK64;
  x = (x ^ (x >> 31n)) & MASK64;
  return x;
}

// ---------------------------------------------------------------------
// Rendezvous (Highest Random Weight) selection.
// Tie-break on smaller replicaID lexicographically — matches Go.
// ---------------------------------------------------------------------
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

// Pick top-N for failover ordering. O(N*K) but K is small (≤2).
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

// ---------------------------------------------------------------------
// KV directory read with module-memory cache. Paginates with cursor so we
// don't silently truncate at growth time.
// ---------------------------------------------------------------------
async function getReplicas(env) {
  const now = Date.now();
  if (now - _cache.at < KV_CACHE_TTL_MS && _cache.replicas.length > 0) {
    return _cache.replicas;
  }

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

  const replicas = [];
  for (const v of values) {
    if (v && typeof v.id === "string" && typeof v.url === "string") {
      replicas.push(v);
    }
  }

  // Stable order so HRW tie-breaking is deterministic across isolates.
  replicas.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));

  _cache = { at: now, replicas };
  return replicas;
}

function bustCache() {
  _cache = { at: 0, replicas: [] };
}

// ---------------------------------------------------------------------
// Workspace ID extraction.
// Order: ?workspace= → ?workspace_id= → x-workspace-id → JWT claim.
// JWT decode is unsigned — the origin still validates the signature.
// ---------------------------------------------------------------------
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
        // atob requires padding
        const padded = payload + "=".repeat((4 - (payload.length % 4)) % 4);
        const json = JSON.parse(atob(padded));
        if (json.workspace_id) return String(json.workspace_id);
      }
    } catch (_) { /* malformed JWT — fall through */ }
  }
  return "";
}

// ---------------------------------------------------------------------
// Forward a single request to a chosen replica.
// Faithfully proxies Method/Body/Upgrade by passing the original Request
// as the init template (`new Request(url, req)`), which preserves the
// readable stream and `Upgrade: websocket` semantics on Cloudflare.
// ---------------------------------------------------------------------
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
    // Append client IP to XFF chain rather than overwrite.
    const xff = req.headers.get("x-forwarded-for");
    headers.set("x-forwarded-for", xff ? `${xff}, ${cfIP}` : cfIP);
  }
  headers.set("x-forwarded-proto", incoming.protocol.replace(":", ""));
  headers.set("x-forwarded-host", incoming.host);

  // `new Request(url, req)` keeps method, body stream, and upgrade headers.
  const upstreamReq = new Request(upstream.toString(), req);
  return fetch(upstreamReq, { headers, redirect: "manual" });
}

// Forward with failover to 2nd HRW pick. Only used for idempotent methods,
// because retrying a non-idempotent request risks duplicate writes — and
// because the second pick may not own this workspace's SIP trunk anyway.
async function forwardWithMaybeFailover(req, replicas, env, workspaceId) {
  const canFailover = FAILOVER_METHODS.has(req.method) && replicas.length > 1;
  const targets = canFailover ? pickRanked(workspaceId, replicas, 2) : [pick(workspaceId, replicas)];

  let lastErr;
  for (let i = 0; i < targets.length; i++) {
    const target = targets[i];
    if (!target) continue;
    try {
      const resp = await forward(req, target, env, workspaceId);
      // Pure transport-failure-style upstream errors → try next replica.
      // 5xx from a healthy origin is left to the caller to surface — we
      // only failover on 502/503/504 which usually mean upstream unreachable.
      if (i + 1 < targets.length && (resp.status === 502 || resp.status === 503 || resp.status === 504)) {
        bustCache();
        continue;
      }
      return { resp, target };
    } catch (err) {
      lastErr = err;
      bustCache();
      // Loop to next target.
    }
  }
  throw lastErr || new Error("all replicas failed");
}

// =====================================================================
// Handler
// =====================================================================
export default {
  async fetch(req, env, _ctx) {
    const url = new URL(req.url);

    if (!env.REPLICAS_KV || !env.LB_SECRET) {
      return new Response("load balancer is not configured", { status: 503 });
    }

    // Health endpoint — no KV, no workspace.
    if (url.pathname === "/__lb/health") {
      return new Response(JSON.stringify({ ok: true, ts: Date.now() }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }

    // Debug access is disabled unless a dedicated token is configured.
    if (url.pathname === "/__lb/replicas") {
      if (!env.LB_DEBUG_TOKEN || req.headers.get("x-debug-token") !== env.LB_DEBUG_TOKEN) {
        return new Response("forbidden", { status: 403 });
      }
      const replicas = await getReplicas(env);
      return new Response(JSON.stringify({
        replicas,
        cached_at: _cache.at,
        cache_age_ms: Date.now() - _cache.at,
      }, null, 2), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }

    const workspaceId = getWorkspaceId(req, url);
    const replicas = await getReplicas(env);
    if (replicas.length === 0) {
      return new Response("no replicas available", { status: 503 });
    }

    let resp, target;
    try {
      ({ resp, target } = await forwardWithMaybeFailover(req, replicas, env, workspaceId));
    } catch (err) {
      bustCache();
      return new Response(`upstream error: ${err && err.message ? err.message : err}`, {
        status: 502,
      });
    }

    // WebSocket upgrade: never re-wrap the response (it would drop the
    // webSocket pair). Just return as-is.
    if (resp.status === 101) return resp;

    const out = new Response(resp.body, resp);
    out.headers.set("x-origin-replica", target.id);
    // Note: do NOT echo workspace ID back to the browser in prod — it can
    // leak tenant info into client logs. Gated on a debug header.
    if (workspaceId && req.headers.get("x-origin-debug") === "1") {
      out.headers.set("x-origin-workspace", workspaceId);
    }
    return out;
  },
};
