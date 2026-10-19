import { test } from "node:test";
import assert from "node:assert/strict";
import {
  needsAttention,
  isFinished,
  uniqueMessages,
  reconcileLatest,
  isActive,
  isWorking,
  hasBackgroundTask,
  stateLabel,
  isUncertain,
  isHistorical,
  inWorkspace,
  dayLabel,
  formatRelative,
} from "./model.ts";
import type { Session } from "./types.ts";

test("notification day labels use local calendar days", () => {
  const now = new Date(2026, 9, 18, 0, 30);
  assert.equal(dayLabel(new Date(2026, 9, 18, 0, 5).toISOString(), now), "今天");
  assert.equal(dayLabel(new Date(2026, 9, 17, 23, 59).toISOString(), now), "昨天");
  assert.match(dayLabel(new Date(2026, 9, 15, 12).toISOString(), now), /10月15日/);
  assert.equal(dayLabel("not a date", now), "更早");
});

test("unknown and alive executions are never inferred completed", () => {
  const session = {
    turnState: "unknown",
    executionState: "alive",
    pending: [],
    metadata: { readAt: "" },
  } as unknown as Session;
  assert.equal(isFinished(session), false);
  assert.equal(needsAttention(session), false);
  session.executionState = "unknown";
  assert.equal(isUncertain(session), true);
  assert.equal(isHistorical(session), false);
  assert.equal(needsAttention(session), false);
  session.turnState = "idle";
  assert.equal(isFinished(session), false);
});
test("completed unread results are history updates, not requests for action", () => {
  const session = {
    turnState: "completed",
    executionState: "alive",
    updatedAt: "2026-10-17T02:00:00Z",
    metadata: { readAt: "2026-10-17T01:00:00Z" },
  } as Session;
  assert.equal(needsAttention(session), false);
  session.metadata.readAt = "2026-10-17T03:00:00Z";
  assert.equal(needsAttention(session), false);
});
test("background tasks remain active without model work or attention counts", () => {
  const session = { executionState: "alive", turnState: "background" } as Session;
  assert.equal(stateLabel(session.turnState), "后台任务中");
  assert.equal(hasBackgroundTask(session), true);
  assert.equal(isActive(session), true);
  assert.equal(isWorking(session), false);
  assert.equal(needsAttention(session), false);
  assert.equal(isFinished(session), false);
  session.executionState = "exited";
  assert.equal(hasBackgroundTask(session), false);
  session.executionState = "unknown";
  assert.equal(hasBackgroundTask(session), false);
  session.executionState = "alive";
  session.parentId = "parent";
  assert.equal(hasBackgroundTask(session), false);
});
test("exited waiting and working states cannot create live work or requests", () => {
  const session = {
    executionState: "exited",
    turnState: "waiting_input",
    pending: [{ id: "stale" }],
  } as Session;
  assert.equal(needsAttention(session), false);
  assert.equal(isActive(session), false);
  session.turnState = "working";
  assert.equal(isWorking(session), false);
  session.executionState = "alive";
  assert.equal(isWorking(session), true);
  assert.equal(needsAttention(session), true);
  session.parentId = "parent";
  assert.equal(isWorking(session), false);
  assert.equal(needsAttention(session), false);
});
test("focused workspaces isolate devices and directory boundaries without hiding all history", () => {
  const preferences = {
    workspaceScope: "focused" as const,
    workspaces: [{ nodeId: "a", path: "/projects/project-alpha/" }],
  };
  const session = {
    nodeId: "a",
    cwd: "/projects/project-alpha",
    executionState: "exited",
  } as Session;
  assert.equal(inWorkspace(session, preferences), true);
  session.cwd += "/subproject";
  assert.equal(inWorkspace(session, preferences), true);
  session.cwd = "/projects/project-alpha-other";
  assert.equal(inWorkspace(session, preferences), false);
  session.cwd = "/projects/project-alpha";
  session.nodeId = "b";
  assert.equal(inWorkspace(session, preferences), false);
  assert.equal(
    inWorkspace(session, { ...preferences, workspaceScope: "all" }),
    true,
  );
  assert.equal(inWorkspace(session, { ...preferences, workspaces: [] }), false);
});
test("history reconciliation preserves IDs and updates content once", () => {
  const items = uniqueMessages([
    { id: "a", role: "assistant", text: "partial", at: "" },
    { id: "b", role: "user", text: "hi", at: "" },
    { id: "a", role: "assistant", text: "complete", at: "" },
  ]);
  assert.equal(items.length, 2);
  assert.equal(items[0].text, "complete");
});
test("native rewrites invalidate stale display messages", () => {
  const old = {
    messages: [{ id: "old", role: "assistant", text: "stale", at: "" }],
    cursor: "old-cursor",
    hasMore: true,
  };
  const latest = {
    messages: [{ id: "new", role: "assistant", text: "compacted", at: "" }],
    cursor: "new-cursor",
    hasMore: false,
  };
  assert.deepEqual(reconcileLatest(old, latest), latest);
  assert.deepEqual(
    reconcileLatest(old, { messages: [], hasMore: false }).messages,
    [],
  );
});
test("latest page replaces changed native tail and retains loaded older pages", () => {
  const message = (id: string, text = id) => ({
    id,
    role: "assistant",
    text,
    at: "",
  });
  const result = reconcileLatest(
    { messages: [message("a"), message("b"), message("c")], hasMore: false },
    { messages: [message("b", "changed")], hasMore: true, cursor: "newer" },
  );
  assert.deepEqual(
    result.messages.map((m) => m.text),
    ["a", "changed"],
  );
  assert.equal(result.hasMore, false);
});

test("session times are relative when recent", () => {
  const now = new Date(2026, 9, 18, 10, 0);
  const at = (h: number, m: number, d = 18) => new Date(2026, 9, d, h, m).toISOString();
  assert.equal(formatRelative(at(9, 59, 18), now), "1 分钟前");
  assert.equal(formatRelative(new Date(now.getTime() - 20000).toISOString(), now), "刚刚");
  assert.equal(formatRelative(at(7, 5), now), "07:05");
  assert.equal(formatRelative(at(21, 4, 17), now), "昨天 21:04");
  assert.match(formatRelative(at(9, 0, 10), now), /10\/10/);
});
