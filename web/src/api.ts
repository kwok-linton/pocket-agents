import { t, tServer } from "./i18n.ts";
import { applyHistoryDelta } from "./history-sync.ts";
import type { History } from "./types.ts";
export class APIError extends Error {
  status: number;
  constructor(message: string, status: number) { super(message); this.status = status; }
}
const validated = new Map<string, { tag: string; body: unknown }>();
type Read = { controller: AbortController; promise: Promise<unknown>; consumers: number };
const pending = new Map<string, Read>();
let epoch = 0;
export function peekValidated<T>(path: string): T | undefined {
  return validated.get(path)?.body as T | undefined;
}
export function forgetValidated() {
  epoch++;
  validated.clear();
  for (const read of pending.values()) read.controller.abort();
  pending.clear();
}

// Concurrent GET callers share a download. Each caller can leave independently;
// the request is aborted when its last reader leaves. Writes never join/retry.
export function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const read = (options.method || "GET").toUpperCase() === "GET";
  if (!read) return request<T>(path, options, epoch);
  if (options.signal?.aborted) return Promise.reject(new DOMException("Aborted", "AbortError"));
  const key = JSON.stringify([path, { ...options, signal: undefined, headers: [...new Headers(options.headers).entries()] }]);
  let entry = pending.get(key);
  if (!entry) {
    const controller = new AbortController();
    entry = { controller, consumers: 0, promise: request(path, {...options, signal: controller.signal}, epoch) };
    const current = entry;
    pending.set(key, entry);
    // Attach both handlers so a canceled final subscriber cannot leave an
    // unhandled rejection behind.
    const remove = () => { if (pending.get(key) === current) pending.delete(key); };
    entry.promise.then(remove, remove);
  }
  const current = entry;
  current.consumers++;
  return new Promise<T>((resolve, reject) => {
    let settled = false;
    const finish = (error: unknown, value?: unknown) => {
      if (settled) return;
      settled = true;
      options.signal?.removeEventListener("abort", abort);
      if (--current.consumers === 0) {
        if (pending.get(key) === current) pending.delete(key);
        current.controller.abort();
      }
      if (error) reject(error); else resolve(value as T);
    };
    const abort = () => finish(new DOMException("Aborted", "AbortError"));
    options.signal?.addEventListener("abort", abort, {once: true});
    current.promise.then(value => finish(null, value), error => finish(error));
  });
}

async function request<T>(path: string, options: RequestInit, generation: number): Promise<T> {
  const read = (options.method || "GET").toUpperCase() === "GET";
  const controller = read ? new AbortController() : null;
  const abort = () => controller?.abort(options.signal?.reason);
  if (options.signal?.aborted) abort();
  else options.signal?.addEventListener("abort", abort, { once: true });
  const deadline = controller ? setTimeout(() => controller.abort(), 12000) : undefined;
  try {
    let known = read ? validated.get(path) : undefined;
    for (let attempt = 0; attempt < 2; attempt++) {
      const headers = new Headers(options.headers);
      if (options.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
      if (known) headers.set("If-None-Match", known.tag);
      else headers.delete("If-None-Match");
      const response = await fetch(path, {
        credentials: "same-origin", cache: "no-store", ...options,
        ...(controller ? { signal: controller.signal } : {}), headers,
      });
      if (read && (generation !== epoch || controller?.signal.aborted)) throw new DOMException("Aborted", "AbortError");
      if (response.status === 304 && known) return known.body as T;
      const text = await response.text();
      let body: unknown;
      try { body = text ? JSON.parse(text) : null; } catch { body = null; }
      if (!response.ok) {
        if (response.status === 401) {
          forgetValidated();
          window.dispatchEvent(new Event("pocket:unauthorized"));
        }
        const detail = body as { error?: string; message?: string } | null;
        throw new APIError(tServer(detail?.error || detail?.message) || t("请求失败（{0}）", { 0: response.status }), response.status);
      }
      if (read && response.headers.get("X-Pocket-History-Delta") === "1") {
        body = applyHistoryDelta(known?.body as History | undefined, known?.tag, body);
        if (!body) { known = undefined; continue; } // one full GET repairs an evicted base
      }
      if (read && (generation !== epoch || controller?.signal.aborted)) throw new DOMException("Aborted", "AbortError");
      const tag = read && response.headers.get("ETag");
      if (tag) {
        validated.delete(path);
        validated.set(path, {tag, body});
        if (validated.size > 40) validated.delete(validated.keys().next().value!);
      }
      return body as T;
    }
    throw new APIError(t("请求失败（{0}）", {0: 502}), 502);
  } finally {
    clearTimeout(deadline);
    options.signal?.removeEventListener("abort", abort);
  }
}
export const post = <T>(path: string, body?: unknown, signal?: AbortSignal) =>
  api<T>(path, {
    method: "POST", signal,
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
export function readLocal<T>(key: string, fallback: T): T {
  try {
    const value = localStorage.getItem(`pocket:${key}`);
    return value ? (JSON.parse(value) as T) : fallback;
  } catch {
    return fallback;
  }
}
export function writeLocal(key: string, value: unknown) {
  try {
    localStorage.setItem(`pocket:${key}`, JSON.stringify(value));
  } catch {
    /* Private mode/storage exhaustion must not prevent conversation. */
  }
}
