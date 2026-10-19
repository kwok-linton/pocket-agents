import { t } from "./i18n.ts";
import { memo } from "react";
import { artifactPaths } from "./artifact-links";
import { WorkspaceImage } from "./workspace-image";
export const MessageFiles = memo(function MessageFiles({ text, cwd, sessionId }: { text: string; cwd: string; sessionId: string }) {
  const paths = artifactPaths(text, cwd);
  if (!paths.length) return null;
  return <div className="message-files">{paths.map((path) => <div className="message-artifact" key={path}>
    <a href={`/api/sessions/${encodeURIComponent(sessionId)}/file?path=${encodeURIComponent(path)}`} download={path.split("/").pop()}>{t("下载 · {0}", { 0: path.split("/").pop() })}</a>
    {/\.(png|jpe?g|webp)$/i.test(path) && <WorkspaceImage key={`${sessionId}:${path}`} sessionId={sessionId} path={path} />}
  </div>)}</div>;
});
