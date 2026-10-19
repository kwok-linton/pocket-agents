// One active read and at most one pending refresh. Events coalesce, leaving a
// view aborts its work, and hidden pages do no polling or catch-up downloads.
export function readLoop(work: (signal: AbortSignal) => Promise<void>, active: () => boolean, interval: () => number) {
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false, dirty = false, last = 0;
  const run = () => {
    timer = undefined;
    if (stopped || !active()) return;
    if (controller) { dirty = true; return; }
    const current = new AbortController();
    controller = current;
    last = Date.now();
    dirty = false;
    void work(current.signal).catch(() => {}).finally(() => {
      if (controller !== current) return;
      controller = undefined;
      if (!stopped && active()) timer = setTimeout(run, dirty ? Math.max(0, 800 - (Date.now() - last)) : interval());
    });
  };
  return {
    trigger() {
      if (stopped || !active()) return;
      if (controller) { dirty = true; return; }
      clearTimeout(timer);
      timer = setTimeout(run, Math.max(0, 800 - (Date.now() - last)));
    },
    pause() { clearTimeout(timer); controller?.abort(); controller = undefined; dirty = false; },
    stop() { stopped = true; this.pause(); },
  };
}
