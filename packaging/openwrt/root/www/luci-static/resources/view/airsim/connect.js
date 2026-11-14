'use strict';
'require view';
'require view.airsim.manage as manage';
return view.extend({load:function(){return manage.load();},render:function(data){return manage.render(data,'connect');},handleSaveApply:null,handleSave:null,handleReset:null});
