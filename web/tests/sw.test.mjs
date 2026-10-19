import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";
import { createServer } from "node:http";
import { once } from "node:events";
import { gzipSync } from "node:zlib";

const source = await readFile(
  new URL("../public/sw.js", import.meta.url),
  "utf8",
);
const origin = "https://pocket.example.test";
const diagnosticsCache = "pocket-push-diagnostics-v1";
const diagnosticsKey = `${origin}/.pocket-push-diagnostics`;

function harness(options = {}) {
  const handlers = new Map();
  const saved = options.saved || new Map();
  const notifications = [];
  const navigations = [];
  const opens = [];
  const focuses = [];
  const clientQueries = [];
  const navigatedClient = {
    async focus() {
      focuses.push("navigated");
      if (options.failFocus) throw new Error("secret focus error");
      return this;
    },
  };
  const windowClient = {
    id: "controlled-window",
    type: "window",
    url: `${origin}/settings`,
    async navigate(url) {
      navigations.push(url);
      if (options.failNavigate) throw new Error("secret navigation error");
      return options.nullNavigate ? null : navigatedClient;
    },
    async focus() {
      focuses.push("original");
      throw new Error("the pre-navigation client must not be focused");
    },
  };
  const controlled = options.controlled || [windowClient];
  const cache = {
    async match(key) {
      const found = saved.get(key);
      return found?.clone();
    },
    async put(key, response) {
      if (options.failPut) throw new Error("secret quota error");
      if (options.putBarrier) await options.putBarrier;
      saved.set(key, response.clone());
    },
  };
  vm.runInNewContext(source, {
    URL,
    Response,
    caches: {
      async open(name) {
        if (name === "pocket-preferences-v1")
          return { async match(key) { return key === "/.pocket-language" && options.language ? new Response(options.language) : undefined; } };
        assert.equal(name, diagnosticsCache);
        if (options.failOpen) throw new Error("secret storage error");
        return cache;
      },
    },
    self: {
      location: { origin },
      addEventListener(type, callback) {
        handlers.set(type, callback);
      },
      registration: {
        showNotification(title, notification) {
          notifications.push({ title, ...notification });
          return (
            options.showNotification?.(title, notification) || Promise.resolve()
          );
        },
      },
      clients: {
        async matchAll(query) {
          clientQueries.push(query);
          if (query.includeUncontrolled === false) return controlled;
          return [windowClient];
        },
        async openWindow(url) {
          opens.push(url);
          if (options.failOpenWindow) throw new Error("secret open error");
          return options.nullOpenWindow ? null : windowClient;
        },
      },
    },
  });
  function dispatch(type, fields) {
    const lifetimes = [];
    handlers.get(type)({
      ...fields,
      waitUntil(promise) {
        assert.equal(typeof promise?.then, "function");
        lifetimes.push(promise);
      },
    });
    assert.equal(
      lifetimes.length,
      type === "fetch" ? 0 : 1,
      `${type} must register its work synchronously`,
    );
    return Promise.all(lifetimes);
  }
  return {
    notifications,
    navigations,
    opens,
    focuses,
    clientQueries,
    saved,
    dispatch,
    push(payload) {
      return dispatch("push", { data: { json: () => payload } });
    },
    async read(fields = {}) {
      const replies = [];
      await dispatch("message", {
        data: { type: "pocket:push-diagnostics" },
        origin,
        source: windowClient,
        ports: [{ postMessage: (value) => replies.push(value) }],
        ...fields,
      });
      // Values originate in the service worker VM, so compare JSON data only.
      return JSON.parse(JSON.stringify(replies));
    },
  };
}

function deferred() {
  let resolve;
  const promise = new Promise((res) => (resolve = res));
  return { promise, resolve };
}

function assertPrivateRecords(records) {
  for (const record of records) {
    assert.deepEqual(Object.keys(record).sort(), ["at", "eventId", "stage"]);
    assert.ok(Number.isFinite(Date.parse(record.at)));
    assert.ok(record.eventId === null || Number.isSafeInteger(record.eventId));
  }
}

