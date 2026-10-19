import { test } from "node:test";
import assert from "node:assert/strict";
import { terminalConnection, type TerminalData } from "./terminal-connection.ts";
const flush = () => new Promise<void>(resolve => setImmediate(resolve));
const environment = () => {
  const page = Object.assign(new EventTarget(), { visibilityState: "visible" });
  const events = new EventTarget();
  return { page: page as unknown as Document, events: events as Window, online: () => true };
};
const initial: TerminalData = { terminalId: "first", state: "connected", cursor: 10, requestId: "token" };

test("disconnected reads stop polling and only explicit reconnect opens", async context => {
  context.mock.timers.enable({ apis: ["setTimeout", "Date"], now: 10000 });
  let reads = 0, opens = 0; const shown: TerminalData[] = [];
  const controller = terminalConnection(initial, {
    read: async data => { reads++; return { ...data, state: "disconnected" }; },
    open: async () => { opens++; return { ...initial, terminalId: "second" }; },
    changed: data => shown.push(data),
  }, environment());
  context.mock.timers.tick(0); await flush();
  context.mock.timers.tick(60000); await flush();
  assert.equal(reads, 1); assert.equal(opens, 0);
  assert.equal(shown.at(-1)?.state, "disconnected");
  await controller.reconnect(); assert.equal(opens, 1);
  assert.equal(shown.at(-1)?.terminalId, "second");
  controller.stop();
});

test("old reads and key replies cannot revive or overwrite a new connection", async context => {
  context.mock.timers.enable({ apis: ["setTimeout", "Date"], now: 10000 });
  let finish!: (data: TerminalData) => void; const shown: TerminalData[] = [];
  const controller = terminalConnection(initial, {
    read: () => new Promise(resolve => { finish = resolve; }),
    open: async () => ({ terminalId: "second", state: "connected", cursor: 1 }),
    changed: data => shown.push(data),
  }, environment());
  const keyReply = controller.capture();
  context.mock.timers.tick(0); await flush();
  await controller.reconnect();
  finish({ ...initial, cursor: 100, screen: "old read" }); await flush();
  keyReply({ ...initial, cursor: 101, screen: "old key" });
  assert.equal(shown.at(-1)?.terminalId, "second");
  const count = shown.length;
  controller.stop(); keyReply(initial);
  assert.equal(shown.length, count);
});

test("visibility pauses reads, cursor cannot go backwards, and disconnect cannot revive", async context => {
  context.mock.timers.enable({ apis: ["setTimeout", "Date"], now: 10000 });
  const env = environment(); let reads = 0; let shown = initial;
  const controller = terminalConnection(initial, {
    read: async data => { reads++; return data; }, open: async () => initial,
    changed: data => { shown = data; },
  }, env);
  context.mock.timers.tick(0); await flush(); assert.equal(reads, 1);
  Object.assign(env.page, { visibilityState: "hidden" }); env.page.dispatchEvent(new Event("visibilitychange"));
  context.mock.timers.tick(60000); await flush(); assert.equal(reads, 1);
  Object.assign(env.page, { visibilityState: "visible" }); env.page.dispatchEvent(new Event("visibilitychange"));
  context.mock.timers.tick(0); await flush(); assert.equal(reads, 2);
  controller.accept({ ...initial, cursor: 9, screen: "stale" }); assert.equal(shown.screen, undefined);
  controller.accept({ ...initial, state: "disconnected" });
  controller.accept({ ...initial, cursor: 11 }); assert.equal(shown.state, "disconnected");
  controller.stop();
});

test("closing during reconnect discards the late response", async () => {
  let finish!: (data: TerminalData) => void; const shown: TerminalData[] = [];
  const controller = terminalConnection({ ...initial, state: "disconnected" }, {
    read: async () => { throw new Error("must not read"); },
    open: () => new Promise(resolve => { finish = resolve; }),
    changed: data => shown.push(data),
  }, environment());
  const pending = controller.reconnect(); controller.stop(); const count = shown.length;
  finish({ ...initial, terminalId: "late" }); await pending;
  assert.equal(shown.length, count);
});
