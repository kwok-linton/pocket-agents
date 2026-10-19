import {test} from 'node:test';
import assert from 'node:assert/strict';
import {apiFailure,latestApiFailure} from './native-failure.ts';
const m=(role:string,text:string)=>({id:'fixture',at:'',role,text});
test('API failures are distinguished from questions and quoted errors',()=>{
 assert.equal(apiFailure('API Error: 524 {"status":524}')?.code,524);
 assert.equal(apiFailure('Please explain API Error: 524'),null);
 assert.equal(apiFailure('May I run this command?'),null);
 assert.equal(latestApiFailure([m('assistant','API Error: 524'),m('user','continue')]),null);
 assert.equal(latestApiFailure([m('assistant','API Error: 524'),m('assistant','Recovered')]),null);
 assert.equal(latestApiFailure([m('user','task'),m('assistant','API Error: 524')])?.code,524);
});
