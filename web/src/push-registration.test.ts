import { test } from "node:test";
import assert from "node:assert/strict";
import { createPushRegistration, type PushInfo } from "./push-registration.ts";
import type { PushEnvironment } from "./push-state.ts";

test("granted phone repairs a lost server registration, coalesces callers and keeps its subscription", async () => {
  let saved = false, writes = 0, subscriptions = 0;
  const sub = {endpoint: "https://web.push.apple.com/fixture"} as PushSubscription;
  const env: PushEnvironment = {secure:true,ios:true,standalone:true,supported:true,permission:"granted"};
  const sync = createPushRegistration({
    environment: () => env,
    info: async () => ({enabled:true, publicKey:"AA", activeSubscriptionIds:saved?["fixture"]:[],tests:[]} as PushInfo),
    identify: async s => s ? "fixture" : "",
    worker: async () => ({pushManager:{getSubscription:async()=>sub,subscribe:async()=>{subscriptions++;return sub;}}} as unknown as ServiceWorkerRegistration),
    register: async () => {writes++;saved=true;},
  });
  const [a,b] = await Promise.all([sync(),sync()]);
  assert.equal(a,b); assert.equal(writes,1); assert.equal(subscriptions,0);
  await sync(); assert.equal(writes,1);
  env.permission = "denied"; saved=false;
  await sync(); assert.equal(writes,1); assert.equal(subscriptions,0);
});

test("failed registration can recover later, without asking again for system permission", async () => {
  let attempt=0, saved=false;
  const sub={} as PushSubscription;
  const sync=createPushRegistration({
    environment:()=>({secure:true,ios:true,standalone:true,supported:true,permission:"granted"}),
    info:async()=>({enabled:true,publicKey:"AA",activeSubscriptionIds:saved?["id"]:[],tests:[]}),
    identify:async()=>"id",
    worker:async()=>({pushManager:{getSubscription:async()=>sub}} as unknown as ServiceWorkerRegistration),
    register:async()=>{if(++attempt===1)throw Error("offline");saved=true;},
  });
  await assert.rejects(sync());
  assert.equal((await sync()).subID,"id"); assert.equal(attempt,2);
});
