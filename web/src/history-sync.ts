import { transcriptGroups } from "./transcript.ts";
import type { Event as AgentEvent, History, Message } from "./types.ts";

export function applyHistoryDelta(previous: History | undefined, tag: string | undefined, input: unknown): History | undefined {
  const delta = input as History & { base?: string; order?: string[] };
  if (!previous || !tag || delta?.base !== tag || !Array.isArray(delta.order) || !Array.isArray(delta.messages)) return;
  const messages = new Map(previous.messages.map(m => [m.id, m]));
  for (const m of delta.messages) {
    if (!m || typeof m.id !== "string") return;
    messages.set(m.id, m);
  }
  const ordered: Message[] = [];
  const seen = new Set<string>();
  for (const id of delta.order) {
    const message = messages.get(id);
    if (!message || seen.has(id)) return;
    seen.add(id);
    ordered.push(message);
  }
  return { messages: ordered, cursor: delta.cursor, hasMore: delta.hasMore, boundary: delta.boundary };
}

export function affectsSession(events: AgentEvent[], id: string, nodeId?: string): boolean {
  return events.some(event => {
    if (event.sessionId === id) return true;
    if (nodeId && event.nodeId && event.nodeId !== nodeId) return false;
    if (event.kind === "device") return true;
    if (event.kind !== "snapshot") return false;
    const ids = (event.data as {sessionIds?: string[]} | undefined)?.sessionIds;
    return !ids || ids.includes(id);
  });
}

// Tool-only native pages must not render an empty conversation when internal
// activity is hidden. Fetch bounded context and retain the latest page on a
// transient older-page failure; the UI can still expose its activity summary.
export function hasVisibleMessages(messages: Message[]): boolean {
  return transcriptGroups(messages).some(group => !group.internal && group.messages.some(m => m.text.trim()));
}

export async function withVisibleHistory(latest: History, older: (cursor: string) => Promise<History>, maxPages = 6): Promise<History> {
  let result = latest;
  const visited = new Set<string>();
  for (let n = 0; n < maxPages && !hasVisibleMessages(result.messages || []) && result.hasMore && result.cursor; n++) {
    const cursor = result.cursor;
    if (visited.has(cursor)) break;
    visited.add(cursor);
    let page: History;
    try { page = await older(cursor); }
    catch (error) {
      if ((error as Error).name === "AbortError") throw error;
      break;
    }
    const messages = new Map((page.messages || []).map(m => [m.id, m]));
    for (const message of result.messages || []) messages.set(message.id, message);
    result = { ...page, messages: [...messages.values()] };
  }
  return result;
}
