import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,mkdtempSync,mkdirSync,writeFileSync,existsSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFileSync} from 'node:child_process';
const root=new URL('../',import.meta.url);
const code=readFileSync(new URL('packaging/openwrt/root/www/luci-static/resources/view/airsim/home.js',root),'utf8');
function setup(hasHero=true){
 const hero={name:'signal'},traffic={name:'traffic'},card={name:'airsim',airsimDispose(){events.push('panel-dispose');}},events=[];
 const nativeRoot={children:[hero,traffic],firstChild:hero,querySelector:()=>hasHero?hero:null,insertBefore(node,before){this.children.splice(this.children.indexOf(before),0,node);}};hero.parentNode=nativeRoot;hero.nextSibling=traffic;
 const native={load:async()=>{events.push('native-load');return 'native-data';},render:d=>{assert.equal(d,'native-data');events.push('native-render');return nativeRoot;},unload:()=>events.push('native-unload')};
 const panel={load:async()=>{events.push('status-read');return {running:false};},render:(s,embedded)=>{assert.equal(embedded,true);events.push('panel-render');return card;}};
 const home=new Function('view','panel','L','E',code)({extend:x=>x},panel,{require:async name=>{assert.equal(name,'view.airsim_native_home');return native;},url:x=>x},()=>({name:'fallback-link'}));
 return {home,nativeRoot,events,panel};
}
test('homepage inserts shared controls between signal and traffic, then releases both pollers',async()=>{
 const t=setup();const data=await t.home.load();const root=t.home.render(data);
 assert.deepEqual(root.children.map(c=>c.name),['signal','airsim','traffic']);
 assert.deepEqual(t.events.slice(0,2).sort(),['native-load','status-read']);
 assert.deepEqual(t.events.slice(2),['native-render','panel-render']);
 t.home.unload();assert.deepEqual(t.events.slice(-2),['panel-dispose','native-unload']);
});
test('missing insertion anchor leaves native cards intact and adds only an AirSIM link',async()=>{
 const t=setup(false);t.home.render(await t.home.load());
 assert.deepEqual(t.nativeRoot.children.map(c=>c.name),['fallback-link','signal','traffic']);
 assert.ok(!t.events.includes('panel-render'));
});
test('failed AirSIM status cannot prevent native homepage rendering',async()=>{
 const t=setup();t.panel.load=async()=>{throw Error('offline');};
 const data=await t.home.load();assert.deepEqual(data[1],{});t.home.render(data);
 assert.equal(t.nativeRoot.children[2].name,'traffic');
});
test('unreviewed native homepage is preserved and stale adapter removed',()=>{
 const temp=mkdtempSync(join(tmpdir(),'airsim-home-'));
 try{
  const source=join(temp,'www/luci-static/resources/view/mu300/home.js');mkdirSync(join(source,'..'),{recursive:true});writeFileSync(source,'unknown firmware view');
  const menu=join(temp,'usr/share/luci/menu.d/zz-airsim-home.json');mkdirSync(join(menu,'..'),{recursive:true});writeFileSync(menu,'old adapter');
  const native=join(temp,'www/luci-static/resources/view/airsim_native_home.js');writeFileSync(native,'old copy');
  const helper=new URL('packaging/openwrt/root/usr/lib/airsim/dashboard-setup',root);
  execFileSync('sh',[fileURLToPath(helper),'--install'],{env:{...process.env,AIRSIM_DASHBOARD_ROOT:temp}});
  assert.equal(readFileSync(source,'utf8'),'unknown firmware view');assert.equal(existsSync(menu),false);assert.equal(existsSync(native),false);
 }finally{rmSync(temp,{recursive:true,force:true});}
});
