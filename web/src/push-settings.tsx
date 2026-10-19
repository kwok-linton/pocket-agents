import { useCallback, useEffect, useRef, useState } from "react";
import { post } from "./api";
import { t } from "./i18n";
import { pushRecords, type PushRecord } from "./push";
import { pushStage, PushSetupError } from "./push-state";
import { allowPush } from "./push-permission";
import { pushEnvironment, syncPushRegistration, type PushInfo } from "./push-registration";

export function PushSettings() {
  const [env, setEnv] = useState(pushEnvironment), [info, setInfo] = useState<PushInfo | null>(null);
  const [subID, setSubID] = useState(""), [records, setRecords] = useState<PushRecord[]>([]);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const working = useRef<Promise<void> | null>(null);
  const allowing = useRef(false);
  const registered = !!subID && !!info?.activeSubscriptionIds.includes(subID);
  const stage = pushStage(env, !!info, !!subID, registered);
  // System permission is the user's preference. Once granted, register on this
  // device automatically; no second application-level enable switch is needed.
  const refresh = useCallback(async (repair = true) => {
    const next = await syncPushRegistration(repair);
    setEnv(next.env); setInfo(next.info); setSubID(next.subID);
    if (repair) setNotice("");
    setRecords(await pushRecords());
  }, []);
  const runRefresh = useCallback((repair = true): Promise<void> => {
    if (working.current) return working.current;
    const operation = refresh(repair).then(() => { if (repair) setError(""); })
      .catch(err => setError(err instanceof PushSetupError ? err.message : t("无法完成通知登记或检查。请确认网络已连接，刷新应用后重试。")))
      .finally(() => { if (working.current === operation) working.current = null; });
    working.current = operation;
    return operation;
  }, [refresh]);
  useEffect(() => {
    void runRefresh();
    const visible = () => { if (document.visibilityState === "visible") void runRefresh(); };
    const timer = setInterval(() => { if (document.visibilityState === "visible") void runRefresh(false); }, 5000);
    document.addEventListener("visibilitychange", visible);
    return () => { clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [runRefresh]);
  const allow = async () => {
    if (allowing.current) return;
    allowing.current = true; setBusy(true); setError("");
    try {
      await allowPush(() => Notification.requestPermission(), working.current, () => runRefresh(true), () => setEnv(pushEnvironment()));
    } catch { setError(t("无法完成通知登记或检查。请确认网络已连接，刷新应用后重试。")); }
    finally { allowing.current = false; setBusy(false); }
  };
  const check = info?.tests?.find(item => item.subscriptionId === subID);
  const delivery = info?.lastDelivery?.find(item => item.subscriptionId === subID);
  const lastFailed = check && (!delivery || Date.parse(check.dueAt) > Date.parse(delivery.attemptAt))
    ? check.state === "rejected" || check.state === "unknown"
    : !!delivery && !delivery.accepted;
  const rejection = (reason: string | undefined, status: number) => ["BadJwtToken", "VapidPkHashMismatch", "BadVapidPublicKey"].includes(reason || "")
    ? t("推送身份校验失败，请检查控制端的推送身份地址与签名配置；手机无需重新授权。")
    : status === 0 ? t("控制端未能确认推送发送结果，请检查网络后重试。") : t("推送服务拒绝了请求（HTTP {0}），请检查控制端网络和推送配置。", { 0: status });
  const received = check && records.some(r => r.testId === check.id && r.stage === "push_received");
  const shown = check && records.some(r => r.testId === check.id && r.stage === "show_notification_resolved");
  const clicked = check && records.some(r => r.testId === check.id && r.stage === "notification_clicked");
  const failed = check && records.some(r => r.testId === check.id && r.stage === "show_notification_rejected");
  const status = {
    https: t("需要安全连接"), install: t("请从主屏幕打开"), unsupported: t("此浏览器不支持推送"),
    denied: t("系统通知已关闭"), permission: t("等待系统授权"), loading: t("正在检查通知…"),
    register: t("设备尚未登记"), ready: lastFailed ? t("设备已登记 · 最近投递失败") : t("系统已允许 · 设备已登记"),
  }[stage];
  return <section className="settings-card push-settings">
    <div className="section-heading"><h2>{t("消息推送")}</h2><span className={`pill ${stage === "ready" && !lastFailed ? "green" : ""}`}>{status}</span></div>
    <p>{t("仅提醒当前待处理事项和未读结果。推送失败最多重试 3 次，之后放弃；离线期间不补发。是否接收由系统通知设置决定。")}</p>
    {stage === "https" && <p className="notice warning">{t("请使用手机访问页面提供的 Tailscale HTTPS 地址。")}</p>}
    {stage === "install" && <p className="notice warning">{t("iPhone 需要 iOS 16.4 或更新版本：在 Safari 点分享 → 添加到主屏幕，再点主屏幕上的 Pocket Agents 图标打开。普通 Safari 标签页不能接收推送。")}</p>}
    {stage === "unsupported" && <p className="notice warning">{t("请更新系统和浏览器。iPhone 请使用主屏幕应用，Android 请使用支持通知的浏览器。")}</p>}
    {stage === "denied" && <p className="notice warning">{env.ios ? t("前往 iPhone 设置 → 通知 → Pocket Agents，打开允许通知，并勾选锁定屏幕、通知中心和横幅；然后返回这里。") : t("请在浏览器的网站设置和系统通知设置中允许 Pocket Agents 通知，然后返回这里。")}</p>}
    {stage === "permission" && <button className="button primary" disabled={busy} onClick={allow}>{t("允许系统通知")}</button>}
    {stage === "register" && <button className="button primary" disabled={busy} onClick={allow}>{t("登记这台设备")}</button>}
    <div className="button-row">
      {stage === "ready" && <button className="button primary" disabled={busy || check?.state === "queued"} onClick={async () => {
        setBusy(true); setError("");
        try {
          await post("/api/push/tests", { id: crypto.randomUUID(), subscriptionId: subID });
          setNotice(t("测试已排队。现在锁屏，通常约 10–30 秒后收到；点通知返回这里查看结果。"));
          await runRefresh(false);
        } catch (err) { setError((err as Error).message); }
        finally { setBusy(false); }
      }}>{t("发送锁屏测试通知")}</button>}
      <button className="button" disabled={busy} onClick={() => runRefresh()}>{t("重新检查")}</button>
    </div>
    {lastFailed && delivery && (!check || Date.parse(delivery.attemptAt) >= Date.parse(check.dueAt)) && <p className="notice warning" role="status">{rejection(delivery.reason, delivery.httpStatus)}</p>}
    {check && <div className="notice" role="status">
      <p>{check.state === "queued" ? t("测试正在等待发送，请先锁屏。") : check.state === "accepted" ? t("控制端已发送，推送服务已接受。") : check.state === "expired" ? t("推送地址已失效，请重新检查并登记。") : check.state === "unknown" ? (check.reason === "TransportTimeout" ? t("控制端连接推送服务超时，未确认发送成功。请稍后重新测试。") : t("测试发送结果未知，不会自动重发。请先查看手机通知，再决定是否重新测试。")) : rejection(check.reason, check.httpStatus)}</p>
      <p>{clicked ? t("你已点击这条测试通知。") : failed ? t("手机收到了测试，但系统拒绝显示；请检查系统通知设置。") : shown ? t("手机已收到，系统已接受显示请求；请确认锁屏实际出现通知。") : received ? t("手机已收到测试，正在检查显示结果。") : t("尚未读到这台设备的接收记录，解锁回到应用后会自动检查。")}</p>
    </div>}
    <p className="field-hint">{t("系统授权不代表锁屏一定显示；专注模式、定时摘要和锁屏通知选项会影响提醒。网页无法读取这些系统设置。")}</p>
    {notice && <p className="notice" role="status">{notice}</p>}
    {error && <p className="form-error" role="alert">{error}</p>}
  </section>;
}
