import {test} from "node:test";
import assert from "node:assert/strict";
import {readLoop} from "./read-loop.ts";
const flush = () => new Promise<void>(resolve => setImmediate(resolve));

test("events coalesce, hidden pages abort, and idle polling waits thirty seconds", async context => {
  context.mock.timers.enable({apis:["setTimeout","Date"],now:10000});
  let visible=true, calls=0;
  let signal: AbortSignal | undefined;
  let finish!:()=>void;
  const loop=readLoop(s=>{ calls++;signal=s;return new Promise<void>(resolve=>{finish=resolve;}); },()=>visible,()=>30000);
  loop.trigger(); context.mock.timers.tick(0);
  assert.equal(calls,1);
  for(let i=0;i<20;i++) loop.trigger();
  finish();await flush();
  context.mock.timers.tick(799); assert.equal(calls,1);
  context.mock.timers.tick(1); assert.equal(calls,2);
  visible=false;loop.pause();assert.equal(signal?.aborted,true);
  finish();await flush();
  context.mock.timers.tick(60000);assert.equal(calls,2);
  visible=true;loop.trigger();context.mock.timers.tick(0);assert.equal(calls,3);
  finish();await flush();
  context.mock.timers.tick(29999);assert.equal(calls,3);
  context.mock.timers.tick(1);assert.equal(calls,4);
  loop.stop();assert.equal(signal?.aborted,true);
  finish();await flush();context.mock.timers.tick(60000);assert.equal(calls,4);
  context.mock.timers.reset();
});
