// iOS may suspend a page without closing its socket. Recreate that connection
// on return instead of trusting OPEN on a socket from the previous network.
export function eventConnection(options: {
  url: () => string;
  connected: (value: boolean) => void;
  message: (event: MessageEvent) => void;
}, environment = {
  page: document,
  events: window,
  online: () => navigator.onLine,
  socket: (url: string) => new WebSocket(url),
}) {
  let socket: WebSocket | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let deadline: ReturnType<typeof setTimeout> | undefined;
  let receiveDeadline: ReturnType<typeof setTimeout> | undefined;
  let stopped = false, attempts = 0;
  const active = () => !stopped && environment.page.visibilityState === "visible" && environment.online();
  const close = () => {
    clearTimeout(timer);
    clearTimeout(deadline);
    clearTimeout(receiveDeadline);
    const old = socket;
    socket = undefined;
    if (old) {
      old.onopen = old.onclose = old.onerror = old.onmessage = null;
      old.close();
    }
    options.connected(false);
  };
  const retry = () => {
    close();
    if (active()) timer = setTimeout(connect, Math.min(15000, 1000 * 2 ** Math.min(attempts++, 4)));
  };
  const connect = () => {
    if (!active()) return;
    let current: WebSocket;
    try { current = environment.socket(options.url()); }
    catch { retry(); return; }
    socket = current;
    const received = () => {
      clearTimeout(receiveDeadline);
      // The server emits a tiny heartbeat every 15s even when nothing changes.
      // OPEN alone is not evidence that a VPN route still works.
      receiveDeadline = setTimeout(() => { if (socket === current) retry(); }, 45000);
    };
    deadline = setTimeout(() => { if (socket === current) retry(); }, 12000);
    current.onopen = () => {
      if (socket !== current) return;
      clearTimeout(deadline);
      attempts = 0;
      received();
      options.connected(true);
    };
    current.onmessage = event => {
      if (socket !== current) return;
      received();
      options.message(event);
    };
    current.onclose = current.onerror = () => { if (socket === current) retry(); };
  };
  const resume = () => {
    close();
    attempts = 0;
    connect();
  };
  const show = (event: PageTransitionEvent) => { if (event.persisted) resume(); };
  environment.page.addEventListener("visibilitychange", resume);
  environment.events.addEventListener("online", resume);
  environment.events.addEventListener("offline", close);
  environment.events.addEventListener("pageshow", show);
  environment.events.addEventListener("pagehide", close);
  environment.events.addEventListener("pocket:retry-connection", resume);
  connect();
  return {
    isOpen: () => socket?.readyState === 1,
    stop() {
      stopped = true;
      environment.page.removeEventListener("visibilitychange", resume);
      environment.events.removeEventListener("online", resume);
      environment.events.removeEventListener("offline", close);
      environment.events.removeEventListener("pageshow", show);
      environment.events.removeEventListener("pagehide", close);
      environment.events.removeEventListener("pocket:retry-connection", resume);
      close();
    },
  };
}
