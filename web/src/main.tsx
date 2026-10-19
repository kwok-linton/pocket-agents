import { terminalConnection, type TerminalData } from "./terminal-connection";
import { SessionRename } from "./session-rename";
import { followKeyboard } from "./keyboard-viewport";
import { watchActionResults } from "./action-recovery";
import { pushEnvironment, syncPushRegistration } from "./push-registration";
import { affectsSession } from "./history-sync";
import { loadView } from "./view-data";
import { readLoop } from "./read-loop";
import { eventConnection } from "./event-connection";
import { languageSetting, setLanguage, t, tServer } from "./i18n.ts";
import { apiFailure, latestApiFailure } from "./native-failure";
import { needsConfirmation } from "./terminal-input";
import { moveKeys, terminalMenu } from "./terminal-menu";
import {Setup} from "./setup";
import { ModelControls, type ModelChoice } from "./model-controls";
import { AppUpdate } from "./app-update";
import { inBrowseScope } from "./discovery";
import { replyProgress } from "./reply-progress";
import { projectPath } from "./project-groups";
import { VoiceInput } from "./voice-input";
import { appendSpeech } from "./voice-text";
import { restoreDraft, settleDraft, type PendingDraft } from "./send-draft";
import { MessageFiles } from "./message-files";
import { AttachmentPreview } from "./attachment-preview";
import { SessionFiles } from "./session-files";
import { transcriptGroups, internalLabel } from "./transcript";
import React, { Suspense, lazy, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
// The Markdown stack is about a third of the bundle and only conversations use
// it; load it on first open instead of on every cold start.
const MessageMarkdown = lazy(() => import("./message-markdown").then((m) => ({ default: m.MessageMarkdown })));
import { createRoot } from "react-dom/client";
import { api, post, APIError, readLocal, writeLocal, forgetValidated, peekValidated } from "./api";
import { PushSettings } from "./push-settings";
import { WorkspaceSettings } from "./workspaces";
import { latestOperation } from "./latest-operation";
import {
  overviewMemory,
  updateOverview,
  matchesOverviewSelection,
  overviewHistoryHints,
  overviewCounts,
  matchesOverviewFilter,
  sortSessions,
  type OverviewView,
} from "./overview";
import {
  eventLabel,
  executionLabel,
  folderName,
  formatTime,
  formatClock,
  formatRelative,
  dayLabel,
  isFinished,
  isActive,
  isUncertain,
  isHistorical,
  inWorkspace,
  isWorking,
  hasBackgroundTask,
  needsAttention,
  providerName,
  stateLabel,
  titleOf,
  uniqueMessages,
  reconcileLatest,
  unread,
} from "./model";
import type {
  Action,
  Approval,
  Candidates,
  Device,
  DirectoryListing,
  Event as AgentEvent,
  History,
  Message,
  Metadata,
  Result,
  Session,
  TrackedAction,
  WorkspacePreferences,
} from "./types";
import "./style.css";

type IconName =
  | "grid"
  | "devices"
  | "plus"
  | "arrow"
  | "back"
  | "bell"
  | "search"
  | "folder"
  | "chevron"
  | "terminal"
  | "refresh"
  | "close"
  | "check"
  | "send"
  | "settings"
  | "star"
  | "copy"
  | "stop"
  | "wifi"
  | "eye";
function Icon({ name, size = 20 }: { name: IconName; size?: number }) {
  const paths: Record<IconName, React.ReactNode> = {
    grid: (
      <>
        <rect x="3" y="3" width="7" height="7" rx="2" />
        <rect x="14" y="3" width="7" height="7" rx="2" />
        <rect x="3" y="14" width="7" height="7" rx="2" />
        <rect x="14" y="14" width="7" height="7" rx="2" />
      </>
    ),
    devices: (
      <>
        <rect x="3" y="3" width="18" height="13" rx="2" />
        <path d="M8 21h8M12 16v5" />
      </>
    ),
    plus: <path d="M12 5v14M5 12h14" />,
    arrow: <path d="M5 12h14m-5-5 5 5-5 5" />,
    back: <path d="m15 5-7 7 7 7" />,
    bell: (
      <>
        <path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4" />
      </>
    ),
    search: (
      <>
        <circle cx="10.5" cy="10.5" r="6.5" />
        <path d="m16 16 5 5" />
      </>
    ),
    folder: (
      <path d="M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />
    ),
    chevron: <path d="m9 5 7 7-7 7" />,
    terminal: (
      <>
        <rect x="2" y="3" width="20" height="18" rx="3" />
        <path d="m6 8 4 4-4 4m7 0h5" />
      </>
    ),
    refresh: (
      <>
        <path d="M20 10a8 8 0 1 0 0 6M20 3v7h-7" />
      </>
    ),
    close: <path d="m6 6 12 12M6 18 18 6" />,
    check: <path d="m4 12 5 5L20 6" />,
    send: (
      <>
        <path d="m3 3 18 9-18 9 4-9-4-9Zm4 9h14" />
      </>
    ),
    settings: (
      <>
        <path d="M4 7h16M4 17h16" />
        <circle cx="9" cy="7" r="3" />
        <circle cx="15" cy="17" r="3" />
      </>
    ),
    star: (
      <path d="m12 3 2.8 5.8 6.4.9-4.6 4.5 1.1 6.4-5.7-3-5.7 3 1.1-6.4-4.6-4.5 6.4-.9L12 3Z" />
    ),
    copy: (
      <>
        <rect x="8" y="8" width="13" height="13" rx="2" />
        <path d="M16 8V3H3v13h5" />
      </>
    ),
    stop: <rect x="6" y="6" width="12" height="12" rx="2" />,
    wifi: (
      <>
        <path d="M2 8a16 16 0 0 1 20 0M5 12a11 11 0 0 1 14 0m-10 4a5 5 0 0 1 6 0" />
        <circle cx="12" cy="20" r=".5" />
      </>
    ),
    eye: (
      <>
        <path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z" />
        <circle cx="12" cy="12" r="3" />
      </>
    ),
  };
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.65"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}
function Empty({
  title,
  children,
}: {
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="empty">
      <span className="empty-icon">
        <Icon name="folder" size={27} />
      </span>
      <h3>{title}</h3>
      <p>{children}</p>
    </div>
  );
}
function Badge({ session }: { session: Session }) {
  return (
    <span
      className={`status ${isWorking(session) ? "working" : session.pending?.length || ["error", "failed"].includes(session.turnState) ? "attention" : isFinished(session) ? "finished" : ""}`}
    >
      <i />
      {session.turnState === "unknown"
        ? isActive(session)
          ? t("已打开 · 轮次未知")
          : isHistorical(session)
            ? t("历史记录")
            : t("运行状态待确认")
        : stateLabel(session.turnState)}
    </span>
  );
}
function Provider({ provider }: { provider: string }) {
  return (
    <span className={`provider ${provider}`}>
      <svg className="provider-symbol" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {provider === "claude" ? Array.from({length: 12}, (_, i) => <path key={i} d="M12 3v6" transform={`rotate(${i * 30} 12 12)`} />) : <><path d="m8 6-6 6 6 6m8-12 6 6-6 6M14 4l-4 16" /></>}
      </svg>
      {providerName(provider)}
    </span>
  );
}
function ActionStatus({
  tracked,
  onQuery,
  onAcknowledge,
}: {
  tracked: TrackedAction;
  onQuery: () => void;
  onAcknowledge: () => void;
}) {
  const { result } = tracked;
  const dismiss = useRef(onAcknowledge);
  dismiss.current = onAcknowledge;
  useEffect(() => {
    // A rejected rename never reached Claude. It is temporary feedback, not
    // an unresolved delivery that should remain across visits and devices.
    if (tracked.action.kind !== "rename" || result.status !== "rejected") return;
    const remaining = Math.max(0, 60_000 - (Date.now() - Date.parse(tracked.at)));
    const timer = setTimeout(() => dismiss.current(), remaining);
    return () => clearTimeout(timer);
  }, [tracked.action.id, tracked.at, result.status]);
  const operation = ({ delete: t("删除原生会话"), rename: t("原生会话改名"), send: t("发送消息"), interrupt: t("停止当前轮次"), terminate: t("结束实例"), create: t("创建会话"), resume: t("继续会话"), approve: t("处理审批"), terminal_input: t("终端输入") } as Record<string, string>)[tracked.action.kind] || t("操作");
  if (result.status === "accepted") {
    // Opening the terminal or pressing a key is visible in its own panel; a
    // lasting "delivered" line there says nothing about the conversation.
    const accepted = ({ send: t("已送达"), rename: t("原生名称已更新"), interrupt: t("停止请求已送达，等待本轮结束"), terminate: t("结束请求已送达"), resume: t("已继续会话"), create: t("会话已创建") } as Record<string, string>)[tracked.action.kind];
    if (!accepted) return null;
    return <p className="delivery-status" role="status">{(result.data as { delivery?: string } | undefined)?.delivery === "native_queue" ? t("已排队，等待当前任务结束") : accepted}</p>;
  }
  return (
    <details
      className={`notice ${result.status === "rejected" || result.status === "unknown" ? "warning" : ""}`}
      role="status"
      // An unconfirmed result blocks new input; show the way out up front.
      open={result.status === "unknown" || undefined}
    >
      <summary>{operation} · {result.status === "submitted" ? t("等待确认") : result.status === "unknown" ? t("结果待核对") : t("未成功")}{result.status === "rejected" && result.message ? `：${tServer(result.message)}` : ""}<span>{t("查看详情")}</span></summary>
      <div>
        <strong>
          {operation} · {
            {
              accepted: (result.data as { delivery?: string } | undefined)?.delivery === "native_queue" ? t("已进入原生队列") : t("原生已接受"),
              submitted: t("已提交 · 等待原生确认"),
              unknown: t("交付状态未知"),
              rejected: t("操作被拒绝"),
            }[result.status]
          }
        </strong>
        <p>
          {(result.message?.startsWith("process restarted;") ? t("服务重启后无法确认此操作结果，请核对原生会话。系统不会自动重复执行。") : tServer(result.message)) ||
            (result.status === "unknown"
              ? t("操作可能已到达原生实例。请先核对会话再决定是否继续，系统不会自动重发。")
              : result.status === "submitted"
                ? t("正在等待原生实例确认；请勿重复发送。")
                : "")}
        </p>
      </div>
      {result.status === "rejected" && (
        <button className="quiet" onClick={onAcknowledge}>{t("关闭错误")}</button>
      )}
      {["unknown", "submitted"].includes(result.status) && (
        <div className="notice-actions">
          <button onClick={onQuery}>{t("查询结果")}</button>
          {["unknown", "submitted"].includes(result.status) && (
            <button className="quiet" onClick={onAcknowledge}>
              {tracked.action.kind === "interrupt" ? t("收起提示，继续操作") : t("我已核对，允许新操作")}
            </button>
          )}
        </div>
      )}
    </details>
  );
}

function App() {
  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const [unreachable, setUnreachable] = useState(false);
  const authentication = useRef<Promise<{ authenticated: boolean }> | null>(
    null,
  );
  const retryConnection = useRef(() => {});
  const [path, setPath] = useState(location.pathname);
  const currentPath = useRef(path);
  currentPath.current = path;
  const viewRequest = useRef<{controller: AbortController; promise: Promise<void>} | undefined>(undefined);
  const [devices, setDevices] = useState<Device[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [preferences, setPreferences] = useState<WorkspacePreferences>({
    workspaceScope: "all",
    workspaces: [],
  });
  const preferencesEpoch = useRef(0);
  const preferencesSaving = useRef(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [online, setOnline] = useState(navigator.onLine);
  const [connected, setConnected] = useState(false);
  const [tracked, setTracked] = useState<TrackedAction[]>(() =>
    readLocal("actions", []),
  );
  const [dismissed, setDismissed] = useState<string[]>(() =>
    readLocal("acknowledged-actions", []),
  );
  const [recent, setRecent] = useState<string[]>(() => readLocal("recent", []));
  const [busy, setBusy] = useState(false);
  const actionLock = useRef(false);
  const terminalEpoch = useRef(0);
  useEffect(() => { terminalEpoch.current++; setTerminal(null); }, [path]);
  const [terminal, setTerminal] = useState<{
    result: Result;
    target: Action;
  } | null>(null);
  const [notifications, setNotifications] = useState<AgentEvent[]>([]);
  const [created, setCreated] = useState<Result | null>(null);
  const refresh = useCallback(() => {
    if (document.visibilityState === "hidden") return Promise.resolve();
    if (viewRequest.current) return viewRequest.current.promise;
    const controller = new AbortController();
    const preferenceReadEpoch = preferencesEpoch.current;
    const promise = loadView(currentPath.current, controller.signal, (value) => {
      if (value.devices) setDevices(value.devices);
      if (value.sessions) setSessions(value.sessions);
      if (value.session) setSessions(old => [...old.filter(s => s.id !== value.session!.id), value.session!]);
      if (value.device) setDevices(old => [...old.filter(d => d.id !== value.device!.id), value.device!]);
      if (value.preferences && preferenceReadEpoch === preferencesEpoch.current && !preferencesSaving.current) setPreferences(value.preferences);
      if (value.session || value.sessions) setLoading(false);
      setError("");
    }).catch(err => {
      if (controller.signal.aborted) return;
      if (err instanceof APIError) {
        setLoading(false);
        if (err.status === 404 && currentPath.current.startsWith("/session/")) {
          const missing = decodeURIComponent(currentPath.current.slice(9));
          setSessions(old => old.filter(s => s.id !== missing));
        }
      }
      setError(err instanceof APIError ? err.message : t("暂时连不上控制端，请确认手机已连接 Tailscale。会自动重试。"));
    }).finally(() => { if (viewRequest.current?.controller === controller) viewRequest.current = undefined; });
    viewRequest.current = {controller,promise};
    return promise;
  }, []);
  useEffect(() => {
    if (!authenticated) return;
    setLoading(true);
    refresh();
    return () => { viewRequest.current?.controller.abort(); viewRequest.current = undefined; };
  }, [authenticated,path,refresh]);
  const navigate = useCallback((next: string) => {
    if (next === location.pathname) return;
    history.replaceState({...history.state, scrollY: window.scrollY}, "");
    history.pushState({pocketParent: location.pathname, scrollY: 0}, "", next);
    setPath(next);
  }, []);
  const goBack = useCallback(() => {
    if (history.state?.pocketParent) history.back();
    else navigate("/");
  }, [navigate]);
  useLayoutEffect(() => {
    const previous = history.scrollRestoration;
    history.scrollRestoration = "manual";
    window.scrollTo({top: history.state?.scrollY || 0, behavior: "instant"});
    return () => { history.scrollRestoration = previous; };
  }, [path]);
  useEffect(() => {
    const pop = () => {
      setPath(location.pathname);
    };
    const connection = () => setOnline(navigator.onLine);
    const unauthorized = () => {
      setAuthenticated(false);
      setSessions([]);
      setDevices([]);
    };
    addEventListener("popstate", pop);
    addEventListener("online", connection);
    addEventListener("offline", connection);
    addEventListener("pocket:unauthorized", unauthorized);
    const authenticate = () => {
      const code = new URLSearchParams(location.hash.slice(1)).get("login");
      if (code) {
        // Remove the single-use code before any navigation. Keep it out of
        // history, localStorage, service-worker caches and API query strings.
        history.replaceState({}, "", location.pathname + location.search);
        setAuthenticated(null);
        authentication.current = post<{ authenticated: boolean }>(
          "/api/login/qr/redeem",
          { code },
        );
      } else if (!authentication.current) {
        authentication.current = api<{ authenticated: boolean }>(
          "/api/bootstrap",
        );
      }
      const request = authentication.current!;
      request
        .then((v) => {
          if (authentication.current !== request) return;
          setAuthenticated(v.authenticated);
          setError("");
        })
        .catch((err) => {
          if (authentication.current !== request) return;
          // A phone with Tailscale off cannot reach the Hub at all. That is
          // not a logout: keep the session and retry instead of showing login.
          if (!code && (!(err instanceof APIError) || err.status !== 401)) {
            authentication.current = null;
            setUnreachable(true);
            return;
          }
          setError(
            code
              ? t("扫码登录未完成，二维码可能已过期或已使用。请在已登录的设备上重新生成后扫码。")
              : err.message,
          );
          setAuthenticated(false);
        })
        .then(() => {
          if (authentication.current === request) setUnreachable(false);
        });
    };
    const retry = () => {
      if (document.visibilityState === "visible" && !authentication.current)
        authenticate();
    };
    authenticate();
    retryConnection.current = retry;
    addEventListener("hashchange", authenticate);
    addEventListener("online", retry);
    document.addEventListener("visibilitychange", retry);
    const retryTimer = setInterval(retry, 5000);
    return () => {
      removeEventListener("popstate", pop);
      removeEventListener("online", connection);
      removeEventListener("offline", connection);
      removeEventListener("pocket:unauthorized", unauthorized);
      removeEventListener("hashchange", authenticate);
      removeEventListener("online", retry);
      document.removeEventListener("visibilitychange", retry);
      clearInterval(retryTimer);
    };
  }, []);
  useEffect(() => {
    writeLocal("actions", tracked.slice(0, 30));
  }, [tracked]);
  useEffect(() => {
    writeLocal("acknowledged-actions", dismissed.slice(-50));
  }, [dismissed]);
  useEffect(() => {
    if (!authenticated) return;
    let reload: ReturnType<typeof setTimeout>, disposed = false;
    let cursor = readLocal<number>("event-cursor", 0);
    const apply = (events: AgentEvent[]) => {
      if (disposed) return;
      let changed = false;
      const fresh: AgentEvent[] = [];
      for (const event of events) {
        if (!event || !Number.isFinite(event.id) || event.id <= cursor)
          continue;
        cursor = event.id;
        fresh.push(event);
        changed = true;
      }
      if (changed) {
        writeLocal("event-cursor", cursor);
        // Busy sessions change every few seconds. A backgrounded app catches
        // up once when it becomes visible instead of reloading every list.
        if (document.visibilityState === "hidden") {
          return;
        }
        window.dispatchEvent(new CustomEvent("pocket:session-change",{detail:fresh}));
        if (fresh.some(event => event.kind === "action" || event.kind === "snapshot"))
          window.dispatchEvent(new Event("pocket:action-change"));
        const current = currentPath.current.startsWith("/session/") ? decodeURIComponent(currentPath.current.slice(9)) : "";
        if (!current || affectsSession(fresh,current)) {
          clearTimeout(reload);
          reload = setTimeout(() => { refresh(); }, 300);
        }
        if (fresh.some(event => ["completed","waiting_input","waiting_approval","error","interrupted"].includes(event.kind))) loadNotifications();
      }
    };
    const loadNotifications = () => {
      if (document.visibilityState === "hidden") return;
      api<AgentEvent[]>("/api/notifications").then(v => { if (!disposed) setNotifications(v || []); }).catch(() => {});
    };
    const reloadAll = () => {
      if (document.visibilityState === "hidden") return;
      refresh();
      loadNotifications();
    };
    const visible = () => {
      if (document.visibilityState === "hidden") {
        clearTimeout(reload);
        viewRequest.current?.controller.abort();
        viewRequest.current = undefined;
      }
    };
    document.addEventListener("visibilitychange", visible);
    const connection = eventConnection({
      url: () => `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/events/ws?after=${cursor}`,
      connected: value => {
        setConnected(value);
        if (value) {
          reloadAll();
          window.dispatchEvent(new Event("pocket:connection-restored"));
        }
      },
      message: message => {
        try {
          const value = JSON.parse(message.data);
          apply(Array.isArray(value) ? value : [value]);
        } catch {
          /* Ignore malformed events; polling reconciles authoritative state. */
        }
      },
    });
    const poll = setInterval(() => {
      if (document.visibilityState === "hidden") return;
      if (!connection.isOpen()) api<AgentEvent[]>(`/api/events?after=${cursor}`)
        .then((v) => apply(v || []))
        .catch(() => {});
      reloadAll();
    }, 30000);
    loadNotifications();
    return () => {
      disposed = true;
      clearTimeout(reload);
      clearInterval(poll);
      document.removeEventListener("visibilitychange", visible);
      connection.stop();
    };
  }, [authenticated, refresh]);
  const selectedId = path.startsWith("/session/")
    ? decodeURIComponent(path.slice(9))
    : "";
  const selected = sessions.find((session) => session.id === selectedId);
  useEffect(() => {
    if (!selectedId) return;
    setRecent((previous) => {
      const next = [
        selectedId,
        ...previous.filter((id) => id !== selectedId),
      ].slice(0, 8);
      writeLocal("recent", next);
      return next;
    });
  }, [selectedId]);
  const updateMetadata = async (
    session: Session,
    change: Partial<Metadata>,
  ) => {
    try {
      const metadata = await api<Metadata>(
        `/api/sessions/${encodeURIComponent(session.id)}/metadata`,
        {
          method: "PATCH",
          body: JSON.stringify({ ...session.metadata, ...change }),
        },
      );
      setSessions((old) =>
        old.map((item) =>
          item.id === session.id ? { ...item, metadata } : item,
        ),
      );
      return true;
    } catch (err) {
      setError((err as Error).message);
      return false;
    }
  };
  const saveResult = (action: Action, result: Result) => {
    const clean = {
      id: action.id,
      kind: action.kind,
      nodeId: action.nodeId,
      provider: action.provider,
      profile: action.profile,
      nativeId: action.nativeId,
    };
    setTracked((previous) =>
      [
        { action: clean, result, at: new Date().toISOString() },
        ...previous.filter((item) => item.action.id !== action.id),
      ].slice(0, 30),
    );
  };
  const execute = async (
    action: Omit<Action, "id">,
    requestId?: string,
  ): Promise<Result | null> => {
    if (actionLock.current) return null;
    actionLock.current = true;
    setBusy(true);
    const request: Action = { ...action, ...(action.kind === "terminal" ? { data: { open: true } } : {}), id: requestId || crypto.randomUUID() };
    const requestPath = currentPath.current;
    const terminalVersion = terminalEpoch.current;
    saveResult(request, { actionId: request.id, status: "submitted" });
    try {
      const result = await post<Result>("/api/actions", request);
      saveResult(request, result);
      if (action.kind === "terminal" && result.status === "accepted") {
        if (currentPath.current === requestPath && terminalEpoch.current === terminalVersion) setTerminal({ result, target: request });

      }
      if (
        action.kind === "create" &&
        result.nativeId &&
        result.status === "accepted"
      )
        setCreated(result);
      if (action.kind !== "terminal_input") await refresh();
      return result;
    } catch (err) {
      const result: Result = {
        actionId: request.id,
        status:
          err instanceof APIError && err.status >= 400 && err.status < 500
            ? "rejected"
            : "unknown",
        message: (err as Error).message,
      };
      saveResult(request, result);
      return result;
    } finally {
      setBusy(false);
      actionLock.current = false;
    }
  };
  const queryResult = async (item: TrackedAction) => {
    try {
      const result = await api<Result>(
        `/api/actions/${encodeURIComponent(item.action.id)}`,
      );
      // Startup prompts poll this every ~1s. An unchanged result needs no
      // tracked-state write, localStorage write or list reload.
      if (JSON.stringify(result) === JSON.stringify(item.result)) return;
      setTracked((old) =>
        old.map((record) =>
          record.action.id === item.action.id ? { ...record, result } : record,
        ),
      );
      if (
        item.action.kind === "create" &&
        result.status === "accepted" &&
        result.nativeId
      ) {
        writeLocal("create-draft", "");
        setCreated(result);
      }
      await refresh();
    } catch (err) {
      setError(t("暂时无法确认操作：{0}", { 0: (err as Error).message }));
    }
  };
  const trackedRef = useRef(tracked);
  trackedRef.current = tracked;
  const pendingActionIDs = tracked.filter(item => !dismissed.includes(item.action.id) && ["submitted", "unknown"].includes(item.result.status)).map(item => item.action.id).sort().join(",");
  useEffect(() => {
    if (!authenticated || !pendingActionIDs) return;
    const ids = pendingActionIDs.split(",");
    return watchActionResults(async signal => {
      // Limit concurrent native reads; never re-post an action to resolve it.
      for (let offset = 0; offset < ids.length && !signal.aborted; offset += 4) {
        await Promise.all(ids.slice(offset, offset + 4).map(async id => {
          const item = trackedRef.current.find(item => item.action.id === id);
          if (!item || !["submitted", "unknown"].includes(item.result.status)) return;
          try {
            const result = await api<Result>(`/api/actions/${encodeURIComponent(id)}`, {signal});
            if (signal.aborted || result.actionId !== id || JSON.stringify(result) === JSON.stringify(item.result)) return;
            setTracked(old => old.map(record => record.action.id === id && ["submitted", "unknown"].includes(record.result.status) ? {...record, result} : record));
            if (item.action.kind === "create" && result.status === "accepted" && result.nativeId) setCreated(result);
          } catch { /* A lost connection is not a rejected or successful action. */ }
        }));
      }
    });
  }, [authenticated, pendingActionIDs]);
  const [pushIssue, setPushIssue] = useState(false);
  useEffect(() => {
    if (!authenticated) return;
    let failed = false;
    const loop = readLoop(async signal => {
      const env = pushEnvironment();
      if (!env.secure || !env.supported || env.permission !== "granted" || (env.ios && !env.standalone)) return;
      try {
        await syncPushRegistration();
        if (!signal.aborted) { failed = false; setPushIssue(false); }
      } catch { if (!signal.aborted) { failed = true; setPushIssue(true); } }
    }, () => document.visibilityState === "visible" && navigator.onLine, () => failed ? 15000 : 300000);
    const resume = () => { if (document.visibilityState === "hidden") loop.pause(); else loop.trigger(); };
    document.addEventListener("visibilitychange", resume);
    window.addEventListener("online", resume);
    window.addEventListener("pocket:connection-restored", resume);
    loop.trigger();
    return () => {
      loop.stop();
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("online", resume);
      window.removeEventListener("pocket:connection-restored", resume);
    };
  }, [authenticated]);
  useEffect(() => {
    if (!created?.nativeId) return;
    const action = tracked.find(
      (item) => item.result.actionId === created.actionId,
    )?.action;
    const match = sessions.find(
      (session) =>
        session.nativeId === created.nativeId &&
        (!action ||
          (session.nodeId === action.nodeId &&
            session.provider === action.provider &&
            session.profile === action.profile)),
    );
    if (match) {
      setCreated(null);
      navigate(`/session/${encodeURIComponent(match.id)}`);
    } else {
      const timer = setTimeout(refresh, 1000);
      return () => clearTimeout(timer);
    }
  }, [created, sessions, tracked, navigate, refresh]);
  const acknowledge = (id: string) => {
    const item = tracked.find((item) => item.action.id === id);
    if (item?.result.status === "rejected" || item?.action.kind === "interrupt") {
      setDismissed((old) => [...old, id]);
      return;
    }
    if (
      window.confirm(
        t("已在原生会话中核对该操作的实际结果？继续发送可能产生重复操作。此操作不会自动重发内容。"),
      )
    )
      setDismissed((old) => [...old, id]);
  };
  const unresolved = (session?: Session) =>
    tracked.find(
      (item) =>
        !dismissed.includes(item.action.id) &&
        ["unknown", "submitted"].includes(item.result.status) &&
        !(item.action.kind === "terminal_input" && item.result.status === "submitted") &&
        (session
          ? item.action.nativeId === session.nativeId &&
            item.action.nodeId === session.nodeId &&
            item.action.provider === session.provider &&
            item.action.profile === session.profile
          : item.action.kind === "create"),
    );
  const latest = selected
    ? tracked.find(
        (item) =>
          item.action.nativeId === selected.nativeId &&
          item.action.nodeId === selected.nodeId &&
          item.action.provider === selected.provider &&
          item.action.profile === selected.profile,
      )
    : tracked.find((item) => item.action.kind === "create");

  // Show "something is waiting for you" outside the app: the tab title on
  // desktop and the home-screen icon badge where the browser supports it.
  const waitingCount = sessions.filter((s) => !s.metadata?.hidden && !s.parentId && needsAttention(s)).length;
  useEffect(() => {
    document.title = waitingCount ? `(${waitingCount}) Pocket Agents` : "Pocket Agents";
    const badge = navigator as Navigator & { setAppBadge?: (n: number) => Promise<void>; clearAppBadge?: () => Promise<void> };
    (waitingCount ? badge.setAppBadge?.(waitingCount) : badge.clearAppBadge?.())?.catch(() => {});
  }, [waitingCount]);
  if (authenticated === null)
    return (
      <div className="boot">
        <Brand />
        {unreachable ? <>
          <p>{t("暂时连不上控制端")}</p>
          <p className="small">{t("请确认手机已连接 Tailscale、控制端电脑已开机。登录状态仍然保留，网络恢复后会自动重连。")}</p>
          <button className="button primary" onClick={() => retryConnection.current()}>{t("重试")}</button>
        </> : <p>{t("连接你的工作空间…")}</p>}
      </div>
    );
  if (!authenticated)
    return (
      <Login
        error={error}
        onLogin={() => {
          setAuthenticated(true);
          setLoading(true);
        }}
      />
    );
  const visibleSessions = sessions.filter(
    (s) => !s.metadata?.hidden && !s.parentId,
  );
  const focusedSessions = visibleSessions.filter((s) =>
    inBrowseScope(s, preferences),
  );
  const focusedIDs = new Set(focusedSessions.map((s) => s.id));
  const focusedNotifications = notifications.filter(
    (event) => !!event.sessionId && focusedIDs.has(event.sessionId),
  );
  return (
    <div className={`app ${selectedId ? "conversation" : ""}`}>
      <aside className="sidebar">
        <Brand />
        <div className="sidebar-caption">{t("你的原生工作空间")}</div>
        <Navigation path={path} navigate={navigate} />
        <button className={`nav-item ${path === "/projects" ? "active" : ""}`} onClick={() => navigate("/projects")}><Icon name="folder" /><span>{t("项目")}</span></button>
        <button className={`nav-item ${path === "/connect" || path === "/setup" ? "active" : ""}`} onClick={() => navigate("/connect")}><Icon name="wifi" /><span>{t("手机访问")}</span></button>
        <button className={`nav-item ${path === "/status" ? "active" : ""}`} onClick={() => navigate("/status")}><Icon name="wifi" /><span>{t("连接状态")}</span></button>
        <div className="sidebar-bottom">
          <span className={`connection-dot ${connected ? "live" : ""}`} />
          {connected ? t("已连接控制端") : t("正在连接控制端")}
          <small>{t("通过你的私有网络")}</small>
        </div>
      </aside>
      <div className="app-body">
        <header className="app-header">
          {!selectedId && ["/settings", "/notifications", "/new", "/setup", "/connect", "/projects", "/status"].includes(path) && <button className={`back-button ${["/new", "/connect"].includes(path) ? "tab-destination" : ""}`} aria-label={t("返回上一页")} onClick={goBack}><Icon name="back" size={20} />{t("返回")}</button>}
          <button
            className="mobile-brand"
            onClick={() => navigate("/")}
            aria-label={t("返回概览")}
          >
            <Brand />
          </button>
          <div className="desktop-breadcrumb">
            {t("工作空间")}{" "}<span>/</span>{" "}
            {selectedId
              ? t("会话")
              : path === "/setup" || path === "/connect"
                ? t("连接与管理")
                : path === "/devices"
                ? t("AI执行设备")
                : path === "/new"
                  ? t("新建AI对话")
                  : path === "/status"
                    ? t("连接状态")
                  : path === "/projects"
                    ? t("项目")
                    : path === "/settings"
                    ? t("设置")
                    : path === "/notifications"
                      ? t("通知")
                      : t("概览")}
          </div>
          <div className="header-actions">
            <span className={`connect-label ${connected ? "live" : ""}`}>
              <i />
              {connected ? t("实时同步") : t("重新连接")}
            </span>
            <button
              className="icon-button"
              aria-label={t("通知收件箱")}
              onClick={() => navigate("/notifications")}
            >
              <Icon name="bell" />
              {focusedSessions.some(needsAttention) && (
                <b className="notification-dot" />
              )}
            </button>
            <button
              className="icon-button"
              title={t("设置")}
              aria-label={t("设置")}
              onClick={() => navigate("/settings")}
            >
              <Icon name="settings" />
            </button>
          </div>
        </header>
        {path !== "/settings" && <AppUpdate settings={false} />}
        {!online && (
          <div className="network-banner" role="status">
            <Icon name="wifi" size={16} />
            {t("手机当前离线。草稿已保留，连接 tailnet 后可继续。")}</div>
        )}
        {error && (
          <div className="error-banner" role="alert">
            <span>{error}</span>
            <button
              className="icon-button"
              aria-label={t("关闭错误")}
              onClick={() => setError("")}
            >
              <Icon name="close" size={16} />
            </button>
          </div>
        )}
        {selectedId ? (
          selected ? (
            <Conversation
              key={selected.id}
              onBack={goBack}
              session={selected}
              connected={connected}
              pushIssue={pushIssue}
              device={devices.find((d) => d.id === selected.nodeId)}
              navigate={navigate}
              execute={execute}
              busy={busy}
              action={unresolved(selected) || (latest && !dismissed.includes(latest.action.id) ? latest : undefined)}
              blocked={!!unresolved(selected)}
              onQuery={queryResult}
              onAcknowledge={acknowledge}
              updateMetadata={updateMetadata}
              recent={
                recent
                  .map((id) => sessions.find((s) => s.id === id))
                  .filter(
                    (s) => s && (s.id === selected.id || focusedIDs.has(s.id)),
                  ) as Session[]
              }
            />
          ) : (
            <main className="page">
              <button className="back-link" onClick={() => navigate("/")}>
                <Icon name="back" />
                {t("概览")}</button>
              <Empty title={loading ? t("正在读取会话…") : t("会话暂时不可用")}>
                {t("请确认AI执行设备已经连接。目标链接已保留；会话被清理后无法从索引恢复。")}</Empty>
              <button className="button" onClick={refresh}>
                <Icon name="refresh" />
                {t("重新读取")}</button>
            </main>
          )
        ) : (path === "/setup" || path === "/connect") ? (
 <Setup devices={devices} navigate={navigate} phoneFirst={path === "/connect"}/>
 ) : path === "/status" ? (
          <ConnectionStatus devices={devices} connected={connected} refresh={refresh}/>
        ) : path === "/devices" ? (
          <Devices
            connected={connected}
            preferences={preferences}
            devices={devices}
            sessions={focusedSessions}
            navigate={navigate}
            refresh={refresh}
          />
        ) : path === "/new" ? (
          <NewSession
            navigate={navigate}
            loading={loading}
            devices={devices}
            preferences={preferences}
            execute={execute}
            busy={busy}
            blocked={!!unresolved()}
            action={unresolved() || latest}
            onQuery={queryResult}
            onAcknowledge={acknowledge}
          />
        ) : (path === "/settings" || path === "/projects") ? (
          <PreferencesPage
            projectsOnly={path === "/projects"}
            sessions={visibleSessions}
            devices={devices}
            preferences={preferences}
            savePreferences={async (value) => {
              preferencesEpoch.current++;
              preferencesSaving.current = true;
              try {
                const saved = await api<WorkspacePreferences>(
                  "/api/preferences",
                  { method: "PUT", body: JSON.stringify(value) },
                );
                setPreferences(saved);
              } finally {
                preferencesSaving.current = false;
                preferencesEpoch.current++;
              }
            }}
            onLogout={async () => {
              await post("/api/logout");
              forgetValidated();
              setAuthenticated(false);
            }}
          />
        ) : path === "/notifications" ? (
          <Notifications
            notifications={focusedNotifications}
            sessions={sessions}
            navigate={navigate}
          />
        ) : (
          <Overview
            sessions={visibleSessions}
            allSessions={sessions}
            preferences={preferences}
            devices={devices}
            loading={loading}
            navigate={navigate}
          />
        )}
        {!selectedId && (
          <nav className="mobile-nav">
            <Navigation path={path} navigate={navigate} />
            <button className={`nav-item ${path === "/connect" ? "active" : ""}`} onClick={() => navigate("/connect")}><Icon name="wifi" /><span>{t("手机访问")}</span></button>
          </nav>
        )}
      </div>
      {terminal && (
        <TerminalPanel
          initial={terminal.result}
          target={terminal.target}
          onClose={() => { terminalEpoch.current++; setTerminal(null); }}
          execute={execute}
          busy={busy}
          blocked={selected ? !!unresolved(selected) : false}
          onQueryPending={() => { const pending = selected && unresolved(selected); if (pending) void queryResult(pending); }}
          onConfirmPending={(label) => {
            const pending = selected && unresolved(selected);
            if (!pending) return true;
            if (!window.confirm(t("上次操作的结果尚未确认。请核对终端当前选项，确定发送“{0}”？只发送这次按键，不会重发之前的操作。", { 0: label }))) return false;
            setDismissed((old) => [...old, pending.action.id]);
            return true;
          }}
        />
      )}
    </div>
  );
}

function Brand() {
  return (
    <div className="brand">
      <svg className="brand-symbol" viewBox="40 68 176 156" aria-hidden="true">
        <circle cx="128" cy="126" r="54" fill="#e6f8b0" />
        <rect x="102" y="96" width="16" height="26" rx="8" fill="#0f1a14" />
        <rect x="138" y="96" width="16" height="26" rx="8" fill="#0f1a14" />
        <path d="M44 132H212V164Q212 180 197 188L142 214Q128 220 114 214L59 188Q44 180 44 164Z" fill="#a9d064" />
        <rect x="66" y="121" width="26" height="23" rx="11.5" fill="#e6f8b0" />
        <rect x="164" y="121" width="26" height="23" rx="11.5" fill="#e6f8b0" />
      </svg>
      <span>
        Pocket<span className="brand-light"> Agents</span>
      </span>
    </div>
  );
}
function Navigation({
  path,
  navigate,
}: {
  path: string;
  navigate: (p: string) => void;
}) {
  return (
    <>
      {[
        { path: "/", label: t("概览"), icon: "grid" },
        { path: "/devices", label: t("AI执行设备"), icon: "devices" },
        { path: "/new", label: t("新建AI对话"), icon: "plus" },
      ].map((item) => (
        <button
          key={item.path}
          className={`nav-item ${path === item.path ? "active" : ""}`}
          onClick={() => navigate(item.path)}
        >
          <Icon name={item.icon as IconName} />
          <span>{item.label}</span>
        </button>
      ))}
    </>
  );
}
function Login({ onLogin, error }: { onLogin: () => void; error: string }) {
  const [token, setToken] = useState(""),
    [busy, setBusy] = useState(false),
    [failure, setFailure] = useState("");
  return (
    <div className="login-page">
      <div className="login-card">
        <Brand />
        <h1>
          {t("你的 Agent，")}<br />
          {t("随时在手边。")}</h1>
        <p>
          {t("连接私有工作空间，继续各台设备上的 Claude Code 与 Codex 原生会话。")}</p>
        <div className="notice">
          <strong>{t("在安装控制端的 Mac 上")}</strong>
          <p>{t("打开本机控制台，管理电脑并生成手机登录二维码。")}</p>
          <a className="button primary" href="pocket-agents://open">{t("打开本机控制台")}{" "}<Icon name="arrow" /></a>
          <p className="small">{t("首次打开请允许浏览器唤起应用。也可在 Finder 的用户「应用程序」中双击 Pocket Agents。此入口仅用于已安装控制端的 Mac。")}</p>
        </div>
        <div className="notice">
          <strong>{t("连接手机或其他浏览器")}</strong>
          <p>{t("在任意已登录的电脑或手机上打开「手机访问」，点击「生成手机登录二维码」，再用另一部手机的相机扫码。")}</p>
        </div>
        <details><summary>{t("安装与恢复帮助")}</summary><p>{t("尚未安装控制端时，需要先在自己的电脑安装。Linux 或无桌面服务器可使用安装后生成的一次性登录链接；已有安装可运行")}{" "}<code>pocket open</code> {" "}{t("恢复入口。")}</p></details>
        <details>
          <summary>{t("使用访问口令登录")}</summary>
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              try {
                await post("/api/login", { token });
                setToken("");
                onLogin();
              } catch (err) {
                setFailure((err as Error).message);
              } finally {
                setBusy(false);
              }
            }}
          >
            <label htmlFor="token">{t("工作空间访问令牌")}</label>
            <input
              id="token"
              type="password"
              autoComplete="current-password"
              required
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={t("粘贴电脑上 pocket token 显示的口令")}
            />
            <button className="button primary" disabled={busy || !token}>
              {busy ? t("正在连接…") : t("进入工作空间")}
              <Icon name="arrow" />
            </button>
          </form>
        </details>
        {(failure || error) && (
          <p className="form-error" role="alert">
            {failure || error}
          </p>
        )}
        <div className="login-foot">
          <Icon name="wifi" size={16} />
          {t("手机与控制端需在同一个 Tailscale 网络中")}</div>
      </div>
    </div>
  );
}
function SessionCardSkeleton() {
  return (
    <div className="session-card skeleton-card" aria-hidden="true">
      <div className="session-card-top"><i className="skeleton-line" style={{ width: "38%" }} /><i className="skeleton-line" style={{ width: "14%", marginLeft: "auto" }} /></div>
      <i className="skeleton-line title" style={{ width: "62%" }} />
      <i className="skeleton-line" style={{ width: "44%" }} />
    </div>
  );
}
function SessionCard({
  session,
  navigate,
  compact = false,
}: {
  session: Session;
  navigate: (p: string) => void;
  compact?: boolean;
}) {
  return (
    <button
      className={`session-card ${compact ? "compact" : ""}`}
      onClick={() => navigate(`/session/${encodeURIComponent(session.id)}`)}
    >
      {/* Status sits beside the agent so a card is three short rows; the
          extra row appears only for user marks and pending requests. */}
      <div className="session-card-top">
        <Provider provider={session.provider} />
        <Badge session={session} />
        <time className="session-time" dateTime={session.updatedAt} title={formatTime(session.updatedAt)}>{formatRelative(session.updatedAt)}</time>
      </div>
      <div className="session-title">
        {unread(session) && <i className="unread-dot" aria-label={t("未读")} />}
        <h3 title={titleOf(session)}>{titleOf(session)}</h3>
        {session.metadata?.favorite && <Icon name="star" size={15} />}
        <Icon name="chevron" size={17} />
      </div>
      <div className="session-context">
        <span>
          <Icon name="devices" size={13} />
          {session.deviceName || t("AI执行设备")}
        </span>
        <span>
          <Icon name="folder" size={13} />
          {folderName(session.cwd)}
        </span>
      </div>
      {(session.metadata?.done || !!session.pending?.length) && (
        <div className="session-card-bottom">
          {session.metadata?.done && (
            <span className="user-done">{t("已标记完成")}</span>
          )}
          {session.pending?.length ? (
            <span className="pending-count">
              {t("{0} 项待处理", { 0: session.pending.length })}</span>
          ) : null}
        </div>
      )}
    </button>
  );
}
function Overview({
  sessions,
  allSessions,
  devices,
  loading,
  navigate,
  preferences,
}: {
  sessions: Session[];
  allSessions: Session[];
  devices: Device[];
  loading: boolean;
  navigate: (p: string) => void;
  preferences: WorkspacePreferences;
}) {
  const [view, setView] = useState(overviewMemory.read);
  const [filtersOpen, setFiltersOpen] = useState(false);
  // Cards show "N 分钟前"; refresh that once a minute even when idle.
  const [, setMinute] = useState(0);
  useEffect(() => { const timer = setInterval(() => setMinute((m) => m + 1), 60000); return () => clearInterval(timer); }, []);
  const { filter, showAllProjects, projectKey, query, provider, node, limit } =
    view;
  useEffect(() => overviewMemory.write(view), [view]);
  const fieldSetter =
    <K extends keyof OverviewView>(key: K) =>
    (value: React.SetStateAction<OverviewView[K]>) =>
      setView((old) =>
        updateOverview(old, {
          [key]: typeof value === "function" ? value(old[key]) : value,
        }),
      );
  const setFilter = fieldSetter("filter"),
    setShowAllProjects = fieldSetter("showAllProjects"),
    setProjectKey = fieldSetter("projectKey"),
    setQuery = fieldSetter("query"),
    setProvider = fieldSetter("provider"),
    setNode = fieldSetter("node"),
    setLimit = fieldSetter("limit");
  const selectedProject =
    !showAllProjects &&
    preferences.workspaceScope === "focused" &&
    preferences.workspaces.find(
      (workspace) => workspace.nodeId + workspace.path === projectKey,
    );
  const viewPreferences: WorkspacePreferences = selectedProject
    ? { workspaceScope: "focused", workspaces: [selectedProject] }
    : preferences;
  const scoped = sessions.filter(
    (s) => showAllProjects || (selectedProject ? inWorkspace(s, viewPreferences) : inBrowseScope(s, preferences)),
  );
  const selectedSessions = scoped.filter((s) =>
    matchesOverviewSelection(s, view),
  );
  const uncertain = selectedSessions.filter(isUncertain);
  const counts = overviewCounts(selectedSessions);
  const historyOnlyProviders = overviewHistoryHints(selectedSessions, query);
  const matching = (
    filter === "hidden"
      ? allSessions.filter(
          (s) =>
            s.metadata?.hidden &&
            !s.parentId &&
            (showAllProjects || (selectedProject ? inWorkspace(s, viewPreferences) : inBrowseScope(s, preferences))),
        )
      : scoped
  )
    .filter(
      (s) =>
        matchesOverviewSelection(s, view) &&
        matchesOverviewFilter(s, filter),
    );
  const filtered = sortSessions(matching, filter === "active" || filter === "attention");
  return (
    <main className="page overview">
      <div className="page-heading">
        <div>
          <span className="eyebrow">WORKSPACE OVERVIEW</span>
          <h1>{t("工作，都在这里。")}</h1>
          <p>
            {loading ? t("正在读取工作空间…") : <>
            {preferences.workspaceScope === "focused" && !showAllProjects
              ? t("{0} 个关注项目", { 0: preferences.workspaces.length })
              : t("所有已发现项目")}{" "}
            <span className="dot-divider">·</span> {" "}{counts.background > 0
              ? t("{0} 个正在工作 · {1} 个有后台任务", { 0: counts.working, 1: counts.background })
              : t("{0} 个会话正在工作", { 0: counts.working })}</>}
          </p>
        </div>
        <button
          className="button primary desktop-new"
          onClick={() => navigate("/new")}
        >
          <Icon name="plus" />
          {t("新建AI对话")}</button>
      </div>
      <div className="project-scope-bar">
        <div className="project-chips">
          {preferences.workspaceScope === "focused" &&
            !showAllProjects &&
            preferences.workspaces.map((workspace) => (
              <button
                className={
                  projectKey === workspace.nodeId + workspace.path
                    ? "selected"
                    : ""
                }
                aria-pressed={projectKey === workspace.nodeId + workspace.path}
                onClick={() =>
                  setProjectKey((old) =>
                    old === workspace.nodeId + workspace.path
                      ? ""
                      : workspace.nodeId + workspace.path,
                  )
                }
                key={workspace.nodeId + workspace.path}
                title={`${devices.find((d) => d.id === workspace.nodeId)?.name || t("设备")} · ${workspace.path}`}
              >
                {folderName(workspace.path)}
              </button>
            ))}
        </div>
        <button className="text-button" onClick={() => navigate("/projects")}>
          {t("发现 / 管理项目")}</button>
        {preferences.workspaceScope === "focused" && (
          <button
            className="text-button"
            onClick={() => setShowAllProjects((old) => !old)}
          >
            {showAllProjects ? t("返回关注项目") : t("临时查看所有目录")}
          </button>
        )}
      </div>
      {!!uncertain.length && (
        <p className="notice warning">
          {t("{0} 个会话的运行状态暂时无法确认；它们没有被当成已结束。", { 0: uncertain.length })}{" "}<button
            className="text-button"
            onClick={() => setFilter("uncertain")}
          >
            {t("查看待确认会话")}</button>
        </p>
      )}
      <div className="summary-grid">
        {[
          {
            id: "active",
            label: t("活跃会话"),
            value: counts.active,
            hint: t("当前仍打开的主会话"),
            tone: "green",
          },
          {
            id: "attention",
            label: t("需要回答 / 审批"),
            value: counts.attention,
            hint: t("仅当前实例的真实等待"),
            tone: "amber",
          },
          {
            id: "history",
            label: t("历史会话"),
            value: counts.history,
            hint: t("选择原生记录，按需恢复"),
            tone: "gray",
          },
        ].map((item) => (
          <button
            key={item.id}
            className={`summary-card ${item.tone} ${filter === item.id ? "selected" : ""}`}
            onClick={() => setFilter(item.id)}
          >
            <span className="summary-label">
              <i />
              {item.label}
            </span>
            <strong aria-busy={loading || undefined}>{loading ? "–" : item.value}</strong>
            <span className="summary-hint">{item.hint}</span>
          </button>
        ))}
      </div>
      <div className="section-heading">
        <h2>
          {
            {
              attention: t("需要你的下一步"),
              active: t("活跃主会话"),
              history: t("选择历史，继续对话"),
              uncertain: t("运行状态待确认"),
              working: t("Agent 正在处理"),
              background: t("后台任务中的会话"),
              finished: t("已完成的这一轮"),
              all: t("全部会话"),
              favorite: t("收藏会话"),
              hidden: t("已隐藏会话"),
            }[filter]
          }
        </h2>
        <select
          aria-label={t("会话视图")}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        >
          <option value="active">{t("活跃会话")}</option>
          <option value="attention">{t("需要回答 / 审批")}</option>
          <option value="history">{t("历史会话")}</option>
          {!!uncertain.length && <option value="uncertain">{t("状态待确认")}</option>}
          <option value="working">{t("正在工作")}</option>
          <option value="background">{t("后台任务")}</option>
          <option value="finished">{t("本轮结束")}</option>
          <option value="all">{t("全部会话")}</option>
          <option value="favorite">{t("收藏")}</option>
          <option value="hidden">{t("已隐藏")}</option>
        </select>
      </div>
      {/* On phones the device/agent pickers fold behind one button so the
          session list starts within the first screen. */}
      <div className={`filters ${filtersOpen || node || provider ? "open" : ""}`}>
        <div className="search-field">
          <Icon name="search" size={18} />
          <input
            aria-label={t("搜索会话")}
            placeholder={t("搜索会话、目录或设备")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        <button
          type="button"
          className={`filter-toggle ${node || provider ? "active" : ""}`}
          aria-expanded={filtersOpen || !!node || !!provider}
          onClick={() => setFiltersOpen((open) => !open)}
        >
          <Icon name="settings" size={16} />
          {t("筛选{0}", { 0: node || provider ? ` · ${[node, provider].filter(Boolean).length}` : "" })}</button>
        <div className="filter-selects">
          <select
            aria-label={t("按设备筛选")}
            value={node}
            onChange={(e) => setNode(e.target.value)}
          >
            <option value="">{t("所有设备")}</option>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
          <select
            aria-label={t("按 Agent 筛选")}
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
          >
            <option value="">{t("所有 Agent")}</option>
            <option value="claude">Claude Code</option>
            <option value="codex">Codex</option>
          </select>
        </div>
      </div>
      {!loading &&
        filter === "active" &&
        historyOnlyProviders.map((item) => (
          <div className="provider-history-hint" key={item.provider}>
            <span>
              {query
                ? t("{0} 有 {1} 条匹配的历史会话", { 0: providerName(item.provider), 1: item.history })
                : t("{0} 当前没有活跃会话", { 0: providerName(item.provider) })}
            </span>
            <button
              className="text-button"
              onClick={() =>
                setView((old) =>
                  updateOverview(old, {
                    provider: item.provider,
                    filter: "history",
                  }),
                )
              }
            >
              {t("在 {0} 历史中查看（{1}）", { 0: providerName(item.provider), 1: item.history })}<Icon name="arrow" size={14} />
            </button>
          </div>
        ))}
      {loading ? (
        <div className="session-grid" aria-busy="true" aria-label={t("正在读取原生会话…")}>
          {[0, 1, 2].map((i) => <SessionCardSkeleton key={i} />)}
        </div>
      ) : filtered.length ? (
        <div className="session-grid">
          {filtered.slice(0, limit).map((session) => (
            <SessionCard
              key={session.id}
              session={session}
              navigate={navigate}
            />
          ))}
        </div>
      ) : (
        <Empty
          title={
            query
              ? t("当前视图没有匹配的会话")
              : filter === "attention"
                ? t("目前没有需要回答或批准的请求")
                : filter === "active"
                  ? t("这些项目当前没有活跃实例")
                  : t("这里还没有会话")
          }
        >
          {sessions.length
            ? query
              ? t("搜索词会保留；可以调整会话视图、设备、Agent 或关注项目。")
              : t("可以从历史选择原生会话继续，或切换关注的项目。")
            : t("接入一台设备后，已有原生会话会自动出现在这里。")}
          {(!sessions.length ||
            (counts.history > 0 && filter !== "history")) && (
            <button
              className="text-button"
              onClick={() =>
                sessions.length ? setFilter("history") : navigate("/devices")
              }
            >
              {sessions.length ? t("选择历史会话") : t("接入设备")}
              <Icon name="arrow" size={16} />
            </button>
          )}
        </Empty>
      )}
      {filtered.length > limit && (
        <button
          className="load-more"
          onClick={() => setLimit((value) => value + 40)}
        >
          {t("显示更多（已显示 {0} / {1}）", { 0: limit, 1: filtered.length })}</button>
      )}
      <div className="workspace-note">
        <span className="connection-dot live" />
        {t("会话内容来自设备上的原生记录；收藏、隐藏等整理只保存在本应用")}</div>
    </main>
  );
}

function latestHistoryPath(id: string) {
  return `/api/sessions/${encodeURIComponent(id)}/history?limit=20&sync=1`;
}

type Execute = (action: Omit<Action, "id">, requestId?: string) => Promise<Result | null>;
function Conversation({
  connected, pushIssue,
  onBack,
  session,
  device,
  navigate,
  execute,
  busy,
  blocked,
  action,
  onQuery,
  onAcknowledge,
  updateMetadata,
  recent,
}: {
  connected: boolean; pushIssue: boolean;
  onBack: () => void;
  session: Session;
  device?: Device;
  navigate: (p: string) => void;
  execute: Execute;
  busy: boolean;
  blocked: boolean;
  action?: TrackedAction;
  onQuery: (a: TrackedAction) => void;
  onAcknowledge: (id: string) => void;
  updateMetadata: (s: Session, m: Partial<Metadata>) => Promise<boolean>;
  recent: Session[];
}) {
  const [history, setHistory] = useState<History>(() => peekValidated<History>(latestHistoryPath(session.id)) || {
      messages: [],
      hasMore: false,
    }),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true),
    [older, setOlder] = useState(false);
  const [draft, setDraft] = useState(() =>
      readLocal<string>(`draft:${session.id}`, ""),
    ),
    [more, setMore] = useState(false),
    [showInternal, setShowInternal] = useState(false),
    [alias, setAlias] = useState(session.metadata?.alias || "");
  const [sending, setSending] = useState(false);
 const [modelChoice,setModelChoice] = useState<ModelChoice>({model:"",effort:""});
 const [toolPanel,setToolPanel] = useState<"model"|"actions"|null>(null);
  const [progressNow, setProgressNow] = useState(Date.now);
  useEffect(() => { const timer = setInterval(() => setProgressNow(Date.now()), 5000); return () => clearInterval(timer); }, []);
  const [uploading, setUploading] = useState(false);
  const [voicing, setVoicing] = useState(false);
  const [draftExpanded, setDraftExpanded] = useState(false);
  const [writing, setWriting] = useState(false);
  const longDraft = draft.length > 48 || draft.includes("\n");
  const draftInput = useRef<HTMLTextAreaElement>(null);
  // One forced layout per change, before paint; covers typing, voice and restore.
  useLayoutEffect(() => {
    const input = draftInput.current;
    if (input) { input.style.height = "auto"; input.style.height = `${Math.min(input.scrollHeight, 120)}px`; }
  }, [draft, writing]);
  const [files, setFiles] = useState<string[]>(() => readLocal<string[]>(`files:${session.id}`, []));
  useEffect(() => { writeLocal(`files:${session.id}`, files); }, [session.id, files]);
  const [pendingDraft, setPendingDraft] = useState<PendingDraft | null>(() => readLocal(`pending-draft:${session.id}`, null));
  useEffect(() => {
    if (!pendingDraft || !action || action.action.id !== pendingDraft.actionId) return;
    const settled = settleDraft(pendingDraft, action.result, draft, files);
    if (!settled) {
      // Migrate old saved submissions out of the composer once, without losing them.
      if (!pendingDraft.detached) {
        setDraft((current) => current === pendingDraft.text ? "" : current);
        setFiles((current) => current.filter(path => !pendingDraft.files.includes(path)));
        const detached = { ...pendingDraft, detached: true };
        writeLocal(`pending-draft:${session.id}`, detached);
        setPendingDraft(detached);
      }
      return;
    }
    setDraft(settled.text); setFiles(settled.files);
    writeLocal(`pending-draft:${session.id}`, null);
    setPendingDraft(null);
  }, [pendingDraft, action, draft, files, session.id]);
  const [newOutput, setNewOutput] = useState(false);
  // Errors from one-off actions; the history poll clears its own error.
  const [actionError, setActionError] = useState("");
  const [awayFromBottom, setAwayFromBottom] = useState(false);
  const composing = useRef(false),
    pane = useRef<HTMLDivElement>(null),
    bottom = useRef<HTMLDivElement>(null),
    initialized = useRef(false),
    previousSignature = useRef(""),
    latestSession = useRef(session),
    awayRef = useRef(false);
  useEffect(() => {
    const scroll = pane.current;
    if (!scroll) return;
    let measuredHeight = scroll.clientHeight;
    let measuredContent = scroll.scrollHeight;
    const measure = () => {
      // A growing draft or keyboard can dispatch scroll before ResizeObserver.
      // Keep the previous reading intent through that layout change.
      const resized = measuredHeight !== scroll.clientHeight || measuredContent !== scroll.scrollHeight;
      if (resized && !awayRef.current) scroll.scrollTop = scroll.scrollHeight;
      measuredHeight = scroll.clientHeight;
      measuredContent = scroll.scrollHeight;
      const away = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight > 64;
      awayRef.current = away;
      setAwayFromBottom(away);
      if (!away) setNewOutput(false);
    };
    scroll.addEventListener("scroll", measure, {passive: true});
    // Content also grows without new history: the progress row, code blocks
    // and images finishing layout, the keyboard shrinking the pane. A reader
    // who was at the bottom stays there; one reading older messages is left.
    const observer = new ResizeObserver(() => {
      if (!awayRef.current) scroll.scrollTop = scroll.scrollHeight;
      measure();
    });
    observer.observe(scroll);
    if (scroll.firstElementChild) observer.observe(scroll.firstElementChild);
    measure();
    return () => { scroll.removeEventListener("scroll", measure); observer.disconnect(); };
  }, [session.id]);
  latestSession.current = session;
  const atBottom = () => {
    const scroll = pane.current;
    return !scroll || scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight <= 64;
  };
  // Output watched as it arrives is read. Each mark is a metadata write that
  // fans out to every client, so throttle it and settle once when leaving.
  const lastRead = useRef(0);
  const markRead = (now = false) => {
    if (!initialized.current || (!now && Date.now() - lastRead.current < 30000)) return;
    lastRead.current = Date.now();
    updateMetadata(latestSession.current, { readAt: new Date().toISOString() });
  };
  useEffect(() => () => {
    const current = latestSession.current;
    if (lastRead.current && !awayRef.current && Date.parse(current.updatedAt) > Date.parse(current.metadata?.readAt || "")) markRead(true);
  }, [session.id]);
  const target = {
    nodeId: session.nodeId,
    provider: session.provider,
    profile: session.profile,
    nativeId: session.nativeId,
    executionId: session.executionId,
  };
  useEffect(() => {
    const root = pane.current?.closest<HTMLElement>(".app-body");
    if (root) return followKeyboard(root, window.visualViewport, ({composing}) => {
      const active = window.matchMedia("(max-width: 760px)").matches && composing;
      setWriting(active);
      if (active) setToolPanel(null);
    });
  }, [session.id]);
  const capability = session.capabilities;
  const nativeFailure = initialized.current && !isWorking(session) ? latestApiFailure(history.messages) : null;
  const nativeAttention = !nativeFailure && needsAttention(session) && !(session.pending?.length);
  const permissionNotice = !nativeFailure && session.turnState === "waiting_approval" && session.error?.includes("open native terminal");
  useEffect(() => {
    writeLocal(`draft:${session.id}`, draft);
  }, [session.id, draft]);
  useEffect(() => {
    let stale = false;
    let last: History | undefined;
    const loop = readLoop((signal) => api<History>(
      latestHistoryPath(session.id), {signal},
    )
      .then((value) => {
        if (stale || signal.aborted) return;
        if (value === last) {
          // Unchanged (304), but a previous read may have failed.
          setError("");
          setLoading(false);
          return;
        }
        last = value;
        const next = { ...value, messages: value.messages || [] };
        const signature = JSON.stringify(
          next.messages.map((m) => [m.id, m.text]),
        );
        const initial = !initialized.current;
        if (!initial && signature !== previousSignature.current) {
          // Follow the reply while the reader is already at the bottom; only
          // someone reading older messages gets the "new output" marker.
          if (atBottom()) requestAnimationFrame(() => {
            pane.current?.scrollTo({top: pane.current.scrollHeight, behavior: "instant"});
            markRead();
          });
          else setNewOutput(true);
        }
        previousSignature.current = signature;
        setHistory((old) => initial ? next : reconcileLatest(old, next));
        setError("");
        setLoading(false);
        if (initial) {
          initialized.current = true;
          requestAnimationFrame(() => {
            bottom.current?.scrollIntoView({ block: "end" });
            markRead(true);
          });
        }
      })
      .catch((err) => {
        if (!stale && !signal.aborted) {
          setError(err.message.includes("node is offline") ? t("AI执行设备暂时未连接，连接恢复后会自动重试。") : err.message);
          setLoading(false);
        }
      }), () => document.visibilityState === "visible", () => isWorking(latestSession.current) ? 5000 : 30000);
    const changed = (event: Event) => {
      const events = (event as CustomEvent<AgentEvent[]>).detail;
      if (affectsSession(events,session.id,session.nodeId)) loop.trigger();
    };
    const visible = () => { if (document.visibilityState === "hidden") loop.pause(); else loop.trigger(); };
    loop.trigger();
    const restored = () => { loop.pause(); loop.trigger(); };
    window.addEventListener("pocket:session-change",changed);
    window.addEventListener("pocket:connection-restored", restored);
    document.addEventListener("visibilitychange", visible);
    return () => {
      stale = true;
      loop.stop();
      window.removeEventListener("pocket:session-change",changed);
      window.removeEventListener("pocket:connection-restored", restored);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [session.id, device?.online]);
  const olderRequest = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => olderRequest.current?.abort(),[session.id]);
  const loadOlder = async () => {
    const scroll = pane.current,
      height = scroll?.scrollHeight || 0,
      top = scroll?.scrollTop || 0;
    if (olderRequest.current) return;
    const controller = new AbortController();
    olderRequest.current = controller;
    setOlder(true);
    setActionError("");
    try {
      const next = await api<History>(
        `/api/sessions/${encodeURIComponent(session.id)}/history?cursor=${encodeURIComponent(history.cursor || "")}&limit=60`, {signal:controller.signal},
      );
      if (controller.signal.aborted) return;
      setHistory((old) => ({
        ...next,
        messages: uniqueMessages([...(next.messages || []), ...old.messages]),
      }));
      requestAnimationFrame(() => {
        if (scroll) scroll.scrollTop = top + (scroll.scrollHeight - height);
      });
    } catch (err) {
      if (!controller.signal.aborted) setActionError(t("更早的消息读取失败：{0}", { 0: (err as Error).message }));
    } finally {
      olderRequest.current = undefined;
      if (!controller.signal.aborted) setOlder(false);
    }
  };
  const send = async () => {
    if (
      (!draft.trim() && files.length === 0) || uploading || voicing ||
      busy ||
      blocked ||
      !(capability.send || capability.resume) ||
      !device?.online ||
      composing.current
    )
      return;
    const sent = draft;
    const sentFiles = files;
    const requestId = crypto.randomUUID();
    const pending = { actionId: requestId, text: sent, files: sentFiles, detached: true };
    writeLocal(`pending-draft:${session.id}`, pending);
    setPendingDraft(pending);
    // The submission owns its content now; the composer is for the next message.
    writeLocal(`draft:${session.id}`, "");
    writeLocal(`files:${session.id}`, []);
    setDraft("");
    setFiles([]);
    setSending(true);
    requestAnimationFrame(() => bottom.current?.scrollIntoView({block: "end"}));
    let result: Result | null;
    try { result = await execute({ ...target, model: modelChoice.model || undefined, effort: modelChoice.effort || undefined, kind: capability.send ? "send" : "resume", cwd: session.cwd, text: sent, files: sentFiles }, requestId); }
    finally { setSending(false); }
    // Rejected means nothing reached the native session, so putting the text
    // back is not a resend. Unknown results stay out of the composer.
    if (!result || result.status === "rejected") {
      setDraft((current) => restoreDraft(pending, current, []).text);
      setFiles((current) => restoreDraft(pending, "", current).files);
      writeLocal(`pending-draft:${session.id}`, null);
      setPendingDraft(null);
    }
    if (result?.status === "accepted") {
      setModelChoice({model:"",effort:""});
      requestAnimationFrame(() =>
        bottom.current?.scrollIntoView({ behavior: "smooth", block: "end" }),
      );
    }
  };
  const resume = () =>
    execute({
      ...target,
      kind: "resume",
      cwd: session.cwd,
      text: undefined,
    });
  const progress = replyProgress({online: !!device?.online, sending, working: isWorking(session), background: hasBackgroundTask(session), attention: !nativeFailure && needsAttention(session), action, messages: history.messages, now: progressNow});
  // Consecutive replies from the same sender within five minutes read as one
  // block: only the first shows the name and time. Hidden tool activity in
  // between does not break a block.
  const continued = new Set<string>();
  {
    let previous: Message | undefined;
    for (const group of transcriptGroups(history.messages)) {
      if (group.internal) {
        if (showInternal) previous = undefined;
        continue;
      }
      for (const message of group.messages) {
        if (previous && previous.role === message.role && Date.parse(message.at) - Date.parse(previous.at) < 300000) continued.add(message.id);
        previous = message;
      }
    }
  }
  const viewLatest = () => {
    awayRef.current = false;
    pane.current?.scrollTo({top: pane.current.scrollHeight, behavior: "instant"});
    setAwayFromBottom(false);
    setNewOutput(false);
    markRead(true);
  };
  return (
    <section className="conversation-page">
      <div className="conversation-header">
        <button
          className="back-button"
          aria-label={t("返回上一页")}
          onClick={onBack}
        >
          <Icon name="back" size={20} />{t("返回")}</button>
        <div className="conversation-heading">
          <h1>{titleOf(session)}</h1>
          <div>
            <span>{session.deviceName || device?.name}</span>
            <span> / </span>
            <span title={session.cwd}>{folderName(session.cwd)}</span>
          </div>
        </div>
        <button
          className="icon-button"
          aria-label={t("会话整理与详情")}
          onClick={() => setMore(!more)}
        >
          <Icon name="settings" />
        </button>
      </div>
      <div className="conversation-info">
        <Provider provider={session.provider} />
        <Badge session={session} />
        <span className={`device-state ${device?.online ? "online" : ""}`}>
          {device?.online ? t("设备在线") : t("设备离线")}
        </span>
      </div>
      {!connected && <div className="connection-recovery" role="status">
        <span>{t("连接中断，正在自动重连。草稿已保留。")}</span>
        <button type="button" onClick={() => window.dispatchEvent(new Event("pocket:retry-connection"))}>{t("立即重连")}</button>
      </div>}
      {pushIssue && <div className="connection-recovery" role="status">
        <span>{t("通知登记暂未恢复，正在自动重试。")}</span>
        <button type="button" onClick={() => navigate("/notifications")}>{t("查看通知")}</button>
      </div>}
      {more && (
        <div className="session-details">
          <label><input type="checkbox" checked={showInternal} onChange={(e) => setShowInternal(e.target.checked)} /> {" "}{t("显示工具及后台活动")}</label>
          <label>
            {t("显示别名")}<div className="inline-input">
              <input
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
                placeholder={t("仅改变本应用中的名称")}
              />
              <button
                className="button"
                onClick={() => updateMetadata(session, { alias })}
              >
                {t("保存")}</button>
            </div>
          </label>
          <SessionRename title={session.title} onRename={(session.provider === "codex" || (session.provider === "claude" && (isHistorical(session) || capability.terminal))) && !busy && !!device?.online ? async name => {
                const result = await execute({ ...target, kind: "rename", text: name });
                if (result?.status !== "accepted") throw new Error(result?.message || t("原生改名尚未确认，请稍后核对会话名称"));
                if (!await updateMetadata(session, {alias: ""})) throw new Error("native name saved but old display alias remains");
                setAlias("");
              } : undefined} />
          <div className="button-row">
            <button
              className={`button ${session.metadata?.favorite ? "selected" : ""}`}
              onClick={() =>
                updateMetadata(session, {
                  favorite: !session.metadata?.favorite,
                })
              }
            >
              <Icon name="star" />
              {session.metadata?.favorite ? t("取消收藏") : t("收藏")}
            </button>
            <button
              className="button"
              onClick={() =>
                updateMetadata(session, { done: !session.metadata?.done })
              }
            >
              <Icon name="check" />
              {session.metadata?.done ? t("取消标记完成") : t("标记完成")}
            </button>
            <button
              className="button"
              onClick={() =>
                updateMetadata(session, { hidden: !session.metadata?.hidden })
              }
            >
              <Icon name="eye" />
              {session.metadata?.hidden ? t("取消隐藏") : t("隐藏会话")}
            </button>
            <button
              className="button"
              onClick={() =>
                updateMetadata(session, {
                  readAt: unread(session)
                    ? new Date().toISOString()
                    : "0001-01-01T00:00:00Z",
                })
              }
            >
              {unread(session) ? t("标为已读") : t("标为未读")}
            </button>
          </div>
          <p className="small muted">
            {t("“标记完成”是你的整理标记，不代表代码或测试已验证。")}</p>
          {/* Irreversible: kept apart from the everyday organizing buttons. */}
          <div className="button-row danger-zone">
            <button className="button danger" disabled={busy || !device?.online} onClick={async () => {
              if (!window.confirm(t("永久删除「{0}」的原生会话记录？\n电脑上的该会话历史也会删除，无法撤销。项目目录、代码及附件不会删除。Claude 后台实例会先结束，正在执行的任务会中断。其他原生客户端需先关闭会话。", { 0: session.metadata?.alias || session.title }))) return;
              setActionError("");
              const result = await execute({ ...target, cwd: session.cwd, kind: "delete", decision: session.provider === "claude" ? "stop_and_delete" : undefined });
              if (result?.status === "accepted") navigate("/");
              else setActionError(t("删除未完成：{0}", { 0: tServer(result?.message) || t("结果尚未确认，请查看操作结果。") }));
            }}>{session.provider === "claude" && session.executionState === "alive" ? t("结束并删除会话") : t("删除原生会话")}</button>
          </div>
          <div className="path-detail">
            <code>{session.cwd}</code>
            <button
              className="icon-button"
              aria-label={t("复制目录")}
              onClick={() => copy(session.cwd)}
            >
              <Icon name="copy" size={16} />
            </button>
          </div>
          <p className="small">
            {executionLabel(session.executionState)} · Profile:{" "}
            {session.profile}
          </p>
          {recent.length > 1 && (
            <label>
              {t("最近会话")}<select
                value={session.id}
                onChange={(e) =>
                  navigate(`/session/${encodeURIComponent(e.target.value)}`)
                }
              >
                {recent.map((s) => (
                  <option key={s.id} value={s.id}>
                    {titleOf(s)} · {s.deviceName}
                  </option>
                ))}
              </select>
            </label>
          )}
          <details className="diagnostic">
            <summary>{t("身份与运行诊断")}</summary>
            <pre>
              {JSON.stringify(
                {
                  nodeId: session.nodeId,
                  provider: session.provider,
                  profile: session.profile,
                  nativeId: session.nativeId,
                  executionId: session.executionId,
                  executionState: session.executionState,
                  capabilities: session.capabilities,
                },
                null,
                2,
              )}
            </pre>
          </details>
        </div>
      )}
      <div className="chat-region">
      <div className="chat-scroll" ref={pane}>
        <div className="chat-content">
          {actionError && <div className="notice warning" role="alert">{actionError}</div>}
          {session.metadata?.hidden && <div className="notice">{t("此会话已从日常列表隐藏。")}<button className="button" onClick={onBack}>{t("返回会话列表")}</button></div>}
          {history.hasMore && (
            <button className="load-more" disabled={older} onClick={loadOlder}>
              {older ? t("正在读取…") : t("读取更早的消息")}
            </button>
          )}
          {showInternal && history.boundary && (
            <details className="history-boundary">
              <summary>{t("原生记录与历史边界")}</summary>
              {history.boundary}
            </details>
          )}
          {error && (
            <div className="notice warning" role="alert">
              {error.includes('no rollout found') || error.includes('No native conversation history') ? t("这条会话尚无已保存的原生聊天记录。") : t("历史读取暂未完成：{0}", { 0: tServer(error) })}
            </div>
          )}
          {!initialized.current && !!history.messages.length && <p className="field-hint" role="status">{t("先显示上次查看的内容，正在核对最新原生记录…")}</p>}
          {loading && !history.messages.length ? (
            <div className="transcript-skeleton" aria-busy="true" aria-label={t("正在读取原生记录…")}>
              {[72, 46, 88, 60].map((width, i) => (
                <div className={`skeleton-message ${i === 1 ? "user" : ""}`} key={i}>
                  <i className="skeleton-line short" />
                  <i className="skeleton-line" style={{ width: `${width}%` }} />
                  {i !== 1 && <i className="skeleton-line" style={{ width: `${width - 20}%` }} />}
                </div>
              ))}
            </div>
          ) : error && !history.messages.length ? (
            error.includes('no rollout found') || error.includes('No native conversation history') ?
            <Empty title={t("没有已保存的聊天记录")}>{t("这可能是尚未开始对话的空会话。返回列表选择其他会话即可，无需等待重连。")}</Empty> :
            <Empty title={t("暂时无法读取会话")}>{t("请稍后重试；若持续失败，可查看原生客户端中的会话状态。")}</Empty>
          ) : !history.messages.length ? (
            <Empty title={t("还没有可显示的消息")}>
              {t("原生记录会在设备同步后显示。")}</Empty>
          ) : (
            // Keys are message ids: group positions shift when older pages
            // are prepended, which would remount and re-parse every message.
            transcriptGroups(history.messages).map((group) =>
              group.internal ? (showInternal &&
                <details
                  className="tool-message"
                  key={`internal:${group.messages[0].id}`}
                >
                  <summary>
                    <Icon name="terminal" size={15} />
                    <span>{t("内部活动 · {0} 条", { 0: group.messages.length })}</span>
                    <span className="tool-preview">
                      {t("后台通知、工具或系统上下文")}</span>
                    <Icon name="chevron" size={13} />
                  </summary>
                  {group.messages.map((message, entry) => (
                    <div key={`${message.id}:${entry}`}>
                      <div className="message-label">
                        <span>{internalLabel(message)}</span>
                        <time>{formatTime(message.at)}</time>
                      </div>
                      <pre>{message.text}</pre>
                    </div>
                  ))}
                </details>
              ) : (
                group.messages.map((message) => (
                  <article
                    className={`message ${message.role} ${continued.has(message.id) ? "continued" : ""}`}
                    key={message.id}
                  >
                    <div className="message-label">
                      <span>
                        {message.role === "user"
                          ? t("你")
                          : message.role === "assistant"
                            ? providerName(session.provider)
                            : message.role === "error"
                              ? t("错误")
                              : t("原生事件")}
                      </span>
                      <time>{formatTime(message.at)}</time>
                    </div>
                    {apiFailure(message.text) ? <div className="notice warning"><strong>{apiFailure(message.text)!.title}</strong><p>{apiFailure(message.text)!.detail}</p><details><summary>{t("错误详情")}</summary><pre style={{whiteSpace: "pre-wrap", overflowWrap: "anywhere"}}>{message.text}</pre></details></div> : message.role === "assistant" ? <Suspense fallback={<div className="message-body">{message.text}</div>}><MessageMarkdown text={message.text} cwd={session.cwd} sessionId={session.id} /></Suspense> : <div className="message-body">{message.text}</div>}
                    {message.role === "assistant" && <MessageFiles text={message.text} cwd={session.cwd} sessionId={session.id} />}
                  </article>
                ))
              ),
            )
          )}
          {!loading && progress && <div className="reply-progress" role="status" aria-live="polite">
            <span className={progress.animated ? "reply-dots animated" : "reply-dots"} aria-hidden="true"><i /><i /><i /></span>
            <span>{progress.label}</span>
          </div>}
          <div ref={bottom} />
        </div>
      </div>
      {awayFromBottom && (
        <button className="jump-to-latest" onClick={viewLatest} aria-label={newOutput ? t("有新消息，跳到最新消息") : t("跳到最新消息")} title={t("跳到最新消息")}>
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M12 4v15m-6-6 6 6 6-6" /></svg>
          {newOutput && <i className="jump-unread" aria-hidden="true" />}
        </button>
      )}
      </div>
      <div className={`conversation-controls${writing ? " is-writing" : ""}`}>
        {nativeFailure && <div className="conversation-recovery">
          <button type="button" className="button" disabled={busy || !device?.online} title={t("填入继续对话的草稿，确认后发送")} onClick={() => { setDraft(current => current || t("请继续刚才未完成的任务。")); draftInput.current?.focus(); }}>{t("继续对话")}<Icon name="chevron" size={16} /></button>
        </div>}
        {(nativeAttention || permissionNotice) && <section className="native-attention" aria-label={t("需要你的确认")}>
          <div><strong>{session.turnState === "waiting_approval" || permissionNotice ? t("会话正在等待你的批准") : t("会话正在等待你的回答")}</strong>
          <p>{capability.terminal ? (session.turnState === "waiting_approval" || permissionNotice ? t("打开请求，查看内容后选择允许或拒绝，即可继续任务。") : t("查看具体问题，再按提示输入回答或选择选项。")) : t("当前连接无法在手机处理，请在这台设备的原生终端中确认。")}</p></div>
          {capability.terminal && <button className="button primary" disabled={busy || !device?.online} onClick={() => execute({...target, kind:"terminal"})}>{busy ? t("正在打开…") : session.turnState === "waiting_approval" || permissionNotice ? t("查看并处理") : t("查看问题")}</button>}
          {!device?.online && <small>{t("设备暂时离线，重新连接后可处理。")}</small>}
        </section>}
        {session.error && !permissionNotice && !nativeFailure && <div className="notice warning">{tServer(session.error)}</div>}
        {session.pending?.map((approval) => (
          <ApprovalCard
            key={`${approval.id}:${approval.executionId}`}
            approval={approval}
            enabled={
              capability.approve &&
              approval.executionId === session.executionId &&
              !!device?.online &&
              !busy &&
              !blocked
            }
            onAnswer={(decision, text, data) =>
              execute({
                ...target,
                executionId: approval.executionId,
                kind: "approve",
                requestId: approval.id,
                decision,
                text,
                data,
              })
            }
            onTerminal={() => execute({ ...target, kind: "terminal" })}
            terminalEnabled={capability.terminal && !!device?.online && !busy}
          />
        ))}
        {action &&
          action.result.status === "submitted" &&
          (action.result.data as { startupId?: string } | undefined)
            ?.startupId && <StartupPrompt action={action} onQuery={onQuery} />}
        {action && !(["send", "resume"].includes(action.action.kind) && action.result.status === "accepted") && (
          <ActionStatus
            tracked={action}
            onQuery={() => onQuery(action)}
            onAcknowledge={() => onAcknowledge(action.action.id)}
          />
        )}
        {pendingDraft?.detached && action?.action.id === pendingDraft.actionId && action.result.status !== "accepted" && (
          <details className="conversation-tools">
            <summary>{action.result.status === "rejected" ? t("发送失败 · 查看保留内容") : t("查看本次发送内容")}</summary>
            <div className="message-body">{pendingDraft.text}</div>
            {pendingDraft.files.map(path => <div key={path}>{path.split("/").pop()}</div>)}
            {action.result.status === "rejected" && <button onClick={() => {
              setDraft(current => restoreDraft(pendingDraft, current, []).text);
              setFiles(current => restoreDraft(pendingDraft, "", current).files);
              writeLocal(`pending-draft:${session.id}`, null);
              setPendingDraft(null);
              draftInput.current?.focus();
            }}>{t("恢复到输入框")}</button>}
          </details>
        )}
        {more && capability.reason &&
          (capability.send ? (
            session.provider === "claude" && (
              <p className="capability-reason">
                {capability.terminal
                  ? t("已连接原生后台会话。审批请打开「终端」；手机与本机共享终端输入。")
                  : capability.inputMode === "bridge"
                    ? t("已连接此 Claude 会话的原生桥接。继续同一实例对话；显式接收限制与工具审批仍按原生设置执行。")
                    : t("已发现同一 Claude 的消息入口，但可能需要在电脑终端确认投递。完成一次原生桥接接入后可直接续聊。")}
              </p>
            )
          ) : (
            <p className="capability-reason">{tServer(capability.reason)}</p>
          ))}
        {more && capability.inputMode === "queue" && <p className="capability-reason">{t("发送到正在运行的原生会话，当前任务结束后自动处理。权限审批仍在电脑端完成。")}</p>}
        <div className="chat-tool-row" aria-label={t("会话设置")}>
          <button type="button" className="chat-tool-button" aria-expanded={toolPanel === "model"} aria-controls="chat-model-panel" onClick={() => { draftInput.current?.blur(); setToolPanel("model"); }}><Icon name="settings" size={15}/><span>{t("模型与强度")}</span><Icon name="chevron" size={13}/></button>
          <button type="button" className="chat-tool-button" aria-expanded={toolPanel === "actions"} aria-controls="chat-actions-panel" onClick={() => { draftInput.current?.blur(); setToolPanel("actions"); }}><Icon name="terminal" size={15}/><span>{t("会话操作")}</span><Icon name="chevron" size={13}/></button>
        </div>
        {draftExpanded && <Dialog className="draft-dialog" title={t("编辑完整草稿")} onClose={() => setDraftExpanded(false)}>
          <textarea autoFocus aria-label={t("完整草稿")} value={draft} onChange={(e) => setDraft(e.target.value)} className="full-draft-input" />
          <div className="button-row"><span>{t("{0} 字 · 修改自动保存", { 0: Array.from(draft).length })}</span><button type="button" className="button primary" onClick={() => setDraftExpanded(false)}>{t("完成编辑")}</button></div>
        </Dialog>}
        <div className={`composer${!writing && !longDraft ? " compact" : ""}${draft ? " has-draft" : ""}`}>
          {draft && <div className="draft-summary">
            <span className="small">{t("草稿 · {0} 字", { 0: Array.from(draft).length })}</span>
            <button type="button" className="text-button" title={t("展开全文 ↗")} onClick={() => setDraftExpanded(true)}>{t("展开全文 ↗")}</button>
          </div>}
          <div className="composer-attachments">
            <SessionFiles sessionId={session.id} cwd={session.cwd} online={!!device?.online && !busy}
              count={files.length} onBusy={setUploading}
              onAttach={(path) => setFiles((current) => current.includes(path) ? current : [...current, path].slice(0, 8))} />
            {files.length > 0 && <span className="attachment-hint">{t("{0} 个附件", { 0: files.length })}</span>}
          </div>
          {files.length > 0 && <div className="pending-attachments">{files.map((path) => <div className="pending-attachment" key={path}>
            {/\.(png|jpe?g|webp|gif)$/i.test(path) && <AttachmentPreview sessionId={session.id} path={path} cwd={session.cwd} />}
            <span>{path.split("/").pop()}</span><button disabled={uploading || busy} aria-label={t("移除 {0}", { 0: path.split("/").pop() })} onClick={() => setFiles((current) => current.filter((p) => p !== path))}>×</button>
          </div>)}</div>}
          <textarea ref={draftInput}
            aria-label={t("发送给原生会话的消息")}
            rows={1}
            autoCapitalize="sentences"
            value={draft}
            placeholder={
              capability.send
                ? t("发消息，或点麦克风说话…")
                : capability.resume
                  ? t("发送即可恢复这段会话…")
                  : t("当前实例仅可查看；草稿仍可保存")
            }
            onChange={(e) => setDraft(e.target.value)}
            onCompositionStart={() => {
              composing.current = true;
            }}
            onCompositionEnd={() => {
              composing.current = false;
            }}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                (e.metaKey || e.ctrlKey) &&
                !e.nativeEvent.isComposing &&
                !composing.current
              ) {
                e.preventDefault();
                send();
              }
            }}
          />
          <VoiceInput disabled={busy || uploading} onActive={setVoicing} onText={(text) => { setDraft((current) => appendSpeech(current, text)); requestAnimationFrame(() => { const input = draftInput.current; if (input) input.scrollTop = input.scrollHeight; }); }} />
          <div className="composer-bottom">
            <span className="draft-hint">
              {draft ? t("草稿保留在此设备") : t("中文 · 多行 · 粘贴")}
            </span>
            <button
              className="send-button"
              title={t("发送消息")}
              aria-label={t("发送消息")}
              disabled={
                (!draft.trim() && files.length === 0) || uploading || voicing ||
                !(capability.send || capability.resume) ||
                busy ||
                blocked ||
                !device?.online
              }
              onPointerDown={(event) => {
                // Keep the editor focused until click: iOS keyboard dismissal
                // can move this button between pointerdown and pointerup.
                // Sending stays on click so dragging/cancelling cannot submit.
                if (event.isPrimary && event.button === 0 && document.activeElement === draftInput.current) {
                  event.preventDefault();
                }
              }}
              onClick={send}
            >
              <Icon name="send" size={18} />
              <span>{t("发送")}</span>
            </button>
          </div>
        </div>
        {toolPanel === "model" && <Dialog className="composer-tool-dialog" title={t("模型与强度")} onClose={() => setToolPanel(null)}><div id="chat-model-panel" className="chat-tool-panel">
          {session.provider === "claude" || capability.inputMode === "queue" ? <div className="model-controls">
            <p>{session.provider === "claude" ? t("Claude 使用原生选择器切换模型与 effort，保留同一会话。") : t("此会话由电脑客户端持有，消息队列不支持更改模型和 effort，请在原客户端切换。")}</p>
            {capability.terminal && <button className="button" disabled={busy || !device?.online} onClick={() => execute({...target,kind:"terminal"})}>{t("打开原生设置终端")}</button>}
            {session.provider === "claude" && !capability.terminal && capability.resume && <button className="button" disabled={busy || blocked || !device?.online} onClick={async()=>{const r=await execute({...target,kind:"resume",cwd:session.cwd});if(r?.status==="accepted" && r.executionId)await execute({...target,executionId:r.executionId,kind:"terminal"})}}>{t("恢复原会话并打开设置")}</button>}
            {session.provider === "claude" && !capability.terminal && !capability.resume && <p>{t("此原生实例暂不支持手机终端操作，请在电脑端使用 /model 或 /effort。")}</p>}
          </div> : <ModelControls nodeId={session.nodeId} provider={session.provider} profile={session.profile} value={modelChoice} onChange={setModelChoice} disabled={busy || !device?.online} />}
        </div></Dialog>}
        {toolPanel === "actions" && <Dialog className="composer-tool-dialog" title={t("会话操作")} onClose={() => setToolPanel(null)}><div id="chat-actions-panel" className="chat-tool-panel"><div className="execution-actions">
          {capability.resume && !capability.send && (
            <button
              className="text-button"
              disabled={busy || blocked || !device?.online}
              onClick={resume}
            >
              <Icon name="arrow" size={15} />
              {t("仅恢复会话")}</button>
          )}
          <button
            className="text-button"
            disabled={
              !capability.interrupt || busy || blocked || !device?.online
            }
            title={!capability.interrupt ? t("当前实例不支持中断") : ""}
            onClick={() => execute({ ...target, kind: "interrupt" })}
          >
            <Icon name="stop" size={14} />
            {t("停止当前轮次")}</button>
          <button
            className="text-button"
            disabled={!capability.terminal || busy || !device?.online}
            onClick={() => execute({ ...target, kind: "terminal" })}
          >
            <Icon name="terminal" size={15} />
            {t("终端")}</button>
          {session.executionId && (
            <button
              className="text-button danger"
              disabled={
                busy || blocked || !device?.online || !capability.terminate
              }
              onClick={() => {
                if (
                  window.confirm(
                    t("结束当前原生执行实例？正在进行的任务将停止，历史会保留。"),
                  )
                )
                  execute({ ...target, kind: "terminate" });
              }}
            >
              {t("结束实例")}</button>
          )}
        </div></div></Dialog>}
      </div>
    </section>
  );
}
interface NativeQuestion {
  id: string;
  header?: string;
  question: string;
  isSecret?: boolean;
  options?: { label: string; description?: string }[];
}
function ApprovalCard({
  approval,
  enabled,
  onAnswer,
  onTerminal,
  terminalEnabled,
}: {
  approval: Approval;
  enabled: boolean;
  onAnswer: (decision: string, text?: string, data?: unknown) => void;
  onTerminal: () => void;
  terminalEnabled: boolean;
}) {
  const [answer, setAnswer] = useState(""),
    [answers, setAnswers] = useState<Record<string, string>>({});
  const question =
    approval.kind === "item/tool/requestUserInput" ||
    (approval.options || []).includes("answer");
  const questions =
    (approval.data as { questions?: NativeQuestion[] } | undefined)
      ?.questions || [];
  const options = approval.options || [];
  const terminalOnly = !options.length || options.includes("open-terminal");
  const ready = questions.length
    ? questions.every((q) => answers[q.id]?.trim())
    : !!answer.trim();
  const respond = (option: string) => {
    if (questions.length) {
      const result = Object.fromEntries(
        questions.map((q) => [q.id, { answers: [answers[q.id]] }]),
      );
      onAnswer(option, undefined, { answers: result });
    } else onAnswer(option, answer || undefined);
  };
  return (
    <div className="approval-card">
      <span className="eyebrow">
        {question ? t("需要你的回答") : t("原生权限请求")}
      </span>
      <h3>{({
        "item/commandExecution/requestApproval": t("是否允许执行这条命令？"),
        "item/fileChange/requestApproval": t("是否允许修改这些文件？"),
        "item/tool/requestUserInput": t("请回答原生问题"),
      } as Record<string, string>)[approval.kind] || tServer(approval.title) || t("等待你处理")}</h3>
      {/* A command must be readable before "允许本次" is pressed. */}
      <details open={approval.kind === "item/commandExecution/requestApproval" || undefined}>
        <summary>{approval.kind === "item/commandExecution/requestApproval" ? t("将要执行的命令") : t("查看请求详情")}</summary>
        <pre>{approval.detail}</pre>
      </details>
      {question &&
        (questions.length ? (
          questions.map((q) => (
            <div className="native-question" key={q.id}>
              <label>
                {q.header && <strong>{q.header} · </strong>}
                {q.question}
                {q.isSecret ? (
                  <input
                    type="password"
                    autoComplete="off"
                    value={answers[q.id] || ""}
                    onChange={(e) =>
                      setAnswers((old) => ({ ...old, [q.id]: e.target.value }))
                    }
                  />
                ) : (
                  <textarea
                    rows={2}
                    value={answers[q.id] || ""}
                    onChange={(e) =>
                      setAnswers((old) => ({ ...old, [q.id]: e.target.value }))
                    }
                  />
                )}
              </label>
              {q.options?.map((option) => (
                <button
                  key={option.label}
                  className={`question-option ${answers[q.id] === option.label ? "selected" : ""}`}
                  onClick={() =>
                    setAnswers((old) => ({ ...old, [q.id]: option.label }))
                  }
                >
                  <strong>{option.label}</strong>
                  {option.description && <span>{option.description}</span>}
                </button>
              ))}
            </div>
          ))
        ) : (
          <textarea
            aria-label={t("回答原生问题")}
            rows={2}
            value={answer}
            onChange={(e) => setAnswer(e.target.value)}
          />
        ))}
      <div className="button-row">
        {terminalOnly ? (
          <button
            className="button"
            disabled={!terminalEnabled}
            onClick={onTerminal}
          >
            <Icon name="terminal" size={16} />
            {t("在原生终端处理")}</button>
        ) : (
          options.map((option, index) => (
            <button
              className={`button ${index === 0 ? "primary" : ""}`}
              key={option}
              disabled={!enabled || (question && !ready)}
              onClick={() => respond(option)}
            >
              {{
                accept: t("允许本次"),
                decline: t("拒绝"),
                answer: t("提交回答"),
                // Codex "cancel" declines and also stops the current turn.
                cancel: t("拒绝并停止本轮"),
              }[option] || option}
            </button>
          ))
        )}
      </div>
      {!enabled && !terminalOnly && (
        <p className="small muted">
          {t("请求已失效、设备离线或原生通道暂不可操作。")}</p>
      )}
    </div>
  );
}

