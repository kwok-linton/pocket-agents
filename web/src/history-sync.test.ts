import {test} from "node:test";
import assert from "node:assert/strict";
import {affectsSession,applyHistoryDelta} from "./history-sync.ts";
import type {History, Event as AgentEvent} from "./types.ts";

test("delta order removes deleted messages and never appends a mismatched base",()=>{
  const previous={messages:[{id:"a",text:"kept"},{id:"b",text:"removed"},{id:"c",text:"old"}],hasMore:true} as History;
  const delta={base:"old",order:["a","c","d"],messages:[{id:"c",text:"new"},{id:"d",text:"appended"}],hasMore:false};
  const next=applyHistoryDelta(previous,"old",delta)!;
  assert.deepEqual(next.messages.map(m=>m.id),["a","c","d"]);
  assert.equal(next.messages[0],previous.messages[0]);
  assert.equal(previous.messages[2].text,"old");
  assert.equal(applyHistoryDelta(previous,"different",delta),undefined);
  assert.equal(applyHistoryDelta(previous,"old",{...delta,order:["missing"]}),undefined);
});
test("another session's snapshot does not trigger history rereads",()=>{
  const events=[{kind:"snapshot",nodeId:"n",data:{sessionIds:["other"]}}] as AgentEvent[];
  assert.equal(affectsSession(events,"current","n"),false);
  assert.equal(affectsSession(events,"other","n"),true);
  assert.equal(affectsSession(events,"other","other-node"),false);
  assert.equal(affectsSession([{kind:"snapshot",nodeId:"n"}] as AgentEvent[],"current","n"),true);
});

import {hasVisibleMessages, withVisibleHistory} from "./history-sync.ts";
const activity = (id: string) => ({id, role:"tool", kind:"tool_result", text:"done", at:""} as History["messages"][number]);
test("tool-only latest pages fetch visible context without losing newer activities",async()=>{
  const latest: History={messages:[activity("new")],cursor:"older",hasMore:true};
  const result=await withVisibleHistory(latest,async cursor=>{
    assert.equal(cursor,"older");
    return {messages:[{id:"question",role:"user",kind:"message",text:"Please check",at:""},activity("new")],hasMore:false};
  });
  assert.equal(hasVisibleMessages(result.messages),true);
  assert.deepEqual(result.messages.map(m=>m.id),["question","new"]);
  assert.equal(result.messages[1],latest.messages[0]);
  assert.equal(latest.messages.length,1);
});
test("visible history avoids extra requests; hidden scheduled replies still need context",async()=>{
  const latest: History={messages:[{id:"user",role:"user",kind:"message",text:"hello",at:""}],hasMore:true,cursor:"old"};
  assert.equal(await withVisibleHistory(latest,async()=>{throw Error("unexpected read")}),latest);
  assert.equal(hasVisibleMessages([{id:"timer",role:"system",kind:"scheduled_trigger",text:"trigger",at:""},{id:"reply",role:"assistant",kind:"message",text:"scheduled reply",at:""}]),false);
});
test("empty-history context loading is bounded and handles cursor loops and failures",async()=>{
  const latest: History={messages:[activity("new")],hasMore:true,cursor:"old"};
  let calls=0;
  await withVisibleHistory(latest,async()=>{calls++;return latest;});
  assert.equal(calls,1);
  calls=0;
  const result=await withVisibleHistory(latest,async()=>({messages:[activity(String(++calls))],hasMore:true,cursor:String(calls)}),3);
  assert.equal(calls,3);assert.equal(result.hasMore,true);
  assert.equal(await withVisibleHistory(latest,async()=>{throw Error("offline")}),latest);
  await assert.rejects(withVisibleHistory(latest,async()=>{throw new DOMException("cancelled","AbortError");}),{name:"AbortError"});
});
