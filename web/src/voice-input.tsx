import { t } from "./i18n.ts";
import { useEffect, useRef, useState } from "react";
import { readLocal, writeLocal } from "./api";
import { voiceSession, type Recognition } from "./voice-session";

type SpeechWindow = Window & { SpeechRecognition?: new () => Recognition; webkitSpeechRecognition?: new () => Recognition };

export function VoiceInput({ disabled, onText, onActive }: {
  disabled: boolean; onText: (text: string) => void; onActive: (active: boolean) => void;
}) {
  const [active, setActive] = useState(false);
  const [interim, setInterim] = useState("");
  const [phase, setPhase] = useState<"connecting" | "listening" | "speaking" | "finishing">("connecting");
  const [seconds, setSeconds] = useState(0);
  const recordingDialog = useRef<HTMLDialogElement>(null);
  const preview = useRef<HTMLDivElement>(null);
  const [notice, setNotice] = useState("");
  const [language, setLanguage] = useState(() => readLocal("voice-language", "zh-CN"));
  const dialog = useRef<HTMLDialogElement>(null);
  const session = useRef<ReturnType<typeof voiceSession> | null>(null);
  const callbacks = useRef({ onText, onActive });
  callbacks.current = { onText, onActive };
  const w = window as SpeechWindow;
  const Constructor = w.SpeechRecognition || w.webkitSpeechRecognition;
  useEffect(() => {
    const hide = () => { if (document.hidden) session.current?.stop(); };
    document.addEventListener("visibilitychange", hide);
    return () => {
      document.removeEventListener("visibilitychange", hide);
      session.current?.dispose(); session.current = null;
    };
  }, []);
  useEffect(() => {
    if (!active) { recordingDialog.current?.close(); return; }
    recordingDialog.current?.showModal();
    setSeconds(0);
    const tick = setInterval(() => setSeconds((value) => value + 1), 1000);
    return () => clearInterval(tick);
  }, [active]);
  useEffect(() => { if (preview.current) preview.current.scrollTop = preview.current.scrollHeight; }, [interim]);
  function start() {
    dialog.current?.close();
    if (!Constructor || active) return;
    writeLocal("voice-language", language);
    writeLocal("voice-explained", true);
    session.current?.dispose();
    session.current = voiceSession(() => new Constructor(), language, {
      text: (text) => callbacks.current.onText(text),
      interim: setInterim,
      phase: setPhase,
      active: (value) => { setActive(value); callbacks.current.onActive(value); },
      notice: setNotice,
    });
    setInterim(""); setPhase("connecting");
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    session.current.start();
  }
  return <div className="voice-input">
    <button type="button" className={`voice-button ${active ? "listening" : ""}`} aria-label={active ? t("结束语音输入") : t("语音输入")} aria-pressed={active} disabled={disabled && !active}
      onClick={() => { if (active) session.current?.stop(); else if (!Constructor || !readLocal("voice-explained", false)) dialog.current?.showModal(); else start(); }}>
      {active ? <span aria-hidden="true">■</span> : <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8"/></svg>}
    </button>
    {!active && notice && <div className="voice-feedback" role="status"><span>{notice}</span><button aria-label={t("收起语音提示")} onClick={() => setNotice("")}>×</button></div>}
    <dialog ref={recordingDialog} className="voice-recording-sheet" aria-label={t("正在语音输入")} onCancel={(e) => { e.preventDefault(); session.current?.cancel(); }}>
      <VoicePanel phase={phase} seconds={seconds} interim={interim} preview={preview} onCancel={() => session.current?.cancel()} onDone={() => session.current?.stop()} />
    </dialog>
    <dialog ref={dialog} className="workspace-dialog" aria-label={t("语音输入设置")}>
      <h3>{t("语音输入")}</h3>
      {Constructor ? <><p>{t("支持连续口述，短暂停顿会继续听；点「完成」后转成草稿，点「取消」丢弃本次口述；单次最多 5 分钟，不会自动发送。识别由浏览器提供，可能通过浏览器厂商的在线服务处理语音；本应用不保存录音。")}</p>
        <label>{t("识别语言")}<select value={language} onChange={(e) => setLanguage(e.target.value)}><option value="zh-CN">{t("普通话")}</option><option value="en-AU">English</option><option value="zh-HK">{t("粤语")}</option></select></label>
        <button onClick={start}>{t("开始说话")}</button></> : <p>{t("这个浏览器暂不支持内置语音识别。请点输入框，使用手机键盘上的麦克风。")}</p>}
      <button onClick={() => dialog.current?.close()}>{t("关闭")}</button>
    </dialog>
  </div>;
}

export function VoicePanel({ phase, seconds, interim, preview, onCancel, onDone }: {
  phase: "connecting" | "listening" | "speaking" | "finishing";
  seconds: number; interim: string; preview?: React.Ref<HTMLDivElement>; onCancel: () => void; onDone: () => void;
}) { return <>
      <header><strong>{t("语音输入")}</strong><time>{Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, "0")}</time></header>
      <div className={`voice-activity ${phase}`} aria-hidden="true">{[0,1,2,3,4,5,6,7,8].map((i) => <i key={i} style={{ animationDelay: `${i * -0.13}s` }} />)}</div>
      <p className="voice-phase" role="status">{{ connecting: t("正在连接麦克风…"), listening: t("正在收音，可以继续说"), speaking: t("正在识别你的话…"), finishing: t("正在整理文字…") }[phase]}</p>
      <div className="voice-transcript" ref={preview}>{interim || t("识别到的文字会显示在这里")}</div>
      <footer><button onClick={onCancel}>{t("取消")}</button><button className="voice-done" disabled={phase === "finishing"} onClick={onDone}>{phase === "finishing" ? t("整理中…") : t("完成")}</button></footer>
      <small>{t("完成后放入草稿，由你确认发送 · 最长 5 分钟")}</small>
  </>;
}