function NewSession({
  navigate,
  loading,
  devices,
  preferences,
  execute,
  busy,
  blocked,
  action,
  onQuery,
  onAcknowledge,
}: {
  navigate: (path:string)=>void;
  loading: boolean;
  devices: Device[];
  preferences: WorkspacePreferences;
  execute: Execute;
  busy: boolean;
  blocked: boolean;
  action?: TrackedAction;
  onQuery: (a: TrackedAction) => void;
  onAcknowledge: (id: string) => void;
}) {
  const [node, setNode] = useState(""),
    [provider, setProvider] = useState("codex"),
    [profile, setProfile] = useState("default"),
    [cwd, setCwd] = useState(""),
    [text, setText] = useState(() => readLocal<string>("create-draft", "")),
    [directory, setDirectory] = useState<DirectoryListing | null>(null),
    [error, setError] = useState(""),
    [picking, setPicking] = useState(false),
    [name, setName] = useState("");
  const [modelChoice,setModelChoice] = useState<ModelChoice>({model:"",effort:""});
 useEffect(()=>setModelChoice({model:"",effort:""}),[node,provider,profile]);
 const directoryOperation = useRef(latestOperation());
  const chooseDirectory = (path: string, nodeId = node) => {
    directoryOperation.current.invalidate();
    setPicking(false);
    setError("");
    setName("");
    setDirectory(null);
    setNode(nodeId);
    setCwd(path);
  };
  const device = devices.find((d) => d.id === node);
  const diagnostics =
    device?.diagnostics?.filter((d) => d.provider === provider) || [];
  const diagnostic = diagnostics.find((d) => d.profile === profile);
  useEffect(() => {
    writeLocal("create-draft", text);
  }, [text]);
  useEffect(() => () => directoryOperation.current.invalidate(), []);
  useEffect(() => {
    const firstOnline =
      devices.find(
        (d) =>
          d.online && preferences.workspaces.some((w) => w.nodeId === d.id),
      ) || devices.find((d) => d.online);
    if (!node && firstOnline) {
      chooseDirectory(
        cwd ||
          preferences.workspaces.find((w) => w.nodeId === firstOnline.id)
            ?.path ||
          firstOnline.roots?.[0] ||
          "",
        firstOnline.id,
      );
    }
  }, [node, devices, preferences]);
  useEffect(() => {
    setProfile(diagnostics[0]?.profile || "default");
  }, [node, provider, diagnostics.map((d) => d.profile).join(",")]);
  const browse = async (path: string, create = false) => {
    if (!node || !device?.online) return;
    setPicking(true);
    setError("");
    setCwd(path);
    setDirectory(null);
    setName("");
    await directoryOperation.current.run(
      () =>
        post<DirectoryListing>(
          `/api/devices/${encodeURIComponent(node)}/directories`,
          { path, create },
        ),
      {
        success: (value) => {
          setDirectory(value);
          setCwd(value.path);
          setName("");
        },
        error: (err) => setError((err as Error).message),
        settled: () => setPicking(false),
      },
    );
  };
  if (!devices.length || !devices.some(d => d.online)) {
    const checking = loading && !devices.length;
    const missing = !devices.length;
    return <main className="page new-page">
      <div className="page-heading"><div><h1>{t("新建AI对话")}</h1><p>{t("AI 对话需要在你的执行设备上运行。")}</p></div></div>
      <Empty title={checking ? t("正在检查AI执行设备…") : missing ? t("先配置一台AI执行设备") : t("AI执行设备暂时离线")}>
        {checking ? t("正在读取设备状态。") : missing ? t("接入你的 Mac、Linux 电脑或服务器，并完成 Claude Code 或 Codex 的安装与登录，即可开始对话。") : t("已接入的设备当前均不在线。请启动设备上的服务，连接恢复后即可继续创建对话。")}
      </Empty>
      {!checking && <button className="button primary" onClick={() => navigate(missing ? "/setup" : "/devices")}><Icon name="devices"/>{missing ? t("配置AI执行设备") : t("查看AI执行设备")}</button>}
    </main>;
  }
  return (
    <main className="page new-page">
      <div className="page-heading">
        <div>
          <span className="eyebrow">START SOMETHING</span>
          <h1>{t("新建AI对话")}</h1>
          <p>{t("任务在你选择的设备和目录中执行。")}</p>
        </div>
      </div>
      <form
        className="creation-form"
        onSubmit={async (e) => {
          e.preventDefault();
          if (
            busy ||
            blocked ||
            directoryOperation.current.pending ||
            !device?.online ||
            !cwd.trim() ||
            !text.trim() ||
            !diagnostic?.capabilities.create
          )
            return;
          const result = await execute({
            nodeId: node,
            provider,
            profile,
            kind: "create",
            model: modelChoice.model || undefined, effort: modelChoice.effort || undefined,
            cwd,
            text,
          });
          if (result?.status === "accepted") {
            setText("");
            writeLocal("create-draft", "");
          }
        }}
      >
        <section className="form-section">
          <div className="step-title">
            <span>01</span>
            <h2>{t("选择AI执行设备")}</h2>
          </div>
          <div className="device-picker">
              {devices.map((d) => (
                <button
                  type="button"
                  key={d.id}
                  disabled={!d.online}
                  onClick={() => {
                    if (node !== d.id) {
                      chooseDirectory(
                        preferences.workspaces.find((w) => w.nodeId === d.id)
                          ?.path ||
                          d.roots?.[0] ||
                          "",
                        d.id,
                      );
                    }
                  }}
                  className={`device-option ${node === d.id ? "selected" : ""}`}
                >
                  <Icon name="devices" />
                  <strong>{d.name}</strong>
                  <span>
                    {d.os} · {d.online ? t("在线") : t("离线")}
                  </span>
                  {node === d.id && <Icon name="check" size={16} />}
                </button>
              ))}
          </div>
        </section>
        <section className="form-section">
          <div className="step-title">
            <span>02</span>
            <h2>{t("选择工作目录")}</h2>
          </div>
          <label htmlFor="cwd">{t("设备上的绝对路径")}</label>
          {!!preferences.workspaces.filter((w) => w.nodeId === node).length && (
            <div className="root-chips" aria-label={t("关注项目快捷选择")}>
              {preferences.workspaces
                .filter((w) => w.nodeId === node)
                .map((w) => (
                  <button
                    type="button"
                    key={w.path}
                    title={w.path}
                    onClick={() => chooseDirectory(w.path)}
                  >
                    {folderName(w.path)}
                  </button>
                ))}
            </div>
          )}
          <div className="inline-input">
            <input
              id="cwd"
              disabled={!device?.online}
              value={cwd}
              onChange={(e) => chooseDirectory(e.target.value)}
              placeholder="/Users/you/projects/my-project"
              required
            />
            <button
              type="button"
              className="button"
              disabled={!device?.online || picking}
              onClick={() => browse(cwd)}
            >
              <Icon name="folder" />
              {t("浏览")}</button>
          </div>
          {device?.roots?.length ? (
            <div className="root-chips">
              {device.roots.map((root) => (
                <button
                  type="button"
                  key={root}
                  onClick={() => browse(root)}
                  title={root}
                >
                  {folderName(root)}
                </button>
              ))}
            </div>
          ) : null}
          {directory && (
            <div className="directory-browser">
              <div className="directory-current">
                <code>{directory.path}</code>
              </div>
              <div className="directory-list">
                {directory.entries?.map((entry) => (
                  <button
                    type="button"
                    key={entry.path}
                    onClick={() => browse(entry.path)}
                  >
                    <Icon name="folder" size={17} />
                    <span>{entry.name}</span>
                    <Icon name="chevron" size={16} />
                  </button>
                ))}
                {!directory.entries?.length && (
                  <p className="small muted">{t("没有子目录，可以使用当前目录。")}</p>
                )}
              </div>
              <div className="inline-input">
                <input
                  aria-label={t("新目录名称")}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t("新建子目录名称")}
                />
                <button
                  type="button"
                  className="button"
                  disabled={
                    !name.trim() ||
                    name.includes("/") ||
                    name === ".." ||
                    name === "." ||
                    picking
                  }
                  onClick={() =>
                    browse(`${directory.path.replace(/\/$/, "")}/${name}`, true)
                  }
                >
                  {t("新建目录")}</button>
              </div>
            </div>
          )}
          <p className="field-hint">
            {t("仅可在节点配置的工作根目录中创建。目录失效时会明确报错。")}</p>
        </section>
        <section className="form-section">
          <div className="step-title">
            <span>03</span>
            <h2>{t("选择 Agent，写下任务")}</h2>
          </div>
          <div className="provider-picker">
            {["codex", "claude"].map((p) => (
              <button
                type="button"
                key={p}
                className={p === provider ? "selected" : ""}
                onClick={() => setProvider(p)}
              >
                <Provider provider={p} />
                {p === provider && <Icon name="check" size={17} />}
              </button>
            ))}
          </div>
          <details className="model-choice"><summary className="compact-disclosure"><Icon name="settings" size={15}/><span>{t("{0} 模型与思考强度{1}", { 0: providerName(provider), 1: modelChoice.model || modelChoice.effort ? ` · ${[modelChoice.model, modelChoice.effort].filter(Boolean).join(" / ")}` : t(" · 默认") })}</span><Icon name="chevron" size={14}/></summary><ModelControls nodeId={node} provider={provider} profile={profile} value={modelChoice} onChange={setModelChoice} disabled={!device?.online || busy}/></details>
          {diagnostic && !diagnostic.capabilities.create && (
            <p className="notice warning">
              {tServer(diagnostic.capabilities.reason) ||
                tServer(diagnostic.details) ||
                t("当前原生环境无法创建会话，请查看设备诊断。")}
            </p>
          )}
          {provider === "claude" && <p className="field-hint">{diagnostic?.createPermissionMode === "bypassPermissions" ? t("执行模式：自动执行 · 跳过 Claude 权限审批。可以直接运行命令、修改或删除文件；系统授权和任务澄清仍可能需要你处理。") : t("执行模式：沿用 Claude 原生权限设置，需要时在手机确认。")}</p>}
          <label htmlFor="task">{t("首条任务")}</label>
          <textarea
            id="task"
            rows={5}
            placeholder={t("让我们从什么开始？")}
            value={text}
            onChange={(e) => setText(e.target.value)}
            required
          />
          <details className="advanced">
            <summary>{t("高级选项")}</summary>
            <label>
              {t("原生 Profile")}<select
                value={profile}
                onChange={(e) => setProfile(e.target.value)}
              >
                {diagnostics.length ? (
                  diagnostics.map((d) => (
                    <option key={d.profile} value={d.profile}>
                      {d.profile}
                    </option>
                  ))
                ) : (
                  <option value="default">default</option>
                )}
              </select>
            </label>
            <p className="field-hint">
              {t("沿用节点上的 CLI 配置、认证和项目权限。不会自动跳过原生审批。")}</p>
          </details>
        </section>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        {action &&
          action.result.status === "submitted" &&
          (action.result.data as { startupId?: string } | undefined)
            ?.startupId && <StartupPrompt action={action} onQuery={onQuery} />}
        {action && (
          <ActionStatus
            tracked={action}
            onQuery={() => onQuery(action)}
            onAcknowledge={() => onAcknowledge(action.action.id)}
          />
        )}
        <button
          className="button primary create-submit"
          disabled={
            busy ||
            blocked ||
            picking ||
            !device?.online ||
            !cwd.trim() ||
            !text.trim() ||
            !diagnostic?.capabilities.create
          }
        >
          {busy ? t("正在提交…") : t("创建原生会话")}
          <Icon name="arrow" />
        </button>
      </form>
    </main>
  );
}

