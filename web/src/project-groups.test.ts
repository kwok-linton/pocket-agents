import test from "node:test";
import assert from "node:assert/strict";
import { projectPath } from "./project-groups.ts";
import type { Session, WorkspacePreferences } from "./types.ts";
const preferences: WorkspacePreferences = { workspaceScope: "focused", workspaces: [{nodeId:"one",path:"/work/project-beta"}, {nodeId:"one",path:"/work/project-alpha/"}] };
const s = (cwd: string, nodeId = "one") => ({ cwd, nodeId } as Session);
test("temporary, hidden and nested directories belong to the focused project", () => {
 for (const path of ["/work/project-beta", "/work/project-beta/tmp/test", "/work/project-beta/.claude/skills", "/work/project-beta/reports/example"]) assert.equal(projectPath(s(path), preferences), "/work/project-beta");
 assert.equal(projectPath(s("/work/project-alpha/tmp"), preferences), "/work/project-alpha");
});
test("project matching respects path boundaries, node and nested explicit roots", () => {
 assert.equal(projectPath(s("/work/project-beta-other/tmp"), preferences), "/work/project-beta-other/tmp");
 assert.equal(projectPath(s("/work/project-beta/tmp", "two"), preferences), "/work/project-beta/tmp");
 const nested = {...preferences, workspaces:[...preferences.workspaces,{nodeId:"one",path:"/work/project-beta/independent"}]};
 assert.equal(projectPath(s("/work/project-beta/independent/src"), nested), "/work/project-beta/independent");
});
