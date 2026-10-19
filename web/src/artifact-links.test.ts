import { test } from "node:test";
import assert from "node:assert/strict";
import { artifactPaths } from "./artifact-links.ts";
test("only explicit workspace file links become artifacts", () => {
  assert.deepEqual(artifactPaths("[报告](<reports/报告 1.pdf>) [图](/work/a.png) [again](a.png)", "/work"), ["reports/报告 1.pdf", "a.png"]);
  assert.deepEqual(artifactPaths("[x](../secret) [x](https://host/file) [x](/other/a) [x](%2e%2e/private) [x](javascript:alert) bare.txt `code.png`", "/work"), []);
});
