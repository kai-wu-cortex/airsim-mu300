// Framework-free unit harness for the LuCI view. No device/network writes.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {webcrypto} from 'node:crypto';
const source=readFileSync(new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/overview_panel.js',import.meta.url),'utf8');
const idle=()=>({running:true,service_alive:true,agent:{reachable:true},calls:{available:true,calls:[]}});
function setup(initial=idle()) {
 const elements=[],requests=[];let state=initial,poll,answer=async()=>({ok:true,call_id:'mu300-fixture'});
 function E(tag,attrs={},children=[]) {
  const node={tag,attrs,children:[],value:attrs.value??'',disabled:false,hidden:!!attrs.hidden,className:attrs.class??'',focus(){this.focused=true},setAttribute(k,v){this.attrs[k]=v},appendChild(c){this.children.push(c)},replaceChildren(...c){this.children=c;this.value=''}};
  let content='';Object.defineProperty(node,'textContent',{get(){return content||this.children.map(x=>typeof x==='string'?x:x.textContent).join('')},set(v){content=String(v);this.children=[]}});
  if(Array.isArray(children))node.children=children;else node.textContent=children;
  elements.push(node);return node;
 }
 const view=new Function('baseclass','rpc','poll','E','L','crypto','sms','ui',source)(
  {extend:x=>x},{declare:o=>(...args)=>o.method==='status'?Promise.resolve(state):(requests.push({method:o.method,args}),answer(...args))},
  {add:fn=>{poll=fn}},E,{resource:x=>x,url:x=>x},webcrypto,{render:()=>E('section',{'class':'as-card as-sms'})},{decorate:x=>x});
 const root=view.render(initial);
 const button=label=>elements.find(x=>x.tag==='button'&&x.textContent===label);
 const input=elements.find(x=>x.attrs.id==='as-dial-number'),select=elements.find(x=>x.attrs.id==='as-call-select');
 const buttons={dial:button('拨号'),answer:button('接听'),hangup:button('挂断')};
 return {root,elements,requests,input,select,buttons,setReply:fn=>{answer=fn},setState:s=>{state=s},poll:()=>poll(),type:s=>{input.value=s;input.attrs.input()},choose:s=>{select.value=s;select.attrs.change()}};
}
test('dial is below aligned top grid; idle controls and input survive polling',async()=>{
 const t=setup();const grid=t.elements.find(x=>x.className==='as-layout');
 assert.equal(grid.children[2].className,'as-card as-dial');
 assert.equal(t.buttons.dial.disabled,true);assert.equal(t.buttons.answer.disabled,true);
 t.type('15550001111');assert.equal(t.buttons.dial.disabled,false);
 await t.poll();assert.equal(t.input.value,'15550001111');
});
test('invalid numbers do not call backend',async()=>{
 const t=setup();t.type('123;reboot');await t.buttons.dial.attrs.click();
 assert.equal(t.requests.length,0);assert.equal(t.input.focused,true);
});
test('double click is blocked, success has operation ID and no automatic redial',async()=>{
 const t=setup();let finish;t.setReply(()=>new Promise(resolve=>{finish=resolve}));t.type('+86 15550001111');
 const first=t.buttons.dial.attrs.click();await t.buttons.dial.attrs.click();assert.equal(t.requests.length,1);
 assert.deepEqual(t.requests[0].args.slice(0,3),['dial','+8615550001111','']);assert.match(t.requests[0].args[3],/^[0-9a-f]{32}$/);
 finish({ok:true});await first;await t.poll();assert.equal(t.requests.length,1);
});
test('uncertain dial retains operation ID and cannot change destination',async()=>{
 const t=setup();t.setReply(async()=>{throw Error('timeout')});t.type('15550001111');await t.buttons.dial.attrs.click();
 const id=t.requests[0].args[3];await t.buttons.dial.attrs.click();assert.equal(t.requests[1].args[3],id);
 t.type('15550002222');await t.buttons.dial.attrs.click();assert.equal(t.requests.length,2);
});
test('call selection is explicit and never retargets a vanished call',async()=>{
 const a={id:'mu300-one',state:'active'},b={id:'mu300-two',state:'waiting'};
 const t=setup({...idle(),calls:{available:true,calls:[a,b]}});
 assert.equal(t.buttons.dial.disabled,true);assert.equal(t.buttons.answer.disabled,true);
 t.choose(b.id);await t.buttons.answer.attrs.click();assert.deepEqual(t.requests[0].args.slice(0,3),['answer','',b.id]);
 t.setState({...idle(),calls:{available:true,calls:[a]}});await t.poll();
 assert.equal(t.select.value,'');assert.equal(t.buttons.hangup.disabled,true);
 await t.buttons.hangup.attrs.click();assert.equal(t.requests.length,1);
 t.choose(a.id);await t.buttons.hangup.attrs.click();assert.equal(t.requests[1].args[2],a.id);
});
test('unavailable call status disables all call controls',()=>{
 const t=setup({...idle(),calls:{available:false,error:'offline',calls:[]}});t.type('12345');
 for(const button of Object.values(t.buttons))assert.equal(button.disabled,true);
});
test('incomplete audio installation is visible and cannot start calls or pairing',()=>{
 for(const audio_setup of ['reboot_required','failed','not_prepared','preparing']) {
  const t=setup({...idle(),audio_available:true,audio_setup});t.type('12345');
  assert.equal(t.buttons.dial.disabled,true);
  assert.equal(t.elements.find(x=>x.tag==='button'&&x.textContent==='启动服务').disabled,true);
  assert.equal(t.elements.find(x=>x.tag==='button'&&x.textContent==='获取配对码').disabled,true);
  assert.match(t.root.textContent,audio_setup==='reboot_required'?/重启进入 OpenWrt/:/准备/);
  assert.equal(t.requests.length,0);
 }
});

test('operator and signal source are visible and failed readings clear old values',async()=>{
 const t=setup({...idle(),sim:{operator:'中国联通',signal_dbm:-96,signal_source:'vendor_rsrp'}});
 assert.match(t.root.textContent,/中国联通/);assert.match(t.root.textContent,/-96 dBm · RSRP/);
 t.setState({...idle(),sim:{signal_dbm:null}});await t.poll();
 assert.doesNotMatch(t.root.textContent,/中国联通|-96 dBm/);assert.match(t.root.textContent,/基带暂未提供/);
});

test('pairing code uses device remaining lifetime despite browser clock skew',async()=>{
 for(const skew of [-7200000,7200000]){
  const t=setup({...idle(),pairing:{code:'012345',expires_at:Date.now()+skew,remaining_ms:90000}});
  assert.ok(t.elements.some(n=>n.className==='as-code is-active'&&n.textContent==='012345'));
  assert.match(t.root.textContent,/90 秒后失效/);
  t.setState({...idle(),pairing:null});await t.poll();
  assert.ok(t.elements.some(n=>n.className==='as-code'&&n.textContent==='— — — — — —'));
 }
});
test('invalid or expired relative TTL cannot revive a code; old server remains compatible',()=>{
 for(const remaining_ms of [0,-1,'120000',null,NaN]){
  const t=setup({...idle(),pairing:{code:'012345',expires_at:Date.now()+120000,remaining_ms}});
  assert.ok(t.elements.some(n=>n.className==='as-code'&&n.textContent==='— — — — — —'));
 }
 const t=setup({...idle(),pairing:{code:'012345',expires_at:Date.now()+90000}});
 assert.ok(t.elements.some(n=>n.className==='as-code is-active'&&n.textContent==='012345'));
});
