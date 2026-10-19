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
