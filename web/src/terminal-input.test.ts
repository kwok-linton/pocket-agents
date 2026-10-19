import { test } from "node:test";
import assert from "node:assert/strict";
import { needsConfirmation } from "./terminal-input.ts";

test("unknown prior delivery permits focus movement but never submission or cancellation", () => {
  for (const key of ["\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D", "\t", "\x1b[Z", "\x1b[B\x1b[B"]) {
    assert.equal(needsConfirmation(key, true), false, JSON.stringify(key));
  }
  for (const key of ["\r", " ", "\x1b", "\x03", "yes\r", "/model\r", "\x1b[B\r"]) {
    assert.equal(needsConfirmation(key, true), true, JSON.stringify(key));
    assert.equal(needsConfirmation(key, false), false);
  }
});