function StartupPrompt({
  action,
  onQuery,
}: {
  action: TrackedAction;
  onQuery: (a: TrackedAction) => void;
}) {
  const data = action.result.data as {
    startupId: string;
    executionId: string;
    screen?: string;
  };
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    const timer = setInterval(() => onQuery(action), 1200);
    return () => clearInterval(timer);
  }, [action.action.id, data.startupId]);
  const input = async (text: string) => {
    setBusy(true);
    setError("");
    try {
      const result = await post<Result>("/api/actions", {
        ...action.action,
        id: crypto.randomUUID(),
        kind: "startup_input",
        requestId: data.startupId,
        executionId: data.executionId,
        text,
      });
      if (result.status === "rejected" || result.status === "unknown")
        setError(tServer(result.message) || t("无法确认原生输入结果"));
      onQuery(action);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="form-section" aria-label={t("原生启动对话")}>
      <h2>{t("Claude 原生目录信任")}</h2>
      <p>
        {t("会话尚未创建。请阅读原生提示并使用方向键选择；确认后继续创建，不会更改其他项目的权限。")}</p>
      <pre
        className="terminal-output"
        style={{ overflowX: "auto", whiteSpace: "pre-wrap", maxHeight: 440 }}
      >
        {data.screen || t("正在等待原生启动界面，CLI 首次初始化可能需要几十秒…")}
      </pre>
      <div className="button-row">
        <button type="button" disabled={busy} onClick={() => input("\u001b[A")}>
          {t("↑ 上一项")}</button>
        <button type="button" disabled={busy} onClick={() => input("\u001b[B")}>
          {t("↓ 下一项")}</button>
        <button type="button" disabled={busy} onClick={() => input("\r")}>
          {t("Enter 确认当前选项")}</button>
        <button type="button" disabled={busy} onClick={() => input("\u0003")}>
          {t("取消启动")}</button>
      </div>
      {error && <p role="alert">{error}</p>}
    </section>
  );
}

