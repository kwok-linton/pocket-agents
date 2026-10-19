import type { Session, WorkspacePreferences } from "./types.ts";
export function projectPath(session: Session, preferences: WorkspacePreferences) {
  const roots = preferences.workspaces
    .filter((w) => w.nodeId === session.nodeId)
    .map((w) => w.path.replace(/\/+$/, "") || "/")
    .filter((root) => root === "/" || session.cwd === root || session.cwd.startsWith(root + "/"))
    .sort((a, b) => b.length - a.length);
  return roots[0] || session.cwd;
}
