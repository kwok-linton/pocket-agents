export interface PendingDraft {
  actionId: string;
  text: string;
  files: string[];
  detached?: boolean;
}

/** A late receipt may clear only the draft that was actually submitted. */
export function settleDraft(pending: PendingDraft, result: { actionId: string; status: string }, text: string, files: string[]) {
  if (result.actionId !== pending.actionId || result.status !== "accepted") return null;
  if (pending.detached) return { text, files };
  return { text: text === pending.text ? "" : text, files: files.filter(path => !pending.files.includes(path)) };
}

/** Restore a failed submission without replacing a newer draft. Never auto-resend. */
export function restoreDraft(pending: PendingDraft, text: string, files: string[]) {
  return { text: text ? `${text}\n\n${pending.text}`.trimEnd() : pending.text,
    files: [...new Set([...files, ...pending.files])] };
}
