import { t } from "./i18n.ts";
import { withTimeout } from "./push-state.ts";
export type PushRecord = { eventId: number | null; testId?: string; stage: string; at: string };

export async function subscriptionID(subscription: PushSubscription | null) {
  if (!subscription) return "";
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(subscription.endpoint),
  );
  return Array.from(new Uint8Array(digest), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}

export async function pushRecords(): Promise<PushRecord[]> {
  const reg = await readyWorker();
  const worker = navigator.serviceWorker.controller || reg.active;
  if (!worker) return [];
  return new Promise((resolve) => {
    const channel = new MessageChannel();
    const finish = (records: PushRecord[]) => {
      clearTimeout(timer);
      channel.port1.close();
      resolve(records);
    };
    const timer = setTimeout(() => finish([]), 2000);
    channel.port1.onmessage = (event) =>
      finish(Array.isArray(event.data?.records) ? event.data.records : []);
    worker.postMessage({ type: "pocket:push-diagnostics" }, [channel.port2]);
  });
}

export function readyWorker() {
  return withTimeout(navigator.serviceWorker.ready, 8000, t("通知组件尚未就绪，请刷新应用后重试。"));
}
