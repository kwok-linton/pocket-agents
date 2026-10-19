import test from "node:test";
import assert from "node:assert/strict";
import {
  createOverviewMemory,
  updateOverview,
  matchesOverviewSelection,
  overviewHistoryHints,
  sortSessions,
  overviewCounts,
  matchesOverviewFilter,
} from "./overview.ts";
import type { Session } from "./types.ts";

test("returning to overview preserves search, provider, project and pagination without changing launch defaults", () => {
  const memory = createOverviewMemory();
  const selected = {
    ...memory.read(),
    filter: "history",
    provider: "codex",
    query: "checkout",
    node: "node-a",
    projectKey: "project-a",
    showAllProjects: true,
    limit: 80,
  };
  memory.write(selected);
  assert.deepEqual(memory.read(), selected);
  assert.equal(createOverviewMemory().read().filter, "active");
  assert.equal(updateOverview(selected, { limit: 120 }).limit, 120);
  assert.equal(updateOverview(selected, { query: "payment" }).limit, 40);
});

test("history hints count only search matches on the selected device and provider", () => {
  const view = {
    ...createOverviewMemory().read(),
    query: "checkout",
    node: "node-a",
    provider: "codex",
  };
  const session = (title: string, nodeId = "node-a", provider = "codex") =>
    ({
      title,
      nodeId,
      provider,
      cwd: "/project",
      executionState: "exited",
      metadata: {},
    }) as Session;
  const selected = [
    session("Fix checkout"),
    session("Fix payment"),
    session("Fix checkout", "node-b"),
    session("Fix checkout", "node-a", "claude"),
  ].filter((s) => matchesOverviewSelection(s, view));
  assert.equal(selected.length, 1);
  assert.deepEqual(overviewHistoryHints(selected, view.query), [
    { provider: "codex", active: 0, uncertain: 0, history: 1 },
  ]);
});

test("active view lists sessions waiting on the user before busier ones", () => {
  const s = (title: string, turnState: string, updatedAt: string, readAt = "2030-01-01T00:00:00Z") =>
    ({ title, turnState, updatedAt, executionState: "alive", metadata: { readAt } }) as Session;
  const list = [
    s("working", "working", "2026-10-18T10:05:00Z"),
    s("approval", "waiting_approval", "2026-10-18T09:00:00Z"),
    s("failed", "error", "2026-10-18T09:30:00Z"),
    s("unread done", "completed", "2026-10-18T09:40:00Z", "2026-10-18T09:00:00Z"),
    s("read done", "completed", "2026-10-18T10:00:00Z"),
  ];
  assert.deepEqual(sortSessions(list, true).map((x) => x.title), ["approval", "failed", "unread done", "working", "read done"]);
  assert.deepEqual(sortSessions(list, false).map((x) => x.title), ["working", "read done", "unread done", "failed", "approval"]);
  assert.equal(list[0].title, "working", "input order is not mutated");
});

test("background counts and filters follow live main sessions and state changes", () => {
  const session = (id: string, turnState: string, executionState = "alive") =>
    ({ id, turnState, executionState, metadata: {} }) as Session;
  const background = session("background", "background");
  const sessions = [background, session("working", "working"), session("approval", "waiting_approval"),
    session("exited", "background", "exited"), session("unknown", "background", "unknown"),
    { ...session("child", "background"), parentId: "parent" }];
  assert.deepEqual(overviewCounts(sessions), {active: 3, attention: 1, working: 1, background: 1, history: 1});
  const ids = (filter: string) => sessions.filter(s => matchesOverviewFilter(s, filter)).map(s => s.id);
  assert.deepEqual(ids("background"), ["background"]);
  assert.deepEqual(ids("active"), ["background", "working", "approval"]);
  assert.deepEqual(ids("working"), ["working"]);
  assert.deepEqual(ids("attention"), ["approval"]);
  background.turnState = "working";
  assert.deepEqual(ids("background"), []);
  assert.equal(overviewCounts(sessions).working, 2);
  background.turnState = "completed";
  assert.deepEqual(ids("finished"), ["background"]);
  assert.equal(overviewCounts(sessions).working, 1);
  assert.equal(overviewCounts(sessions).background, 0);
});

test("background counts respect the same selection as the session list", () => {
  const view = {...createOverviewMemory().read(), provider: "claude", node: "a", query: "monitor"};
  const s = {title: "Monitor", provider: "claude", nodeId: "a", cwd: "/project", executionState: "alive", turnState: "background"} as Session;
  const sessions = [s, {...s, nodeId: "b"}, {...s, provider: "codex"}, {...s, title: "Build"}];
  assert.equal(overviewCounts(sessions.filter(s => matchesOverviewSelection(s, view))).background, 1);
});
