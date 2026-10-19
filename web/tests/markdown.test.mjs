import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { createRequire } from 'node:module';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
// i18n.ts loads its table with top-level await, which CommonJS cannot hold;
// these tests check the Chinese source text, so t() is the identity here.
const i18nStub = { name: 'i18n-stub', setup(b) {
 b.onResolve({ filter: /\/i18n(\.ts)?$/ }, () => ({ path: 'i18n', namespace: 'stub' }));
 b.onLoad({ filter: /.*/, namespace: 'stub' }, () => ({ contents: 'export const t = (s) => s; export const tServer = (s) => s || "";', loader: 'js' }));
} };
const result = await build({ entryPoints: ['src/message-markdown.tsx'], bundle: true, platform: 'node', format: 'cjs', external: ['react', 'react/*'], write: false, plugins: [i18nStub] });
const mod = { exports: {} };
new Function('require', 'module', 'exports', result.outputFiles[0].text)(createRequire(import.meta.url), mod, mod.exports);
const render = text => renderToStaticMarkup(createElement(mod.exports.MessageMarkdown, { text, cwd: '/work', sessionId: 'test' }));
test('answers render semantic Markdown, tables, and copyable code', () => {
 const html = render('# 标题\n\n**重点**\n\n- 一\n- 二\n\n```go\nfmt.Println("你好")\n```\n\n| A | B |\n| - | - |\n| 1 | 2 |');
 for (const tag of ['<h1>', '<strong>', '<ul>', '<pre', '<table>']) assert.ok(html.includes(tag), tag);
 assert.ok(html.includes('复制代码'));
});
test('transcript HTML, script URLs and images cannot execute or beacon', () => {
 const html = render('<script>alert(1)</script>\n\n[x](javascript:alert%281%29)\n\n![tracking](https://example.com/pixel)\n\n<a href="/api/actions">x</a>');
 assert.ok(!html.includes('<script'));
 assert.ok(!html.includes('href="javascript:'));
 assert.ok(!html.includes('<img'));
 assert.ok(!html.includes('href="/api/actions"'));
});
test('workspace links download while traversal and external navigation stay separated', () => {
 const html = render('[report](/work/report.pdf) [escape](../secret) [web](https://example.com)');
 assert.ok(html.includes('/api/sessions/test/file?path=report.pdf'));
 assert.ok(!html.includes('href="../secret"'));
 assert.ok(html.includes('rel="noopener noreferrer"'));
});
