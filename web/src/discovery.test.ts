import test from 'node:test';
import assert from 'node:assert/strict';
import {inBrowseScope,discoveredProjects} from './discovery.ts';
import type {Session,WorkspacePreferences} from './types.ts';
const prefs:WorkspacePreferences={workspaceScope:'focused',workspaces:[{nodeId:'m3',path:'/work'}]};
const session=(patch:Partial<Session> = {}) => ({nodeId:'m5',cwd:'/home/new',executionState:'alive',updatedAt:'2026-10-17T00:00:00Z',metadata:{},...patch} as Session);
test('unconfigured device has live and historical sessions without manual roots',()=>{
 assert.equal(inBrowseScope(session(),prefs),true);
 assert.equal(inBrowseScope(session({executionState:'exited'}),prefs),true);
});
test('active outside focus remains visible, old unrelated history remains scoped',()=>{
 assert.equal(inBrowseScope(session({nodeId:'m3'}),prefs),true);
 assert.equal(inBrowseScope(session({nodeId:'m3',executionState:'exited'}),prefs),false);
});
test('discovery folds descendants, excludes subagents and isolates devices',()=>{
 const rows=discoveredProjects([session(),session({cwd:'/home/new/tmp'}),session({cwd:'/other',parentId:'parent'}),session({nodeId:'m3',cwd:'/private'})],'m5',prefs);
 assert.equal(rows.length,1);assert.equal(rows[0].count,2);assert.equal(rows[0].path,'/home/new');assert.equal(rows[0].followed,false);
 assert.equal(prefs.workspaces.length,1);
});
