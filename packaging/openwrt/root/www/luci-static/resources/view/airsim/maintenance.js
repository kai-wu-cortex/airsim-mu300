'use strict';
'require view';
'require view.airsim.ui as ui';
'require view.airsim.manage as manage';
'require view.airsim.extensions as extensions';
return view.extend({load:function(){return manage.load();},render:function(data){
 var p=extensions.render('maintenance'),m=manage.render(data,'maintenance');
 if(!m.update)return m;
 return ui.page('maintenance','备份、更新、诊断。','保存回退资料，查看更新与设备状态。',[
 p.cards[0],p.cards[1],m.update,ui.card('可选配件',[E('h4',{},'蓝牙配件包'),E('p',{'class':'as-caption'},'只使用当前系统已安装的驱动，不自动安装未知 kmod。'),E('a',{'class':'as-button as-secondary',href:L.url('admin/services/airsim/bluetooth')},'管理蓝牙'),E('h4',{},'音频模块'),E('p',{'class':'as-caption'},'安装前严格检查内核、配置与本地包摘要。'),E('a',{'class':'as-button as-secondary',href:L.url('admin/services/airsim/audio')},'查看音频')]),p.cards[2],m.diagnostics,m.version
 ],null,p.feedback);
},handleSaveApply:null,handleSave:null,handleReset:null});
