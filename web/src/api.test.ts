import { test } from "node:test";
import assert from "node:assert/strict";
import { api, forgetValidated } from "./api.ts";

test("unchanged reads reuse the same object; writes and 401 never use the tag", async () => {
  const sent: (string | null)[] = [];
  let status = 200, body = '[{"id":"a"}]';
  (globalThis as any).window = { dispatchEvent() {} };
  globalThis.fetch = (async (_path: string, init: RequestInit) => {
    sent.push(new Headers(init.headers).get("If-None-Match"));
    return new Response(status === 304 ? null : body, { status, headers: { ETag: '"v1"' } });
  }) as typeof fetch;
  const first = await api<{ id: string }[]>("/api/sessions");
  status = 304;
  const second = await api<{ id: string }[]>("/api/sessions");
  assert.equal(second, first, "304 must return the identical object so React skips the render");
  await api("/api/actions", { method: "POST", body: "{}" }).catch(() => {});
  status = 200; body = '[{"id":"b"}]';
  const third = await api<{ id: string }[]>("/api/sessions");
  assert.equal(third[0].id, "b");
  forgetValidated();
  await api("/api/sessions");
  assert.deepEqual(sent, [null, '"v1"', null, '"v1"', null]);
});

test("stalled reads abort and release callers; writes have no automatic deadline", async (context) => {
 context.mock.timers.enable({apis:["setTimeout"]});
 const previous=globalThis.fetch;
 let readSignal: AbortSignal | null | undefined;
 globalThis.fetch=(async (_path: string,init: RequestInit)=>{
   readSignal=init.signal;
   return new Promise((_resolve,reject)=>init.signal?.addEventListener("abort",()=>reject(new DOMException("Aborted","AbortError")),{once:true}));
 }) as typeof fetch;
 try {
   const pending=api("/api/bootstrap");
   const rejected=assert.rejects(pending,{name:"AbortError"});
   context.mock.timers.tick(12000);
   await rejected;assert.equal(readSignal?.aborted,true);
   globalThis.fetch=(async (_path:string,init:RequestInit)=>{
     assert.equal(init.signal,undefined);
     return new Response('{}');
   }) as typeof fetch;
   await api("/api/actions",{method:"POST",body:"{}"});
 } finally {globalThis.fetch=previous;context.mock.timers.reset();}
});

test("previous history is available in memory but cannot survive logout or authorization failure", async () => {
 const {peekValidated}=await import("./api.ts");
 const previous=globalThis.fetch;
 const path="/api/sessions/cache-test/history?limit=20";
 try {
  forgetValidated();assert.equal(peekValidated(path),undefined);
  globalThis.fetch=(async()=>new Response('{"messages":[{"id":"native-message"}]}',{headers:{ETag:'"history-v1"'}})) as typeof fetch;
  const history=await api(path);assert.equal(peekValidated(path),history);
  forgetValidated();assert.equal(peekValidated(path),undefined);
  await api(path);
  globalThis.fetch=(async()=>new Response('{}',{status:401})) as typeof fetch;
  await assert.rejects(api("/api/bootstrap"));
  assert.equal(peekValidated(path),undefined);
 } finally {forgetValidated();globalThis.fetch=previous;}
});

test("concurrent readers share one download and cancel independently", async () => {
  forgetValidated();
  const previous = globalThis.fetch;
  let calls = 0, signal: AbortSignal | null | undefined;
  let complete!: (value: Response) => void;
  globalThis.fetch = (async (_path: string, options: RequestInit) => {
    calls++; signal = options.signal;
    return new Promise<Response>((resolve, reject) => {
      complete = resolve;
      signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), {once:true});
    });
  }) as typeof fetch;
  try {
    const first = new AbortController(), second = new AbortController();
    const a = api("/api/shared", {signal:first.signal});
    const b = api("/api/shared", {signal:second.signal});
    const rejected = assert.rejects(a, {name:"AbortError"});
    first.abort(); await rejected;
    assert.equal(calls,1); assert.equal(signal?.aborted,false);
    complete(new Response('{"value":1}', {headers:{ETag:'"one"'}}));
    assert.deepEqual(await b,{value:1});
    const last = new AbortController();
    const c = api("/api/cancel-last", {signal:last.signal});
    const canceled = assert.rejects(c,{name:"AbortError"});
    last.abort(); await canceled;
    assert.equal(signal?.aborted,true);
  } finally { forgetValidated(); globalThis.fetch = previous; }
});

test("logout prevents a late response from repopulating native history", async () => {
  const {peekValidated} = await import("./api.ts");
  forgetValidated();
  const previous=globalThis.fetch;
  let complete!: (value:Response)=>void;
  globalThis.fetch=(()=>new Promise<Response>(resolve=>{complete=resolve;})) as typeof fetch;
  try {
    const pending=api("/api/late-history");
    const rejected=assert.rejects(pending,{name:"AbortError"});
    forgetValidated();
    complete(new Response('{"messages":[]}',{headers:{ETag:'"late"'}}));
    await rejected;
    assert.equal(peekValidated("/api/late-history"),undefined);
  } finally {forgetValidated();globalThis.fetch=previous;}
});

test("history delta updates changed messages and repairs a missing base with one full read", async () => {
  forgetValidated();
  const previous=globalThis.fetch;
  const headers: (string|null)[]=[];
  let turn=0;
  globalThis.fetch=(async (_path:string,options:RequestInit)=>{
    headers.push(new Headers(options.headers).get("If-None-Match"));
    const bodies=[
      {messages:[{id:"a",text:"unchanged"},{id:"b",text:"old"}],hasMore:false},
      {base:'"v1"',order:["a","b","c"],messages:[{id:"b",text:"updated"},{id:"c",text:"new"}],hasMore:false},
      {base:'"wrong"',order:["unknown"],messages:[],hasMore:false},
      {messages:[{id:"d",text:"compacted"}],hasMore:false},
    ];
    const at=turn++;
    return new Response(JSON.stringify(bodies[at]),{headers:{ETag:`"v${at+1}"`,...([1,2].includes(at)?{"X-Pocket-History-Delta":"1"}:{})}});
  }) as typeof fetch;
  try {
    const first=await api<any>("/api/delta");
    const next=await api<any>("/api/delta");
    assert.equal(next.messages[0],first.messages[0]);
    assert.deepEqual(next.messages.map((m:any)=>m.text),["unchanged","updated","new"]);
    const repaired=await api<any>("/api/delta");
    assert.deepEqual(repaired.messages.map((m:any)=>m.id),["d"]);
    assert.deepEqual(headers,[null,'"v1"','"v2"',null]);
  } finally {forgetValidated();globalThis.fetch=previous;}
});
