'use strict';
'require view';
'require view.airsim.overview_panel as panel';
// The device generates an isolated baseclass copy of its known MU300 dashboard.
// The firmware's original module and its cellular controls remain unchanged.
return view.extend({
 load:function(){
  return Promise.all([L.require('view.airsim_native_home').then(function(native){return Promise.resolve(native.load()).then(function(data){return {native:native,data:data};});}),panel.load().catch(function(){return {};})]);
 },
 render:function(data){
  this._native=data[0].native;
  var root=this._native.render(data[0].data),hero=root.querySelector('.mud-hero');
  if(hero&&hero.parentNode===root){
   this._panel=panel.render(data[1],true);
   root.insertBefore(this._panel,hero.nextSibling);
  }else{
   root.insertBefore(E('p',{},[E('a',{href:L.url('admin/services/airsim/overview')},'打开 AirSIM 电话网关')]),root.firstChild);
  }
  return root;
 },
 unload:function(){if(this._panel)this._panel.airsimDispose();if(this._native&&this._native.unload)this._native.unload();},
 handleSaveApply:null,handleSave:null,handleReset:null
});
