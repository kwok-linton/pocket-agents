import { readLoop } from "./read-loop.ts";

// Read receipts only while unresolved. Recovery never repeats the action.
export function watchActionResults(read: (signal: AbortSignal) => Promise<void>, environment = {
  page: document, events: window, online: () => navigator.onLine,
}) {
  const loop = readLoop(read, () => environment.page.visibilityState === "visible" && environment.online(), () => 5000);
  const resume = () => { loop.pause(); loop.trigger(); };
  const changed = () => loop.trigger();
  environment.page.addEventListener("visibilitychange", resume);
  environment.events.addEventListener("online", resume);
  environment.events.addEventListener("offline", resume);
  environment.events.addEventListener("pocket:connection-restored", resume);
  environment.events.addEventListener("pocket:action-change", changed);
  loop.trigger();
  return () => {
    loop.stop();
    environment.page.removeEventListener("visibilitychange", resume);
    environment.events.removeEventListener("online", resume);
    environment.events.removeEventListener("offline", resume);
    environment.events.removeEventListener("pocket:connection-restored", resume);
    environment.events.removeEventListener("pocket:action-change", changed);
  };
}
