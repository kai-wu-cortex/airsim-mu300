'use strict';
'require view';
'require view.airsim.overview_panel as panel';
return view.extend({
 load:function(){return panel.load();},
 render:function(data){this._panel=panel.render(data);return this._panel;},
 unload:function(){if(this._panel)this._panel.airsimDispose();},
 handleSaveApply:null,handleSave:null,handleReset:null
});
