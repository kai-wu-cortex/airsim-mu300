'use strict';
'require baseclass';
'require rpc';
'require poll';
'require view.airsim.ui as ui';
'require view.airsim.device as device';

var getStatus=rpc.declare({object:'airsim',method:'status',expect:{},reject:true});
function valid(s){return s&&Number.isInteger(s.downlink_gain_db)&&s.downlink_gain_db>=0&&s.downlink_gain_db<=12&&Number.isInteger(s.uplink_gain_db)&&s.uplink_gain_db>=0&&s.uplink_gain_db<=12;}
return baseclass.extend({
 load:function(){return Promise.all([device.read('pcm_gain').catch(function(e){return {error:e.message};}),getStatus().catch(function(){return {};})]);},
 render:function(data){
  var state=data&&data[1]||{},known=false,pending=false,uncertain=false,fields={},outputs={};
  var feedback=E('p',{'class':'as-caption','role':'status','aria-live':'polite'});
  var runtime=E('p',{'class':'as-caption','role':'status'});
  function stopped(){return state.running===false&&state.service_alive===false;}
  function refreshControls(){
   var locked=pending||!known||uncertain||!stopped();
   Object.keys(fields).forEach(function(k){fields[k].disabled=locked;});
   save.disabled=reset.disabled=locked;reload.disabled=pending;
   var active=state.audio;
   runtime.textContent=stopped()?'服务已关闭，保存后请在设备概览启动服务。':
    state.running===true||state.service_alive===true?'请先结束通话，在设备概览关闭服务，再修改音量。':'服务状态未知，暂不能保存音量。';
   if(state.running===true&&valid(active))runtime.textContent+=' 当前生效：我听对方 +'+active.downlink_gain_db+' dB，对方听我 +'+active.uplink_gain_db+' dB。';
  }
  function paintValues(s){Object.keys(fields).forEach(function(k){fields[k].value=String(s[k]);outputs[k].textContent=s[k]===0?'0 dB · 原音量':'+'+s[k]+' dB';});}
  function accept(r){
   known=!!(r&&r.ok&&valid(r.settings));
   if(known){paintValues(r.settings);uncertain=false;feedback.textContent='已读取设备保存的音量。';}
   else feedback.textContent=r&&r.error||'无法读取音量设置，不会用默认值覆盖。';
  }
  function slider(key,label,help){
   var id='as-pcm-'+key;
   outputs[key]=E('output',{'for':id},'尚未读取');
   fields[key]=E('input',{id:id,type:'range',min:'0',max:'12',step:'1',value:'0','aria-label':label,input:function(){outputs[key].textContent=fields[key].value==='0'?'0 dB · 原音量':'+'+fields[key].value+' dB';}});
   return E('div',{'class':'as-setting'},[E('label',{'for':id,'class':'as-field-label'},label),E('div',{'class':'as-buttons'},[fields[key],outputs[key]]),E('p',{'class':'as-caption'},help)]);
  }
  async function read(){
   if(pending)return;
   pending=true;refreshControls();
   try{var result=await Promise.all([device.read('pcm_gain'),getStatus().catch(function(){return {};})]);state=result[1];accept(result[0]);}
   catch(e){known=false;feedback.textContent=e.message||'读取失败，请重试。';}
   finally{pending=false;refreshControls();}
  }
  async function submit(){
   if(save.disabled||pending)return;
   var next={downlink_gain_db:Number(fields.downlink_gain_db.value),uplink_gain_db:Number(fields.uplink_gain_db.value)};
   if(!valid(next)){feedback.textContent='请选择 0～12 dB 的整数增益。';return;}
   pending=true;refreshControls();feedback.textContent='正在保存音量…';
   try{
    var r=await device.write('pcm_gain_save',{payload:JSON.stringify(next)});
    if(r.pending||!valid(r.settings)||r.settings.downlink_gain_db!==next.downlink_gain_db||r.settings.uplink_gain_db!==next.uplink_gain_db)throw Error('未收到音量保存确认');
    paintValues(r.settings);feedback.textContent=r.message||'音量已保存，下次启动 AirSIM 服务生效。';
   }catch(e){uncertain=true;feedback.textContent=(e.message||'保存结果未知')+'；请先点击“重新读取”，不会自动重试。';}
   finally{pending=false;refreshControls();}
  }
  var down=slider('downlink_gain_db','我听对方（下行）','AirSIM 听到对方声音小，可先试 +3 dB，仍偏小再试 +6 dB；也影响运营商提示音。');
  var up=slider('uplink_gain_db','对方听我（上行）','只有对方听你的声音小时才调高；通常保持 0 dB。');
  var save=E('button',{type:'button','class':'as-button as-primary',click:submit},'保存音量');
  var reset=E('button',{type:'button','class':'as-button as-secondary',click:function(){if(reset.disabled)return;paintValues({downlink_gain_db:0,uplink_gain_db:0});feedback.textContent='已选择原音量，点击“保存音量”后保存到设备。';}},'恢复原音量');
  var reload=E('button',{type:'button','class':'as-button as-secondary',click:read},'重新读取');
  accept(data&&data[0]);refreshControls();
  poll.add(function(){if(pending)return Promise.resolve();return getStatus().catch(function(){return {};}).then(function(s){if(!pending){state=s;refreshControls();}});},5);
  return ui.card('通话音量',[E('p',{'class':'as-copy'},'保存到设备，对局域网和云端通话生效。'),down,up,E('p',{'class':'as-caption'},'放大带峰值限制，但增益过高仍可能增加底噪或失真。0 dB 保持原音量。'),runtime,E('div',{'class':'as-buttons'},[save,reset,reload]),feedback]);
 }
});
