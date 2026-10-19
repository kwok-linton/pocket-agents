import { test } from "node:test";
import assert from "node:assert/strict";
import { pushStage, withTimeout, type PushEnvironment } from "./push-state.ts";
const ready: PushEnvironment = {secure:true, ios:true, standalone:true, supported:true, permission:"granted"};
test("system denial overrides a retained registered subscription", () => {
 assert.equal(pushStage({...ready,permission:"denied"},true,true,true),"denied");
 assert.equal(pushStage({...ready,permission:"default"},true,true,true),"permission");
 assert.equal(pushStage(ready,true,true,true),"ready");
});
test("iPhone requires Home Screen installation before permission and registration", () => {
 assert.equal(pushStage({...ready,standalone:false,supported:false},true,false,false),"install");
 assert.equal(pushStage({...ready,secure:false},false,false,false),"https");
 assert.equal(pushStage({...ready,supported:false},true,false,false),"unsupported");
 assert.equal(pushStage(ready,true,true,false),"register");
 assert.equal(pushStage(ready,true,false,true),"register");
 assert.equal(pushStage({...ready,ios:false,standalone:false},true,true,true),"ready");
});
test("unavailable service workers time out instead of leaving setup loading forever", async () => {
 await assert.rejects(withTimeout(new Promise(()=>{}),10,"not ready"),/not ready/);
 assert.equal(await withTimeout(Promise.resolve(42),100,"timeout"),42);
 await assert.rejects(withTimeout(Promise.reject(new Error("failed")),100,"timeout"),/failed/);
});

test("a permission tap during a background check is retained and requests permission immediately", async () => {
 const { allowPush } = await import("./push-permission.ts");
 let release!: () => void;
 const pending = new Promise<void>(resolve => {release=resolve;});
 const calls: string[] = [];
 const done = allowPush(() => {calls.push("permission");return Promise.resolve("granted");},pending,async()=>{calls.push("register");},()=>{calls.push("state");});
 assert.deepEqual(calls,["permission"]);
 await Promise.resolve();
 assert.deepEqual(calls,["permission","state"]);
 release();await done;
 assert.deepEqual(calls,["permission","state","register"]);
});
test("denied permission never registers; failed background checks do not discard an allowed tap", async () => {
 const { allowPush } = await import("./push-permission.ts");
 let registered=0;
 await allowPush(async()=>"denied",null,async()=>{registered++;},()=>{});
 assert.equal(registered,0);
 await allowPush(async()=>"granted",Promise.reject(new Error("network")),async()=>{registered++;},()=>{});
 assert.equal(registered,1);
});
