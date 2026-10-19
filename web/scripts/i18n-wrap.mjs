// Wrap user-visible Chinese text in t(). Idempotent: run again after adding
// UI text, then add the new keys to src/locales/en.ts (npm test lists any that
// are missing). Usage: node scripts/i18n-wrap.mjs [--check]
//
//   <p>中文</p>             -> <p>{t("中文")}</p>
//   placeholder="中文"      -> placeholder={t("中文")}
//   "中文"                  -> t("中文")
//   `共 ${n} 条`            -> t("共 {0} 条", { 0: n })
//
// Not touched: comments, import paths, object keys, strings already passed to
// t()/tServer(), and files under locales/ or ending in .test.ts.
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
const ts = createRequire(import.meta.url)("typescript");

const SRC = fileURLToPath(new URL("../src/", import.meta.url));
const CJK = /[㐀-鿿]/;
const SKIP = new Set(["i18n.ts"]);
const check = process.argv.includes("--check");

export function sourceFiles() {
  return readdirSync(SRC)
    .filter((f) => /\.tsx?$/.test(f) && !f.endsWith(".test.ts") && !f.endsWith(".d.ts") && !SKIP.has(f))
    .map((f) => path.join(SRC, f));
}

const quote = (s) => JSON.stringify(s);
const isTCall = (node) =>
  node && ts.isCallExpression(node) && ts.isIdentifier(node.expression) && ["t", "tServer", "translate"].includes(node.expression.text);

// React's JSX whitespace rule: trim each line except the outer edges of the
// first/last line, drop empty lines, join with single spaces.
function jsxText(raw) {
  const lines = raw.split(/\r\n|\n|\r/);
  let out = "";
  lines.forEach((line, i) => {
    let s = line.replace(/\t/g, " ");
    if (i !== 0) s = s.replace(/^[ ]+/, "");
    if (i !== lines.length - 1) s = s.replace(/[ ]+$/, "");
    if (s) out += (out && i !== 0 ? " " : "") + s;
  });
  return out;
}

function transform(file, text) {
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const edits = [];
  const containsJsx = (node) => {
    let found = false;
    const walk = (n) => {
      if (found) return;
      if (ts.isJsxElement(n) || ts.isJsxSelfClosingElement(n) || ts.isJsxFragment(n)) found = true;
      else ts.forEachChild(n, walk);
    };
    walk(node);
    return found;
  };
  // Text mixed with values ("{n} 个会话正在工作") becomes one sentence key so a
  // translation can reorder words and pluralise. Elements split the run.
  const mergeRuns = (children) => {
    const handled = new Set();
    let run = [];
    const flush = () => {
      const hasText = run.some((c) => ts.isJsxText(c) && CJK.test(c.text));
      const hasValue = run.some((c) => ts.isJsxExpression(c) && !(c.expression && ts.isStringLiteral(c.expression)));
      if (hasText && hasValue) {
        let key = "";
        const params = [];
        for (const c of run) {
          if (ts.isJsxText(c)) key += jsxText(c.text);
          else if (ts.isStringLiteral(c.expression)) key += c.expression.text;
          else {
            key += `{${params.length}}`;
            params.push(`${params.length}: ${c.expression.getText(source)}`);
          }
          handled.add(c);
        }
        const core = key.trim();
        const lead = key.startsWith(" ") ? '{" "}' : "";
        const trail = key.endsWith(" ") ? '{" "}' : "";
        edits.push([run[0].getStart(), run[run.length - 1].end, `${lead}{t(${quote(core)}, { ${params.join(", ")} })}${trail}`]);
      }
      run = [];
    };
    for (const c of children) {
      const value = ts.isJsxExpression(c) && c.expression && !containsJsx(c.expression);
      if (ts.isJsxText(c) || value) run.push(c);
      else flush();
    }
    flush();
    return handled;
  };
  const visit = (node) => {
    // Already translated: leave the key and its parameters alone this pass;
    // nested Chinese inside parameters is handled on the next pass.
    if (isTCall(node)) {
      node.arguments.slice(1).forEach(visit);
      return;
    }
    // translate="no" marks text that stays as written (language names, brands).
    const opening = ts.isJsxElement(node) ? node.openingElement : ts.isJsxSelfClosingElement(node) ? node : undefined;
    if (opening?.attributes.properties.some((a) => ts.isJsxAttribute(a) && a.name.getText(source) === "translate" && a.initializer?.getText(source) === '"no"')) return;
    if (ts.isJsxElement(node) || ts.isJsxFragment(node)) {
      const handled = mergeRuns(node.children);
      if (ts.isJsxElement(node)) visit(node.openingElement);
      node.children.forEach((c) => handled.has(c) || visit(c));
      return;
    }
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) return;
    if (ts.isJsxText(node) && CJK.test(node.text)) {
      const cleaned = jsxText(node.text);
      const core = cleaned.trim();
      const lead = cleaned.startsWith(" ") ? '{" "}' : "";
      const trail = cleaned.endsWith(" ") ? '{" "}' : "";
      edits.push([node.getStart(), node.end, `${lead}{t(${quote(core)})}${trail}`]);
      return;
    }
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && CJK.test(node.text)) {
      const parent = node.parent;
      if ((ts.isPropertyAssignment(parent) || ts.isPropertySignature(parent) || ts.isPropertyAccessExpression(parent)) && parent.name === node) return;
      if (ts.isLiteralTypeNode(parent)) return;
      const call = `t(${quote(node.text)})`;
      edits.push([node.getStart(), node.end, ts.isJsxAttribute(parent) ? `{${call}}` : call]);
      return;
    }
    if (ts.isTemplateExpression(node) && [node.head, ...node.templateSpans.map((s) => s.literal)].some((p) => CJK.test(p.text))) {
      let key = node.head.text;
      const params = [];
      node.templateSpans.forEach((span, i) => {
        key += `{${i}}` + span.literal.text;
        params.push(`${i}: ${span.expression.getText(source)}`);
      });
      edits.push([node.getStart(), node.end, `t(${quote(key)}, { ${params.join(", ")} })`]);
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  if (!edits.length) return text;
  edits.sort((a, b) => b[0] - a[0]);
  let out = text;
  for (const [start, end, value] of edits) out = out.slice(0, start) + value + out.slice(end);
  return out;
}

function ensureImport(file, text) {
  if (!/\bt\(/.test(text) || /import\s*\{[^}]*\bt\b[^}]*\}\s*from\s*["']\.\/i18n(\.ts)?["']/.test(text)) return text;
  const ext = ".ts"; // Node's test runner needs the extension; Vite accepts it.
  const existing = text.match(/import\s*\{([^}]*)\}\s*from\s*(["'])\.\/i18n(\.ts)?\2;?/);
  if (existing) return text.replace(existing[0], existing[0].replace(existing[1], `${existing[1].trim()}, t `.replace(/^, /, " ")));
  return `import { t } from "./i18n${ext}";\n` + text;
}

let changed = 0;
for (const file of sourceFiles()) {
  const before = readFileSync(file, "utf8");
  let after = before;
  // Template parameters may themselves contain Chinese; repeat to a fixed point.
  for (let pass = 0; pass < 6; pass++) {
    const next = transform(file, after);
    if (next === after) break;
    after = next;
  }
  after = ensureImport(file, after);
  if (after !== before) {
    changed++;
    if (check) console.log("needs wrapping:", path.basename(file));
    else writeFileSync(file, after);
  }
}
console.log(check ? `${changed} file(s) contain unwrapped Chinese text` : `wrapped text in ${changed} file(s)`);
if (check && changed) process.exitCode = 1;
