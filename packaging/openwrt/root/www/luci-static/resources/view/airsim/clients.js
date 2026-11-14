'use strict';
'require view';
'require view.airsim.ui as ui';
'require view.airsim.appearance as appearance';
'require rpc';
'require poll';
var listClients = rpc.declare({object:'airsim',method:'pair_clients',expect:{},reject:true});
var removeClient = rpc.declare({object:'airsim',method:'pair_remove',params:['id'],expect:{},reject:true,nobatch:true});
return view.extend({
 load:function(){return listClients().catch(function(){return {ok:false};});},
 render:function(initial) {
  var state=initial||{},selected='',busy=false;
  var rows=E('div',{'class':'as-management-stack'}), count=E('p',{'class':'as-copy'});
  var feedback=E('p',{'role':'status','aria-live':'polite','class':'as-caption'});
  var confirmation=E('section',{'class':'as-feedback as-feedback-info',hidden:true});
  var question=E('p'), confirm=E('button',{type:'button','class':'as-button as-primary',click:async function(){
   if(busy || !state.ok || !(state.clients||[]).some(function(c){return c.id===selected;})) return;
   var id=selected;busy=true;paint();
   try {
    var result=await removeClient(id);
    if(!result.ok || result.id!==id) throw Error(result.error||'未确认移除结果，请刷新列表。');
    selected='';feedback.textContent='已移除该配对及关联 Watch 授权。重新连接需要配对；云端账户配置需另行管理。';
    state=await listClients();
   } catch(e){feedback.textContent=e.message||'结果未知，请刷新列表；不会自动重试。';}
   finally{busy=false;paint();}
  }},'确认移除');
  var cancel=E('button',{type:'button','class':'as-button as-secondary',click:function(){if(!busy){selected='';paint();}}},'取消');
  confirmation.appendChild(question);confirmation.appendChild(E('div',{'class':'as-buttons'},[confirm,cancel]));
  function paint() {
   var clients=state.ok && Array.isArray(state.clients)?state.clients:[];
   rows.replaceChildren();
   count.textContent=state.ok ? '已保存 '+clients.length+' 条配对记录' : state.error||'无法读取配对列表，请确认 AirSIM 服务已启动。';
   clients.forEach(function(c){
    var id=String(c.id), valid=/^[a-f0-9]{64}$/.test(id);
    var last=c.last_used ? new Date(c.last_used*1000).toLocaleString() : '本次服务启动后暂无活动';
    var button=E('button',{type:'button','class':'as-button as-secondary',click:function(){if(!busy && valid){selected=id;paint();}}},'移除此配对');
    button.disabled=busy||!valid;
    rows.appendChild(E('section',{'class':'as-card'},[
     E('h3',{},'配对记录 · '+id.slice(0,12)),
     E('p',{'class':'as-copy'},'最近活动：'+last),
     E('p',{'class':'as-caption'},c.recent?'最近两分钟有认证活动（不代表持续在线）':'没有近期认证活动'),
     E('details',{},[E('summary',{},'完整指纹'),E('p',{'class':'as-technical'},id)]),button
    ]));
   });
   if(state.ok && !clients.length) rows.appendChild(E('p',{'class':'as-copy'},'尚无本地或隧道配对记录。请到设备概览获取配对码。'));
   var exists=state.ok && clients.some(function(c){return c.id===selected;});
   confirmation.hidden=!selected;
   question.textContent=exists?'确认移除指纹 '+selected.slice(0,12)+' 的配对？请先结束通话，移除会同时撤销其关联 Watch 授权。':'该记录已消失或状态不可用，请刷新后重新选择。';
   confirm.disabled=busy||!exists;cancel.disabled=busy;
  }
  async function refresh(){
   if(busy)return;
   try{state=await listClients();}catch(_){state={ok:false,error:'配对列表读取失败，已暂停移除操作。'};}
   paint();
  }
  poll.add(refresh,15);paint();
  return ui.decorate(E('div',{'class':'as-root'},[
   E('link',{rel:'stylesheet',href:L.resource('view/airsim/overview.css')}),
   E('header',{'class':'as-header'},[E('div',{},[E('h2',{},'配对设备'),count]),E('button',{type:'button','class':'as-button as-secondary',click:refresh},'刷新列表')]),
   E('p',{'class':'as-copy'},'每条记录对应一份配对密钥，同一手机重复配对可能有多条记录。指纹不是密钥，也不是设备名称。'),
   E('p',{'class':'as-caption'},'最近活动仅统计本次服务启动后的局域网、Tailscale 和关联 Watch 认证。纯云端活动不计入；重启服务后重新统计。移除不重启服务、不删除短信或云端账户配置。'),
   feedback,confirmation,rows
  ]),'overview');
 },handleSaveApply:null,handleSave:null,handleReset:null
});
