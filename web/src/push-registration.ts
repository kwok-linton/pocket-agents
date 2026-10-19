import { api, post } from "./api.ts";
import { readyWorker, subscriptionID } from "./push.ts";
import { pushStage, withTimeout, type PushEnvironment } from "./push-state.ts";
import { t } from "./i18n.ts";

export type PushInfo = {
  enabled: boolean; publicKey: string; activeSubscriptionIds: string[];
  tests: { id: string; subscriptionId: string; dueAt: string; state: string; httpStatus: number; reason?: string }[];
  lastDelivery?: { subscriptionId: string; eventId: number; attemptAt: string; httpStatus: number; accepted: boolean; reason?: string }[];
};
export function pushEnvironment(): PushEnvironment {
  return {
    secure: window.isSecureContext,
    ios: /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1),
    standalone: window.matchMedia("(display-mode: standalone)").matches || (navigator as Navigator & { standalone?: boolean }).standalone === true,
    supported: "serviceWorker" in navigator && "PushManager" in window && "Notification" in window,
    permission: "Notification" in window ? Notification.permission : "default",
  };
}

// Share registration between the settings page and application recovery. Only
// subscription registration is retried; this never requests permission or sends
// a test notification. The server's upsert preserves an existing event cursor.
export function createPushRegistration(deps: {
  environment: () => PushEnvironment; info: () => Promise<PushInfo>;
  worker: () => Promise<ServiceWorkerRegistration>; identify: typeof subscriptionID;
  register: (sub: PushSubscription) => Promise<unknown>;
}) {
  let running: Promise<{env: PushEnvironment; info: PushInfo; subID: string}> | undefined;
  const check = async (repair: boolean) => {
    const env = deps.environment();
    let info = await deps.info(), subID = "";
    if (pushStage(env, true, false, false) !== "register") return {env, info, subID};
    const reg = await deps.worker();
    let sub = await withTimeout(reg.pushManager.getSubscription(), 8000, t("通知组件尚未就绪，请刷新应用后重试。"));
    subID = await deps.identify(sub);
    if (repair && info.enabled && (!sub || !info.activeSubscriptionIds.includes(subID))) {
      const expired = info.tests?.some(x => x.subscriptionId === subID && x.state === "expired") ||
        info.lastDelivery?.some(x => x.subscriptionId === subID && [404, 410].includes(x.httpStatus));
      if (sub && expired) {
        await withTimeout(sub.unsubscribe(), 8000, t("通知组件尚未就绪，请刷新应用后重试。"));
        sub = null;
      }
      if (!sub) {
        const key = Uint8Array.from(atob(info.publicKey.replace(/-/g, "+").replace(/_/g, "/")), c => c.charCodeAt(0));
        sub = await withTimeout(reg.pushManager.subscribe({userVisibleOnly: true, applicationServerKey: key}), 30000,
          t("获取推送地址超时，请确认手机网络可连接系统推送服务，再重新检查。"));
      }
      await deps.register(sub);
      subID = await deps.identify(sub);
      info = await deps.info();
      if (!info.activeSubscriptionIds.includes(subID)) throw new Error("registration not confirmed");
    }
    return {env, info, subID};
  };
  return (repair = true) => {
    if (!repair) return running || check(false);
    if (!running) running = check(true).finally(() => { running = undefined; });
    return running;
  };
}
export const syncPushRegistration = createPushRegistration({
  environment: pushEnvironment,
  info: () => api<PushInfo>("/api/push"),
  worker: readyWorker, identify: subscriptionID,
  register: async sub => {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 12000);
    try { return await post("/api/push/subscriptions", sub.toJSON(), controller.signal); }
    finally { clearTimeout(timer); }
  },
});
