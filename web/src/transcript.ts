import { t } from "./i18n.ts";
import type { Message } from "./types.ts";
export function transcriptGroups(messages: Message[]) {
  const groups: { internal: boolean; messages: Message[] }[] = [];
  let scheduled = false;
  for (const message of messages) {
    if (message.kind === "scheduled_trigger") scheduled = true;
    else if (message.role === "user" || message.kind === "compaction")
      scheduled = false;
    const internal =
      (scheduled && message.role === "assistant") ||
      message.role === "system" ||
      message.role === "tool" ||
      message.kind === "tool";
    const last = groups.at(-1);
    if (internal && last?.internal) last.messages.push(message);
    else groups.push({ internal, messages: [message] });
  }
  return groups;
}
export function internalLabel(message: Message) {
  if (message.kind === "scheduled_trigger") return t("原生定时任务触发");
  if (message.role === "assistant" && message.kind !== "tool")
    return t("定时任务回复");
  if (message.kind === "background_notification") return t("后台任务通知");
  if (message.kind === "compaction" || message.kind === "contextCompaction")
    return t("上下文压缩摘要");
  if (message.role === "tool" || message.kind === "tool") return t("工具活动");
  return t("原生系统上下文");
}
