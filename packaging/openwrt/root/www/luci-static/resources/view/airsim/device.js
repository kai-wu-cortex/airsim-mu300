'use strict';
'require baseclass';
'require rpc';
var read = rpc.declare({object:'airsim',method:'device_read',params:['config'],expect:{},reject:true,nobatch:true});
var write = rpc.declare({object:'airsim',method:'device_write',params:['config'],expect:{},reject:true,nobatch:true});
var reads = new Map();
return baseclass.extend({
 // Share only in-flight identical reads, never cache device state or retry writes.
 read:function(action,fields){var key=JSON.stringify(Object.assign({},fields||{},{action:action}));if(reads.has(key))return reads.get(key);
  var request=Promise.resolve().then(function(){return read(key);}).then(function(r){if(!r||r.error||!r.ok)throw Error(r&&r.error||'状态无法读取');return r;});
  reads.set(key,request);function clear(){if(reads.get(key)===request)reads.delete(key);}request.then(clear,clear);return request;
 },
 write:async function(action,fields){reads.clear();try{var r=await write(JSON.stringify(Object.assign({},fields||{},{action:action,confirm:true})));if(!r||r.error||!r.ok)throw Error(r&&r.error||'结果未确认，请刷新；不会自动重试');return r;}finally{reads.clear();}},
 card:function(title,children){return E('section',{'class':'as-card as-management-stack'},[E('h3',{},title)].concat(children));},
 field:function(label,node){return E('label',{'class':'as-field'},[E('span',{},label),node]);},
 input:function(type,placeholder){return E('input',{type:type||'text',placeholder:placeholder||'',autocomplete:'off','class':'cbi-input-text'});},
 select:function(options){return E('select',{'class':'cbi-input-select'},options.map(function(o){return E('option',{value:o[0]},o[1]);}));},
 // The page owns confirmation. This helper sends exactly once and never retries.
 action:function(label,handler){var button=E('button',{type:'button','class':'as-button as-secondary',click:function(event){if(!button.disabled)return handler(event);}},label);return button;}
});
