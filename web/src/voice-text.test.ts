import test from "node:test";
import assert from "node:assert/strict";
import { appendSpeech, speechError } from "./voice-text.ts";
test("speech appends without replacing existing or edited drafts", () => {
 assert.equal(appendSpeech("", "测试"), "测试");
 assert.equal(appendSpeech("已编辑文字", "测试"), "已编辑文字\n测试");
 assert.equal(appendSpeech("旧草稿\n", "测试"), "旧草稿\n测试");
});
test("permission and network failures explain recovery without raw errors", () => {
 assert.match(speechError("not-allowed"), /权限/);
 assert.match(speechError("network"), /网络/);
 assert.match(speechError("unknown"), /草稿/);
});
