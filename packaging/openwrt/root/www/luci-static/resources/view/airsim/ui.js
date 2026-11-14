'use strict';
'require baseclass';
'require view.airsim.appearance as appearance';
// U30 presentation adapter. Layout reference and attribution: docs/UI_ADAPTATION.md.
var pages=[['overview','设备概览'],['connect','连接'],['net','联网'],['call','来电转发'],['sms','短信'],['maintenance','维护'],['settings','外观与设置'],['bluetooth','蓝牙'],['audio','音频']];
function menu(current){
 var label=(pages.find(function(p){return p[0]===current;})||['','AirSIM'])[1];
 var picker=E('select',{'class':'as-menu-select','aria-label':'切换到其它页面',change:function(e){window.location.href=e.target.value;}},pages.map(function(p){return E('option',{value:L.url('admin/services/airsim/'+p[0])},p[1]);}));
 picker.value=L.url('admin/services/airsim/'+current);
 return E('nav',{'class':'as-menu','aria-label':'AirSIM 页面'},[
  E('div',{'class':'as-menu-strip'},pages.map(function(p){var attrs={'class':'as-menu-item'+(p[0]===current?' is-active':''),href:L.url('admin/services/airsim/'+p[0])};if(p[0]===current)attrs['aria-current']='page';return E('a',attrs,p[1]);})),
  E('div',{'class':'as-menu-narrow'},[E('strong',{},label),picker])
 ]);
}
function card(title,children){return E('section',{'class':'as-card as-console-card'},[E('h3',{},title)].concat(children));}
function page(id,title,description,cards,meta,feedback){
 var label=(pages.find(function(p){return p[0]===id;})||['','AirSIM'])[1];
 return appearance.apply(E('div',{'class':'as-root as-console','data-page':id},[
  E('link',{rel:'stylesheet',href:L.resource('view/airsim/overview.css')}),E('link',{rel:'stylesheet',href:L.resource('view/airsim/console.css')}),menu(id),
  E('header',{'class':'as-header'},[E('div',{},[E('div',{'class':'as-eyebrow'},'AirSIM · '+label),E('h2',{},title),E('p',{},description)]),meta||E('span',{'class':'as-version'},'U30 Air')]),
  feedback||E('span'),E('div',{'class':'as-layout'},cards),E('footer',{'class':'as-footer'},'AirSIM for U30 Air · '+label)
 ]));
}
function decorate(root,id){
 root.classList.add('as-console');root.setAttribute('data-page',id);
 root.insertBefore(menu(id),root.firstChild);
 root.appendChild(E('link',{rel:'stylesheet',href:L.resource('view/airsim/console.css')}));
 return appearance.apply(root);
}
return baseclass.extend({page:page,card:card,menu:menu,decorate:decorate,pages:pages});
