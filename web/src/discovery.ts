import type {Session, WorkspacePreferences} from './types.ts';
import {inWorkspace, isActive} from './model.ts';

// Project preferences organize browsing; they must not hide live main sessions
// or make a newly connected device look empty before setup.
export function inBrowseScope(s: Session, p: WorkspacePreferences) {
  return inWorkspace(s,p) || isActive(s) || !p.workspaces.some(w => w.nodeId === s.nodeId);
}
export function discoveredProjects(sessions: Session[], nodeId: string, p: WorkspacePreferences) {
  const main = sessions.filter(s => s.nodeId === nodeId && !s.parentId && !s.metadata?.hidden);
  const paths = [...new Set(main.map(s => s.cwd.replace(/\/+$/, '') || '/'))];
  const roots = paths.filter(path => !paths.some(parent => parent !== path && (parent === '/' || path.startsWith(parent + '/'))));
  return roots.map(path => {
    const matches = main.filter(s => s.cwd === path || s.cwd.startsWith(path === '/' ? '/' : path + '/'));
    return {path, active: matches.filter(isActive).length, count: matches.length,
      updated: Math.max(...matches.map(s => Date.parse(s.updatedAt) || 0)),
      followed: p.workspaces.some(w => w.nodeId === nodeId && (w.path === path || path.startsWith(w.path.replace(/\/+$/, '') + '/')))};
  }).sort((a,b) => b.active-a.active || b.updated-a.updated || a.path.localeCompare(b.path));
}
