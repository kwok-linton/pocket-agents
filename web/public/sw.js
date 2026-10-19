const CACHE = "pocket-shell-v1";
const PUSH_DIAGNOSTICS_CACHE = "pocket-push-diagnostics-v1";
// The page stores its interface language here (src/i18n.ts). The server sends
// Chinese text; English pages get the same message chosen by event kind.
const PREFERENCES_CACHE = "pocket-preferences-v1";
const ENGLISH_PUSH = {
  test: "Test notification arrived. Tap to review notification settings.",
  completed: "Turn finished. Tap to view.",
  waiting_approval: "A native request is waiting for approval",
  waiting_input: "The agent is waiting for your answer",
  interrupted: "The current turn was interrupted",
  error: "A session hit an error",
};
async function pushLanguage() {
  try {
    const stored = await (await caches.open(PREFERENCES_CACHE)).match("/.pocket-language");
    return stored ? (await stored.text()).trim() : "zh";
  } catch {
    return "zh";
  }
}
const PUSH_DIAGNOSTICS_KEY = new URL(
  "/.pocket-push-diagnostics",
  self.location.origin,
).href;
const PUSH_DIAGNOSTICS_STAGES = new Set([
  "push_received",
  "show_notification_resolved",
  "show_notification_rejected",
  "notification_clicked",
  "notification_opened",
  "notification_open_failed",
]);
let pushDiagnosticWrites = Promise.resolve();

function diagnosticTestId(value) {
  return typeof value === "string" && /^[a-f0-9-]{36}$/.test(value) ? value : undefined;
}

function diagnosticEventId(value) {
  return Number.isSafeInteger(value) && value > 0 ? value : null;
}

async function storedPushDiagnostics(cache) {
  const response = await cache.match(PUSH_DIAGNOSTICS_KEY);
  if (!response) return [];
  const value = await response.json();
  if (!Array.isArray(value)) return [];
  // Reconstruct each record so cached data cannot add notification content or
  // credentials to the diagnostic response, even after a format change.
  return value
    .filter(
      (record) =>
        record &&
        PUSH_DIAGNOSTICS_STAGES.has(record.stage) &&
        typeof record.at === "string" &&
        Number.isFinite(Date.parse(record.at)),
    )
    .slice(-20)
    .map((record) => ({
      eventId: diagnosticEventId(record.eventId),
      ...(diagnosticTestId(record.testId) ? { testId: record.testId } : {}),
      stage: record.stage,
      at: new Date(record.at).toISOString(),
    }));
}

function recordPushDiagnostic(eventId, stage, testId) {
  const record = {
    eventId: diagnosticEventId(eventId),
    ...(diagnosticTestId(testId) ? { testId } : {}),
    stage,
    at: new Date().toISOString(),
  };
  // Serialize read/modify/write operations from overlapping push and click
  // events. Diagnostics are best effort and never gate showNotification.
  pushDiagnosticWrites = pushDiagnosticWrites
    .then(async () => {
      const cache = await caches.open(PUSH_DIAGNOSTICS_CACHE);
      let records = [];
      try {
        records = await storedPushDiagnostics(cache);
      } catch {
        // A corrupt old record must not prevent a new diagnostic entry.
      }
      records.push(record);
      await cache.put(
        PUSH_DIAGNOSTICS_KEY,
        new Response(JSON.stringify(records.slice(-20)), {
          headers: { "Content-Type": "application/json" },
        }),
      );
    })
    .catch(() => {});
  return pushDiagnosticWrites;
}

self.addEventListener("message", (event) => {
  if (event.data?.type !== "pocket:push-diagnostics" || !event.ports?.[0])
    return;
  event.waitUntil(
    (async () => {
      const source = event.source;
      if (
        event.origin !== self.location.origin ||
        source?.type !== "window" ||
        typeof source.id !== "string"
      )
        return;
      const controlled = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: false,
      });
      const allowed = controlled.some((client) => {
        try {
          return (
            client.id === source.id &&
            new URL(client.url).origin === self.location.origin
          );
        } catch {
          return false;
        }
      });
      if (!allowed) return;
      await pushDiagnosticWrites;
      let records = [];
      try {
        records = await storedPushDiagnostics(
          await caches.open(PUSH_DIAGNOSTICS_CACHE),
        );
      } catch {
        // Storage may be unavailable in private browsing or under pressure.
      }
      event.ports[0].postMessage({ records });
    })().catch(() => {}),
  );
});

