import { t } from "./i18n.ts";
import { useEffect, useState } from "react";

export function WorkspaceImage({ sessionId, path }: { sessionId: string; path: string }) {
  const [image, setImage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => () => { if (image) URL.revokeObjectURL(image); }, [image]);
  async function load() {
    setBusy(true); setError("");
    try {
      const response = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/file?path=${encodeURIComponent(path)}`, {credentials: "same-origin", cache: "no-store"});
      if (!response.ok) throw new Error(t("图片读取失败，请确认执行设备在线"));
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.length > 8 * 1024 * 1024) throw new Error(t("图片超过 8 MiB，请下载查看"));
      const png = bytes[0] === 137 && bytes[1] === 80 && bytes[2] === 78 && bytes[3] === 71;
      const jpeg = bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
      const webp = new TextDecoder().decode(bytes.slice(0,4)) === "RIFF" && new TextDecoder().decode(bytes.slice(8,12)) === "WEBP";
      if (!png && !jpeg && !webp) throw new Error(t("此文件不是支持预览的 PNG、JPEG 或 WebP 图片"));
      setImage(URL.createObjectURL(new Blob([bytes], {type: png ? "image/png" : jpeg ? "image/jpeg" : "image/webp"})));
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  return <div className="workspace-image">
    <button type="button" disabled={busy} onClick={() => image ? setImage("") : void load()}>{busy ? t("读取图片…") : image ? t("收起图片") : t("查看图片")}</button>
    {error && <p role="alert">{error}</p>}
    {image && <img src={image} alt={path.split("/").pop() || t("会话生成的图片")} />}
  </div>;
}
