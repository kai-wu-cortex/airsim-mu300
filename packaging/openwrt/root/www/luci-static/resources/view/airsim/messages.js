'use strict';
'require view';
'require view.airsim.ui as ui';
'require poll';
'require view.airsim.sms as sms';
'require view.airsim.device as device';
'require view.airsim.appearance as appearance';
return view.extend({render:function(){
 var busy=false,jobID='',hint=E('p',{'role':'status'}),buttons=[];
 function lock(value){busy=value;buttons.forEach(function(b){b.disabled=value;});}
 async function pollJob(){try{var r=await device.read('maintenance_job');if(!r.job)return;var j=r.job;if(!jobID&&!j.done){jobID=j.id;lock(true);}if(j.id!==jobID)return;if(r.uncertain){hint.textContent='结果未知，请检查设备，不要重复操作。';return;}if(j.done){jobID='';lock(false);hint.textContent=j.result.error||j.result.message||'操作完成，请刷新短信列表。';}}catch(_){if(busy)hint.textContent='连接已断开，任务可能仍在执行。恢复后查询，不会重试删除。';}}
 async function batch(action){if(busy)return;lock(true);try{var p=await device.read('sms_preview');if(!window.confirm((action==='sms_clear'?'永久删除':'标记已读')+'当前 '+p.count+' 条短信？\n请先关闭 AirSIM 服务。删除不可撤销，原生接口不支持原子批量删除，短信到达期间请勿操作。')){lock(false);return;}var r=await device.write(action,{token:p.token});if(r.pending){jobID=r.id;hint.textContent=r.message;}else{hint.textContent=r.message;lock(false);}}catch(e){hint.textContent=e.message;lock(false);}}
 buttons=[device.action('全部标记已读',function(){return batch('sms_mark_all');}),device.action('清空当前短信',function(){return batch('sms_clear');})];poll.add(pollJob,3);pollJob();
 var parts=sms.render(true);parts.inbox.appendChild(E('div',{'class':'as-buttons'},buttons));parts.inbox.appendChild(hint);parts.inbox.appendChild(E('p',{'class':'as-caption'},'批量管理须先关闭 AirSIM 服务。删除会再次确认；结果未知时停止，不自动重试。'));
 return ui.page('sms','短信，一页管好。','查看、标记已读、清空与发送本机 SIM 卡的短信。',[parts.inbox,parts.compose]);
},handleSaveApply:null,handleSave:null,handleReset:null});
