import { test } from "node:test";
import assert from "node:assert/strict";
import { followKeyboard, keyboardGeometry } from "./keyboard-viewport.ts";

test("iPhone keyboard uses visible height without mistaking browser chrome or zoom for a keyboard", () => {
  assert.deepEqual(keyboardGeometry(932, 520, 42, 1, true), {open:true,height:520,top:42});
  assert.equal(keyboardGeometry(932, 880, 0, 1, true).open, false);
  assert.equal(keyboardGeometry(932, 520, 0, 2, true).open, false);
  assert.equal(keyboardGeometry(932, 520, 0, 1, false).open, false);
  assert.equal(keyboardGeometry(932, 932, 0, 1, true).open, false);
});

// Exercise focus and viewport events together: blur can precede the iOS
// keyboard animation, and Android can resize window.innerHeight as well.
test("composer intent starts before the keyboard and survives blur until dismissal", () => {
  const originals = Object.fromEntries(["window", "document", "requestAnimationFrame", "cancelAnimationFrame"].map(k => [k, Object.getOwnPropertyDescriptor(globalThis, k)]));
  const view = Object.assign(new EventTarget(), {height: 844, offsetTop: 0, scale: 1});
  const win = Object.assign(new EventTarget(), {innerHeight: 844, visualViewport: view});
  const editor = {matches: (selector: string) => selector.includes("textarea")};
  const doc = {activeElement: null as null | typeof editor};
  const classes = new Set<string>();
  const root = Object.assign(new EventTarget(), {
    contains: (el: unknown) => el === editor,
    classList: {toggle: (key: string, enabled: boolean) => enabled ? classes.add(key) : classes.delete(key), remove: (key: string) => classes.delete(key)},
    style: {setProperty() {}, removeProperty() {}},
  });
  const frames = new Map<number, FrameRequestCallback>();
  let id = 0;
  const flush = () => { for (const [key, fn] of frames) { frames.delete(key); fn(0); } };
  let state = {open: false, composing: false};
  let stop = () => {};
  try {
    Object.defineProperty(globalThis, "window", {configurable: true, value: win});
    Object.defineProperty(globalThis, "document", {configurable: true, value: doc});
    Object.defineProperty(globalThis, "requestAnimationFrame", {configurable: true, value: (fn: FrameRequestCallback) => {frames.set(++id, fn); return id;}});
    Object.defineProperty(globalThis, "cancelAnimationFrame", {configurable: true, value: (key: number) => frames.delete(key)});
    stop = followKeyboard(root as unknown as HTMLElement, view as unknown as VisualViewport, next => { state = next; });
    doc.activeElement = editor; root.dispatchEvent(new Event("focusin"));
    assert.deepEqual(state, {open: false, composing: true});
    // Android resizes both viewports; the baseline must survive.
    win.innerHeight = view.height = 470; view.dispatchEvent(new Event("resize")); flush();
    assert.deepEqual(state, {open: true, composing: true});
    doc.activeElement = null; root.dispatchEvent(new Event("focusout")); flush();
    assert.deepEqual(state, {open: true, composing: true});
    win.innerHeight = view.height = 844; view.dispatchEvent(new Event("resize")); flush();
    assert.deepEqual(state, {open: false, composing: false});
    // Android's Back can hide the keyboard without blurring the editor.
    doc.activeElement = editor; root.dispatchEvent(new Event("focusin"));
    view.height = 470; view.dispatchEvent(new Event("resize")); flush();
    view.height = 844; view.dispatchEvent(new Event("resize")); flush();
    assert.deepEqual(state, {open: false, composing: false});
    view.dispatchEvent(new Event("scroll")); flush();
    assert.equal(state.composing, false);
    view.height = 470; view.dispatchEvent(new Event("resize")); flush();
    assert.deepEqual(state, {open: true, composing: true});
    stop();
    assert.equal(classes.has("keyboard-open"), false);
    view.height = 844; view.dispatchEvent(new Event("resize")); flush();
    assert.equal(state.open, true, "cleanup removed viewport listeners");
  } finally {
    stop();
    for (const [key, descriptor] of Object.entries(originals)) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  }
});