const SHELL = [
  "/",
  "/index.html",
  "/manifest.webmanifest",
  "/icon.svg",
  "/icon-192.png",
  "/icon-512.png",
];
self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(SHELL))
      .then(() => self.skipWaiting()),
  );
});
self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((key) => key.startsWith("pocket-shell-") && key !== CACHE)
            .map((key) => caches.delete(key)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

async function navigationShell(request) {
  let cached;
  try {
    cached = await (await caches.open(CACHE)).match("/index.html");
  } catch {
    // Storage pressure or unavailable Cache Storage must not hide a live server.
  }
  if (!cached) return fetch(request);
  if (!cached.redirected) return cached;
  // Older servers redirected /index.html to /. Cache Storage retains that
  // response's URL list; Safari rejects it for a navigation with manual
  // redirects. Rebuild only the static shell, preserving its security headers.
  const headers = new Headers(cached.headers);
  headers.delete("Content-Encoding");
  headers.delete("Content-Length");
  return new Response(cached.body, {
    status: cached.status,
    statusText: cached.statusText,
    headers,
  });
}

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  // Authentication, native history and all API calls must always go to the server.
  if (
    event.request.method !== "GET" ||
    url.origin !== self.location.origin ||
    url.pathname.startsWith("/api/") ||
    event.request.cache === "no-store" ||
    url.searchParams.has("pocket_update_check")
  )
    return;
  if (event.request.mode === "navigate") {
    event.respondWith(
      // Render the installed shell immediately, even while Tailscale is
      // reconnecting. Only static UI is cached; authentication and session data
      // still come from the live API. Explicit update checks bypass this path.
      navigationShell(event.request),
    );
    return;
  }
  if (!url.pathname.startsWith("/assets/") && !SHELL.includes(url.pathname))
    return;
  event.respondWith(
    caches.match(event.request).then(
      (cached) =>
        cached ||
        fetch(event.request).then((response) => {
          if (response.ok) {
            const copy = response.clone();
            caches.open(CACHE).then((cache) => cache.put(event.request, copy));
          }
          return response;
        }),
    ),
  );
});
self.addEventListener("push", (event) => {
  let payload = {};
  try {
    const decoded = event.data?.json();
    if (decoded && typeof decoded === "object" && !Array.isArray(decoded))
      payload = decoded;
  } catch {
    payload = {};
  }
  const eventId = diagnosticEventId(payload.eventId);
  const testId = diagnosticTestId(payload.testId);
  let candidate = "/notifications";
  if (typeof payload.url === "string" && payload.url) {
    candidate = payload.url;
  } else if (typeof payload.sessionId === "string" && payload.sessionId) {
    try {
      candidate = "/session/" + encodeURIComponent(payload.sessionId);
    } catch {
      // Malformed Unicode in an invalid payload must not suppress the alert.
    }
  }
  const target = (testId && candidate === "/settings") || /^\/session\/[^/?#]+$/.test(candidate)
    ? candidate
    : "/notifications";
  const received = recordPushDiagnostic(eventId, "push_received", testId);
  const shown = pushLanguage()
    .then((language) => {
      const english = language === "en";
      const kind = typeof payload.tag === "string" ? payload.tag.split(":").pop() : "";
      return self.registration.showNotification(
        typeof payload.title === "string" && payload.title
          ? payload.title
          : english ? "Pocket Agents has an update" : "Pocket Agents 有新进展",
        {
          body: english
            ? ENGLISH_PUSH[kind] || "A session needs your attention"
            : typeof payload.body === "string" && payload.body
              ? payload.body
              : "打开私有工作空间查看详情",
          icon: "/icon-192.png",
          badge: "/icon-192.png",
          tag:
            typeof payload.tag === "string" && payload.tag
              ? payload.tag
              : String(eventId || target),
          // Repeated completion events for the same session should alert again
          // rather than silently replacing a notification with the same tag.
          renotify: true,
          data: { target, eventId, ...(testId ? { testId } : {}) },
        },
      );
    })
    .then(
      () => recordPushDiagnostic(eventId, "show_notification_resolved", testId),
      async () => {
        await recordPushDiagnostic(eventId, "show_notification_rejected", testId);
        // Preserve the failed event's lifetime result without retaining the
        // native error message, which can include private data.
        throw new Error("show_notification_rejected");
      },
    );
  event.waitUntil(Promise.all([received, shown]));
});
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const testId = diagnosticTestId(event.notification.data?.testId);
  const clicked = recordPushDiagnostic(
    event.notification.data?.eventId,
    "notification_clicked",
    testId,
  );
  const candidate = event.notification.data?.target;
  const target =
    typeof candidate === "string" && ((testId && candidate === "/settings") || /^\/session\/[^/?#]+$/.test(candidate))
      ? candidate
      : "/notifications";
  // Start opening immediately; awaiting diagnostic storage here could consume
  // the notification click's transient user activation.
  const opened = (async () => {
    let clients = [];
    try {
      // navigate() rejects for clients not controlled by this worker.
      clients = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: false,
      });
    } catch {
      // A failed lookup must still allow opening the requested route.
    }
    for (const client of clients) {
      try {
        if (new URL(client.url).origin !== self.location.origin) continue;
        const navigated = await client.navigate(target);
        if (navigated) {
          await navigated.focus();
          return;
        }
      } catch {
        // A window may close or lose its controller between lookup and use.
      }
    }
    const created = await self.clients.openWindow(target);
    if (!created) throw new Error("notification_open_failed");
  })().then(
    () =>
      recordPushDiagnostic(
        event.notification.data?.eventId,
        "notification_opened",
        testId,
      ),
    async () => {
      await recordPushDiagnostic(
        event.notification.data?.eventId,
        "notification_open_failed",
        testId,
      );
      throw new Error("notification_open_failed");
    },
  );
  event.waitUntil(
    Promise.allSettled([clicked, opened]).then((results) => {
      // Finish the click diagnostic even when navigation/focus is rejected.
      if (results[1].status === "rejected")
        throw new Error("notification_open_failed");
    }),
  );
});
