// List every translation key: the first argument of each t("...") call in
// src (excluding tests). Usage: node scripts/i18n-keys.mjs [--missing]
// --missing prints only keys without an entry in src/locales/en.ts.
import { readFileSync, readdirSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
const ts = createRequire(import.meta.url)("typescript");
const SRC = fileURLToPath(new URL("../src/", import.meta.url));

export function translationKeys() {
  const keys = new Map();
  for (const name of readdirSync(SRC)) {
    if (!/\.tsx?$/.test(name) || name.endsWith(".test.ts")) continue;
    const file = path.join(SRC, name);
    const text = readFileSync(file, "utf8");
    const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, name.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
    const visit = (node) => {
      if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === "t") {
        const first = node.arguments[0];
        if (first && (ts.isStringLiteral(first) || ts.isNoSubstitutionTemplateLiteral(first))) {
          if (!keys.has(first.text)) keys.set(first.text, `${name}:${source.getLineAndCharacterOfPosition(node.getStart()).line + 1}`);
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
  }
  return keys;
}

// Chinese messages the Go backend sends and the UI shows through tServer();
// en.ts translates these too.
export function serverChineseMessages() {
  const root = fileURLToPath(new URL("../../", import.meta.url));
  const out = new Set();
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go"))
        for (const m of readFileSync(full, "utf8").matchAll(/"((?:[^"\\\n]|\\.)*[㐀-鿿](?:[^"\\\n]|\\.)*)"/g)) out.add(JSON.parse(`"${m[1]}"`));
    }
  };
  for (const dir of ["internal", "cmd"]) walk(path.join(root, dir));
  return out;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  const keys = translationKeys();
  let en = {};
  if (process.argv.includes("--missing")) {
    // en.ts is plain data with a type annotation; Node strips types on import.
    en = (await import(path.join(SRC, "locales/en.ts"))).en;
  }
  const out = {};
  for (const [key, where] of keys) if (!(key in en)) out[key] = where;
  console.log(JSON.stringify(out, null, 1));
}
