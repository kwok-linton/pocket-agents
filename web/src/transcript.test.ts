import test from "node:test";
import assert from "node:assert/strict";
import { transcriptGroups, internalLabel } from "./transcript.ts";
import type { Message } from "./types.ts";
test("internal runs collapse without hiding or relabelling human and assistant messages", () => {
  const m = (role: string, kind = "message") =>
    ({ id: role + kind, role, kind, text: "unchanged", at: "" }) as Message;
  const rows = [
    m("user"),
    m("system", "background_notification"),
    m("tool"),
    m("assistant"),
    m("system", "compaction"),
    m("user"),
  ];
  const groups = transcriptGroups(rows);
  assert.deepEqual(
    groups.map((g) => [g.internal, g.messages.length]),
    [
      [false, 1],
      [true, 2],
      [false, 1],
      [true, 1],
      [false, 1],
    ],
  );
  assert.deepEqual(
    groups.flatMap((g) => g.messages),
    rows,
  );
  assert.equal(internalLabel(rows[1]), "后台任务通知");
});

test("explicit scheduled task folds replies until a genuine user message; unknown page starts remain visible", () => {
  const m = (role: string, kind = "message") =>
    ({ id: role + kind, role, kind, text: "original", at: "" }) as Message;
  const rows = [
    m("assistant"),
    m("system", "scheduled_trigger"),
    m("assistant"),
    m("tool"),
    m("assistant"),
    m("user"),
    m("assistant"),
  ];
  assert.deepEqual(
    transcriptGroups(rows).map((g) => [g.internal, g.messages.length]),
    [
      [false, 1],
      [true, 4],
      [false, 1],
      [false, 1],
    ],
  );
});
