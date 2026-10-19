import { t } from "./i18n.ts";
export type SpeechResults = { length: number; [index: number]: { isFinal: boolean; 0: { transcript: string } } };
export function appendSpeech(draft: string, text: string) {
  return draft + (draft && !/\s$/.test(draft) ? "\n" : "") + text;
}
export function speechError(error: string) {
  switch (error) {
    case "not-allowed": case "service-not-allowed": return t("麦克风或语音识别未获允许，请在浏览器网站权限中开启；也可以使用键盘语音输入。");
    case "audio-capture": return t("无法使用麦克风，请检查是否被其他应用占用。");
    case "network": return t("语音识别服务连接失败，请检查网络或使用键盘语音输入。");
    case "no-speech": return t("没有听到声音，请再试一次。");
    case "aborted": return t("语音输入已停止，已识别文字保留在草稿。");
    case "language-not-supported": return t("浏览器不支持此识别语言，请使用键盘语音输入。");
    default: return t("语音识别未完成，已识别文字保留在草稿。");
  }
}

/** Rebuild each browser result snapshot, rather than appending each callback.
 * Some mobile engines expose expanding hypotheses as adjacent final entries.
 * Only collapse exact/prefix expansions inside this one recognition cycle.
 */
export function speechSnapshot(results: SpeechResults) {
  const segments: string[] = [];
  let interim = "";
  for (let i = 0; i < results.length; i++) {
    const value = (results[i][0]?.transcript || "").trim();
    if (!value) continue;
    if (!results[i].isFinal) { interim += value; continue; }
    const last = segments.at(-1);
    if (last && value.startsWith(last)) segments[segments.length - 1] = value;
    else if (last !== value) segments.push(value);
  }
  const preview = [...segments];
  if (interim) {
    const last = preview.at(-1);
    if (last && interim.startsWith(last)) preview[preview.length - 1] = interim;
    else preview.push(interim);
  }
  return { text: segments.join("\n"), interim, preview: preview.join("\n") };
}
