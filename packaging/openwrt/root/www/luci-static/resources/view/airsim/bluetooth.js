'use strict';
'require view';
'require view.airsim.ui as ui';
'require view.airsim.extensions as extensions';
return view.extend({render:function(){var p=extensions.render("bluetooth");return ui.page("bluetooth","蓝牙在这儿。","开关、已配对设备与扫描；只管理当前系统已有的控制器。",p.cards,null,p.feedback);},handleSaveApply:null,handleSave:null,handleReset:null});
