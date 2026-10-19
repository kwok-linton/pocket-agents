import {test} from "node:test";
import assert from "node:assert/strict";
import {loadView} from "./view-data.ts";
import {forgetValidated} from "./api.ts";

test("opening a conversation does not download the full session/device lists",async()=>{
  forgetValidated();
  const old=globalThis.fetch, calls:string[]=[],values:any[]=[];
  globalThis.fetch=(async(path:string)=>{calls.push(path);return new Response(JSON.stringify({session:{id:"chosen"},device:{id:"node"}}));}) as typeof fetch;
  try {
    await loadView("/session/chosen",new AbortController().signal,v=>values.push(v));
    assert.deepEqual(calls,["/api/sessions/chosen"]);
    assert.equal(values[0].session.id,"chosen");
  } finally {forgetValidated();globalThis.fetch=old;}
});

test("overview sessions render without waiting for a slower device request",async()=>{
  forgetValidated();
  const old=globalThis.fetch,values:any[]=[];
  let devices!:(v:Response)=>void;
  globalThis.fetch=(async(path:string)=>path==="/api/devices"?new Promise<Response>(resolve=>{devices=resolve;}):new Response(path==="/api/sessions"?'[{"id":"ready"}]':'{"workspaceScope":"all","workspaces":[]}')) as typeof fetch;
  try {
    const read=loadView("/",new AbortController().signal,v=>values.push(v));
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(values.find(v=>v.sessions)?.sessions[0].id,"ready");
    devices(new Response('[]'));await read;
  } finally {forgetValidated();globalThis.fetch=old;}
});
