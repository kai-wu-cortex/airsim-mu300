import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/manage.js',import.meta.url),'utf8');
const settings=()=>({mode:'cloud',relay_url:'',interface:'br-lan',pair_port:8580,control_port:8581,pcm_port:8582});
const stopped=()=>({running:false,service_alive:false});
function setup(initial=stopped()){
 const nodes=[],requests=[];let state=initial,poll,response={ok:true,message:'saved'},download,info={ok:true,policy:{automatic:true,previews:true},cache:{},job:{phase:'idle'},busy:false};
 function E(tag,attrs={},children=[]){
  const n={tag,attrs,children:Array.isArray(children)?children:[children],value:attrs.value??'',disabled:false,checked:!!attrs.checked,href:attrs.href,click(){download=this},remove(){},appendChild(x){this.children.push(x)},addEventListener(event,fn){this.attrs[event]=fn}};
  let content;Object.defineProperty(n,'textContent',{get(){return content??this.children.map(c=>typeof c==='object'?c.textContent:c).join('')},set(v){content=String(v)}});nodes.push(n);return n;
 }
 const view=new Function('view','rpc','poll','E','L','URL','Blob','document','setTimeout',source)(
  {extend:x=>x},{declare:spec=>(...args)=>{if(spec.method==='status')return Promise.resolve(state);if(spec.method==='updates_status')return Promise.resolve(info);requests.push({method:spec.method,args});return Promise.resolve(response)}},
  {add:fn=>poll=fn},E,{url:x=>'/'+x,resource:x=>x},Object.assign(class extends URL {},{createObjectURL:()=> 'blob:test',revokeObjectURL:()=>{}}),Blob,{body:{appendChild(){}}},fn=>fn());
 const root=view.render([{ok:true,settings:settings(),version:'0.1.0-r14'},initial,info]);
 const button=name=>nodes.find(n=>n.tag==='button'&&n.textContent===name);
 return {root,nodes,requests,button,field:key=>nodes.find(n=>n.attrs.id==='as-setting-'+key),reply:v=>response=v,updates:v=>info=v,state:v=>state=v,poll:()=>poll(),download:()=>download};
}
test('settings editable only when service explicitly stopped; poll keeps user draft',async()=>{
 const t=setup({running:true,service_alive:true});assert.equal(t.button('保存设置').disabled,true);
 t.field('relay_url').value='https://draft.example';t.state(stopped());await t.poll();assert.equal(t.button('保存设置').disabled,false);assert.equal(t.field('relay_url').value,'https://draft.example');
 t.state({});await t.poll();assert.equal(t.button('保存设置').disabled,true);
});
test('invalid settings never reach RPC; valid save only sends settings',async()=>{
 const t=setup();t.field('pair_port').value='8581';await t.button('保存设置').attrs.click();assert.equal(t.requests.length,0);
 t.field('pair_port').value='18580';t.field('relay_url').value='https://user:secret@relay.example';await t.button('保存设置').attrs.click();assert.equal(t.requests.length,0);
 t.field('relay_url').value='https://relay.example/';await t.button('保存设置').attrs.click();assert.equal(t.requests.length,1);
 assert.equal(t.requests[0].method,'settings_save');assert.equal(JSON.parse(t.requests[0].args[0]).relay_url,'https://relay.example');
 assert.ok(t.nodes.some(n=>n.textContent==='saved'));
});
test('failed save does not show success or reset fields',async()=>{
 const t=setup();t.reply({error:'busy'});t.field('control_port').value='18581';await t.button('保存设置').attrs.click();assert.ok(t.nodes.some(n=>n.textContent==='busy'));assert.equal(t.field('control_port').value,'18581');
});
test('manual update check includes chosen channel, never follows remote arbitrary link',async()=>{
 const t=setup();assert.equal(t.requests.length,0);
 t.reply({ok:true,status:'available',latest:'v0.1.0-r14',prerelease:true,url:'https://untrusted.example'});
 await t.button('检查更新').attrs.click();assert.deepEqual(t.requests,[{method:'updates',args:[true]}]);
 assert.ok(t.nodes.some(n=>n.href==='https://github.com/kai-wu-cortex/airsim-mu300/releases/tag/v0.1.0-r14'));
 t.nodes.find(n=>n.attrs.id==='as-previews').checked=false;t.reply({error:'offline'});await t.button('检查更新').attrs.click();assert.equal(t.requests[1].args[0],false);assert.ok(t.nodes.some(n=>n.textContent==='offline'));
});
test('diagnostics downloads only on click; no call or SMS requests',async()=>{
 const t=setup();t.reply({ok:true,report:{schema:1,mode:'local'}});await t.button('导出脱敏诊断').attrs.click();
 assert.equal(t.download().attrs.download,'airsim-mu300-diagnostics.json');assert.deepEqual(t.requests,[{method:'diagnostics',args:[]}]);
});
test('relay readiness is not inferred from configuration',()=>{
 const t=setup({running:true,service_alive:true,agent:{reachable:true},relay:{configured:true}});
 assert.ok(t.nodes.some(n=>n.textContent.includes('尚未确认实时命令通道连接')));
});
test('ACL keeps saves and network update checks out of read permission',()=>{
 const acl=JSON.parse(readFileSync(new URL('../packaging/openwrt/root/usr/share/rpcd/acl.d/luci-app-airsim.json',import.meta.url),'utf8'))['luci-app-airsim'];
 assert.ok(acl.read.ubus.airsim.includes('settings_get'));assert.ok(acl.read.ubus.airsim.includes('diagnostics'));
 for(const method of ['settings_save','updates','updates_policy','update_install']){assert.ok(!acl.read.ubus.airsim.includes(method));assert.ok(acl.write.ubus.airsim.includes(method))}
 assert.ok(acl.read.ubus.airsim.includes('updates_status'));
});

