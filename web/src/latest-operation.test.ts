import { test } from "node:test";
import assert from "node:assert/strict";
import { latestOperation } from "./latest-operation.ts";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

test("changing device or choosing a path keeps late browse results and errors out", async () => {
  for (const failure of [false, true]) {
    const operation = latestOperation();
    const request = deferred<string>();
    const state = { path: "/old", error: "", picking: true };
    let settled = 0;
    const completion = operation.run(() => request.promise, {
      success: (value) => {
        state.path = value;
      },
      error: () => {
        state.error = "old error";
      },
      settled: () => {
        state.picking = false;
        settled++;
      },
    });
    assert.equal(operation.pending, true, "submit is blocked synchronously");
    operation.invalidate();
    state.path = "/chosen";
    state.picking = false;
    if (failure) request.reject(new Error("old device unavailable"));
    else request.resolve("/old/device/result");
    await completion;
    assert.deepEqual(state, { path: "/chosen", error: "", picking: false });
    assert.equal(settled, 0, "old finally cannot update the current selection");
    assert.equal(operation.pending, false);
  }
});

test("overlapping picker requests preserve the latest choice and submission guard", async () => {
  for (const staleFails of [false, true]) {
    const operation = latestOperation();
    const old = deferred<string>();
    const current = deferred<string>();
    const calls: string[] = [];
    const callbacks = (label: string) => ({
      success: (value: string) => {
        calls.push(`${label}:${value}`);
      },
      error: () => {
        calls.push(`${label}:error`);
      },
      settled: () => {
        calls.push(`${label}:settled`);
      },
    });
    const first = operation.run(() => old.promise, callbacks("old"));
    const second = operation.run(() => current.promise, callbacks("current"));
    if (staleFails) old.reject(new Error("old failure"));
    else old.resolve("/stale");
    await first;
    assert.deepEqual(calls, []);
    assert.equal(operation.pending, true, "old finally cannot enable submit");
    current.resolve("/chosen");
    await second;
    assert.deepEqual(calls, ["current:/chosen", "current:settled"]);
    assert.equal(operation.pending, false);
  }
});

test("a current browse failure is shown and releases the submission guard", async () => {
  const operation = latestOperation();
  const calls: string[] = [];
  await operation.run(() => Promise.reject(new Error("current failure")), {
    success: () => {
      calls.push("unexpected success");
    },
    error: (error) => {
      calls.push((error as Error).message);
    },
    settled: () => {
      calls.push("settled");
    },
  });
  assert.deepEqual(calls, ["current failure", "settled"]);
  assert.equal(operation.pending, false);
});

test("a response arriving after the chosen directory resolves cannot overwrite it", async () => {
  const operation = latestOperation();
  const old = deferred<string>();
  const current = deferred<string>();
  let path = "/initial";
  const callbacks = {
    success: (value: string) => {
      path = value;
    },
    error: () => {
      assert.fail("unexpected error");
    },
    settled: () => {},
  };
  const first = operation.run(() => old.promise, callbacks);
  const second = operation.run(() => current.promise, callbacks);
  current.resolve("/chosen");
  await second;
  old.resolve("/stale");
  await first;
  assert.equal(path, "/chosen");
  assert.equal(operation.pending, false);
});
