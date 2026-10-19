import { test } from "node:test";
import assert from "node:assert/strict";
import { restoreDraft, settleDraft } from "./send-draft.ts";
const pending = {actionId:"sent-a",text:"original",files:["/workspace/image.png"]};
test("late accepted receipt clears original text and attachment after restoring saved pending draft", () => {
 const restored=JSON.parse(JSON.stringify(pending));
 assert.deepEqual(settleDraft(restored,{actionId:"sent-a",status:"accepted"},"original",pending.files),{text:"",files:[]});
});
test("late receipt preserves a subsequently edited draft and newly added attachments", () => {
 assert.deepEqual(settleDraft(pending,{actionId:"sent-a",status:"accepted"},"next message",[...pending.files,"/workspace/next.txt"]),{text:"next message",files:["/workspace/next.txt"]});
});
test("unconfirmed, rejected, and unrelated actions never clear composer content", () => {
 for (const status of ["unknown","submitted","rejected"]) assert.equal(settleDraft(pending,{actionId:"sent-a",status},"original",pending.files),null);
 assert.equal(settleDraft(pending,{actionId:"another",status:"accepted"},"original",pending.files),null);
});

test("receipt for detached submission never erases a new identical message or reused attachment", () => {
 assert.deepEqual(settleDraft({...pending,detached:true},{actionId:"sent-a",status:"accepted"},pending.text,pending.files),{text:pending.text,files:pending.files});
});

test("failed send recovery preserves newer text and merges attachments without duplicates", () => {
 assert.deepEqual(restoreDraft(pending,"next",[...pending.files,"new.txt"]),{text:"next\n\noriginal",files:[...pending.files,"new.txt"]});
 assert.deepEqual(restoreDraft(pending,"",[]),{text:pending.text,files:pending.files});
});
