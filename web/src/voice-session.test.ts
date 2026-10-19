import test from "node:test";
import assert from "node:assert/strict";
import { voiceSession, type Recognition } from "./voice-session.ts";
function fixture() {
 const tasks = new Map<number, { fn: () => void; ms: number }>(); let id = 0;
 const engines: (Recognition & { stops: number; aborted: boolean })[] = [];
 const texts: string[] = [], states: boolean[] = [], notices: string[] = [];
 const session = voiceSession(() => {
  const r = { lang: "", continuous: false, interimResults: false, onresult: null, onerror: null, onend: null,
   stops: 0, aborted: false, start() {}, stop() { this.stops++; }, abort() { this.aborted = true; } } as Recognition & { stops: number; aborted: boolean };
  engines.push(r); return r;
 }, "zh-CN", { text: t => texts.push(t), active: s => states.push(s), notice: n => notices.push(n), interim() {} },
 (fn, ms) => { const key = ++id; tasks.set(key, { fn, ms }); return () => { tasks.delete(key); }; });
 const tick = (ms: number) => { for (const [key, task] of [...tasks]) if (task.ms === ms) { tasks.delete(key); task.fn(); } };
 return { session, engines, texts, states, notices, tasks, tick };
}
const final = (text: string) => ({ resultIndex: 0, results: [{ isFinal: true, 0: { transcript: text } }] });
test("continuous recognition survives normal Android end and resets result indexes", () => {
 const f=fixture(); f.session.start(); const first=f.engines[0]; assert.equal(first.continuous,true);
 first.onresult?.(final("第一句")); first.onend?.(); assert.deepEqual(f.states,[true]);
 f.tick(300); assert.equal(f.engines.length,2); f.engines[1].onresult?.(final("第二句"));
 assert.deepEqual(f.texts,[]); f.session.stop(); f.engines[1].onend?.();
 assert.deepEqual(f.texts,["第一句\n第二句"]);
});
test("silence retries but permission/network errors do not", () => {
 const f=fixture(); f.session.start(); f.engines[0].onerror?.({error:"no-speech"}); f.engines[0].onend?.(); f.tick(300);
 assert.equal(f.engines.length,2); f.engines[1].onerror?.({error:"network"}); f.tick(300);
 assert.equal(f.engines.length,2); assert.equal(f.states.at(-1),false); assert.equal(f.tasks.size,0);
});
test("manual stop during restart gap never restarts microphone", () => {
 const f=fixture(); f.session.start(); f.engines[0].onend?.(); f.session.stop(); f.tick(300);
 assert.equal(f.engines.length,1); assert.equal(f.tasks.size,0); assert.equal(f.states.at(-1),false);
});
test("stop retains final words but ends without restarting; dispose ignores late events", () => {
 const f=fixture(); f.session.start(); const r=f.engines[0]; f.session.stop();
 r.onresult?.(final("最后一句")); r.onend?.(); f.tick(300); assert.deepEqual(f.texts,["最后一句"]); assert.equal(f.tasks.size,0);
 f.session.start(); const old=f.engines[1].onresult; f.session.dispose(); old?.(final("不应加入")); assert.equal(f.texts.length,1);
});
test("five minute limit and missing end callback release recording state", () => {
 const f=fixture(); f.session.start(); f.tick(300000); assert.equal(f.engines[0].stops,1);
 f.tick(3000); assert.equal(f.engines[0].aborted,true); assert.equal(f.states.at(-1),false); assert.equal(f.tasks.size,0);
});
test("repeated empty ends eventually stop instead of spinning forever", () => {
 const f=fixture(); f.session.start(); for(let i=0;i<5;i++) { f.engines.at(-1)?.onend?.(); f.tick(300); }
 assert.equal(f.engines.length,5); assert.equal(f.states.at(-1),false); assert.equal(f.tasks.size,0);
});

test("progressive cumulative mobile finals are replaced, not appended to draft", () => {
 const f=fixture(); f.session.start(); const r=f.engines[0];
 r.onresult?.({resultIndex:0,results:[{isFinal:true,0:{transcript:"语音交互"}}]});
 r.onresult?.({resultIndex:1,results:[{isFinal:true,0:{transcript:"语音交互"}},{isFinal:true,0:{transcript:"语音交互有点奇怪"}}]});
 r.onresult?.({resultIndex:2,results:[{isFinal:true,0:{transcript:"语音交互"}},{isFinal:true,0:{transcript:"语音交互有点奇怪"}},{isFinal:true,0:{transcript:"语音交互有点奇怪"}}]});
 assert.deepEqual(f.texts,[]); f.session.stop(); r.onend?.();
 assert.deepEqual(f.texts,["语音交互有点奇怪"]); r.onend?.(); assert.equal(f.texts.length,1);
});
test("cancel discards this recording and does not commit to existing draft", () => {
 const f=fixture(); f.session.start(); f.engines[0].onresult?.(final("不要提交"));
 f.session.cancel(); f.session.stop(); f.tick(300); assert.deepEqual(f.texts,[]); assert.equal(f.tasks.size,0);
});
test("legitimate repetition after a new recognition cycle is preserved", () => {
 const f=fixture(); f.session.start(); f.engines[0].onresult?.(final("你好")); f.engines[0].onend?.(); f.tick(300);
 f.engines[1].onresult?.(final("你好")); f.session.stop(); f.engines[1].onend?.(); assert.deepEqual(f.texts,["你好\n你好"]);
});

test("stop without final callback retains the visible long hypothesis", () => {
 const f = fixture(); f.session.start();
 f.engines[0].onresult?.({resultIndex: 0, results: [
  {isFinal: true, 0: {transcript: "第一件事"}},
  {isFinal: false, 0: {transcript: "第二件事还有很多内容"}},
 ]});
 f.session.stop(); f.tick(3000);
 assert.deepEqual(f.texts, ["第一件事\n第二件事还有很多内容"]);
 assert.ok(f.notices.some(n => n.includes("尚未定稿")));
});
test("natural end preserves interim across restart; final correction replaces preview", () => {
 const f = fixture(); f.session.start();
 f.engines[0].onresult?.({resultIndex: 0, results: [{isFinal: false, 0: {transcript: "前半段"}}]});
 f.engines[0].onend?.(); f.tick(300);
 f.engines[1].onresult?.({resultIndex: 0, results: [{isFinal: false, 0: {transcript: "后半断"}}]});
 f.engines[1].onresult?.(final("后半段"));
 f.session.stop(); f.engines[1].onend?.();
 assert.deepEqual(f.texts, ["前半段\n后半段"]);
});
