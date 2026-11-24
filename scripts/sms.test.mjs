// Synthetic fixtures only. Never submits SMS to a device.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('../packaging/openwrt/root/www/luci-static/resources/view/airsim/sms.js',import.meta.url),'utf8');
const flush=async()=>{for(let i=0;i<12;i++)await Promise.resolve()};
async function setup(phoneReply) {
 const elements=[],requests=[];let poll;
 const replies={sms_status:async()=>phoneReply||({available:true,source:'native',state:'ready',last_success_at:1}),sms_list:async()=>({total:1,unread:1,page:1,pages:1,msgs:[{id:'000001',dir:'mt',status:'unread',peer:'fixture',preview:'<script>fixture</script>'}]}),sms_show:async()=>({ok:1,text:'id: 000001\n---\n<script>full fixture</script>'}),sms_job:async()=>({none:1}),sms_send:async()=>({ok:1,started:1})};
 function E(tag,attrs={},children=[]) {
  const node={tag,attrs,children:[],value:attrs.value??'',disabled:false,hidden:!!attrs.hidden,className:attrs.class??'',focus(){},appendChild(c){this.children.push(c)},replaceChildren(...c){this.children=c}};
  let content='';Object.defineProperty(node,'textContent',{get(){return content||this.children.map(x=>typeof x==='string'?x:x.textContent).join('')},set(v){content=String(v);this.children=[]}});
  if(Array.isArray(children))node.children=children;else node.textContent=children;
  elements.push(node);return node;
 }
 const module=new Function('baseclass','rpc','poll','E',source)({extend:x=>x},{declare:spec=>(...args)=>{requests.push({method:spec.method,args});return replies[spec.method](...args)}},{add:fn=>{poll=fn}},E);
 const root=module.render();await flush();
 const byId=id=>elements.find(e=>e.attrs.id===id);
 const button=label=>elements.find(e=>e.tag==='button'&&e.textContent===label);
 const number=byId('as-sms-number'),body=byId('as-sms-text');
 return {root,elements,requests,replies,number,body,button,poll:()=>poll(),compose(n='15550001111',text='测试 fixture'){number.value=n;body.value=text;body.attrs.input()},sends:()=>requests.filter(r=>r.method==='sms_send')};
}
test('only listing and job status load automatically; content remains plain text',async()=>{
 const t=await setup();assert.deepEqual(t.requests.map(r=>r.method),['sms_list','sms_job','sms_status']);
 assert.ok(t.root.textContent.includes('<script>fixture</script>'));
 assert.equal(t.elements.some(e=>e.tag==='script'||'innerHTML' in e),false);
 await t.button('查看全文 · 标记已读').attrs.click();
 assert.deepEqual(t.requests.find(r=>r.method==='sms_show').args,['000001']);
 assert.ok(t.root.textContent.includes('<script>full fixture</script>'));
});
test('confirmation required; double click is fenced and queued is not delivered',async()=>{
 const t=await setup();t.compose();t.button('发送短信').attrs.click();assert.equal(t.sends().length,0);
 let finish;t.replies.sms_send=()=>new Promise(resolve=>finish=resolve);
 const first=t.button('确认发送').attrs.click();await t.button('确认发送').attrs.click();assert.equal(t.sends().length,1);
 assert.deepEqual(t.sends()[0].args,['15550001111','测试 fixture']);
 finish({ok:1,started:1});await first;
 assert.ok(t.root.textContent.includes('正在等待基带确认'));assert.equal(t.button('发送短信').disabled,true);
 t.replies.sms_job=async()=>({done:1,ok:1});await t.poll();
 assert.ok(t.root.textContent.includes('不代表对方已收到'));assert.equal(t.body.value,'测试 fixture');
 assert.equal(t.sends().length,1);
});
test('invalid recipients, oversized text, controls and emoji cannot submit',async()=>{
 const t=await setup();
 for(const [n,b] of [['12;sh','x'],['15550001111','x'.repeat(71)],['15550001111','🙂'],['15550001111','\x01'],['15550001111','  ']]){
  t.compose(n,b);assert.equal(t.button('发送短信').disabled,true);t.button('发送短信').attrs.click();await t.button('确认发送').attrs.click();
 }
 assert.equal(t.sends().length,0);
});
test('unknown submission never retries automatically and requires acknowledgment',async()=>{
 const t=await setup();t.compose();t.replies.sms_send=async()=>{throw Error('timeout')};
 t.button('发送短信').attrs.click();await t.button('确认发送').attrs.click();
 await t.poll();await t.poll();assert.equal(t.sends().length,1);assert.equal(t.button('发送短信').disabled,true);
 assert.equal(t.body.value,'测试 fixture');t.button('已核对结果，继续编辑').attrs.click();assert.equal(t.button('发送短信').disabled,false);
});
test('existing native job locks send, polling preserves draft',async()=>{
 const t=await setup();t.compose();t.replies.sms_job=async()=>({running:1,elapsed:5});await t.poll();
 assert.equal(t.button('发送短信').disabled,true);assert.equal(t.body.value,'测试 fixture');assert.equal(t.sends().length,0);
 t.replies.sms_job=async()=>({done:1,ok:0,error:'fixture'});await t.poll();assert.equal(t.button('发送短信').disabled,true);
});
test('missing native service fails closed and does not disturb other view modules',async()=>{
 const t=await setup();t.compose();t.replies.sms_job=async()=>{throw Error('no ubus object')};await t.poll();
 assert.equal(t.button('发送短信').disabled,true);assert.equal(t.sends().length,0);
});
test('cancelling confirmation returns unchanged draft without sending',async()=>{
 const t=await setup();t.compose();t.button('发送短信').attrs.click();t.button('返回编辑').attrs.click();
 assert.equal(t.body.value,'测试 fixture');assert.equal(t.number.disabled,false);assert.equal(t.sends().length,0);
});

// A readable LuCI inbox must not hide a broken phone/Agent inbox.
test('phone SMS diagnostic distinguishes native, compatibility, waiting and failure',async()=>{
 for (const [reply,text] of [
  [{available:true,source:'native',state:'ready'},'手机短信源可读（原生短信池）'],
  [{available:true,source:'compat',state:'ready'},'手机短信源可读（兼容快照）'],
  [{available:true,source:'native_json',state:'ready'},'原生 JSON 收件库（SIM 1）'],
  [{available:true,source:'compat',state:'error'},'手机短信链路异常'],
  [{available:true,source:'unavailable',state:'waiting'},'等待首次有效读取'],
  [{available:false},'无法查询']
 ]) {
  const t=await setup(reply);
  assert.ok(t.root.textContent.includes(text));
  assert.ok(t.root.textContent.includes('<script>fixture</script>'));
  assert.equal(t.sends().length,0);
 }
});
test('phone status permission is read-only and arbitrary error text is not rendered',async()=>{
 const acl=JSON.parse(readFileSync(new URL('../packaging/openwrt/root/usr/share/rpcd/acl.d/luci-app-airsim.json',import.meta.url),'utf8'))['luci-app-airsim'];
 assert.ok(acl.read.ubus.airsim.includes('sms_status'));
 assert.ok(!acl.write.ubus.airsim.includes('sms_status'));
 const t=await setup({available:true,source:'<script>private</script>',state:'error',last_poll_error:'secret fixture'});
 assert.ok(!t.root.textContent.includes('secret fixture'));
 assert.ok(!t.root.textContent.includes('<script>private</script>'));
});
