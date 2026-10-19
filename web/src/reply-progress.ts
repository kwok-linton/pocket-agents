import { t } from "./i18n.ts";
import type { Message, TrackedAction } from './types.ts';
import { transcriptGroups } from './transcript.ts';

export function replyProgress(input: {
  online: boolean; sending: boolean; working: boolean; attention: boolean; background?: boolean;
  action?: TrackedAction; messages: Message[]; now: number;
}): {label: string; animated: boolean} | null {
  if (!input.online) return null;
  if (input.sending) return {label: t("正在发送"), animated: true};
  if (input.attention) return {label: t("等待你回答或审批"), animated: false};
  const a = input.action;
  const sentAt = a ? Date.parse(a.at) : NaN;
  const replied = transcriptGroups(input.messages).some(g => !g.internal && g.messages.some(m => m.role === 'assistant' && Date.parse(m.at) >= sentAt));
  const waiting = (a?.action.kind === 'send' || a?.action.kind === 'resume') && a.result.status === 'accepted' && !replied;
  // A queue receipt confirms enqueueing, not that this particular turn is running.
  if (waiting && (a.result.data as {delivery?: string})?.delivery === 'native_queue')
    return {label: input.now - sentAt < 90000 ? t("已排队，等待原生会话处理") : t("尚未同步到新的回复"), animated: input.now - sentAt < 90000};
  if (input.working) return {label: t("正在处理"), animated: true};
  if (waiting) return {label: input.now - sentAt < 90000 ? t("已送达，等待回复") : t("尚未同步到新的回复"), animated: input.now - sentAt < 90000};
  if (input.background) return {label: t("本轮已结束，后台任务仍在运行"), animated: false};
  return null;
}
