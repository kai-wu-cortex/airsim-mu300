import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const web=new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/',import.meta.url);
const tick=()=>new Promise(r=>setImmediate(r));
function harness(){
 const requests=[],polls=[],nodes=[];let confirmation=false;
 const states={pcm_gain:{ok:true,settings:{downlink_gain_db:3,uplink_gain_db:0}},network:{ok:true,supported:false,reason:'unknown layout',mode:'cell',admin_known:false},bluetooth:{ok:true,supported:false,reason:'no controller'},notifications:{ok:true,provider:'webhook'},maintenance_job:{ok:true,none:true},flow_status:{ok:true,supported:false},selfcheck:{ok:true,checks:[]},audio_catalog:{ok:true,profiles:[{kernel:'7.3',id:'unknown',installable:false,evidence:'unverified'}]}};
 function E(tag,attrs={},children=[]){
  const n={tag,attrs,children:[],value:attrs.value||'',checked:!!attrs.checked,disabled:!!attrs.disabled,hidden:!!attrs.hidden,style:{},parentNode:null,
   get className(){return this.attrs.class||'';},set className(v){this.attrs.class=v;},
   get firstChild(){return this.children[0];},
   appendChild(child){if(child&&typeof child==='object'){child.remove();child.parentNode=this;}this.children.push(child);return child;},
   insertBefore(child,before){this.appendChild(child);this.children.pop();this.children.splice(Math.max(0,this.children.indexOf(before)),0,child);},
   remove(){if(this.parentNode){this.parentNode.children=this.parentNode.children.filter(c=>c!==this);this.parentNode=null;}},
   replaceChildren(...children){this.children.forEach(c=>{if(c&&typeof c==='object')c.parentNode=null;});this.children=[];children.forEach(c=>this.appendChild(c));},
   setAttribute(k,v){this.attrs[k]=String(v);},getAttribute(k){return this.attrs[k];},addEventListener(k,v){this.attrs[k]=v;},focus(){},
   querySelector(sel){return walk(this).find(c=>c!==this&&(sel.startsWith('.')?c.className.split(' ').includes(sel.slice(1)):c.tag===sel));}
  };
  n.classList={add(...classes){n.className=[...new Set(n.className.split(' ').concat(classes))].join(' ');}};
  Object.defineProperty(n,'textContent',{get(){return n.children.map(c=>typeof c==='object'?c.textContent:c).join('');},set(v){n.replaceChildren(String(v));}});
  (Array.isArray(children)?children:[children]).forEach(c=>n.appendChild(c));nodes.push(n);return n;
 }
 function walk(n){return [n,...n.children.filter(c=>c&&typeof c==='object').flatMap(walk)];}
 const document={documentElement:E('html'),querySelectorAll:()=>[]};
 const storage=new Map(),localStorage={getItem:k=>storage.get(k)||null,setItem:(k,v)=>storage.set(k,v)};
 const status={running:false,service_alive:false,calls:{available:true,calls:[]},agent:{reachable:false},version:'test'};
 const replies={settings_get:{ok:true,version:'test',settings:{mode:'cloud',interface:'br-lan',pair_port:8580,control_port:8581,pcm_port:8582}},status,updates_status:{ok:true,policy:{automatic:true,previews:false},cache:{},busy:false},kernel_status:{ok:true,kernel:'5.4',choices:[],bundles:[],audio:[]},sms_list:{ok:true,msgs:[],total:0,pages:1},sms_job:{none:1},pair_clients:{ok:true,clients:[]}};
 const cache={baseclass:{extend:x=>x},view:{extend:x=>x},poll:{add:f=>polls.push(f)},rpc:{declare:spec=>(...args)=>{
  const req={method:spec.method,args};requests.push(req);
  if(spec.method==='device_read')return Promise.resolve(states[JSON.parse(args[0]).action]);
  if(spec.method==='device_write')return typeof replies.device_write==='function'?replies.device_write():Promise.resolve(replies.device_write||{ok:true,pending:true,id:'job',message:'pending'});
  return Promise.resolve(replies[spec.method]||{ok:false});
 }}};
 function load(name){if(cache[name])return cache[name];const code=readFileSync(new URL(name.replace('view.airsim.','')+'.js',web),'utf8'),names=[],values=[];
  for(const m of code.matchAll(/'require ([a-zA-Z0-9_.]+)(?: as ([a-zA-Z0-9_]+))?';/g)){names.push(m[2]||m[1]);values.push(load(m[1]));}
  return cache[name]=new Function('E','L','window','localStorage','document',...names,code)(E,{resource:x=>x,url:x=>'/cgi-bin/luci/'+x},{confirm:()=>confirmation},localStorage,document,...values);
 }
 return {load,requests,states,replies,nodes,polls,walk,confirm:v=>confirmation=v};
}
const routes=['overview','connect','net','call','sms','maintenance','settings','bluetooth','audio'];
test('shared controllers cannot auto-mount as competing LuCI views',()=>{
 for(const name of ['extensions','manage','kernel','overview_panel']){
  const code=readFileSync(new URL(name+'.js',web),'utf8');
  assert.match(code,/'require baseclass';/);assert.doesNotMatch(code,/'require view';|return view\.extend/);
 }
});
test('nine pages expose matching navigation and load through existing read-only RPCs',async()=>{
 const menu=JSON.parse(readFileSync(new URL('../packaging/openwrt/root/usr/share/luci/menu.d/luci-app-airsim.json',import.meta.url),'utf8'));
 assert.deepEqual(Object.keys(menu).filter(k=>menu[k].title&&k!=='admin/services/airsim').map(k=>k.split('/').at(-1)),routes);
 for(const route of routes){const h=harness(),page=h.load('view.airsim.'+(route==='sms'?'messages':route));const root=page.render(page.load?await page.load():undefined);await tick();
  assert.equal(root.attrs['data-page'],route);
  const nav=h.walk(root).filter(n=>n.tag==='a'&&n.className.includes('as-menu-item'));
  assert.equal(nav.length,9);assert.equal(nav.filter(n=>n.attrs['aria-current']==='page').length,1);
  assert.ok(h.requests.every(r=>['device_read','status','settings_get','updates_status','kernel_status','sms_list','sms_job'].includes(r.method)),route);
  if(route==='net'){assert.deepEqual(h.walk(root).filter(n=>n.tag==='h3').map(n=>n.textContent),['上网方式','连接 Wi-Fi','已保存的 Wi-Fi','管理页面访问','这台机器的限制']);assert.ok(h.requests.filter(r=>r.method==='device_read').every(r=>['network','maintenance_job'].includes(JSON.parse(r.args[0]).action)));}
 }
});
test('network switch confirms once and reflects only confirmed state; unknown firewall stays disabled',async()=>{
 const h=harness();h.states.network={ok:true,supported:true,mode:'cell',admin_known:false};
 const root=h.load('view.airsim.net').render();await tick();const controls=h.walk(root).filter(n=>n.attrs.role==='switch');const net=controls.find(n=>n.attrs['aria-label']==='Wi-Fi 上网'),admin=controls.find(n=>n.attrs['aria-label']==='允许从 Wi-Fi 打开');
 assert.equal(admin.disabled,true);net.checked=true;await net.attrs.change();assert.equal(net.checked,false);assert.equal(h.requests.filter(r=>r.method==='device_write').length,0);
 h.confirm(true);net.checked=true;await net.attrs.change();assert.equal(net.checked,false);assert.equal(net.disabled,true);await net.attrs.change();assert.equal(h.requests.filter(r=>r.method==='device_write').length,1);
 h.states.network.mode='wifi';h.states.maintenance_job={ok:true,job:{id:'job',done:true,result:{ok:true}}};for(const f of h.polls)await f();
 assert.equal(net.checked,true);assert.equal(net.disabled,false);assert.equal(h.requests.filter(r=>r.method==='device_write').length,1);
});

