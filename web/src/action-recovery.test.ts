import { test } from "node:test";
import assert from "node:assert/strict";
import { watchActionResults } from "./action-recovery.ts";

test("late native receipts are checked on reconnect without mounting a status card or resending", async context => {
  context.mock.timers.enable({apis: ["setTimeout", "Date"], now: 10000});
  const page = Object.assign(new EventTarget(), {visibilityState: "visible"});
  const events = new EventTarget();
  let reads = 0, accepted = false, received = false;
  const stop = watchActionResults(async () => { reads++; accepted = received; }, {
    page: page as unknown as Document, events: events as Window, online: () => true,
  });
  const flush = () => new Promise<void>(resolve => setImmediate(resolve));
  context.mock.timers.tick(0); await flush();
  assert.equal(reads, 1); assert.equal(accepted, false);
  page.visibilityState = "hidden"; page.dispatchEvent(new Event("visibilitychange"));
  context.mock.timers.tick(60000); await flush(); assert.equal(reads, 1);
  // The user handles the native terminal while the phone is asleep.
  received = true;
  page.visibilityState = "visible"; events.dispatchEvent(new Event("pocket:connection-restored"));
  context.mock.timers.tick(0); await flush();
  assert.equal(reads, 2); assert.equal(accepted, true);
  stop(); context.mock.timers.tick(60000); assert.equal(reads, 2);
});
