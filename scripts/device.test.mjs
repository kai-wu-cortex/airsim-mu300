import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const web=new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/',import.meta.url);
const code=n=>readFileSync(new URL(n+'.js',web),'utf8');
const settle=()=>new Promise(r=>setImmediate(r));
function setup(){
 const calls=[],elements=[],polls=[];let confirm=true,writeReply={ok:true,pending:true,id:'a'.repeat(32),message:'处理中'};
 const reads={network:{ok:true,supported:false,reason:'未知布局',mode:'unknown'},flow_status:{ok:true,supported:false},bluetooth:{ok:true,supported:false,reason:'无控制器'},notifications:{ok:true,provider:'webhook'},selfcheck:{ok:true,checks:[]},audio_catalog:{ok:true,profiles:[{id:'reference-7.3',kernel:'7.3.1',installable:false,evidence:'未验收'}]},maintenance_job:{ok:true,none:true}};
 function E(tag,attrs={},children=[]){
  const n={tag,attrs,children:Array.isArray(children)?children:[children],value:attrs.value||'',checked:false,disabled:false,hidden:!!attrs.hidden,appendChild(x){this.children.push(x);},replaceChildren(...x){this.children=x;},setAttribute(k,v){this.attrs[k]=v;}};
  let text='';Object.defineProperty(n,'textContent',{get(){return text||this.children.map(x=>typeof x==='string'?x:x.textContent||'').join('');},set(v){text=String(v);this.children=[];}});elements.push(n);return n;
 }
 const rpc={declare:o=>(payload)=>{const q=JSON.parse(payload);calls.push({method:o.method,...q});return Promise.resolve(o.method==='device_read'?reads[q.action]:writeReply);}};
 const device=new Function('baseclass','rpc','E',code('device'))({extend:x=>x},rpc,E);
 const appearance=new Function('baseclass','localStorage','document','E',code('appearance'))({extend:x=>x},{getItem:()=>null},{},E);
 const page=new Function('baseclass','poll','device','appearance','E','L','window',code('extensions'))({extend:x=>x},{add:f=>polls.push(f)},device,appearance,E,{resource:x=>x},{confirm:()=>confirm});
 const root=page.render();
 return {calls,reads,root,elements,button:s=>elements.find(x=>x.tag==='button'&&x.textContent===s),setConfirm:v=>confirm=v,setWrite:v=>writeReply=v,poll:async()=>{for(const f of polls)await f();},device};
}
test('device extension auto-load is read-only; unproven kernels and unknown network disabled',async()=>{
 const t=setup();await settle();assert.ok(t.calls.every(c=>c.method==='device_read'));assert.equal(t.button('切换联网模式').disabled,true);assert.equal(t.button('开启蓝牙').disabled,true);assert.equal(t.button('准备 7.3.1 音频').disabled,true);assert.match(t.root.textContent,/未知布局/);assert.match(t.root.textContent,/未验收/);
});
test('unsupported network and Bluetooth buttons cannot dispatch a write',async()=>{
 const t=setup();await settle();await t.button('切换联网模式').attrs.click();await t.button('开启蓝牙').attrs.click();
 assert.equal(t.calls.filter(c=>c.method==='device_write').length,0);
});
test('maintenance confirmation, pending task and double-click never duplicate writes',async()=>{
 const t=setup();t.reads.bluetooth={ok:true,supported:true,controller:{powered:'no'}};await settle();t.setConfirm(false);await t.button('开启蓝牙').attrs.click();assert.equal(t.calls.filter(c=>c.method==='device_write').length,0);
 t.setConfirm(true);await t.button('开启蓝牙').attrs.click();await t.button('开启蓝牙').attrs.click();let writes=t.calls.filter(c=>c.method==='device_write');assert.equal(writes.length,1);assert.equal(writes[0].confirm,true);assert.equal(writes[0].action,'bt_power');assert.equal(writes[0].enabled,true);
 t.reads.maintenance_job={ok:true,job:{id:'a'.repeat(32),done:true,result:{ok:false,error:'最终状态未确认'}}};await t.poll();assert.match(t.root.textContent,/最终状态未确认/);assert.equal(t.calls.filter(c=>c.method==='device_write').length,1);
});
test('unknown job stays locked and form drafts survive polling',async()=>{
 const t=setup();t.reads.bluetooth={ok:true,supported:true,controller:{powered:'no'}};await settle();const ssid=t.elements.find(x=>x.attrs.placeholder==='Wi-Fi 名称');ssid.value='draft network';await t.button('开启蓝牙').attrs.click();t.reads.maintenance_job={ok:true,uncertain:true,job:{id:'a'.repeat(32),done:false}};await t.poll();assert.equal(t.button('开启蓝牙').disabled,true);assert.equal(ssid.value,'draft network');assert.match(t.root.textContent,/暂未确认/);
});
test('backend action and confirmation cannot be overridden through fields',async()=>{
 const t=setup();await settle();await t.device.write('bt_power',{action:'backup_restore',confirm:false,enabled:true});const last=t.calls.at(-1);assert.equal(last.action,'bt_power');assert.equal(last.confirm,true);
});
test('maintenance RPC is split and backup export is never a read action',()=>{
 const acl=JSON.parse(readFileSync(new URL('../packaging/openwrt/root/usr/share/rpcd/acl.d/luci-app-airsim.json',import.meta.url),'utf8'))['luci-app-airsim'];assert.ok(acl.read.ubus.airsim.includes('device_read'));assert.ok(!acl.read.ubus.airsim.includes('device_write'));assert.ok(acl.write.ubus.airsim.includes('device_write'));
 const source=readFileSync(new URL('../cmd/airsim-mu300d/device_admin.go',import.meta.url),'utf8');assert.doesNotMatch(source.match(/var deviceReadActions[^\n]+/)[0],/backup_export/);
});
test('appearance rejects invalid storage and handles storage unavailable',()=>{
 let raw='{"theme":"evil","density":"dense","transparency":"true"}';const attrs={};
 const page=new Function('baseclass','localStorage','document',code('appearance'))({extend:x=>x},{getItem:()=>raw},{documentElement:{setAttribute:(k,v)=>attrs[k]=v}});
 let root={setAttribute:(k,v)=>attrs[k]=v};page.apply(root);assert.equal(attrs['data-theme'],'system');assert.equal(attrs['data-density'],'comfortable');assert.equal(attrs['data-transparency'],'false');raw='broken';page.apply(root);assert.equal(attrs['data-theme'],'system');
});
test('native SMS and service preparation share the maintenance admission guard',()=>{
 const sms=code('sms');assert.match(sms,/object: 'airsim', method: 'sms_send'/);
 const init=readFileSync(new URL('../packaging/openwrt/root/etc/init.d/airsim',import.meta.url),'utf8');assert.match(init,/airsim-mu300d --prepare-service/);assert.doesNotMatch(init,/\/usr\/lib\/airsim\/prepare \|\|/);
 const acl=JSON.parse(readFileSync(new URL('../packaging/openwrt/root/usr/share/rpcd/acl.d/luci-app-airsim.json',import.meta.url),'utf8'))['luci-app-airsim'];assert.ok(acl.write.ubus.airsim.includes('sms_send'));assert.ok(!acl.write.ubus.mu300dash.includes('sms_send'));
});
