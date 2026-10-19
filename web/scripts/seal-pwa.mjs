import { readFile, readdir, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { brotliCompressSync, gzipSync, constants } from "node:zlib";
const index = await readFile(
  new URL("../dist/index.html", import.meta.url),
  "utf8",
);
// Every built asset, not only those named in index.html: lazily imported
// chunks (Markdown) must also open offline after the first visit.
const assets = (await readdir(new URL("../dist/assets", import.meta.url)))
  .filter(name => !name.endsWith(".br") && !name.endsWith(".gz"))
  .sort()
  .map((name) => `/assets/${name}`);
const path = new URL("../dist/sw.js", import.meta.url);
const source = await readFile(path, "utf8");
// Worker-only fixes must get a fresh shell cache too, without overwriting the
// cache still used by the previous active worker during installation.
const version = createHash("sha256").update(index).update("\0").update(source).digest("hex").slice(0, 12);
await writeFile(
  path,
  source
    .replace(/(["'])pocket-shell-v1\1/, `"pocket-shell-${version}"`)
    .replace(
      "const SHELL = [",
      `const SHELL = [${assets.map((value) => JSON.stringify(value)).join(",")},`,
    ),
);
console.log(
  `PWA shell ${version}: ${assets.length} compiled assets pre-cached; API excluded.`,
);

await writeFile(new URL("../dist/pocket-ui.json", import.meta.url), JSON.stringify({protocol: 1}) + "\n");

// Precompress public build assets once. The service worker caches decoded
// originals, never duplicate .br/.gz downloads. Native/API data is excluded.
let originalBytes = 0, brotliBytes = 0, gzipBytes = 0;
for (const name of ["/index.html", "/sw.js", ...assets].filter(name => /\.(html|js|css)$/.test(name))) {
  const file = new URL(`../dist${name}`, import.meta.url);
  const source = await readFile(file);
  const br = brotliCompressSync(source, {params: {[constants.BROTLI_PARAM_QUALITY]: 9}});
  const gz = gzipSync(source, {level: 9});
  await writeFile(new URL(`${file.href}.br`),br);
  await writeFile(new URL(`${file.href}.gz`),gz);
  originalBytes += source.length; brotliBytes += br.length; gzipBytes += gz.length;
}
console.log(`Precompressed static assets: ${originalBytes} bytes -> Brotli ${brotliBytes}, gzip ${gzipBytes}`);