test('cached update offers explicit installation only when stopped; double click fenced',async()=>{
 const t=setup({running:true,service_alive:true});
 t.updates({ok:true,busy:false,cache:{previews:true,result:{status:'available',latest:'v0.1.0-r15'}},job:{phase:'idle'}});
 await t.poll();assert.equal(t.button('下载并更新').disabled,true);assert.equal(t.requests.length,0);
 t.state(stopped());await t.poll();assert.equal(t.button('下载并更新').disabled,false);
 await t.button('下载并更新').attrs.click();assert.equal(t.requests.length,0);
 t.reply({ok:true,job:{phase:'queued',message:'queued'}});
 await Promise.all([t.button('确认更新').attrs.click(),t.button('确认更新').attrs.click()]);
 assert.deepEqual(t.requests,[{method:'update_install',args:['v0.1.0-r15']}]);assert.equal(t.button('下载并更新').disabled,true);
});
test('unknown update status, changed target, and cancellation never install',async()=>{
 const t=setup();t.updates({ok:true,busy:false,cache:{previews:true,result:{status:'available',latest:'v0.1.0-r15'}},job:{phase:'idle'}});await t.poll();
 await t.button('下载并更新').attrs.click();await t.button('取消').attrs.click();await t.button('确认更新').attrs.click();assert.equal(t.requests.length,0);
 await t.button('下载并更新').attrs.click();t.updates({});await t.poll();await t.button('确认更新').attrs.click();assert.equal(t.requests.length,0);assert.equal(t.button('下载并更新').disabled,true);
});
test('automatic check preferences persist without installing; status polling never checks GitHub',async()=>{
 const t=setup();const automatic=t.nodes.find(n=>n.attrs.id==='as-auto-update');automatic.checked=false;
 t.reply({ok:true,policy:{automatic:false,previews:true},cache:{},job:{phase:'idle'}});
 await automatic.attrs.change();assert.deepEqual(t.requests,[{method:'updates_policy',args:[JSON.stringify({automatic:false,previews:true})]}]);
 await t.poll();assert.equal(t.requests.length,1);
});

test('optional Tailscale interface is empty for old settings and validated before save',async()=>{
 const t=setup();assert.equal(t.field('tunnel_interface').value,'');
 t.field('tunnel_interface').value='br-lan';await t.button('保存设置').attrs.click();assert.equal(t.requests.length,0);
 t.field('tunnel_interface').value='tailscale0;reboot';await t.button('保存设置').attrs.click();assert.equal(t.requests.length,0);
 t.field('tunnel_interface').value='tailscale0';await t.button('保存设置').attrs.click();assert.equal(t.requests.length,1);
 assert.equal(JSON.parse(t.requests[0].args[0]).tunnel_interface,'tailscale0');
});
