// Extract explicit local Markdown links only. Text/code that merely resembles
// a filename must not become an unsolicited download target.
export function artifactPaths(text: string, cwd: string): string[] {
  const result = new Set<string>();
  const root = cwd.replace(/\/$/, "");
  for (const match of text.matchAll(/\[[^\]\n]*\]\((?:<([^>\n]+)>|([^\s)]+))\)/g)) {
    let path = match[1] || match[2];
    try { path = decodeURIComponent(path); } catch { continue; }
    if (/^[a-z][a-z0-9+.-]*:/i.test(path) || path.startsWith("//") || /[\x00-\x1f\\]/.test(path)) continue;
    if (path.startsWith("/")) {
      if (!path.startsWith(root + "/")) continue;
      path = path.slice(root.length + 1);
    }
    path = path.replace(/^\.\//, "");
    if (!path || path.split("/").some((part) => part === ".." || part === "")) continue;
    result.add(path);
  }
  return [...result].slice(0, 12);
}