test("push waitUntil includes notification and durable completion diagnostic", async () => {
  const pending = deferred();
  const h = harness({ showNotification: () => pending.promise });
  let finished = false;
  const done = h
    .push({
      eventId: 17,
      title: "PRIVATE TITLE",
      body: "PRIVATE BODY",
      tag: "session:completed",
      url: "/session/test-session",
      endpoint: "PRIVATE ENDPOINT",
    })
    .then(() => (finished = true));
  await new Promise(setImmediate);
  assert.equal(h.notifications.length, 1);
  assert.equal(finished, false);
  assert.equal(h.notifications[0].renotify, true);
  assert.equal(h.notifications[0].tag, "session:completed");
  pending.resolve();
  await done;
  const [{ records }] = await h.read();
  assert.deepEqual(
    records.map((r) => r.stage),
    ["push_received", "show_notification_resolved"],
  );
  assert.deepEqual(
    records.map((r) => r.eventId),
    [17, 17],
  );
  assertPrivateRecords(records);
  assert.doesNotMatch(
    JSON.stringify(records),
    /PRIVATE|session:completed|test-session/,
  );
  assert.equal(h.notifications[0].data.eventId, 17);
});

test("notification rejection is durable with a fixed label and still rejects waitUntil", async () => {
  const h = harness({
    showNotification: () =>
      Promise.reject(new Error("SECRET endpoint and key")),
  });
  await assert.rejects(
    h.push({ eventId: 18 }),
    /^Error: show_notification_rejected$/,
  );
  const [{ records }] = await h.read();
  assert.equal(records.at(-1).stage, "show_notification_rejected");
  assert.doesNotMatch(JSON.stringify(records), /SECRET|endpoint|key/);
  assertPrivateRecords(records);
});

test("invalid push payloads still display a safe fallback", async () => {
  for (const payload of [
    null,
    42,
    "untrusted",
    [],
    { sessionId: "\ud800" },
    { url: {}, title: {}, body: {}, eventId: "secret" },
  ]) {
    const h = harness();
    await h.push(payload);
    assert.equal(h.notifications.length, 1);
    assert.equal(h.notifications[0].title, "Pocket Agents 有新进展");
    assert.equal(h.notifications[0].data.target, "/notifications");
    const [{ records }] = await h.read();
    assert.equal(records.length, 2);
    assert.equal(records[0].eventId, null);
  }
  const h = harness();
  await h.dispatch("push", {
    data: {
      json() {
        throw new Error("bad JSON secret");
      },
    },
  });
  assert.equal(h.notifications.length, 1);
  assert.equal((await h.read())[0].records.length, 2);
});

test("diagnostic storage failures never prevent notification display", async () => {
  for (const failure of [{ failOpen: true }, { failPut: true }]) {
    const h = harness(failure);
    await h.push({ eventId: 19 });
    assert.equal(h.notifications.length, 1);
    assert.deepEqual(await h.read(), [{ records: [] }]);
  }
});

test("concurrent diagnostics persist at most 20 entries across worker restarts", async () => {
  const h = harness();
  await Promise.all(
    Array.from({ length: 25 }, (_, i) => h.push({ eventId: i + 1 })),
  );
  const restarted = harness({ saved: h.saved });
  const [{ records }] = await restarted.read();
  assert.equal(records.length, 20);
  assertPrivateRecords(records);
  assert.equal(records.at(-1).eventId, 25);
  assert.equal(records.at(-1).stage, "show_notification_resolved");
});

test("click records event ID while opening the safe native session route", async () => {
  const h = harness();
  let closed = false;
  await h.dispatch("notificationclick", {
    notification: {
      data: { target: "/session/test-session", eventId: 20 },
      close() {
        closed = true;
      },
    },
  });
  assert.equal(closed, true);
  assert.deepEqual(h.navigations, ["/session/test-session"]);
  assert.deepEqual(h.focuses, ["navigated"]);
  assert.deepEqual(h.opens, []);
  const [{ records }] = await h.read();
  assert.equal(records[0].stage, "notification_clicked");
  assert.equal(records[0].eventId, 20);
  assert.equal(records[1].stage, "notification_opened");
  assertPrivateRecords(records);
});

test("diagnostics respond only to this worker's controlled same-origin window", async () => {
  const h = harness();
  await h.push({ eventId: 21 });
  assert.deepEqual(
    await h.read({ origin: "https://external.example.test" }),
    [],
  );
  assert.deepEqual(
    await h.read({ source: { id: "uncontrolled-window", type: "window" } }),
    [],
  );
  assert.deepEqual(
    await h.read({ source: { id: "controlled-window", type: "worker" } }),
    [],
  );
  const externalClient = harness({
    controlled: [
      {
        id: "controlled-window",
        type: "window",
        url: "https://external.example.test/",
      },
    ],
  });
  assert.deepEqual(await externalClient.read(), []);
  assert.equal((await h.read())[0].records.length, 2);
});

