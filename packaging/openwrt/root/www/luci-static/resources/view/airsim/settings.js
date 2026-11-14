'use strict';
'require view';
'require rpc';
'require view.airsim.ui as ui';
'require view.airsim.appearance as appearance';
'require view.airsim.pcm_gain as pcmGain';
var status=rpc.declare({object:'airsim',method:'status',expect:{},reject:true});
return view.extend({load:function(){return Promise.all([status().catch(function(){return {};}),pcmGain.load()]);},render:function(loaded){
 var data=loaded&&loaded[0]||{};
 var state=appearance.get(),message=E('p',{'role':'status','class':'as-caption'});
 function segments(key,options){
  var buttons=[],group=E('div',{'class':'as-segments','role':'group','aria-label':{theme:'模式',density:'界面密度',transparency:'卡片背景'}[key]});
  options.forEach(function(opt){var button=E('button',{type:'button','aria-pressed':String(state[key]===opt[0]),click:function(){
   try{var next={};next[key]=opt[0];appearance.update(next);buttons.forEach(function(b,i){b.setAttribute('aria-pressed',String(options[i][0]===opt[0]));});message.textContent='已保存到当前浏览器。';}catch(_){message.textContent='当前浏览器不能保存偏好，原设置保留。';}
  }},opt[1]);buttons.push(button);group.appendChild(button);});return group;
 }
 return ui.page('settings','按你的习惯来。','通话音量保存到设备；外观与显示偏好仅保存在当前浏览器。',[
  pcmGain.render(loaded&&loaded[1]),
  ui.card('外观',[E('p',{'class':'as-caption'},'跟随系统时，手机与电脑切换深色，这里也会一起变化。'),E('p',{'class':'as-field-label'},'模式'),segments('theme',[['system','跟随系统'],['light','浅色'],['dark','深色']]),message]),
  ui.card('显示',[E('p',{'class':'as-field-label'},'界面密度'),segments('density',[['comfortable','舒适'],['compact','紧凑']]),E('p',{'class':'as-field-label'},'卡片背景'),segments('transparency',[[true,'玻璃模糊'],[false,'纯色背景']]),E('p',{'class':'as-caption'},'老浏览器或低性能设备可以使用纯色背景。')]),
  ui.card('关于',[E('p',{'class':'as-copy'},'AirSIM for U30 Air 将电话网关、配对、联网、短信、维护、蓝牙和音频集中到同一管理界面。'),E('dl',{},[['插件版本',data&&data.version||'未知'],['产品','AirSIM for U30 Air'],['运行内核','到音频页查看实际内核与兼容状态'],['外观设置','仅保存在当前浏览器']].map(function(r){return E('div',{'class':'as-row'},[E('dt',{},r[0]),E('dd',{},r[1])]);})),E('p',{'class':'as-caption'},'界面结构参考 shangjie1998/airsim-f50-openwrt，设备接口与保护由本项目维护。'),E('a',{href:'https://github.com/kai-wu-cortex/airsim-mu300',target:'_blank',rel:'noopener noreferrer'},'项目源码与发布')])
 ]);
},handleSaveApply:null,handleSave:null,handleReset:null});
