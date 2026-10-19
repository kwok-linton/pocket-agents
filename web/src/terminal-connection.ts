import { readLoop } from "./read-loop.ts";

export type TerminalData = {
  command?: string;
  description?: string;
  screen?: string;
  suggestedReply?: string;
  output?: string;
  cursor?: number;
  requestId?: string;
  terminalId?: string;
  state?: "connected" | "disconnected" | "exited" | "unknown";
};

// Reading can never open a connection. Each explicit reconnect invalidates all
// previous reads and key callbacks, even if a transport ignores cancellation.
export function terminalConnection(initial: TerminalData, io: {
  read: (data: TerminalData, signal: AbortSignal) => Promise<TerminalData>;
  open: (signal: AbortSignal) => Promise<TerminalData>;
  changed: (data: TerminalData, error: string) => void;
}, environment = { page: document, events: window, online: () => navigator.onLine }) {
  let data = initial, epoch = 0, stopped = false;
  let opening: AbortController | undefined;
  const accept = (next: TerminalData) => {
    if (stopped || next.terminalId !== data.terminalId) return;
    if (data.state !== "connected" && next.state === "connected") return;
    if (next.cursor !== undefined && next.cursor < (data.cursor || 0)) return;
    data = { ...data, ...next };
    io.changed(data, "");
  };
  const loop = readLoop(async signal => {
    const version = epoch;
    try {
      const next = await io.read(data, signal);
      if (!signal.aborted && version === epoch) accept(next);
    } catch (err) {
      if (!stopped && !signal.aborted && version === epoch) {
        data = { ...data, state: "unknown" };
        io.changed(data, (err as Error).message);
      }
    }
  }, () => !stopped && data.state === "connected" && environment.page.visibilityState === "visible" && environment.online(), () => 1000);
  const resume = () => { loop.pause(); loop.trigger(); };
  environment.page.addEventListener("visibilitychange", resume);
  environment.events.addEventListener("online", resume);
  environment.events.addEventListener("offline", resume);
  loop.trigger();
  return {
    accept,
    capture() { const version = epoch; return (next: TerminalData) => { if (version === epoch) accept(next); }; },
    async reconnect() {
      if (stopped || opening) return;
      loop.pause();
      const version = ++epoch;
      const controller = new AbortController(); opening = controller;
      data = { ...data, state: "unknown" }; io.changed(data, "");
      try {
        const next = await io.open(controller.signal);
        if (stopped || version !== epoch) return;
        data = next; io.changed(data, ""); loop.trigger();
      } catch (err) {
        if (!stopped && version === epoch) io.changed(data, (err as Error).message);
      } finally { if (opening === controller) opening = undefined; }
    },
    stop() {
      stopped = true; epoch++; opening?.abort(); loop.stop();
      environment.page.removeEventListener("visibilitychange", resume);
      environment.events.removeEventListener("online", resume);
      environment.events.removeEventListener("offline", resume);
    },
  };
}