test("failed navigation and open retain diagnostics without delaying open on storage", async () => {
  const saving = deferred();
  const h = harness({
    failNavigate: true,
    failOpenWindow: true,
    putBarrier: saving.promise,
  });
  let finished = false;
  const done = h.dispatch("notificationclick", {
    notification: {
      data: { target: "/session/test-session", eventId: 24 },
      close() {},
    },
  });
  const failed = assert
    .rejects(done, /^Error: notification_open_failed$/)
    .then(() => (finished = true));
  await new Promise(setImmediate);
  assert.equal(finished, false);
  assert.deepEqual(h.opens, ["/session/test-session"]);
  saving.resolve();
  await failed;
  const [{ records }] = await h.read();
  assert.equal(records[0].stage, "notification_clicked");
  assert.equal(records[0].eventId, 24);
  assert.equal(records[1].stage, "notification_open_failed");
  assert.doesNotMatch(JSON.stringify(records), /secret|test-session/);
});

test("no controlled window opens the session without trying an uncontrolled client", async () => {
  const h = harness({ controlled: [] });
  await h.dispatch("notificationclick", {
    notification: {
      data: { target: "/session/target", eventId: 30 },
      close() {},
    },
  });
  assert.deepEqual(h.navigations, []);
  assert.deepEqual(h.opens, ["/session/target"]);
  assert.equal(h.clientQueries[0].includeUncontrolled, false);
  const [{ records }] = await harness({ saved: h.saved }).read();
  assert.equal(records.at(-1).stage, "notification_opened");
});

test("navigation rejection, null result, and focus failure fall back to openWindow", async () => {
  for (const failure of [
    { failNavigate: true },
    { nullNavigate: true },
    { failFocus: true },
  ]) {
    const h = harness(failure);
    await h.dispatch("notificationclick", {
      notification: {
        data: { target: "/session/target", eventId: 31 },
        close() {},
      },
    });
    assert.deepEqual(h.opens, ["/session/target"]);
    const [{ records }] = await h.read();
    assert.equal(records.at(-1).stage, "notification_opened");
  }
});

test("null openWindow result is recorded as failure rather than a successful jump", async () => {
  const h = harness({ nullNavigate: true, nullOpenWindow: true });
  await assert.rejects(
    h.dispatch("notificationclick", {
      notification: {
        data: { target: "/session/target", eventId: 32 },
        close() {},
      },
    }),
    /^Error: notification_open_failed$/,
  );
  const [{ records }] = await h.read();
  assert.equal(records.at(-1).stage, "notification_open_failed");
});

test("malformed cached data is replaced and extra fields are never returned", async () => {
  const saved = new Map([[diagnosticsKey, new Response("not JSON")]]);
  const h = harness({ saved });
  await h.push({ eventId: 22 });
  assert.equal((await h.read())[0].records.length, 2);
  saved.set(
    diagnosticsKey,
    new Response(
      JSON.stringify([
        {
          eventId: "SECRET",
          stage: "push_received",
          at: "2026-10-17T00:00:00Z",
          body: "PRIVATE",
        },
        { eventId: 23, stage: "SECRET STAGE", at: "2026-10-17T00:00:00Z" },
      ]),
    ),
  );
  const [{ records }] = await h.read();
  assert.equal(records.length, 1);
  assert.equal(records[0].eventId, null);
  assertPrivateRecords(records);
  assert.doesNotMatch(JSON.stringify(records), /SECRET|PRIVATE/);
});

test("update checks bypass shell cache even for index.html", async () => {
  for (const [url, cache] of [
    [origin + "/index.html", "no-store"],
    [origin + "/index.html?pocket_update_check=unique", "default"],
  ]) {
    const h = harness();
    let intercepted = false;
    await h.dispatch("fetch", {
      request: {url, method:"GET", mode:"cors", cache},
      respondWith() {intercepted = true;},
    });
    assert.equal(intercepted, false, "version check must reach network, not cached shell");
  }
});

test("push text follows the page language; Chinese keeps the server text", async () => {
  const english = harness({ language: "en" });
  await english.push({ eventId: 3, title: "Pocket Agents", body: "本轮已结束，点击查看", tag: "s1:waiting_approval", url: "/session/s1" });
  assert.equal(english.notifications[0].body, "A native request is waiting for approval");
  const chinese = harness({ language: "zh" });
  await chinese.push({ eventId: 4, title: "Pocket Agents", body: "本轮已结束，点击查看", tag: "s1:completed", url: "/session/s1" });
  assert.equal(chinese.notifications[0].body, "本轮已结束，点击查看");
  const unknown = harness({ language: "en" });
  await unknown.push({ eventId: 5, tag: "s1:something_new" });
  assert.equal(unknown.notifications[0].body, "A session needs your attention");
});

