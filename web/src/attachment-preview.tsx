import { t } from "./i18n.ts";
import { useEffect, useState } from "react";

export function AttachmentPreview({ sessionId, path, cwd }: { sessionId: string; path: string; cwd: string }) {
  const [image, setImage] = useState("");
  useEffect(() => {
    const prefix = cwd.replace(/\/$/, "") + "/";
    const relative = path.startsWith(prefix) ? path.slice(prefix.length) : "";
    if (!relative) return;
    const controller = new AbortController();
    let url = "";
    setImage("");
    void (async () => {
      try {
        const response = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/file?path=${encodeURIComponent(relative)}`, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
        if (!response.ok) return;
        const bytes = new Uint8Array(await response.arrayBuffer());
        if (controller.signal.aborted || bytes.length > 8 * 1024 * 1024) return;
        const png = bytes[0] === 137 && bytes[1] === 80 && bytes[2] === 78 && bytes[3] === 71;
        const jpeg = bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
        const webp = new TextDecoder().decode(bytes.slice(0, 4)) === "RIFF" && new TextDecoder().decode(bytes.slice(8, 12)) === "WEBP";
        if (!png && !jpeg && !webp) return;
        url = URL.createObjectURL(new Blob([bytes], { type: png ? "image/png" : jpeg ? "image/jpeg" : "image/webp" }));
        setImage(url);
      } catch { /* The attachment remains sendable if its preview is unavailable. */ }
    })();
    return () => { controller.abort(); if (url) URL.revokeObjectURL(url); };
  }, [sessionId, path, cwd]);
  return image ? <img src={image} alt={t("待发送图片")} /> : <span aria-label={t("图片附件")}>▧</span>;
}
