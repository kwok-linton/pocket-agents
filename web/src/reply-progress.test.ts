import test from 'node:test';
import assert from 'node:assert/strict';
import {replyProgress} from './reply-progress.ts';
import type {TrackedAction} from './types.ts';
const at = '2026-10-17T10:00:00Z';
const action: TrackedAction = {at, action: {id:'test',kind:'send',nodeId:'n',provider:'codex',profile:'default'}, result:{actionId:'test',status:'accepted',data:{delivery:'native_queue'}}};
const base = {online:true,sending:false,working:false,attention:false,action,messages:[],now:Date.parse(at)+1000};
test('queue receipt never claims this request is processing', () => {
 assert.equal(replyProgress({...base,working:true})?.label,'已排队，等待原生会话处理');
 assert.equal(replyProgress({...base,sending:true})?.label,'正在发送');
});
test('visible reply clears old queue; internal tool output does not', () => {
 assert.equal(replyProgress({...base,messages:[{id:'a',role:'assistant',text:'done',at}] }),null);
 assert.notEqual(replyProgress({...base,messages:[{id:'a',role:'tool',text:'done',at}]}),null);
});
test('stale queue stops animating; offline has no false activity', () => {
 assert.equal(replyProgress({...base,now:base.now+90000})?.animated,false);
 assert.equal(replyProgress({...base,online:false,working:true}),null);
 assert.equal(replyProgress({...base,attention:true})?.animated,false);
});
test('background task is a static status and real activity takes priority', () => {
 const input = {...base,action:undefined,background:true};
 assert.deepEqual(replyProgress(input), {label:'本轮已结束，后台任务仍在运行',animated:false});
 assert.equal(replyProgress({...input,sending:true})?.label,'正在发送');
 assert.equal(replyProgress({...input,attention:true})?.label,'等待你回答或审批');
 assert.equal(replyProgress({...input,working:true})?.label,'正在处理');
 assert.equal(replyProgress({...input,online:false}),null);
 assert.equal(replyProgress({...input,background:false}),null);
});
test('background status cannot hide an unfulfilled send or queue receipt', () => {
 assert.equal(replyProgress({...base,background:true})?.label,'已排队，等待原生会话处理');
 const sent = {...action,result:{...action.result,data:undefined}};
 assert.equal(replyProgress({...base,action:sent,background:true})?.label,'已送达，等待回复');
 assert.equal(replyProgress({...base,background:true,messages:[{id:'a',role:'assistant',text:'done',at}]})?.label,'本轮已结束，后台任务仍在运行');
});