const deviceViewMemory = {view: "active", query: ""};
const projectHistoryMemory = new Map<string, {project: string; search: string; page: number}>();

function ProjectHistory({ nodeId, sessions, preferences, navigate }: {
  nodeId: string; sessions: Session[]; preferences: WorkspacePreferences; navigate: (path: string) => void;
}) {
  const saved = projectHistoryMemory.get(nodeId);
  const [project, setProject] = useState(saved?.project || "");
  const [search, setSearch] = useState(saved?.search || "");
  const [page, setPage] = useState(saved?.page || 1);
  useEffect(() => { projectHistoryMemory.set(nodeId, {project, search, page}); }, [nodeId, project, search, page]);
  const heading = useRef<HTMLDivElement>(null);
  const [projectQuery, setProjectQuery] = useState("");
  const [projectLimit, setProjectLimit] = useState(10);
  const dirs = [...new Set(sessions.map(s => projectPath(s, preferences)))].sort();
  const listedDirs = dirs.filter(dir => dir.toLowerCase().includes(projectQuery.toLowerCase()));
  const selected = dirs.includes(project) ? project : "";
  const matches = sessions.filter(s => projectPath(s, preferences) === selected &&
    `${titleOf(s)} ${s.provider} ${s.cwd}`.toLowerCase().includes(search.trim().toLowerCase()))
    .sort((a,b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt) || a.id.localeCompare(b.id));
  const pages = Math.max(1, Math.ceil(matches.length / 10));
  const current = Math.min(page, pages);
  const choose = (dir: string) => { setProject(dir); setSearch(""); setPage(1); };
  const turn = (next: number) => { setPage(next); heading.current?.scrollIntoView({block:"start"}); };
  if (!dirs.length) return <p className="muted small">{t("没有匹配的历史项目")}</p>;
  return <div className="project-history">
    {!selected ? <>
      <p className="muted small">{t("选择一个项目，查看历史会话")}</p>
      <input aria-label={t("搜索历史项目")} placeholder={t("搜索项目")} value={projectQuery} onChange={e => {setProjectQuery(e.target.value); setProjectLimit(10);}} />
      <div className="history-project-picker">{listedDirs.slice(0,projectLimit).map(dir => <button key={dir} onClick={() => choose(dir)}>
        <Icon name="folder" size={18} /><span title={dir}>{folderName(dir)}</span>
        <small>{sessions.filter(s => projectPath(s, preferences) === dir).length}</small><Icon name="chevron" size={16} />
      </button>)}</div>
      {listedDirs.length > projectLimit && <button className="button" onClick={() => setProjectLimit(n => n+10)}>{t("再显示 10 个项目")}</button>}
    </> : <>
      <div className="history-project-heading" ref={heading}>
        <button onClick={() => choose("")}><Icon name="back" size={16} />{t("项目")}</button>
        <strong title={selected}>{folderName(selected)}</strong>
      </div>
      <div className="search-field"><Icon name="search" size={18} /><input
        aria-label={t("搜索当前项目的历史会话")} placeholder={t("搜索会话名称、Claude 或 Codex")}
        value={search} onChange={e => {setSearch(e.target.value); setPage(1);}} /></div>
      <p className="muted small">{matches.length ? t("{0}–{1} / {2} 条", { 0: (current-1)*10+1, 1: Math.min(current*10,matches.length), 2: matches.length }) : t("没有匹配的会话，试试其他关键词")}</p>
      <div className="tree-sessions">{matches.slice((current-1)*10,current*10).map(session =>
        <SessionCard key={session.id} session={session} navigate={navigate} compact />)}</div>
      {pages > 1 && <nav className="history-pagination" aria-label={t("历史会话分页")}>
        <button disabled={current === 1} onClick={() => turn(current-1)}>{t("上一页")}</button>
        <span aria-live="polite">{current} / {pages}</span>
        <button disabled={current === pages} onClick={() => turn(current+1)}>{t("下一页")}</button>
      </nav>}
    </>}
  </div>;
}

