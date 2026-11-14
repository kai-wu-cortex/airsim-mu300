import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
const source=readFileSync(new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/kernel.js',import.meta.url),'utf8');
const snapshot=()=>({ok:true,kernel:'5.4.254-gb50db5b6224c',boot_selection:'6.18',installed_kernel:'5.4',installed_tag:'v2026.10.30.1',config_sha256:'fixture-config',profile_status:'profile_match',bundled_audio:true,reservation_missing:'0',choices:[{series:'5.4',audio:'5.4 需精确匹配'},{series:'6.18',audio:'参考项目提供；本项目尚未提供或验收。'},{series:'7.3',audio:'参考项目提供；本项目尚未提供或验收。'}],bundles:[{path:'/tmp/mu300-kernel.tar.gz',size:1024}],audio:[]});
function setup(initial=snapshot()) {
 const elements=[],requests=[];let reply=initial;
 function E(tag,attrs={},children=[]) {
  const n={tag,attrs,children:[],value:attrs.value??'',disabled:false,appendChild(x){this.children.push(x);},replaceChildren(...x){this.children=x;this.value='';}};
  let text='';Object.defineProperty(n,'textContent',{get(){return text||this.children.map(x=>typeof x==='string'?x:x.textContent).join('');},set(v){text=String(v);this.children=[];}});
  if (Array.isArray(children))n.children=children;else n.textContent=children;
  elements.push(n);return n;
 }
 const view=new Function('baseclass','rpc','E','L',source)({extend:x=>x},{declare:o=>async()=>{requests.push(o);if(reply instanceof Error)throw reply;return reply;}},E,{resource:x=>x});
 const root=view.render(initial),field=id=>elements.find(x=>x.attrs.id===id),button=label=>elements.find(x=>x.tag==='button'&&x.textContent===label);
 const t={view,root,requests,field,button,setReply:r=>reply=r};
 t.select=(id,value)=>{field(id).value=value;(field(id).attrs.change||field(id).attrs.input)();};
 t.ready=()=>{t.select('as-kernel-target','6.18');t.select('as-kernel-release','v2026.11.02.1');t.select('as-kernel-bundle',initial.bundles[0].path);};
 return t;
}
test('kernel selector reads only; separates running/boot versions and audio status',()=>{
 const t=setup();assert.equal(t.root.attrs.class,'as-root as-kernel');assert.match(t.root.textContent,/5\.4\.254-gb50db5b6224c/);assert.match(t.root.textContent,/6\.18/);
 assert.match(t.root.textContent,/不能据此判断系统自带音频/);assert.match(t.root.textContent,/实际声音/);
 assert.equal(t.field('as-kernel-target').value,'5.4');assert.equal(t.field('as-kernel-release').value,'');
 assert.equal(t.button('复制命令').disabled,true);assert.equal(t.requests.length,0);
 t.ready();assert.equal(t.button('复制命令').disabled,false);assert.match(t.root.textContent,/尚未提供或验收/);
 assert.equal(t.field('as-kernel-command').textContent,"MU300_RELEASE='v2026.11.02.1' MU300_KERNEL_BUNDLE='/tmp/mu300-kernel.tar.gz' mu300-update kernel '6.18'");
 assert.equal(t.requests.length,0);
});
test('unknown kernel and unreadable config never appear compatible',()=>{
 for(const status of ['unsupported_kernel','config_unknown','config_mismatch']) {
  const t=setup({...snapshot(),kernel:'9.9.1',profile_status:status});
  assert.equal(t.field('as-kernel-target').value,'');assert.equal(t.button('复制命令').disabled,true);
  assert.doesNotMatch(t.root.textContent,/内核及配置匹配已验证/);
 }
});
test('refresh preserves drafts and clears disappeared package choices',async()=>{
 const t=setup();t.ready();await t.button('刷新状态').attrs.click();
 assert.equal(t.field('as-kernel-target').value,'6.18');assert.equal(t.field('as-kernel-release').value,'v2026.11.02.1');assert.equal(t.field('as-kernel-bundle').value,'/tmp/mu300-kernel.tar.gz');
 assert.deepEqual(t.requests.map(x=>x.method),['kernel_status']);
 t.setReply({...snapshot(),bundles:[]});await t.button('刷新状态').attrs.click();
 assert.equal(t.field('as-kernel-bundle').value,'');assert.equal(t.button('复制命令').disabled,true);
});
test('read failure disables stale command; first-read failure can recover',async()=>{
 const t=setup();t.ready();t.setReply(Error('offline'));await t.button('刷新状态').attrs.click();
 assert.equal(t.button('复制命令').disabled,true);assert.doesNotMatch(t.field('as-kernel-command').textContent,/mu300-update/);
 assert.equal(t.field('as-kernel-target').value,'6.18');assert.match(t.root.textContent,/状态不可用/);
 const recovered=setup({ok:false});recovered.setReply(snapshot());await recovered.button('刷新状态').attrs.click();
 assert.equal(recovered.field('as-kernel-target').children.length,4);
 assert.equal(recovered.field('as-kernel-target').value,'');
});
test('invalid tags, forged packages, and unknown target cannot generate commands',()=>{
 const t=setup();t.ready();
 for(const tag of ['v1;reboot','$(reboot)','v1\nreboot','--help','v1 '+ 'bad']) {t.select('as-kernel-release',tag);assert.equal(t.button('复制命令').disabled,true);}
 t.ready();t.select('as-kernel-bundle','/tmp/unlisted-kernel.tar.gz');assert.equal(t.button('复制命令').disabled,true);
 t.ready();t.select('as-kernel-target','6.18;reboot');assert.equal(t.button('复制命令').disabled,true);
});
test('shell metacharacters in a listed filename remain literal arguments',()=>{
 const path="/tmp/mu300-kernel '$(echo injected)' `id`.tar.gz", state=snapshot();state.bundles=[{path,size:1024}];
 const t=setup(state);t.ready();assert.equal(t.button('复制命令').disabled,false);
 const cmd=t.field('as-kernel-command').textContent.replace(/ mu300-update kernel /, ' fixture_update kernel ');
 // Replace the updater with a local shell function that only prints arguments.
 const got=execFileSync('/bin/sh',['-c','fixture_update() { printf "%s\\n" "$MU300_RELEASE" "$MU300_KERNEL_BUNDLE" "$1" "$2"; }\n'+cmd],{encoding:'utf8'});
 assert.equal(got,'v2026.11.02.1\n'+path+'\nkernel\n6.18\n');
});
test('clipboard unavailable offers manual selection without device action',async()=>{
 const t=setup();t.ready();await t.button('复制命令').attrs.click();assert.equal(t.requests.length,0);
 assert.match(t.root.textContent,/手动选中/);
});
test('kernel RPC is read-only in ACL and reachable from menu and rpcd',()=>{
 const read=p=>readFileSync(new URL('../packaging/openwrt/root/'+p,import.meta.url),'utf8');
 const acl=JSON.parse(read('usr/share/rpcd/acl.d/luci-app-airsim.json'))['luci-app-airsim'];
 assert.ok(acl.read.ubus.airsim.includes('kernel_status'));assert.ok(!acl.write.ubus.airsim.includes('kernel_status'));
 const menu=JSON.parse(read('usr/share/luci/menu.d/luci-app-airsim.json'));
 assert.equal(menu['admin/services/airsim/kernel'].action.path,'airsim/audio');
 const rpc=read('usr/libexec/rpcd/airsim');assert.match(rpc,/"kernel_status":\{\}/);assert.match(rpc,/--manage kernel-status/);
 assert.equal((source.match(/rpc\.declare/g)||[]).length,1);
});

test('7.3 defaults to checked local CLI and requires reviewed updater plus trusted digest',async()=>{
 const t=setup({...snapshot(),updater_73:{status:'patch_available'}});
 t.ready();t.select('as-kernel-target','7.3');assert.equal(t.button('复制命令').disabled,true);
 t.select('as-kernel-sha','a'.repeat(64));
 assert.equal(t.button('复制命令').disabled,false);
 let cmd=t.field('as-kernel-command').textContent;
 assert.match(cmd,/--kernel-7-3 check$/);assert.match(cmd,/"confirm":false/);assert.doesNotMatch(cmd,/mu300-update kernel/);
 const decode=cmd=>JSON.parse(execFileSync('/bin/sh',['-c',cmd.replace(/ \| \/usr\/lib\/airsim\/airsim-mu300d --kernel-7-3 (check|apply)$/, '')],{encoding:'utf8'}));
 assert.equal(decode(cmd).sha256,'a'.repeat(64));
 t.select('as-kernel-action','apply');cmd=t.field('as-kernel-command').textContent;
 assert.match(cmd,/--kernel-7-3 apply$/);assert.equal(decode(cmd).confirm,true);assert.equal(t.requests.length,0);
 t.setReply({...snapshot(),updater_73:{status:'unknown'}});await t.button('刷新状态').attrs.click();
 assert.equal(t.button('复制命令').disabled,true);assert.doesNotMatch(t.field('as-kernel-command').textContent,/--kernel-7-3/);
});
test('7.3 JSON pipe preserves literal filenames and unknown updater stays disabled',()=>{
 const state={...snapshot(),updater_73:{status:'native'},bundles:[{path:"/tmp/kernel '$(echo unsafe)' `id`.tar.gz",size:100}]};
 const t=setup(state);t.ready();t.select('as-kernel-target','7.3');t.select('as-kernel-sha','b'.repeat(64));
 const cmd=t.field('as-kernel-command').textContent.replace(/ \| \/usr\/lib\/airsim\/airsim-mu300d --kernel-7-3 check$/, '');
 const got=JSON.parse(execFileSync('/bin/sh',['-c',cmd],{encoding:'utf8'}));assert.equal(got.bundle,state.bundles[0].path);
 for(const status of ['unknown','missing',undefined]) {
  const x=setup({...snapshot(),updater_73:{status}});x.ready();x.select('as-kernel-target','7.3');x.select('as-kernel-sha','a'.repeat(64));assert.equal(x.button('复制命令').disabled,true);
 }
});
