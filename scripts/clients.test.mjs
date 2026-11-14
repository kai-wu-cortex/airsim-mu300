import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/clients.js',import.meta.url),'utf8');
const one='a'.repeat(64),two='b'.repeat(64);
const snapshot=()=>({ok:true,clients:[{id:one,last_used:1700000000,recent:true},{id:two,recent:false}]});
function setup(initial=snapshot()) {
 let state=initial,reply=async id=>({ok:true,id}),poll;
 const requests=[],elements=[];
 function E(tag,attrs={},children=[]) {
  const n={tag,attrs,children:[],disabled:false,hidden:!!attrs.hidden,appendChild(x){this.children.push(x);},replaceChildren(...x){this.children=x;}};
  let content='';Object.defineProperty(n,'textContent',{get(){return content||this.children.map(x=>typeof x==='string'?x:x.textContent).join('');},set(v){content=String(v);this.children=[];}});
  if(Array.isArray(children))n.children=children;else n.textContent=children;
  elements.push(n);return n;
 }
 const view=new Function('view','rpc','poll','E','L','ui',source)({extend:x=>x},{declare:o=>(...args)=>{requests.push({method:o.method,args});return o.method==='pair_clients'?(state instanceof Error?Promise.reject(state):Promise.resolve(state)):reply(...args);}},{add:fn=>{poll=fn;}},E,{resource:x=>x},{decorate:x=>x});
 const root=view.render(initial),buttons=()=>{const all=[];function walk(n){if(typeof n==='string')return;if(n.tag==='button')all.push(n);n.children.forEach(walk);}walk(root);return all;};
 return {root,requests,button:label=>buttons().find(x=>x.textContent===label),removeButtons:()=>buttons().filter(x=>x.textContent==='移除此配对'),poll:()=>poll(),setState:s=>{state=s;},setReply:fn=>{reply=fn;}};
}
test('paired list shows fingerprint/activity scope; reading never removes a key',async()=>{
 const t=setup();assert.match(t.root.textContent,/配对记录 · aaaaaaaaaaaa/);assert.match(t.root.textContent,/不代表持续在线/);assert.match(t.root.textContent,/本次服务启动后暂无活动/);assert.match(t.root.textContent,/纯云端活动不计入/);
 await t.poll();assert.deepEqual(t.requests.map(x=>x.method),['pair_clients']);
});
test('removal needs explicit confirmation and keeps stable identity after reordering',async()=>{
 const t=setup();t.removeButtons()[0].attrs.click();assert.equal(t.requests.length,0);
 t.setState({ok:true,clients:[{id:two},{id:one}]});await t.poll();await t.button('确认移除').attrs.click();
 const remove=t.requests.filter(x=>x.method==='pair_remove');assert.deepEqual(remove,[{method:'pair_remove',args:[one]}]);
 assert.match(t.root.textContent,/已移除该配对/);
});
test('missing selected client, failed refresh and cancel cannot remove another client',async()=>{
 const t=setup();t.removeButtons()[0].attrs.click();t.setState({ok:true,clients:[{id:two}]});await t.poll();
 assert.equal(t.button('确认移除').disabled,true);await t.button('确认移除').attrs.click();
 assert.equal(t.requests.filter(x=>x.method==='pair_remove').length,0);
 t.setState(Error('offline'));await t.poll();assert.equal(t.button('确认移除').disabled,true);
 t.button('取消').attrs.click();await t.button('确认移除').attrs.click();assert.equal(t.requests.filter(x=>x.method==='pair_remove').length,0);
});
test('busy double click and uncertain response never automatically retry',async()=>{
 const t=setup();let finish;t.setReply(()=>new Promise(resolve=>finish=resolve));t.removeButtons()[0].attrs.click();
 const p=t.button('确认移除').attrs.click();await t.button('确认移除').attrs.click();assert.equal(t.requests.filter(x=>x.method==='pair_remove').length,1);
 finish({error:'通话状态未知'});await p;await t.poll();assert.equal(t.requests.filter(x=>x.method==='pair_remove').length,1);assert.match(t.root.textContent,/通话状态未知/);
});
test('list and revoke have separate RPC permissions',()=>{
 const acl=JSON.parse(readFileSync(new URL('../packaging/openwrt/root/usr/share/rpcd/acl.d/luci-app-airsim.json',import.meta.url),'utf8'))['luci-app-airsim'];
 assert.ok(acl.read.ubus.airsim.includes('pair_clients'));assert.ok(!acl.read.ubus.airsim.includes('pair_remove'));assert.ok(acl.write.ubus.airsim.includes('pair_remove'));
});