function ConnectionStatus({devices,connected,refresh}:{devices:Device[];connected:boolean;refresh:()=>Promise<void>}) {
  const [diagnostics,setDiagnostics]=useState<{tailscale?:{ready:boolean;reason:string};publicUrl?:string}|null>(null);
  const [error,setError]=useState("");
  const [busy,setBusy]=useState(false);
  async function inspect() {
    setBusy(true); setError("");
    try { const [data] = await Promise.all([api<{tailscale?:{ready:boolean;reason:string};publicUrl?:string}>("/api/diagnostics"),refresh()]); setDiagnostics(data); }
    catch(e){setError((e as Error).message);}
    finally{setBusy(false);}
  }
  useEffect(()=>{void inspect();},[]);
  return <main className="page">
    <div className="page-heading"><div><h1>{t("连接状态")}</h1><p>{t("查看浏览器、控制端和AI执行设备之间的连接。")}</p></div>
      <button className="button" disabled={busy} onClick={inspect}>{busy?t("正在检查…"):t("重新检查")}</button></div>
    {error&&<p className="notice warning" role="alert">{error}</p>}
    <section className="settings-card"><h2>{t("控制端连接")}</h2>
      <div className="health-row"><span>{t("当前浏览器与控制端")}</span><strong>{connected?t("实时连接正常"):t("连接中断，正在重连")}</strong></div>
      <div className="health-row"><span>{t("控制端 Tailscale")}</span><strong>{diagnostics?.tailscale ? diagnostics.tailscale.ready?t("已连接"):t("未就绪") : t("尚未读取")}</strong></div>
      {diagnostics?.tailscale?.reason&&<p>{tServer(diagnostics.tailscale.reason)}</p>}
      <p>{t("手机访问地址：{0}", { 0: diagnostics?.publicUrl||t("尚未配置或未读取") })}</p>
    </section>
    <section className="settings-card"><h2>{t("AI执行设备")}</h2>
      {!devices.length&&<p>{t("尚未接入AI执行设备。")}</p>}
      {devices.map(d=><div className="health-row" key={d.id}><span>{d.name}</span><strong>{d.online?t("在线"):t("离线")}</strong></div>)}
    </section>
    <details className="settings-card diagnostic"><summary>{t("高级诊断信息")}</summary><pre>{JSON.stringify(diagnostics,null,2)}</pre></details>
  </main>;
}

