import { test } from "node:test";
import assert from "node:assert/strict";
import { eventConnection } from "./event-connection.ts";

function fixture() {
  const page = Object.assign(new EventTarget(), { visibilityState: "visible" });
  const events = new EventTarget();
  let online = true, cursor = 0;
  const states: boolean[] = [], messages: string[] = [];
  const sockets: Array<{
    url: string; readyState: number; closed: boolean;
    onopen: (() => void) | null; onclose: (() => void) | null;
    onerror: (() => void) | null; onmessage: ((event: {data: string}) => void) | null;
    close: () => void;
  }> = [];
  const connection = eventConnection({
    url: () => `/events?after=${cursor}`,
    connected: value => states.push(value),
    message: event => { messages.push(event.data); cursor++; },
  }, {
    page: page as unknown as Document, events: events as Window,
    online: () => online,
    socket: url => {
      const socket = {
        url, readyState: 0, closed: false,
        onopen: null, onclose: null, onerror: null, onmessage: null,
        close() { this.closed = true; this.readyState = 3; },
      };
      sockets.push(socket);
      return socket as unknown as WebSocket;
    },
  });
  return { connection, sockets, messages, states, page, events,
    open() { const socket = sockets.at(-1)!; socket.readyState = 1; socket.onopen?.(); },
    visibility(value: string) { page.visibilityState = value; page.dispatchEvent(new Event("visibilitychange")); },
    network(value: boolean) { online = value; events.dispatchEvent(new Event(value ? "online" : "offline")); },
  };
}

test("lock and resume replace a stale OPEN socket and retain the event cursor", context => {
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const f = fixture();
  f.open();
  f.sockets[0].onmessage?.({data: "event"});
  const staleMessage = f.sockets[0].onmessage;
  const staleClose = f.sockets[0].onclose;
  f.visibility("hidden");
  assert.equal(f.sockets[0].closed, true);
  assert.equal(f.connection.isOpen(), false);
  context.mock.timers.tick(60000);
  assert.equal(f.sockets.length, 1);
  f.visibility("visible");
  assert.equal(f.sockets.length, 2);
  assert.equal(f.sockets[1].url, "/events?after=1");
  f.open();
  staleMessage?.({data: "stale"}); staleClose?.();
  assert.deepEqual(f.messages, ["event"]);
  assert.equal(f.connection.isOpen(), true);
  f.connection.stop();
  f.events.dispatchEvent(new Event("online"));
  context.mock.timers.tick(60000);
  assert.equal(f.sockets.length, 2);
});

test("network restoration bypasses retry delay, hanging connects time out, retries back off", context => {
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const f = fixture();
  context.mock.timers.tick(12000);
  assert.equal(f.sockets[0].closed, true);
  context.mock.timers.tick(1000);
  assert.equal(f.sockets.length, 2);
  f.sockets[1].onerror?.();
  context.mock.timers.tick(1999);
  assert.equal(f.sockets.length, 2);
  context.mock.timers.tick(1);
  assert.equal(f.sockets.length, 3);
  f.network(false);
  context.mock.timers.tick(60000);
  assert.equal(f.sockets.length, 3);
  f.network(true);
  assert.equal(f.sockets.length, 4);
  f.open();
  assert.equal(f.states.at(-1), true);
  // A network change can emit online even while the old socket says OPEN.
  f.network(true);
  assert.equal(f.sockets[3].closed, true);
  assert.equal(f.sockets.length, 5);
  f.connection.stop();
});

test("back-forward cache restore reconnects; an ordinary pageshow keeps the new connection", context => {
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const f = fixture();
  f.open();
  f.events.dispatchEvent(Object.assign(new Event("pageshow"), {persisted: false}));
  assert.equal(f.sockets.length, 1);
  f.events.dispatchEvent(new Event("pagehide"));
  assert.equal(f.sockets[0].closed, true);
  f.events.dispatchEvent(Object.assign(new Event("pageshow"), {persisted: true}));
  assert.equal(f.sockets.length, 2);
  f.connection.stop();
});

test("a silently dead OPEN connection is replaced without needing an offline event", context => {
  context.mock.timers.enable({apis: ["setTimeout"]});
  const f = fixture();
  f.open();
  context.mock.timers.tick(30000);
  f.sockets[0].onmessage?.({data: '{"type":"heartbeat"}'});
  context.mock.timers.tick(44000);
  assert.equal(f.sockets[0].closed, false);
  context.mock.timers.tick(1000);
  assert.equal(f.sockets[0].closed, true);
  context.mock.timers.tick(1000);
  assert.equal(f.sockets.length, 2);
  f.open();
  f.events.dispatchEvent(new Event("pocket:retry-connection"));
  assert.equal(f.sockets[1].closed, true);
  assert.equal(f.sockets.length, 3);
  f.connection.stop();
});
