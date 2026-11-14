'use strict';
'require view';
'require view.airsim.ui as ui';
'require view.airsim.extensions as extensions';
return view.extend({render:function(){var p=extensions.render("net");return ui.page("net","上网走哪条路。","打开走外部 Wi-Fi，关闭走蜂窝并开启热点。仅在已识别布局上切换，保留 USB 管理配置。",[p.cards[0],p.cards[1],p.cards[2],E('div',{'class':'as-card-stack'},p.cards.slice(3))],null,p.feedback);},handleSaveApply:null,handleSave:null,handleReset:null});