function Devices({
  connected,
  preferences,
  devices,
  sessions,
  navigate,
  refresh,
}: {
  connected: boolean;
  preferences: WorkspacePreferences;
  devices: Device[];
  sessions: Session[];
  navigate: (p: string) => void;
  refresh: () => Promise<void>;
}) {
  const [view, setView] = useState(deviceViewMemory.view);
  const [candidates, setCandidates] = useState<Candidates | null>(null),
    [pairing, setPairing] = useState<{
      code: string;
      expiresAt: string;
      command: string;
    } | null>(null),
    [error, setError] = useState(""),
    [query, setQuery] = useState(deviceViewMemory.query),
    [busy, setBusy] = useState(false);
  useEffect(() => { Object.assign(deviceViewMemory, {view, query}); }, [view, query]);
  useEffect(() => {
    if (!pairing) return;
    api<Candidates>("/api/candidates")
      .then(setCandidates)
      .catch((err) => setError(err.message));
  }, [pairing]);
  const rename = async (d: Device) => {
    const name = window.prompt(t("设备显示名称"), d.name);
    if (!name?.trim()) return;
    try {
      await api(`/api/devices/${encodeURIComponent(d.id)}`, {
        method: "PATCH",
        body: JSON.stringify({ name: name.trim() }),
      });
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  };
  const remove = async (d: Device) => {
    if (
      !window.confirm(
        t("解除「{0}」的应用关联并撤销节点凭据？原生会话和设备文件会保留。", { 0: d.name }),
      )
    )
      return;
    try {
      await api(`/api/devices/${encodeURIComponent(d.id)}`, {
        method: "DELETE",
      });
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  };
  return (
    <main className="page devices-page">
      <div className="page-heading device-page-heading">
        <div className="device-page-intro">
          <span className="eyebrow">YOUR DEVICES</span>
          <div className="device-title-row"><h1>{t("AI执行设备")}</h1>
          <button className={`connection-status-link ${connected ? "live" : "reconnecting"}`} onClick={() => navigate("/status")} aria-label={t("查看连接状态：{0}", { 0: connected ? t("控制端已连接") : t("控制端正在重连") })}>
            <span className={`connection-dot ${connected ? "live" : ""}`} />
            {connected ? t("控制端已连接") : t("正在重连")}<Icon name="chevron" size={14} />
          </button></div>
          <p>{t("运行 Claude Code 和 Codex 的电脑、树莓派与服务器。")}</p>
        </div>
        <button
          className="button primary"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            try {
              setPairing(await post("/api/pairing"));
            } catch (err) {
              setError((err as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <Icon name="plus" />
          {t("接入设备")}</button>
      </div>
      {error && <div className="notice warning">{error}</div>}
      <div className="search-field">
        <Icon name="search" size={18} />
        <input
          aria-label={t("搜索设备和目录")}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("搜索设备、工作目录、会话")}
        />
      </div>
      <div className="session-view-switch" role="group" aria-label={t("设备页会话范围")}>
        <button aria-pressed={view === "active"} onClick={() => setView("active")}>{t("活跃会话")}</button>
        <button aria-pressed={view === "history"} onClick={() => setView("history")}>{t("历史会话")}</button>
      </div>
      <div className="device-tree">
        {devices
          .filter(
            (d) =>
              !query ||
              `${d.name} ${d.os}`.toLowerCase().includes(query.toLowerCase()) ||
              sessions.some(
                (s) =>
                  s.nodeId === d.id &&
                  `${s.cwd} ${titleOf(s)}`
                    .toLowerCase()
                    .includes(query.toLowerCase()),
              ),
          )
          .map((device) => {
            const deviceSessions = sessions.filter(
              (s) =>
                s.nodeId === device.id &&
                !s.metadata?.hidden &&
                !s.parentId &&
                (!query ||
                  `${device.name} ${device.os}`
                    .toLowerCase()
                    .includes(query.toLowerCase()) ||
                  `${s.cwd} ${titleOf(s)} ${s.provider} ${s.profile}`
                    .toLowerCase()
                    .includes(query.toLowerCase())),
            );
            const visibleSessions = deviceSessions.filter(s => view === "history" ? isHistorical(s) : isActive(s) || (isUncertain(s) && !!s.executionId));
            const dirs = [...new Set(visibleSessions.map((s) => projectPath(s, preferences)))].sort();
            return (
              <details className="device-branch" key={device.id} open>
                <summary>
                  <span className="device-icon">
                    <Icon name="devices" size={22} />
                  </span>
                  <span className="device-summary">
                    <strong>{device.name}</strong>
                    <span>
                      {view === "history" ? t("{0} 个历史会话", { 0: visibleSessions.length }) : t("{0} 个当前会话", { 0: visibleSessions.length })}{deviceSessions.some(needsAttention) && t(" · {0} 待处理", { 0: deviceSessions.filter(needsAttention).length })}</span>
                  </span>
                  <span
                    className={`device-state ${device.online ? "online" : ""}`}
                  >
                    {device.online ? t("在线") : t("离线")}
                  </span>
                  <Icon name="chevron" size={17} />
                </summary>
                <div className="device-branch-body">
                  {!preferences.workspaces.some(w => w.nodeId === device.id) && <p className="field-hint">{t("已自动发现此设备的会话，还未选择关注项目。")}<button className="text-button" onClick={() => navigate("/projects")}>{t("发现项目")}</button></p>}
                  {view === "history" ? <ProjectHistory nodeId={device.id} sessions={visibleSessions} preferences={preferences} navigate={navigate} /> : dirs.length ? dirs.map(dir => {
                    const items = visibleSessions.filter(s => projectPath(s, preferences) === dir)
                      .sort((a,b) => Number(needsAttention(b)) - Number(needsAttention(a)) || Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
                    return <section className="device-project-sessions" key={dir}>
                      <h2 title={dir}><Icon name="folder" size={16} />{folderName(dir)}<span>{items.length}</span></h2>
                      <div className="tree-sessions">{items.map(session => <SessionCard key={session.id} session={session} navigate={navigate} compact />)}</div>
                    </section>;
                  }) : <p className="muted small">{t("当前没有活跃会话")}</p>}
                  <details className="diagnostic">
                    <summary>{t("管理设备")}</summary>
                  <div className="device-actions">
                    <span>{t("最近连接 {0}", { 0: formatTime(device.lastSeen) })}</span>
                    <button onClick={() => rename(device)}>{t("改名")}</button>
                    <button className="danger" onClick={() => remove(device)}>
                      {t("解除关联")}</button>
                  </div>
                    <p className="muted small">{t("{0} · 最近连接 {1}", { 0: device.os, 1: formatTime(device.lastSeen) })}</p>
                    {device.diagnostics?.map((d) => (
                      <div
                        className="provider-diagnostic"
                        key={`${d.provider}:${d.profile}`}
                      >
                        <Provider provider={d.provider} />
                        <p>
                          {t("{0} · {1} · 登录：{2}", { 0: d.version || t("版本未知"), 1: d.profile, 2: d.loggedIn })}</p>
                        <p>{tServer(d.details)}</p>
                        <p>{tServer(d.capabilities.reason)}</p>
                        <div className="capability-list">
                          {Object.entries(d.capabilities)
                            .filter(([k]) => k !== "reason")
                            .map(([k, v]) => (
                              <span key={k} className={v ? "available" : ""}>
                                {capabilityNames[k] || k} {v ? "✓" : "—"}
                              </span>
                            ))}
                        </div>
                      </div>
                    ))}
                  </details>
                </div>
              </details>
            );
          })}
      </div>
      {!devices.length && (
        <Empty title={t("还没有接入的设备")}>
          {t("生成接入指令，在你选择的设备上运行 Node。Tailnet 中可见的设备不会被自动连接。")}</Empty>
      )}
      {pairing && (
        <Dialog title={t("接入新设备")} onClose={() => setPairing(null)}>
          <p>{t("在目标设备安装 pocket-agents 后，执行以下一次性接入命令：")}</p>
          <pre className="command">{pairing.command}</pre>
          <button className="button" onClick={() => copy(pairing.command)}>
            <Icon name="copy" />
            {t("复制接入命令")}</button>
          <p className="small muted">
            {t("配对码 {0} · 失效时间 {1}", { 0: pairing.code, 1: formatTime(pairing.expiresAt) })}</p>
          <p className="small">
            {t("设备需连接同一 Tailscale 网络并能访问控制端。命令只接入你主动配置的设备。")}</p>
      <section className="candidate-section">
        <div className="section-heading">
          <h2>{t("网络中发现的设备")}</h2>
          <span className="small muted">{t("网络可见 ≠ 应用已接入")}</span>
        </div>
        {!candidates && <p className="muted small">{t("正在发现网络设备…")}</p>}
        {candidates?.peers?.map((peer) => (
          <div className="candidate" key={peer.id}>
            <Icon name="devices" />
            <div>
              <strong>{peer.name || peer.id || t("未命名 Tailnet 设备")}</strong>
              <span>
                {peer.os} · Tailscale {peer.online ? t("在线") : t("离线")}
              </span>
            </div>
            <span className={`pill ${peer.registered ? "green" : ""}`}>
              {peer.registered ? t("已接入") : t("尚未接入")}
            </span>
          </div>
        ))}
        {candidates && !candidates.peers?.length && (
          <p className="muted small">{t("当前没有可显示的 Tailscale 候选设备。")}</p>
        )}
      </section>
        </Dialog>
      )}
    </main>
  );
}
const capabilityNames: Record<string, string> = {
  discover: t("发现历史"),
  create: t("创建"),
  resume: t("恢复"),
  attach: t("连接实例"),
  send: t("发送"),
  approve: t("审批"),
  interrupt: t("中断"),
  terminate: t("结束实例"),
  terminal: t("终端"),
  persistent: t("持久运行"),
};
function Notifications({
  notifications,
  sessions,
  navigate,
}: {
  notifications: AgentEvent[];
  sessions: Session[];
  navigate: (p: string) => void;
}) {
  return (
    <main className="page">
      <div className="page-heading">
        <div>
          <span className="eyebrow">INBOX</span>
          <h1>{t("值得留意的进展。")}</h1>
          <p>{t("原生事件汇集于此；系统通知状态和锁屏测试见下方。")}</p>
        </div>
        <button className="button" onClick={() => navigate("/settings")}>
          <Icon name="bell" />
          {t("通知设置")}</button>
      </div>
      <PushSettings/>
      {notifications.length ? (
        <div className="notification-list">
          {(() => {
            const byId = new Map(sessions.map((s) => [s.id, s]));
            const newest = new Map<string, number>();
            const sorted = [...notifications].sort((a, b) => b.id - a.id);
            for (const event of sorted)
              if (event.sessionId && !newest.has(event.sessionId)) newest.set(event.sessionId, event.id);
            let lastDay = "";
            return sorted.map((event) => {
              const session = event.sessionId ? byId.get(event.sessionId) : undefined;
              const waiting = ["waiting_approval", "waiting_input", "approval", "question"].includes(event.kind);
              // A wait is still open only if it is the session's latest event
              // and the session is waiting now; older ones read as history.
              const open = waiting && !!session && newest.get(session.id) === event.id && needsAttention(session);
              const tone = waiting ? (open ? "attention" : "stale") : event.kind === "error" ? "error" : event.kind === "completed" || event.kind === "turn.completed" ? "done" : "muted";
              const day = dayLabel(event.at);
              const heading = day !== lastDay ? <h2 className="notification-day" key={`day:${day}`}>{day}</h2> : null;
              lastDay = day;
              return [heading,
                <button
                  key={event.id}
                  className={`notification-item ${tone}`}
                  disabled={!event.sessionId}
                  onClick={() =>
                    event.sessionId &&
                    navigate(`/session/${encodeURIComponent(event.sessionId)}`)
                  }
                >
                  <span className="notification-icon">
                    <Icon name={waiting ? "bell" : tone === "done" ? "check" : "stop"} size={17} />
                  </span>
                  <div>
                    <strong>
                      {eventLabel(event.kind)}
                      {waiting && !open && <span className="notification-note"> {" "}{t("· 已不再等待")}</span>}
                    </strong>
                    <p>
                      {session
                        ? `${titleOf(session)} · ${session.deviceName || ""}`
                        : t("设备事件")}
                    </p>
                  </div>
                  <time>{formatClock(event.at)}</time>
                  <Icon name="chevron" size={17} />
                </button>];
            });
          })()}
        </div>
      ) : (
        <Empty title={t("暂无通知")}>{t("本轮结束、审批请求和异常会显示在这里。")}</Empty>
      )}
    </main>
  );
}
function PreferencesPage({projectsOnly, sessions, devices, preferences, savePreferences, onLogout}: {
  projectsOnly:boolean; sessions:Session[]; devices:Device[]; preferences:WorkspacePreferences;
  savePreferences:(value:WorkspacePreferences)=>Promise<void>; onLogout:()=>Promise<void>;
}) {
  if (!projectsOnly) return <Settings onLogout={onLogout}/>;
  return <main className="page settings-page"><div className="page-heading"><div><h1>{t("项目")}</h1><p>{t("选择日常关注的工作目录，发现的其他目录不会自动加入。")}</p></div></div>
    <WorkspaceSettings sessions={sessions} devices={devices} preferences={preferences} save={savePreferences}/>
  </main>;
}
function Settings({onLogout}:{onLogout:()=>Promise<void>}) {
  const [error, setError] = useState("");
  return (
    <main className="page settings-page">
      <div className="page-heading">
        <div>
          <span className="eyebrow">SETTINGS</span>
          <h1>{t("设置")}</h1>
          <p>{t("管理当前浏览器的语言、通知和登录。")}</p>
        </div>
      </div>
      <section className="settings-card language-card">
        <h2 translate="no">语言 · Language</h2>
        <p>{t("界面文字和日期格式。只影响当前浏览器，切换后页面会重新载入。")}</p>
        <div className="language-options" role="radiogroup" aria-label={t("界面语言")}>
          {(["zh", "en", "system"] as const).map((option) => (
            <button key={option} type="button" role="radio" aria-checked={languageSetting() === option}
              className={`button ${languageSetting() === option ? "selected" : ""}`}
              onClick={() => languageSetting() !== option && setLanguage(option)}>
              {option === "system" ? t("跟随系统") : <span translate="no">{option === "zh" ? "中文" : "English"}</span>}
            </button>
          ))}
        </div>
      </section>
      <PushSettings/>
      {error && <p className="form-error" role="alert">{error}</p>}
      <AppUpdate settings={true}/>
      <section className="settings-card">
        <h2>{t("退出登录")}</h2>
        <p>{t("仅退出当前浏览器，电脑上的 AI 任务会继续运行。")}</p>
        <button
          className="button"
          onClick={async () => {
            try {
              await onLogout();
            } catch (err) {
              setError((err as Error).message);
            }
          }}
        >
          {t("退出当前浏览器")}</button>
      </section>
    </main>
  );
}
function TerminalPanel({
  initial,
  target,
  onClose,
  execute,
  busy,
  blocked,
  onQueryPending,
  onConfirmPending,
}: {
  initial: Result;
  target: Action;
  onClose: () => void;
  execute: Execute;
  busy: boolean;
  blocked: boolean;
  onQueryPending: () => void;
  onConfirmPending: (label: string) => boolean;
}) {
  const [data, setData] = useState<TerminalData>(
      (initial.data || {}) as TerminalData,
    ),
    [input, setInput] = useState(""),
    [error, setError] = useState(""),
    [result, setResult] = useState<Result | null>(null),
    [lastKey, setLastKey] = useState(""),
    [moreKeys, setMoreKeys] = useState(false);
  const connection = useRef<ReturnType<typeof terminalConnection> | null>(null);
  const mounted = useRef(true);
  const screenRef = useRef<HTMLPreElement>(null);
  const pinned = useRef(true);
  const menu = terminalMenu(data.screen);
  // The prompt the user must answer is at the bottom of the native screen.
  useLayoutEffect(() => {
    const el = screenRef.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [data.screen, data.output]);
  const interactive =
    typeof data.screen === "string" || typeof data.output === "string";
  useEffect(() => {
    mounted.current = true;
    if (!interactive || target.provider !== "claude") return;
    const request = async (open: boolean, state: TerminalData, signal: AbortSignal) => {
      const result = await post<Result>("/api/actions", {
        ...target, id: crypto.randomUUID(), kind: "terminal", text: undefined,
        terminalId: open ? undefined : state.terminalId,
        data: open ? { open: true } : { cursor: state.cursor || 0 },
      }, signal);
      if (result.status !== "accepted") throw new Error(tServer(result.message) || t("原生终端暂不可读取"));
      return (result.data || {}) as TerminalData;
    };
    const controller = terminalConnection((initial.data || {}) as TerminalData, {
      read: (state, signal) => request(false, state, signal),
      open: signal => request(true, {}, signal),
      changed: (state, message) => { setData(state); setError(message); },
    });
    connection.current = controller;
    return () => {
      mounted.current = false; controller.stop(); connection.current = null;
    };
  }, [interactive, target, initial]);
  const disconnected = target.provider === "claude" && data.state !== "connected";
  const send = async (bytes: string, label = t("输入")) => {
    if (busy || disconnected) return;
    const accept = connection.current?.capture();
    if (bytes === "\r" && data.suggestedReply) { setError(t("当前只是建议回复。请先点“填入建议”，或在下方输入自己的文字。")); return; }
    setError("");
    if (needsConfirmation(bytes, blocked) && !onConfirmPending(label)) return;
    setLastKey(label);
    setResult(null);
    const response = await execute({
      ...target,
      kind: "terminal_input",
      text: bytes,
      requestId: data.requestId || "",
      terminalId: data.terminalId,
    });
    if (!mounted.current) return;
    setResult(response);
    if (response?.status === "accepted" || response?.status === "submitted") setInput("");
    // The backend returns the screen as redrawn after this key, plus the
    // token for the next one, so the effect shows without waiting for a poll.
    const next = response?.data as TerminalData | undefined;
    if (next?.screen !== undefined) {
      if (accept) accept(next);
      else setData((old) => ({ ...old, ...next }));
    }
  };
  const keys = [
    { label: "Esc", name: t("退出提示 Esc"), value: "\x1b" },
    { label: "Tab", name: t("下一焦点 Tab"), value: "\t" },
    { label: "↑", name: t("上一项 ↑"), value: "\x1b[A" },
    { label: "↓", name: t("下一项 ↓"), value: "\x1b[B" },
    { label: "←", name: t("← 左"), value: "\x1b[D" },
    { label: "→", name: t("→ 右"), value: "\x1b[C" },
  ];
  return (
    <Dialog title={t("电脑终端")} onClose={onClose} className={interactive ? "terminal-dialog" : ""}>
      {interactive ? (
        <>
          {!disconnected && <div role="status" aria-live="polite" className={`terminal-status ${result?.status === "rejected" || result?.status === "unknown" || blocked ? "warning" : ""}`}>
            {blocked ? t("上次操作的结果未确认。仍可移动选项；点击回车时会请你确认当前选择。") : busy ? t("正在发送{0}…", { 0: lastKey }) : result ? (result.status === "rejected" || result.status === "unknown" ? tServer(result.message) || t("按键未确认，请核对终端") : t("已发送「{0}」，屏幕已更新。", { 0: lastKey })) : t("电脑上同一个会话的实时终端。")}
            {blocked && <button className="text-button" disabled={disconnected || busy} onClick={onQueryPending}>{t("重新查询结果")}</button>}
          </div>}
          {disconnected && <div className="notice terminal-disconnected" role="status"><p>{data.state === "exited" ? t("当前实例已退出，历史记录保留。") : t("终端连接已断开或无法确认。重新连接前不会发送按键；后台实例可能仍在运行。")}</p>{data.state !== "exited" && <button className="button" disabled={busy} onClick={() => void connection.current?.reconnect()}>{t("重新连接终端")}</button>}</div>}
          {data.suggestedReply && <div className="notice" role="status">
            <strong>{t("这是建议回复，尚未输入")}</strong>
            <p>{t("原生输入框目前为空。先填入建议，检查文字后再按回车提交。")}</p>
            <button type="button" className="button primary" disabled={disconnected || busy} onClick={() => send("\t", t("填入建议"))}>{t("填入建议")}</button>
          </div>}
          <pre ref={screenRef} className="terminal-screen" role="log" aria-label={t("原生终端屏幕")}
            onScroll={(e) => { const el = e.currentTarget; pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24; }}>
            {data.screen ??
              data.output?.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")}
          </pre>
          {menu && (
            <div className="terminal-menu" role="group" aria-label={t("原生选项")}>
              {menu.options.map((option, i) => {
                const current = i === menu.selected;
                const label = option.number === null ? t("提交选择") : option.label;
                const hint = option.checked === undefined ? t("再点确认") : option.checked ? t("再点取消勾选") : t("再点勾选");
                return (
                  <button key={option.number ?? "submit"} type="button" aria-pressed={current} disabled={disconnected || busy || (!current && menu.selected < 0)}
                    className={`terminal-option ${current ? "selected" : ""}`}
                    // First tap moves the native highlight; tapping the
                    // highlighted option presses Enter on it.
                    onClick={() => current ? send("\r", t("确认 {0}", { 0: label })) : send(moveKeys(menu, i), t("移到 {0}", { 0: label }))}>
                    <span className="terminal-option-number">{option.number ?? "✓"}</span>
                    {option.checked !== undefined && <span className="terminal-option-check" aria-label={option.checked ? t("已勾选") : t("未勾选")}>{option.checked ? "✓" : ""}</span>}
                    <span className="terminal-option-label">{label}</span>
                    {current && <span className="terminal-option-hint">{hint}</span>}
                  </button>
                );
              })}
            </div>
          )}
          <form className="terminal-compose" onSubmit={(e) => { e.preventDefault(); if (input) void send(input + "\r", t("输入")); }}>
            <input value={input} onChange={(event) => setInput(event.target.value)} placeholder={t("输入文字，回车发送…")} aria-label={t("发送原生终端输入")} enterKeyHint="send" autoCapitalize="off" autoCorrect="off" />
            <button className="button" disabled={disconnected || !input || busy}>{t("发送")}</button>
          </form>
          <div className="terminal-keybar" role="group" aria-label={t("终端按键")}>
            {keys.map((key) => (
              <button key={key.value} type="button" className="terminal-key" data-key={key.label} aria-label={key.name} title={key.name} disabled={disconnected || busy} onClick={() => send(key.value, key.label)}>{key.label}</button>
            ))}
            <button type="button" className="terminal-key more" aria-expanded={moreKeys} aria-label={t("更多按键")} onClick={() => setMoreKeys(!moreKeys)}>⋯</button>
            <button type="button" className="terminal-key enter" aria-label={t("回车 Enter")} disabled={disconnected || busy || !!data.suggestedReply} onClick={() => send("\r", "Enter")}>⏎</button>
          </div>
          {moreKeys && (
            <div className="terminal-more">
              <button className="button" disabled={disconnected || busy} onClick={() => send("\x1b[Z", "⇧Tab")}>{t("上一焦点 ⇧ Tab")}</button>
              <button className="button danger" disabled={disconnected || busy} onClick={() => send("\x03", "Ctrl+C")}>{t("中断 Ctrl+C")}</button>
            </div>
          )}
          {target.provider === "claude" && <details className="terminal-advanced"><summary className="compact-disclosure"><Icon name="settings" size={15}/><span>{t("Claude 模型设置")}</span><Icon name="chevron" size={14}/></summary><div className="model-controls"><p>{t("在原生输入框为空时打开选择器。确认模型或强度后点“仅本会话”，避免更改全局默认。")}</p><div className="button-row"><button className="button" disabled={disconnected || busy} onClick={() => {if(window.confirm(t("确认原生输入框为空？将输入 /model 打开模型选择器。"))) void send("/model\r")}}>{t("选择模型")}</button><button className="button" disabled={disconnected || busy} onClick={() => {if(window.confirm(t("确认原生输入框为空？将输入 /effort 打开强度选择器。"))) void send("/effort\r")}}>{t("选择 effort")}</button><button className="button" disabled={disconnected || busy} onClick={() => send("s")}>{t("仅本会话（s）")}</button></div></div></details>}
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
        </>
      ) : (
        <p>{t("在目标设备的终端执行以下命令。此处不会自动运行命令。")}</p>
      )}
      {data.command && (
        <>
          <details open={!interactive}>
            <summary>{t("本机终端连接命令")}</summary>
            <pre className="command">{data.command}</pre>
            <button className="button" onClick={() => copy(data.command || "")}>
              <Icon name="copy" />
              {t("复制命令")}</button>
          </details>
        </>
      )}
      {/* The live panel explains itself in Chinese; the provider text is English. */}
      {data.description && !interactive && <p>{data.description}</p>}
    </Dialog>
  );
}
function Dialog({
  title,
  onClose,
  children,
  className = "",
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  className?: string;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={`dialog ${className}`}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
    >
      <div className="dialog-content">
        <div className="section-heading">
          <h2>{title}</h2>
          <button
            className="icon-button"
            aria-label={t("关闭对话框")}
            onClick={onClose}
          >
            <Icon name="close" />
          </button>
        </div>
        {children}
      </div>
    </dialog>
  );
}
async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    window.prompt(t("复制以下内容"), text);
  }
}

if ("serviceWorker" in navigator)
  navigator.serviceWorker.register("/sw.js").catch(() => {});
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
