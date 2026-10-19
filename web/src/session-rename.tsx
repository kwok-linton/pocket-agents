import { useEffect, useRef, useState } from "react";
import { t } from "./i18n";

export function SessionRename({ title, onRename }: { title: string; onRename?: (name: string) => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (open) dialog.current?.showModal(); else dialog.current?.close();
  }, [open]);
  return <>
    <button type="button" className="button" disabled={!onRename}
      title={!onRename ? t("此原生会话暂不支持直接改名") : t("修改原生历史会话名称")}
      onClick={() => { setName(title); setError(""); setOpen(true); }}>{t("会话改名")}</button>
    <dialog ref={dialog} className="workspace-dialog" aria-label={t("会话改名")} onCancel={() => setOpen(false)} onClose={() => setOpen(false)}>
      <form onSubmit={async e => {
        e.preventDefault();
        if (!name.trim() || saving || !onRename) return;
        setSaving(true); setError("");
        try { await onRename(name.trim()); setOpen(false); }
        catch { setError(t("尚未确认改名结果，请刷新核对后再操作。")); }
        finally { setSaving(false); }
      }}>
        <header><strong>{t("会话改名")}</strong><button type="button" onClick={() => setOpen(false)}>{t("取消")}</button></header>
        <label>{t("会话名称")}<input autoFocus maxLength={120} value={name} onChange={e => setName(e.target.value)} /></label>
        <p className="muted small">{t("直接修改原生会话名称，不发送聊天消息。")}</p>
        {error && <p role="alert">{error}</p>}
        <button className="button primary" type="submit" disabled={saving || !name.trim() || !onRename}>{saving ? t("正在保存…") : t("保存名称")}</button>
      </form>
    </dialog>
  </>;
}
