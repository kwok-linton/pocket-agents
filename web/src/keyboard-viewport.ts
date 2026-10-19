// WebKit does not resize 100dvh for the keyboard. Use the visual viewport only
// while an editor is focused; leave pinch zoom, ordinary pages and desktop alone.
export function keyboardGeometry(layoutHeight: number, height: number, top: number, scale: number, editing: boolean) {
  const open = editing && Math.abs(scale - 1) < 0.05 && layoutHeight - height > 120;
  return {open, height: Math.round(height), top: Math.max(0, Math.round(top))};
}
export function followKeyboard(root: HTMLElement, view = window.visualViewport, onChange?: (state: {open: boolean; composing: boolean}) => void) {
  let frame = 0;
  let baseline = window.innerHeight;
  let wasOpen = false;
  let composing = false;
  let dismissed = false;
  const update = () => {
    frame = 0;
    const focused = document.activeElement;
    const editing = !!focused && root.contains(focused) && focused.matches("textarea,input,[contenteditable=true]");
    // Some Android browsers shrink both viewports. Retain the unobscured
    // height, but do not treat browser chrome or pinch zoom as a keyboard.
    if (!editing && !wasOpen) baseline = window.innerHeight;
    baseline = Math.max(baseline, window.innerHeight);
    const state = keyboardGeometry(baseline, view?.height ?? window.innerHeight, view?.offsetTop ?? 0, view?.scale ?? 1, editing || wasOpen);
    if (!wasOpen && state.open) dismissed = false;
    if (wasOpen && !state.open) dismissed = true;
    const composerFocused = editing && focused.matches(".composer textarea");
    composing = (composerFocused && !dismissed) || (composing && state.open);
    wasOpen = state.open;
    root.classList.toggle("keyboard-open", state.open);
    root.style.setProperty("--keyboard-height", `${state.height}px`);
    root.style.setProperty("--keyboard-top", `${state.top}px`);
    onChange?.({open: state.open, composing});
  };
  const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(update); };
  const focus = () => { dismissed = false; update(); };
  const orientation = () => { baseline = window.innerHeight; schedule(); };
  view?.addEventListener("resize", schedule);
  view?.addEventListener("scroll", schedule);
  window.addEventListener("resize", schedule);
  window.addEventListener("orientationchange", orientation);
  root.addEventListener("focusin", focus);
  root.addEventListener("focusout", schedule);
  update();
  return () => {
    cancelAnimationFrame(frame);
    view?.removeEventListener("resize", schedule);
    view?.removeEventListener("scroll", schedule);
    window.removeEventListener("resize", schedule);
    window.removeEventListener("orientationchange", orientation);
    root.removeEventListener("focusin", focus);
    root.removeEventListener("focusout", schedule);
    root.classList.remove("keyboard-open");
    root.style.removeProperty("--keyboard-height");
    root.style.removeProperty("--keyboard-top");
  };
}
