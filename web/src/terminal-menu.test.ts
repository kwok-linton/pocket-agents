import { test } from "node:test";
import assert from "node:assert/strict";
import { moveKeys, terminalMenu } from "./terminal-menu.ts";

const permission = [
  "Bash command",
  "",
  "  rm -rf build",
  "",
  "Do you want to proceed?",
  "❯ 1. Yes",
  "  2. Yes, and don't ask again for rm commands in this project",
  "  3. No, and tell Claude what to do differently (esc)",
  "",
].join("\n");

test("recognises a native permission menu and the highlighted option", () => {
  const menu = terminalMenu(permission)!;
  assert.deepEqual(menu.options.map((o) => o.number), [1, 2, 3]);
  assert.equal(menu.options[2].label, "No, and tell Claude what to do differently (esc)");
  assert.equal(menu.selected, 0);
  assert.equal(moveKeys(menu, 2), "\x1b[B\x1b[B");
  assert.equal(moveKeys(menu, 0), "");
});

test("checkbox menus, highlight below the first row, and moving up", () => {
  const menu = terminalMenu(["Choose apps", "  1. [✓] Mail", "❯ 2. [ ] Notes", "  3. [ ] Photos", "", "Enter to toggle · Esc to cancel"].join("\n"))!;
  assert.deepEqual(menu.options.map((o) => o.checked), [true, false, false]);
  assert.equal(menu.selected, 1);
  assert.equal(moveKeys(menu, 0), "\x1b[A");
});

test("ordinary output and code are not menus", () => {
  assert.equal(terminalMenu("1. First paragraph of an answer\nsome prose\n\n❯ "), null);
  assert.equal(terminalMenu("Steps:\n2. build\n3. test"), null, "a list must start at 1");
  assert.equal(terminalMenu(["❯ 1. Yes", "  2. No", ...Array(12).fill("log line")].join("\n")), null, "a menu must be near the bottom");
  assert.equal(terminalMenu(undefined), null);
});

// Claude 2.1.302 has an unnumbered Submit row before a separated numbered
// chat option. Every row, including Submit, occupies one native focus step.
const multi = [
  "←  ☐ 排查对象  ✔ Submit  →",
  "要排查哪一件？",
  "",
  "  1. [ ] 检查日志",
  "        只读检查日志中的异常",
  "❯ 2. [✔] 检查配置",
  "        比较配置，暂不修改",
  "  3. [ ] Type something",
  "     Submit",
  "────────────────────",
  "  4. Chat about this",
  "",
  "Enter to select · ↑/↓ to navigate · Esc to cancel",
  "────────────────────",
].join("\n");

test("native checkbox questions include Submit and the separated chat option", () => {
  const menu = terminalMenu(multi)!;
  assert.deepEqual(menu.options.map((o) => o.number), [1, 2, 3, null, 4]);
  assert.deepEqual(menu.options.map((o) => o.checked), [false, true, false, undefined, undefined]);
  assert.equal(menu.selected, 1);
  assert.equal(menu.options[3].label, "Submit");
  assert.equal(moveKeys(menu, 3), "\x1b[B\x1b[B");
  assert.equal(moveKeys(menu, 4), "\x1b[B\x1b[B\x1b[B");
});

test("checkbox redraw, Submit focus and navigation back preserve the whole menu", () => {
  const toggled = terminalMenu(multi.replace("[✔]", "[ ]"))!;
  assert.equal(toggled.options[1].checked, false);
  assert.equal(toggled.selected, 1);
  const submit = terminalMenu(multi.replace("❯ 2.", "  2.").replace("     Submit", "❯    Submit"))!;
  assert.equal(submit.selected, 3);
  assert.equal(moveKeys(submit, 0), "\x1b[A\x1b[A\x1b[A");
  const chat = terminalMenu(multi.replace("❯ 2.", "  2.").replace("  4. Chat", "❯ 4. Chat"))!;
  assert.equal(chat.selected, 4);
  assert.equal(moveKeys(chat, 3), "\x1b[A");
});
