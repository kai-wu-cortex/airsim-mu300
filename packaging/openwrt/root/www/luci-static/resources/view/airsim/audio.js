'use strict';
'require view';
'require view.airsim.ui as ui';
'require view.airsim.extensions as extensions';
'require view.airsim.kernel as kernel';
return view.extend({load:function(){return kernel.load();},render:function(data){
 var p=extensions.render('audio'),k=kernel.render(data);k.className='as-kernel-embedded';
 return ui.page('audio','音频、自检、内核。','组件是否匹配、系统缺少什么，以及内核选择，都在这里查看。',p.cards.concat([ui.card('切换内核',[k])]),null,p.feedback);
},handleSaveApply:null,handleSave:null,handleReset:null});
