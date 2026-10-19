import { t } from "./i18n.ts";
import { discoveredProjects } from "./discovery";
import { useEffect, useState } from "react";
import type { Device, Session, WorkspacePreferences } from "./types";

export function WorkspaceSettings({
  preferences,
  sessions,
  devices,
  save,
}: {
  preferences: WorkspacePreferences;
  sessions: Session[];
  devices: Device[];
  save: (preferences: WorkspacePreferences) => Promise<void>;
}) {
  const [nodeId, setNode] = useState(devices[0]?.id || "");
  const [search, setSearch] = useState("");
  const [limit, setLimit] = useState(6);
  const candidates = discoveredProjects(sessions, nodeId, preferences).filter(c => c.path.toLowerCase().includes(search.toLowerCase()));
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!nodeId && devices.length) setNode(devices[0].id);
  }, [devices, nodeId]);
  const update = async (next: WorkspacePreferences) => {
    setBusy(true);
    setError("");
    try {
      await save(next);
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="settings-card workspace-settings">
      <h2>{t("关注的项目")}</h2>
      <p>
        {t("概览优先显示这些项目的活跃主会话。历史可以随时选择继续；子 agent 与其他目录不会挤进待办或推送。")}</p>
      <label>
        {t("工作区范围")}<select
          aria-label={t("工作区范围")}
          disabled={busy}
          value={preferences.workspaceScope}
          onChange={(event) =>
            update({
              ...preferences,
              workspaceScope: event.target.value as "all" | "focused",
            })
          }
        >
          <option value="focused">{t("只关注下面的项目")}</option>
          <option value="all">{t("所有已发现的目录")}</option>
        </select>
      </label>
      <ul className="workspace-list">
        {preferences.workspaces.map((workspace) => (
          <li key={workspace.nodeId + workspace.path}>
            <div>
              <strong>
                {devices.find((d) => d.id === workspace.nodeId)?.name ||
                  t("离线设备")}
              </strong>
              <code>{workspace.path}</code>
            </div>
            <button
              className="button"
              disabled={busy}
              aria-label={t("取消关注 {0}", { 0: workspace.path })}
              onClick={() =>
                update({
                  ...preferences,
                  workspaces: preferences.workspaces.filter(
                    (w) => w !== workspace,
                  ),
                })
              }
            >
              {t("移除")}</button>
          </li>
        ))}
      </ul>
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          if (!path.trim() || !nodeId) return;
          await update({
            workspaceScope: "focused",
            workspaces: [
              ...preferences.workspaces,
              { nodeId, path: path.trim() },
            ],
          });
        }}
      >
        <label>
          {t("执行设备")}<select
            aria-label={t("项目所在设备")}
            value={nodeId}
            onChange={(event) => {setNode(event.target.value); setSearch(""); setLimit(6);}}
          >
            <option value="" disabled>
              {t("选择设备")}</option>
            {devices.map((device) => (
              <option key={device.id} value={device.id}>
                {device.name}
              </option>
            ))}
          </select>
        </label>
        <div className="discovered-projects">
          <h3>{t("发现项目")}</h3>
          <p className="field-hint">{t("来自此设备已有的原生主会话。只推荐、不自动关注；子目录合并显示。")}</p>
          <input aria-label={t("搜索发现的项目")} placeholder={t("搜索已发现的目录")} value={search} onChange={e => {setSearch(e.target.value); setLimit(6);}} />
          {candidates.slice(0,limit).map(c => <div className="discovered-project" key={c.path}>
            <div><strong>{c.path.split('/').pop() || '/'}</strong><small>{c.path}</small><small>{t("{0} 个活跃 · {1} 个会话", { 0: c.active, 1: c.count })}</small></div>
            <button type="button" className="button" disabled={busy || c.followed} onClick={() => update({...preferences, workspaces:[...preferences.workspaces,{nodeId,path:c.path}]})}>{c.followed ? t("已关注") : t("关注")}</button>
          </div>)}
          {!candidates.length && <p className="field-hint">{t("还没有匹配的项目。设备产生原生会话后会自动出现在这里，也可以手动添加。")}</p>}
          {candidates.length > limit && <button type="button" className="text-button" onClick={() => setLimit(n => n+6)}>{t("再显示 6 个")}</button>}
        </div>
        <label>
          {t("手动添加目录（可选）")}<input
            aria-label={t("关注的项目目录")}
            value={path}
            onChange={(event) => setPath(event.target.value)}
            placeholder={t("/Users/你的用户名/Documents/项目")}
            autoCapitalize="none"
            autoCorrect="off"
          />
        </label>
        <button className="button" disabled={busy || !nodeId || !path.trim()}>
          {t("添加项目")}</button>
      </form>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <p className="field-hint">
        {t("范围在手机与电脑间同步，也用于推送。子目录包括在内；移除关注不会停止会话或删除原生历史。")}</p>
    </section>
  );
}
