import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync,readdirSync,mkdtempSync,cpSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {stampUI} from './stamp-ui.mjs';
const root=new URL('../packaging/openwrt/root/',import.meta.url);
const web=new URL('www/luci-static/resources/view/airsim/',root);
const source=name=>readFileSync(new URL(name+'.js',web),'utf8');
const tick=()=>new Promise(r=>setImmediate(r));

test('shared reads coalesce only while pending; failure clears and writes invalidate them',async()=>{
 const requests=[];
 const rpc={declare:spec=>payload=>new Promise((resolve,reject)=>requests.push({method:spec.method,payload,resolve,reject}))};
 const device=new Function('baseclass','rpc','E',source('device'))({extend:x=>x},rpc,()=>{});
 const first=device.read('network'),second=device.read('network');assert.equal(first,second);await tick();assert.equal(requests.length,1);
 requests[0].resolve({ok:true,mode:'cell'});await first;
 const stale=device.read('network');await tick();assert.equal(requests.length,2);
 const write=device.write('network_mode',{mode:'wifi'});requests[2].resolve({ok:true,pending:true});await write;
 const fresh=device.read('network');await tick();assert.equal(requests.length,4);assert.notEqual(fresh,stale);
 requests[1].resolve({ok:true,mode:'cell'});await stale;
 assert.equal(device.read('network'),fresh);requests[3].reject(Error('offline'));await assert.rejects(fresh,/offline/);
 const retry=device.read('network');await tick();assert.equal(requests.length,5);requests[4].resolve({ok:false,error:'unknown'});await assert.rejects(retry,/unknown/);
 assert.equal(requests.filter(r=>r.method==='device_write').length,1);
});

test('switch keeps original native input and gives it a stable accessible name',()=>{
 const E=(tag,attrs={},children=[])=>({tag,attrs,children,setAttribute(k,v){this.attrs[k]=v;}});
 const appearance=new Function('baseclass','localStorage','document','E',source('appearance'))({extend:x=>x},{getItem:()=>null},{},E);
 const input=E('input',{type:'checkbox'});input.checked=true;input.disabled=true;
 const row=appearance.switchField('启用通知',input);
 assert.equal(row.children[0],input);assert.equal(input.attrs.role,'switch');assert.equal(input.attrs['aria-label'],'启用通知');
 assert.equal(input.checked,true);assert.equal(input.disabled,true);assert.equal(row.children[1].attrs['aria-hidden'],'true');
});

test('staged UI gets deterministic content URLs for the entire dependency graph',()=>{
 const dirs=[];
 const stage=()=>{const p=mkdtempSync(join(tmpdir(),'airsim-ui-'));dirs.push(p);cpSync(root,p,{recursive:true});return p;};
 try{
  const a=stage(),b=stage(),c=stage();
  const rel='www/luci-static/resources/view/airsim/';
  const original=readFileSync(new URL('overview.css',web),'utf8');writeFileSync(join(c,rel,'overview.css'),original+'\n/* updated */\n');
  const ha=stampUI(a);assert.equal(stampUI(b),ha);assert.notEqual(stampUI(c),ha);
  assert.throws(()=>stampUI(a),/already stamped/);
  const menu=JSON.parse(readFileSync(join(a,'usr/share/luci/menu.d/luci-app-airsim.json')));
  for(const item of Object.values(menu))if(item.action.type==='view'){
   assert.ok(item.action.path.endsWith('_'+ha));assert.ok(readFileSync(join(a,'www/luci-static/resources/view',item.action.path+'.js')));
  }
  const homeMenu=JSON.parse(readFileSync(join(a,'usr/lib/airsim/dashboard-menu.json')));
  assert.equal(homeMenu['admin/home'].action.path,'airsim/home_'+ha);
  const files=readdirSync(join(a,rel));
  for(const name of files.filter(n=>n.endsWith('.js'))){
   const content=readFileSync(join(a,rel,name),'utf8');new Function(content);
   for(const [,dep] of content.matchAll(/require view\.airsim\.([a-z0-9_]+) as /g))assert.ok(files.includes(dep+'.js'),dep);
   for(const [,dep] of content.matchAll(/view\/airsim\/([a-z0-9_]+\.css)/g))assert.ok(files.includes(dep),dep);
  }
  assert.equal(readFileSync(new URL('overview.css',web),'utf8'),original);
 }finally{for(const p of dirs)rmSync(p,{recursive:true,force:true});}
});
