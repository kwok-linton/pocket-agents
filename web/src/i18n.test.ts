import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { resolveLanguage, translate, translateServer } from "./i18n.ts";
import { en } from "./locales/en.ts";
import { zhFromServer } from "./locales/zh-server.ts";
// @ts-expect-error plain ESM build script without type declarations
import { serverChineseMessages, translationKeys } from "../scripts/i18n-keys.mjs";

const placeholders = (s: string) => [...new Set([...s.matchAll(/\{(\w+)(?:\|[^}]*)?\}/g)].map((m) => m[1]))].sort();

test("every interface string has an English translation with the same placeholders", () => {
  const keys = translationKeys() as Map<string, string>;
  const missing = [...keys].filter(([key]) => !(key in en)).map(([key, where]) => `${where}  ${key}`);
  assert.deepEqual(missing, [], "add these to src/locales/en.ts");
  for (const [key, value] of Object.entries(en)) assert.deepEqual(placeholders(value), placeholders(key), key);
});

test("en.ts has no entries for text that no longer exists", () => {
  const keys = translationKeys() as Map<string, string>, server = serverChineseMessages() as Set<string>;
  const unused = Object.keys(en).filter((key) => !keys.has(key) && !server.has(key));
  assert.deepEqual(unused, [], "remove these from src/locales/en.ts");
});

test("no Chinese interface text is left outside t()", () => {
  const script = fileURLToPath(new URL("../scripts/i18n-wrap.mjs", import.meta.url));
  // Exits non-zero and names the files when text needs wrapping.
  execFileSync(process.execPath, [script, "--check"], { stdio: "pipe" });
});

test("translation, plurals and server messages", () => {
  assert.equal(translate("{0} 个会话正在工作", {}, { 0: 1 }), "1 个会话正在工作");
  assert.equal(translate("{0} 个会话正在工作", en, { 0: 1 }), "1 session working");
  assert.equal(translate("{0} 个会话正在工作", en, { 0: 3 }), "3 sessions working");
  assert.equal(translate("没有这条", en), "没有这条", "unknown text falls back to the source");
  assert.equal(translate("筛选{0}", {}, { 0: false }), "筛选", "false renders as nothing, like JSX");
  const prefix = Object.keys(zhFromServer).find((k) => k.endsWith(": "))!;
  assert.equal(translateServer(prefix + "EOF", zhFromServer), zhFromServer[prefix] + "EOF");
  assert.equal(translateServer("something new", zhFromServer), "something new");
  assert.equal(translateServer(undefined, en), "");
  assert.equal(resolveLanguage("system", ["zh-Hans-CN"]), "zh");
  assert.equal(resolveLanguage("system", ["en-AU"]), "en");
  assert.equal(resolveLanguage("en", ["zh-CN"]), "en");
});
