'use strict';
'require view';
'require view.airsim.ui as ui';
'require view.airsim.extensions as extensions';
return view.extend({render:function(){var p=extensions.render("call");return ui.page("call","来电话，第一时间知道。","通话事件按已保存的渠道转发；保存前确认，失败不自动重试。",p.cards,null,p.feedback);},handleSaveApply:null,handleSave:null,handleReset:null});
