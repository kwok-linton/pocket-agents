import { hasBackgroundTask, historyCount, isActive, isFinished, isHistorical, isUncertain, isWorking, needsAttention, titleOf, unread } from "./model.ts";
import type { Session } from "./types.ts";

export type OverviewView = {
  filter: string;
  provider: string;
  node: string;
  query: string;
  projectKey: string;
  showAllProjects: boolean;
  limit: number;
};

// Keep navigation context only for this open app, without changing launch defaults.
export function createOverviewMemory() {
  let current: OverviewView = {
    filter: "active",
    provider: "",
    node: "",
    query: "",
    projectKey: "",
    showAllProjects: false,
    limit: 40,
  };
  return {
    read: () => ({ ...current }),
    write: (view: OverviewView) => {
      current = { ...view };
    },
  };
}

export const overviewMemory = createOverviewMemory();

export function updateOverview(
  view: OverviewView,
  patch: Partial<OverviewView>,
): OverviewView {
  const changed = Object.entries(patch).some(
    ([key, value]) =>
      key !== "limit" && view[key as keyof OverviewView] !== value,
  );
  return { ...view, ...patch, ...(changed ? { limit: 40 } : {}) };
}

export function matchesOverviewSelection(session: Session, view: OverviewView) {
  return (
    (!view.provider || session.provider === view.provider) &&
    (!view.node || session.nodeId === view.node) &&
    (!view.query ||
      `${titleOf(session)} ${session.cwd} ${session.deviceName || ""} ${session.provider}`
        .toLowerCase()
        .includes(view.query.toLowerCase()))
  );
}

export function overviewHistoryHints(
  selectedSessions: Session[],
  query: string,
) {
  return ["claude", "codex"]
    .map((provider) => {
      const matches = selectedSessions.filter(
        (session) => session.provider === provider,
      );
      return {
        provider,
        active: matches.filter(isActive).length,
        uncertain: matches.filter(isUncertain).length,
        history: historyCount(matches),
      };
    })
    .filter(
      (item) =>
        item.history > 0 && (!!query || (!item.active && !item.uncertain)),
    );
}

export function overviewCounts(sessions: Session[]) {
  return {
    attention: sessions.filter(needsAttention).length,
    active: sessions.filter(isActive).length,
    working: sessions.filter(isWorking).length,
    background: sessions.filter(hasBackgroundTask).length,
    history: historyCount(sessions),
  };
}

export function matchesOverviewFilter(session: Session, filter: string) {
  switch (filter) {
    case "active": return isActive(session) || (isUncertain(session) && !!session.executionId);
    case "uncertain": return isUncertain(session);
    case "history": return isHistorical(session);
    case "attention": return needsAttention(session);
    case "working": return isWorking(session);
    case "background": return hasBackgroundTask(session);
    case "finished": return isFinished(session);
    case "favorite": return !!session.metadata?.favorite;
    default: return true;
  }
}

// In the active view a session waiting on the user stops updating, so a plain
// newest-first sort lets busy sessions push it out of sight.
const urgency = (s: Session) =>
  needsAttention(s) ? 0 : s.turnState === "error" ? 1 : isWorking(s) ? 2 : isFinished(s) && unread(s) ? 3 : 4;
export function sortSessions(list: Session[], attentionFirst: boolean) {
  const at = new Map(list.map((s) => [s, Date.parse(s.updatedAt) || 0]));
  const rank = new Map(list.map((s) => [s, attentionFirst ? urgency(s) : 0]));
  return [...list].sort((a, b) => rank.get(a)! - rank.get(b)! || at.get(b)! - at.get(a)!);
}
