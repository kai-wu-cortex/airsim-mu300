'use strict';
'require baseclass';
var key='airsim.device.appearance.v1';
function settings(){try{var v=JSON.parse(localStorage.getItem(key)||'{}');return {theme:['system','light','dark'].includes(v.theme)?v.theme:'system',density:v.density==='compact'?'compact':'comfortable',transparency:v.transparency===undefined?true:v.transparency===true};}catch(_){return {theme:'system',density:'comfortable',transparency:false};}}
function apply(root){var v=settings();if(typeof document!=='undefined'&&document.documentElement){document.documentElement.setAttribute('data-airsim-theme',v.theme);document.documentElement.setAttribute('data-airsim-density',v.density);document.documentElement.setAttribute('data-airsim-transparency',String(v.transparency));}root.setAttribute('data-theme',v.theme);root.setAttribute('data-density',v.density);root.setAttribute('data-transparency',String(v.transparency));return root;}
if(typeof document!=='undefined'&&document.documentElement)apply(document.documentElement);
// Keep the native checkbox as the only state source. Keyboard, labels and
// existing change/disabled handlers continue to work without a second toggle.
function switchField(label,input){
 input.setAttribute('role','switch');input.setAttribute('aria-label',label);
 return E('label',{'class':'as-switch-field'},[input,E('span',{'class':'as-switch-track','aria-hidden':'true'},[E('span',{'class':'as-switch-knob'})]),E('span',{},label)]);
}
function update(changes){
 var v=settings();
 if(changes.theme!==undefined){if(!['system','light','dark'].includes(changes.theme))throw Error('主题无效');v.theme=changes.theme;}
 if(changes.density!==undefined){if(!['comfortable','compact'].includes(changes.density))throw Error('间距无效');v.density=changes.density;}
 if(changes.transparency!==undefined){if(typeof changes.transparency!=='boolean')throw Error('背景设置无效');v.transparency=changes.transparency;}
 localStorage.setItem(key,JSON.stringify(v));document.querySelectorAll('.as-root').forEach(apply);return v;
}
return baseclass.extend({apply:apply,get:settings,update:update,switchField:switchField,controls:function(){
 var v=settings(),theme=E('select',{},[['system','跟随系统'],['light','浅色'],['dark','深色']].map(function(o){return E('option',{value:o[0]},o[1]);})),density=E('select',{},[E('option',{value:'comfortable'},'宽松'),E('option',{value:'compact'},'紧凑')]),glass=E('input',{type:'checkbox'}),status=E('p');
 theme.value=v.theme;density.value=v.density;glass.checked=v.transparency;
 var save=E('button',{type:'button','class':'as-button as-secondary',click:function(){try{localStorage.setItem(key,JSON.stringify({theme:theme.value,density:density.value,transparency:glass.checked}));document.querySelectorAll('.as-root').forEach(apply);status.textContent='已保存到当前浏览器。';}catch(_){status.textContent='当前浏览器不能保存偏好。';}}},'保存外观');
 return E('section',{'class':'as-card as-management-stack'},[E('h3',{},'页面外观'),E('label',{'class':'as-field'},['主题',theme]),E('label',{'class':'as-field'},['间距',density]),switchField('半透明卡片',glass),save,status]);
}});
