import { t } from "./i18n.ts";
import { useEffect, useRef, useState } from "react";
import { api } from "./api";

type Entry = { name: string; path: string; size: number; directory: boolean };
const maximum = 8 * 1024 * 1024;

export function SessionFiles({ sessionId, cwd, online, count, onBusy, onAttach }: {
  sessionId: string; cwd: string; online: boolean; count: number; onBusy: (busy: boolean) => void; onAttach: (path: string) => void;
}) {
  const backdropDown = useRef(false);
  const base = `/api/sessions/${encodeURIComponent(sessionId)}`;
  const [path, setPath] = useState(".");
  const [entries, setEntries] = useState<Entry[]>([]);
  const [menu, setMenu] = useState(false);
  const menuDialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (menu) menuDialog.current?.showModal();
    else menuDialog.current?.close();
  }, [menu]);
  const [progress, setProgress] = useState("");
  const photos = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const fileDialog = useRef<HTMLDialogElement>(null);
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (open) fileDialog.current?.showModal();
    else fileDialog.current?.close();
  }, [open]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const uploadErrorDialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (error && !open) uploadErrorDialog.current?.showModal();
    else uploadErrorDialog.current?.close();
  }, [error, open]);
  const [preview, setPreview] = useState<string>();
  const picker = useRef<HTMLInputElement>(null);
  const load = async () => {
    setLoading(true); setError("");
    try { setEntries(await api<Entry[]>(`${base}/files?path=${encodeURIComponent(path)}`)); setError(""); }
    catch { setError(t("项目文件暂时无法读取，请重试。")); }
    finally { setLoading(false); }
  };
  useEffect(() => { if (open && online) void load(); }, [open, path, online, sessionId]);
  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview); }, [preview]);
  async function upload(file: File) {
    if (file.size > maximum) { setError(t("每个附件最多 8 MiB")); return; }
    try {
      const entry = await api<Entry>(`${base}/files?name=${encodeURIComponent(file.name)}&uploadId=${crypto.randomUUID()}`, {
        method: "POST", body: file, headers: { "Content-Type": "application/octet-stream" },
      });
      onAttach(`${cwd.replace(/\/$/, "")}/${entry.path}`);
    } catch (e) { setError(t("附件未能加入消息，请检查设备连接后重新选择。此操作未发送消息。")); }
  }
  async function uploadFiles(selected: FileList | null) {
    if (!selected?.length) return;
    const files = Array.from(selected);
    if (files.length + count > 8) { setError(t("每条消息最多 8 个附件，请减少选择后重试")); return; }
    setMenu(false); setBusy(true); onBusy(true); setError("");
    try {
      for (let i = 0; i < files.length; i++) {
        setProgress(t("正在上传 {0}/{1}：{2}", { 0: i + 1, 1: files.length, 2: files[i].name }));
        await upload(files[i]);
      }
    } finally { setBusy(false); onBusy(false); setProgress(""); }
  }
  async function showImage(entry: Entry) {
    setBusy(true); setError("");
    try {
      const response = await fetch(`${base}/file?path=${encodeURIComponent(entry.path)}`, { credentials: "same-origin", cache: "no-store" });
      if (!response.ok) throw new Error(t("图片读取失败"));
      const bytes = new Uint8Array(await response.arrayBuffer());
      // Content sniffing permits raster formats only; HTML/SVG never execute.
      const png = bytes[0] === 137 && bytes[1] === 80 && bytes[2] === 78 && bytes[3] === 71;
      const jpeg = bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
      const webp = new TextDecoder().decode(bytes.slice(0, 4)) === "RIFF" && new TextDecoder().decode(bytes.slice(8, 12)) === "WEBP";
      if (!png && !jpeg && !webp) throw new Error(t("预览支持 PNG、JPEG 和 WebP；其他格式请下载"));
      setPreview(URL.createObjectURL(new Blob([bytes], { type: png ? "image/png" : jpeg ? "image/jpeg" : "image/webp" })));
    } catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  }
  return <div className="session-files">
    <input hidden type="file" multiple ref={picker} onChange={(e) => { void uploadFiles(e.target.files); e.target.value = ""; }} />
    <input hidden type="file" multiple accept="image/*" ref={photos} onChange={(e) => { void uploadFiles(e.target.files); e.target.value = ""; }} />
    <button className="attachment-plus" aria-label={t("添加图片或文件")} aria-expanded={menu} disabled={!online || busy} onClick={() => setMenu(!menu)}>＋</button>
    <dialog ref={menuDialog} className="attachment-menu attachment-sheet" aria-label={t("添加附件")} onCancel={() => setMenu(false)} onClose={() => setMenu(false)}
      onPointerDown={e => { const r=e.currentTarget.getBoundingClientRect(); backdropDown.current=e.target===e.currentTarget && (e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom); }}
      onClick={e => { const r=e.currentTarget.getBoundingClientRect(); if(backdropDown.current && e.target===e.currentTarget && (e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom)) setMenu(false); backdropDown.current=false; }}>
      <header className="attachment-sheet-heading"><strong>{t("添加附件")}</strong><button type="button" className="attachment-sheet-close" aria-label={t("关闭附件面板")} onClick={() => setMenu(false)}><svg aria-hidden="true" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="m6 6 12 12M18 6 6 18" /></svg></button></header>
      <div className="attachment-sources">
        <button type="button" disabled={count >= 8} onClick={() => { photos.current?.click(); setMenu(false); }}><span className="attachment-source-icon"><QuickIcon kind="image" /></span><span>{t("照片 / 拍照")}</span></button>
        <button type="button" disabled={count >= 8} onClick={() => { picker.current?.click(); setMenu(false); }}><span className="attachment-source-icon"><QuickIcon kind="file" /></span><span>{t("选择文件")}</span></button>
        <button type="button" disabled={count >= 8} onClick={() => { setOpen(true); setMenu(false); }}><span className="attachment-source-icon"><QuickIcon kind="folder" /></span><span>{t("项目文件")}</span></button>
      </div>
      <small>{t("最多 8 个附件 · 每个 8 MiB")}</small>
    </dialog>
    {progress && <p role="status">{progress}</p>}
    <dialog ref={uploadErrorDialog} className="workspace-dialog" aria-label={t("附件未上传")} onCancel={() => setError("")} onClose={() => setError("")}><p role="alert">{error}</p><button onClick={() => setError("")}>{t("知道了")}</button></dialog>
    <dialog ref={fileDialog} className="workspace-dialog" aria-label={t("项目文件")} onCancel={() => setOpen(false)} onClose={() => setOpen(false)}>
      <header><strong>{t("项目文件")}</strong><button onClick={() => setOpen(false)}>{t("关闭")}</button></header>
      {error && <p role="alert">{error}</p>}
      {loading && <p role="status">{t("正在读取…")}</p>}
      <p>{t("执行设备 · {0}", { 0: path === "." ? t("项目目录") : path })}{" "}<button onClick={() => void load()}>{t("刷新")}</button></p>
      {path !== "." && <button onClick={() => setPath(path.split("/").slice(0, -1).join("/") || ".")}>{t("返回上级")}</button>}
      {!loading && !error && entries.length === 0 && <p>{t("此目录没有可显示的文件")}</p>}
      {entries.map((entry) => <div key={entry.path} className="file-row">
        {entry.directory ? <button onClick={() => setPath(entry.path)}>📁 {entry.name}</button> : <>
          <span>{entry.name} <small>({Math.ceil(entry.size / 1024)} KB)</small></span>
          {entry.size <= maximum && <>
            <a href={`${base}/file?path=${encodeURIComponent(entry.path)}`} download={entry.name}>{t("下载")}</a>
            {/\.(png|jpe?g|webp)$/i.test(entry.name) && <button disabled={busy} onClick={() => void showImage(entry)}>{t("查看图片")}</button>}
            <button disabled={count >= 8 || busy} onClick={() => onAttach(`${cwd.replace(/\/$/, "")}/${entry.path}`)}>{t("加入消息")}</button>
          </>}
        </>}
      </div>)}
      {preview && <div><button onClick={() => setPreview(undefined)}>{t("关闭图片")}</button><img src={preview} alt={t("执行设备上的图片")} style={{ maxWidth: "100%" }} /></div>}
    </dialog>
  </div>;
}

function QuickIcon({kind}: {kind: "image" | "file" | "folder"}) {
  const paths = {image:"M4 4h16v16H4z M4 16l5-5 4 4 3-3 4 4 M15 8h.01",file:"M6 3h8l4 4v14H6z M14 3v5h4 M9 12h6 M9 16h6",folder:"M3 7a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v9H3z"};
  return <svg aria-hidden="true" width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"><path d={paths[kind]} /></svg>;
}
