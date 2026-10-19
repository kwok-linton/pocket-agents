import { locale, t } from "./i18n.ts";
import type {
  Session,
  Message,
  History,
  WorkspacePreferences,
} from "./types.ts";

export const providerName = (provider: string) =>
  provider === "claude"
    ? "Claude Code"
    : provider === "codex"
      ? "Codex"
      : provider;
export const titleOf = (session: Session) =>
  session.metadata?.alias || session.title || t("未命名会话");
export const folderName = (path: string) =>
  path.split("/").filter(Boolean).at(-1) || path;
export const unread = (session: Session) =>
  !session.metadata?.readAt ||
  Date.parse(session.updatedAt) > Date.parse(session.metadata.readAt);
export const isActive = (session: Session) =>
  !session.parentId &&
  session.executionState === "alive";
export const isUncertain = (session: Session) =>
  !session.parentId &&
  !isActive(session) &&
  session.executionState !== "exited";
export const isHistorical = (session: Session) =>
  !session.parentId && session.executionState === "exited";
export const isWorking = (session: Session) =>
  isActive(session) &&
  session.turnState === "working";
export const hasBackgroundTask = (session: Session) =>
  isActive(session) && session.turnState === "background";
export const isFinished = (session: Session) =>
  session.turnState === "completed";
export const needsAttention = (session: Session) =>
  isActive(session) &&
  (!!session.pending?.length ||
    session.turnState === "waiting_approval" ||
    session.turnState === "waiting_input");
export function inWorkspace(
  session: Session,
  preferences: WorkspacePreferences,
) {
  return (
    preferences.workspaceScope === "all" ||
    preferences.workspaces.some((workspace) => {
      const root = workspace.path.replace(/\/+$/, "") || "/";
      return (
        workspace.nodeId === session.nodeId &&
        (root === "/" ||
          session.cwd === root ||
          session.cwd.startsWith(root + "/"))
      );
    })
  );
}
export const historyCount = (sessions: Session[]) =>
  sessions.filter(isHistorical).length;
export const stateLabel = (state: string) =>
  ({
    working: t("正在工作"),
    background: t("后台任务中"),
    waiting_approval: t("等待批准"),
    waiting_input: t("等待回答"),
    completed: t("本轮结束"),
    interrupted: t("已中断"),
    error: t("出错"),
  })[state] || t("状态未知");
export const executionLabel = (state: string) =>
  ({
    alive: t("实例存活"),
    exited: t("实例已退出"),
  })[state] || t("实例状态未知");
export const eventLabel = (kind: string) =>
  ({
    completed: t("本轮结束"),
    waiting_approval: t("需要批准"),
    waiting_input: t("需要回答"),
    error: t("运行异常"),
    interrupted: t("已中断"),
  })[kind] || kind;
export const uniqueMessages = (messages: Message[]) => [
  ...new Map(messages.map((message) => [message.id, message])).values(),
];

// Latest native pages replace overlapping tails; compaction drops stale cache.
export function reconcileLatest(existing: History, latest: History): History {
  const overlap = latest.messages.length
    ? existing.messages.findIndex(
        (message) => message.id === latest.messages[0].id,
      )
    : -1;
  if (overlap < 0) return latest;
  return {
    ...latest,
    messages: uniqueMessages([
      ...existing.messages.slice(0, overlap),
      ...latest.messages,
    ]),
    cursor: existing.cursor,
    hasMore: existing.hasMore,
  };
}
// Constructing a formatter costs far more than formatting; lists call this
// once per row on every render.
const timeFormat = new Intl.DateTimeFormat(locale, {
  month: "numeric",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});
const clockFormat = new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" });
const dayFormat = new Intl.DateTimeFormat(locale, { month: "long", day: "numeric", weekday: "short" });
export function formatClock(value: string) {
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? clockFormat.format(date) : "";
}
/** Recent times read faster relative: "刚刚", "12 分钟前", "昨天 21:04". */
export function formatRelative(value: string, now = new Date()) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime()) || date.getFullYear() < 2000) return t("尚无记录");
  const minutes = Math.floor((now.getTime() - date.getTime()) / 60000);
  if (minutes < 1) return t("刚刚");
  if (minutes < 60) return t("{0} 分钟前", { 0: minutes });
  const day = dayLabel(value, now);
  if (day === t("今天")) return clockFormat.format(date);
  if (day === t("昨天")) return t("昨天 {0}", { 0: clockFormat.format(date) });
  return formatTime(value);
}
/** "今天" / "昨天" / "10月16日周五", in local time. */
export function dayLabel(value: string, now = new Date()) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return t("更早");
  const start = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((start(now) - start(date)) / 86400000);
  return days === 0 ? t("今天") : days === 1 ? t("昨天") : dayFormat.format(date);
}
export function formatTime(value: string) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime()) || date.getFullYear() < 2000)
    return t("尚无记录");
  return timeFormat.format(date);
}