test("remote test receipt and click correlate without exposing payload data", async () => {
  const worker = harness({language:"en"});
  const testId = "12345678-1234-1234-1234-123456789abc";
  await worker.push({testId, tag:testId+":test", url:"/settings", secret:"not diagnostic data"});
  assert.equal(worker.notifications[0].body,"Test notification arrived. Tap to review notification settings.");
  assert.equal(worker.notifications[0].data.target,"/settings");
  await worker.dispatch("notificationclick", {notification:{data:worker.notifications[0].data,close(){}}});
  assert.deepEqual(worker.navigations,["/settings"]);
  const [{records}] = await worker.read();
  assert.deepEqual(records.map(r=>r.stage),["push_received","show_notification_resolved","notification_clicked","notification_opened"]);
  for (const record of records) {
    assert.equal(record.testId,testId);
    assert.deepEqual(Object.keys(record).sort(),["at","eventId","stage","testId"]);
  }
});

test("installed navigation renders cached UI without waiting for Tailscale; API remains live", async () => {
 const handlers=new Map();let networkCalls=0;let response;
 const source = await readFile(new URL("../public/sw.js",import.meta.url),"utf8");
 vm.runInNewContext(source,{
  URL,Response,
  self:{location:{origin},addEventListener(type,handler){handlers.set(type,handler);}},
  caches:{async open(){return {async match(path){assert.equal(path,"/index.html");return new Response("cached shell");}};}},
  fetch(){networkCalls++;return new Promise(()=>{});},
 });
 handlers.get("fetch")({request:{url:origin+"/settings",method:"GET",mode:"navigate"},respondWith(value){response=value;}});
 assert.equal(await (await response).text(),"cached shell");assert.equal(networkCalls,0);
 for(const path of ["/api/bootstrap","/api/sessions","/index.html?pocket_update_check=test"]){
  handlers.get("fetch")({request:{url:origin+path,method:"GET",mode:"navigate"},respondWith(){assert.fail("must bypass SW cache");}});
 }
});

function navigationHarness({ cached, storageFails = false } = {}) {
  const handlers = new Map();
  let networkCalls = 0;
  vm.runInNewContext(source, {
    URL, Response, Headers,
    self: { location: { origin }, addEventListener(type, handler) { handlers.set(type, handler); } },
    caches: { async open() {
      if (storageFails) throw new Error("cache unavailable");
      return { async match() { return cached?.clone(); } };
    } },
    fetch() { networkCalls++; return Promise.resolve(new Response("network shell")); },
  });
  return {
    get networkCalls() { return networkCalls; },
    navigate(path) {
      let response;
      handlers.get("fetch")({
        request: { url: origin + path, method: "GET", mode: "navigate", redirect: "manual" },
        respondWith(value) { response = value; },
      });
      return response;
    },
  };
}

test("old redirected shell is safe for Safari navigation without a network request", async (t) => {
  const body = gzipSync("cached shell from old release");
  const server = createServer((req, res) => {
    if (req.url === "/index.html") {
      res.writeHead(301, { Location: "/" }).end();
    } else {
      res.writeHead(200, {
        "Content-Type": "text/html; charset=utf-8",
        "Content-Encoding": "gzip",
        "Content-Length": body.length,
        "Content-Security-Policy": "default-src 'self'",
      }).end(body);
    }
  }).listen(0, "127.0.0.1");
  t.after(() => { server.closeAllConnections(); server.close(); });
  await once(server, "listening");
  const cached = await fetch(`http://127.0.0.1:${server.address().port}/index.html`);
  assert.equal(cached.redirected, true, "reproduce the old server's cached redirect");
  const worker = navigationHarness({ cached });
  for (const path of ["/", "/settings", "/session/existing"]) {
    const response = await worker.navigate(path);
    assert.equal(response.status, 200);
    assert.equal(response.redirected, false, "manual navigations must not receive a redirected response");
    assert.equal(response.headers.get("Content-Type"), "text/html; charset=utf-8");
    assert.equal(response.headers.get("Content-Security-Policy"), "default-src 'self'");
    assert.equal(response.headers.get("Content-Encoding"), null, "body is already decoded");
    assert.equal(response.headers.get("Content-Length"), null);
    assert.equal(await response.text(), "cached shell from old release");
  }
  assert.equal(worker.networkCalls, 0);
});

test("navigation falls back to the network when shell cache is missing or unavailable", async () => {
  for (const storageFails of [false, true]) {
    const worker = navigationHarness({ storageFails });
    assert.equal(await (await worker.navigate("/settings")).text(), "network shell");
    assert.equal(worker.networkCalls, 1);
  }
});