async function gainHarness(){
 const h=harness(),module=h.load('view.airsim.pcm_gain');
 const root=module.render(await module.load());
 return Object.assign(h,{root,button:name=>h.walk(root).find(n=>n.tag==='button'&&n.textContent===name),field:key=>h.walk(root).find(n=>n.attrs.id==='as-pcm-'+key+'_gain_db')});
}
test('settings exposes device volume separately from browser appearance; opening only reads',async()=>{
 const h=harness(),page=h.load('view.airsim.settings'),root=page.render(await page.load());
 assert.ok(h.walk(root).some(n=>n.tag==='h3'&&n.textContent==='通话音量'));
 assert.ok(root.textContent.includes('通话音量保存到设备'));
 assert.equal(h.requests.filter(r=>r.method==='device_write').length,0);
 assert.equal(h.walk(root).find(n=>n.attrs.id==='as-pcm-downlink_gain_db').value,'3');
});
test('volume sends both directions once; reset edits draft without writing',async()=>{
 const h=await gainHarness();
 h.button('恢复原音量').attrs.click();assert.equal(h.field('downlink').value,'0');
 assert.equal(h.requests.filter(r=>r.method==='device_write').length,0);
 h.field('downlink').value='6';
 let finish;h.replies.device_write=()=>new Promise(r=>finish=r);
 const save=h.button('保存音量').attrs.click();
 await h.button('保存音量').attrs.click();assert.equal(h.button('保存音量').disabled,true);
 const writes=h.requests.filter(r=>r.method==='device_write');assert.equal(writes.length,1);
 const payload=JSON.parse(writes[0].args[0]);assert.equal(payload.action,'pcm_gain_save');assert.equal(payload.confirm,true);
 assert.deepEqual(JSON.parse(payload.payload),{downlink_gain_db:6,uplink_gain_db:0});
 finish({ok:true,settings:{downlink_gain_db:6,uplink_gain_db:0},message:'saved'});await save;
 assert.ok(h.root.textContent.includes('saved'));
});
test('volume blocks running or unknown service and polling preserves unfinished draft',async()=>{
 const h=await gainHarness();h.field('downlink').value='5';
 for(const state of [{running:true,service_alive:true,audio:{downlink_gain_db:3,uplink_gain_db:0}},{}]){
  h.replies.status=state;for(const poll of h.polls)await poll();
  assert.equal(h.button('保存音量').disabled,true);assert.equal(h.field('downlink').value,'5');
  await h.button('保存音量').attrs.click();
 }
 assert.equal(h.requests.filter(r=>r.method==='device_write').length,0);
 h.replies.status={running:false,service_alive:false};for(const poll of h.polls)await poll();assert.equal(h.button('保存音量').disabled,false);
 h.field('uplink').value='13';await h.button('保存音量').attrs.click();assert.equal(h.requests.filter(r=>r.method==='device_write').length,0);
});
test('volume read failure never defaults over settings; uncertain save requires explicit reread',async()=>{
 const h=await gainHarness();h.field('downlink').value='6';h.replies.device_write={ok:false,error:'failed'};
 await h.button('保存音量').attrs.click();assert.equal(h.button('保存音量').disabled,true);assert.equal(h.field('downlink').value,'6');
 for(const poll of h.polls)await poll();assert.equal(h.button('保存音量').disabled,true);
 h.states.pcm_gain={ok:false,error:'offline'};await h.button('重新读取').attrs.click();assert.equal(h.button('保存音量').disabled,true);assert.equal(h.field('downlink').value,'6');
 h.states.pcm_gain={ok:true,settings:{downlink_gain_db:3,uplink_gain_db:0}};await h.button('重新读取').attrs.click();
 assert.equal(h.field('downlink').value,'3');assert.equal(h.button('保存音量').disabled,false);
 assert.equal(h.requests.filter(r=>r.method==='device_write').length,1);
});
