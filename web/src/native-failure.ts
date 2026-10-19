import { t } from "./i18n.ts";
import type { Message } from './types.ts';
export function apiFailure(text: string) {
 const match = /^API Error:\s*(\d{3})\b/.exec(text.trim());
 if (!match) return null;
 const code = Number(match[1]);
 return {code, title: code === 524 ? t("AI 服务响应超时") : t("AI 服务请求失败（{0}）", { 0: code }), detail: code === 524 ? t("请求已连接到上游服务，但未能及时得到回复。这不是审批请求。稍后可继续这段对话。") : t("本轮请求失败，没有需要批准的选项。可查看错误详情后继续对话。")};
}
export function latestApiFailure(messages: Message[]) {
 for (let i=messages.length-1;i>=0;i--) {
  const m=messages[i];
  if (m.role === 'user') return null;
  if (m.role === 'assistant' || m.role === 'error') return apiFailure(m.text);
 }
 return null;
}
