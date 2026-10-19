import { api } from "./api.ts";
import type { Device, Session, WorkspacePreferences } from "./types.ts";

export type ViewData = { session?: Session; device?: Device; sessions?: Session[]; devices?: Device[]; preferences?: WorkspacePreferences };

export async function loadView(path: string, signal: AbortSignal, receive: (data: ViewData) => void) {
  const deliver = (data: ViewData) => { if (!signal.aborted) receive(data); };
  if (path.startsWith("/session/")) {
    const detail = await api<{session: Session; device: Device}>(`/api/sessions/${encodeURIComponent(decodeURIComponent(path.slice(9)))}`, {signal});
    deliver(detail);
    return;
  }
  await Promise.all([
    api<Device[]>("/api/devices", {signal}).then(devices => deliver({devices})),
    api<Session[]>("/api/sessions", {signal}).then(sessions => deliver({sessions})),
    api<WorkspacePreferences>("/api/preferences", {signal}).then(preferences => deliver({preferences})),
  ]);
}
