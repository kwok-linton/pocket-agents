import { t } from "./i18n.ts";
import { memo, useRef, useState, type ComponentPropsWithoutRef } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { artifactPaths } from "./artifact-links";

function CodeBlock({ children }: ComponentPropsWithoutRef<"pre">) {
  const pre = useRef<HTMLPreElement>(null);
  const [status, setStatus] = useState("");
  return <div className="code-block">
    <div className="code-toolbar"><span>{t("代码")}</span><button type="button" onClick={async () => {
      try { await navigator.clipboard.writeText(pre.current?.textContent || ""); setStatus(t("已复制")); }
      catch { setStatus(t("复制失败，请长按选择")); }
    }}>{t("复制代码")}</button><span role="status">{status}</span></div>
    <pre ref={pre} tabIndex={0}>{children}</pre>
  </div>;
}

// Parsing every message again on each keystroke or status tick makes typing
// lag on phones with long transcripts; the inputs are plain strings.
export const MessageMarkdown = memo(function MessageMarkdown({ text, cwd, sessionId }: { text: string; cwd: string; sessionId: string }) {
  return <div className="message-body markdown-body"><Markdown remarkPlugins={[remarkGfm]} components={{
    pre: CodeBlock,
    table: ({ children }) => <div className="markdown-table" tabIndex={0} role="region" aria-label={t("表格，可横向滚动")}><table>{children}</table></div>,
    a: ({ href, children }) => {
      if (href && /^https?:\/\//i.test(href)) return <a href={href} target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">{children}</a>;
      const path = href && artifactPaths(`[file](<${href}>)`, cwd)[0];
      return path ? <a href={`/api/sessions/${encodeURIComponent(sessionId)}/file?path=${encodeURIComponent(path)}`} download={path.split("/").pop()}>{children}</a> : <span>{children}</span>;
    },
    // Do not load tracking pixels or untrusted HTML from native transcripts.
    img: ({ alt }) => <span className="markdown-image-label">{t("[图片：{0}]", { 0: alt || t("附件") })}</span>,
  }}>{text}</Markdown></div>;
});
