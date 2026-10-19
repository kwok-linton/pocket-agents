import { t } from "./i18n.ts";
import { speechSnapshot, speechError, type SpeechResults } from "./voice-text.ts";
export type Recognition = {
 lang: string; continuous: boolean; interimResults: boolean;
 onresult: ((e: { resultIndex: number; results: SpeechResults }) => void) | null;
 onerror: ((e: { error: string }) => void) | null;
 onend: (() => void) | null;
 onaudiostart?: (() => void) | null; onspeechstart?: (() => void) | null; onspeechend?: (() => void) | null;
 start(): void; stop(): void; abort(): void;
};
type Schedule = (fn: () => void, ms: number) => () => void;
const schedule: Schedule = (fn, ms) => { const id = setTimeout(fn, ms); return () => clearTimeout(id); };
export function voiceSession(create: () => Recognition, lang: string, callbacks: {
 phase?: (phase: "connecting" | "listening" | "speaking" | "finishing") => void;
 text: (text: string) => void; interim: (text: string) => void; active: (active: boolean) => void; notice: (text: string) => void;
}, later: Schedule = schedule) {
 let wanted = false, current: Recognition | null = null;
 let retry = () => {}, lifetime = () => {}, watchdog = () => {};
 let emptyCycles = 0;
 let completed = "", cycle = "", committed = false, stopping = false, provisional = false, completedProvisional = false;
 const joined = () => [completed, cycle].filter(Boolean).join("\n");
 function detach(r: Recognition) { r.onend = null; r.onerror = null; r.onresult = null; r.onaudiostart = null; r.onspeechstart = null; r.onspeechend = null; }
 function finish(abort = false, discard = false) {
  wanted = false; retry(); lifetime(); watchdog();
  const r = current; current = null;
  if (r) { detach(r); if (abort) r.abort(); }
  const text = joined();
  const deliver = !committed && !discard && !!text;
  committed = true;
  if (discard) { completed = ""; cycle = ""; }
  if (deliver) {
   if (provisional || completedProvisional) callbacks.notice(t("已保留尚未定稿的识别文字，请在发送前核对。") );
   callbacks.text(text);
  }
  callbacks.active(false); callbacks.interim("");
 }
 function run() {
  if (!wanted) return;
  const r = create(); current = r;
  callbacks.phase?.("connecting");
  r.lang = lang; r.continuous = true; r.interimResults = true;
  let heard = false;
  r.onaudiostart = () => { if (current === r && !stopping) callbacks.phase?.("listening"); };
  r.onspeechstart = () => { if (current === r && !stopping) callbacks.phase?.("speaking"); };
  r.onspeechend = () => { if (current === r && !stopping) callbacks.phase?.("listening"); };
  r.onresult = (event) => {
   if (current !== r) return;
   const result = speechSnapshot(event.results);
   // Keep what the user has already seen, including a final hypothesis when
   // stop/end arrives without a final-result callback.
   if (result.preview) { cycle = result.preview; provisional = !!result.interim; }
   if (result.text || result.interim) { heard = true; emptyCycles = 0; }
   callbacks.interim([completed, result.preview].filter(Boolean).join("\n"));
  };
  r.onerror = (event) => {
   if (current !== r) return;
   // Silence is normal during dictation; Android may end each recognition cycle.
   if (event.error === "no-speech" && wanted) return;
   callbacks.notice(speechError(event.error)); finish(true);
  };
  r.onend = () => {
   if (current !== r) return;
   detach(r); current = null;
   if (!wanted) { finish(); return; }
   if (!heard) emptyCycles++;
   if (emptyCycles >= 5) {
    callbacks.notice(t("暂未听到声音，已暂停收音；点麦克风继续。已识别文字保留在草稿。")); finish(); return;
   }
   completed = joined(); completedProvisional ||= provisional; cycle = ""; provisional = false;
   callbacks.phase?.("connecting");
   retry = later(run, 300);
  };
  try { r.start(); } catch { callbacks.notice(t("无法继续语音识别，请检查麦克风权限后重试。")); finish(true); }
 }
 function stop() {
  if (stopping) return;
  stopping = true; callbacks.phase?.("finishing");
  wanted = false; retry(); lifetime();
  if (!current) { finish(); return; }
  watchdog = later(() => finish(true), 3000);
  try { current.stop(); } catch { finish(true); }
 }
 return {
  start() {
   if (wanted || current) return;
   wanted = true; stopping = false; completed = ""; cycle = ""; committed = false; provisional = false; completedProvisional = false; emptyCycles = 0; callbacks.notice(""); callbacks.active(true);
   lifetime = later(() => { callbacks.notice(t("已达到单次 5 分钟上限，文字已保留；可再次点击麦克风继续。")); stop(); }, 300000);
   run();
  },
  stop,
  cancel() { finish(true, true); },
  dispose() { finish(true, true); },
 };
}
