'use strict';
'require baseclass';
'require rpc';
'require poll';

// Use the firmware's PDU service and its AT lock, never the Agent's text-mode SMS API.
var listSMS = rpc.declare({ object: 'airsim', method: 'sms_list', params: ['page'], expect: {}, reject: true, nobatch: true });
var showSMS = rpc.declare({ object: 'mu300dash', method: 'sms_show', params: ['id'], expect: {}, reject: true, nobatch: true });
var sendSMS = rpc.declare({ object: 'airsim', method: 'sms_send', params: ['num', 'text'], expect: {}, reject: true, nobatch: true });
var getJob = rpc.declare({ object: 'mu300dash', method: 'sms_job', expect: {}, reject: true, nobatch: true });

return baseclass.extend({
 render: function(split) {
  var page = 1, pages = 1, loading = false, reading = false, checking = false;
  var sending = false, running = false, jobKnown = false, watching = false, uncertain = false;
  var lastList = 0, signature = '', draft = null, selected = '';
  var list = E('div', { 'class': 'as-sms-list', 'aria-label': '短信列表' });
  var count = E('span', { 'class': 'as-pill as-pill-neutral' }, '读取中');
  var listHint = E('p', { 'class': 'as-caption', role: 'status' }, '正在读取设备短信…');
  var feedback = E('p', { 'class': 'as-feedback', role: 'status', 'aria-live': 'polite', hidden: true });
  var fullTitle = E('strong'), fullBody = E('p', { 'class': 'as-sms-body' });
  var full = E('div', { 'class': 'as-sms-full', hidden: true }, [fullTitle, fullBody]);
  function button(label, action, style) { return E('button', { type: 'button', 'class': 'as-button ' + (style || 'as-secondary'), click: action }, label); }
  function message(kind, value) { feedback.hidden = false; feedback.className = 'as-feedback as-feedback-' + kind; feedback.textContent = value; }
  function valid() {
   return /^\+?[0-9]{1,20}$/.test(number.value.trim()) && body.value.trim().length > 0 && body.value.length <= 70 && !/[\uD800-\uDFFF\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/.test(body.value);
  }
  function update() {
   var locked = sending || running || uncertain || !!draft;
   number.disabled = locked; body.disabled = locked;
   send.disabled = locked || checking || !jobKnown || !valid();
   confirm.disabled = sending || running || checking || uncertain || !jobKnown || !draft;
   cancel.disabled = sending;
   ack.hidden = !uncertain; ack.disabled = sending || running || !jobKnown;
   counter.textContent = body.value.length + ' / 70 字';
   previous.disabled = loading || page <= 1; next.disabled = loading || page >= pages;
   refresh.disabled = loading;
  }
  async function refreshList() {
   if (loading) return;
   loading = true; update();
   try {
    var result = await listSMS(String(page));
    if (!result || result.error || !Array.isArray(result.msgs)) throw new Error('unavailable');
    pages = Math.max(1, Number(result.pages) || 1);
    if (page > pages) { page = pages; signature = ''; return; }
    count.textContent = (Number(result.unread) || 0) + ' 条未读';
    listHint.textContent = '共 ' + (Number(result.total) || 0) + ' 条 · 第 ' + page + ' / ' + pages + ' 页 · 自动刷新';
    var current = JSON.stringify(result.msgs);
    if (current !== signature) {
     list.replaceChildren();
     result.msgs.forEach(function(m) {
      if (!/^[0-9]{1,6}$/.test(String(m.id))) return;
      var read = button(m.status === 'unread' ? '查看全文 · 标记已读' : '查看全文', function() { return openMessage(String(m.id), String(m.peer || '未知号码')); }, 'as-text-button');
      list.appendChild(E('article', { 'class': 'as-sms-item' }, [
       E('div', { 'class': 'as-sms-meta' }, [E('strong', {}, String(m.peer || '未知号码')), E('span', {}, m.dir === 'mo' ? '已发送' : m.status === 'unread' ? '未读' : '已读')]),
       E('p', { 'class': 'as-caption' }, String(m.time || '时间未知')),
       E('p', { 'class': 'as-sms-preview' }, String(m.preview || '（无预览）')), read
      ]));
     });
     if (!result.msgs.length) list.appendChild(E('p', { 'class': 'as-caption' }, '暂无短信。新短信由设备原生服务自动接收。'));
     signature = current;
    }
   } catch (_) { listHint.textContent = '短信读取失败。请确认固件支持 mu300dash 短信接口，并稍后刷新。'; }
   finally { loading = false; lastList = Date.now(); update(); }
  }
  async function openMessage(id, peer) {
   if (reading) return;
   reading = true; selected = id; full.hidden = false; fullTitle.textContent = peer; fullBody.textContent = '正在读取全文…';
   try {
    var result = await showSMS(id);
    if (!result || !result.ok || typeof result.text !== 'string') throw new Error('unavailable');
    var boundary = result.text.indexOf('\n---\n');
    if (boundary < 0) throw new Error('format');
    if (selected === id) fullBody.textContent = result.text.slice(boundary + 5);
    await refreshList();
   } catch (_) { if (selected === id) fullBody.textContent = '未能读取全文，请重试。'; }
   finally { reading = false; }
  }
  async function checkJob() {
   if (checking || sending) return;
   checking = true; update();
   try {
    var result = await getJob();
    if (!result || result.error && !result.done || !(result.none || result.running || result.done)) throw new Error('unavailable');
    jobKnown = true; running = !!result.running;
    if (running) {
     watching = true;
     message('pending', '设备正在发送短信（已等待 ' + (Number(result.elapsed) || 0) + ' 秒）。请勿重复发送。');
    } else if (watching) {
     watching = false;
     if (result.done && result.ok) message('success', '设备发送任务已提交运营商，不代表对方已收到。请核对已发送记录；草稿已保留。');
     else { uncertain = true; message('error', '未取得成功提交回执。请先核对已发送记录和对方收件情况，不要直接重发。'); }
     await refreshList();
    }
   } catch (_) {
    jobKnown = false;
    if (watching || running) uncertain = true;
    message('error', '无法确认设备发送任务状态，已暂停发送。网络恢复后自动查询，不会自动重发。');
   } finally { checking = false; update(); }
  }
  function prepare() {
   if (send.disabled || !valid()) return;
   draft = { number: number.value.trim(), text: body.value };
   confirmationText.textContent = '确认发送至 ' + draft.number + '？将使用 MU300 的 SIM 卡，可能产生短信费用。';
   confirmationBody.textContent = draft.text;
   confirmation.hidden = false; update(); confirm.focus();
  }
  async function submit() {
   if (confirm.disabled || !draft || sending) return;
   sending = true; update(); message('pending', '正在提交短信，请勿重复操作…');
   try {
    var result = await sendSMS(draft.number, draft.text);
    if (result && result.ok && result.started) {
     running = true; watching = true;
     message('pending', '短信已排入设备发送任务，正在等待基带确认。');
    } else if (result && result.busy) {
     running = true; watching = true;
     message('info', '设备已有发送任务，本次短信未提交。请等待任务结束后重新确认。');
    } else {
     uncertain = true;
     message('error', '短信提交未获确认。草稿已保留，请先核对记录和收件情况，再决定是否重试。');
    }
   } catch (_) {
    uncertain = true; jobKnown = false; watching = true;
    message('error', '提交时连接中断，发送结果待确认。不会自动重发，请先核对已发送记录。');
   } finally { sending = false; draft = null; confirmation.hidden = true; update(); }
  }
  var number = E('input', { id: 'as-sms-number', type: 'tel', inputmode: 'tel', autocomplete: 'off', maxlength: 21, 'class': 'as-dial-input', placeholder: '收件号码', input: update });
  var body = E('textarea', { id: 'as-sms-text', rows: 3, maxlength: 70, 'class': 'as-sms-input', placeholder: '输入短信内容', 'aria-describedby': 'as-sms-help', input: update });
  var counter = E('span', { 'class': 'as-caption' });
  var send = button('发送短信', prepare, 'as-primary');
  var confirm = button('确认发送', submit, 'as-primary');
  var cancel = button('返回编辑', function() { if (sending) return; draft = null; confirmation.hidden = true; update(); });
  var ack = button('已核对结果，继续编辑', function() { if (ack.disabled) return; uncertain = false; feedback.hidden = true; update(); });
  var confirmationText = E('p'), confirmationBody = E('p', { 'class': 'as-sms-body' });
  var confirmation = E('div', { 'class': 'as-feedback as-feedback-info', hidden: true }, [confirmationText, confirmationBody, E('div', { 'class': 'as-buttons' }, [confirm, cancel])]);
  var previous = button('上一页', function() { if (previous.disabled) return; page--; signature = ''; return refreshList(); });
  var next = button('下一页', function() { if (next.disabled) return; page++; signature = ''; return refreshList(); });
  var refresh = button('刷新短信', refreshList, 'as-text-button');
  var close = button('收起全文', function() { selected = ''; full.hidden = true; fullBody.textContent = ''; }, 'as-text-button');
  full.appendChild(close);
  var root = E('section', { 'class': 'as-card as-sms', 'aria-label': '短信' }, [
   E('div', { 'class': 'as-card-heading' }, [E('h3', {}, '短信'), count]),
   E('p', { 'class': 'as-copy' }, '查看与发送 MU300 SIM 卡的短信。'),
   refresh, listHint, list, E('div', { 'class': 'as-buttons as-sms-pages' }, [previous, next]), full,
   E('div', { 'class': 'as-sms-compose' }, [
    E('h4', {}, '新短信'), E('label', { 'for': 'as-sms-number', 'class': 'as-field-label' }, '收件号码'), number,
    E('label', { 'for': 'as-sms-text', 'class': 'as-field-label' }, '短信内容'), body, counter,
    E('p', { id: 'as-sms-help', 'class': 'as-caption' }, '本页每条最多 70 字，暂不支持 emoji 和长短信拆分。短信只在本设备管理，不转发至 AirSIM 客户端。'),
    E('div', { 'class': 'as-buttons' }, [send]), confirmation, feedback, ack
   ])
  ]);
  update(); refreshList(); checkJob();
  poll.add(function() {
   return Promise.all([checkJob(), Date.now() - lastList >= 10000 ? refreshList() : Promise.resolve()]);
  }, 2);
  if(split){var compose=root.querySelector('.as-sms-compose');compose.querySelector('h4').remove();compose.classList.add('as-compose-separated');return {inbox:root,compose:E('section',{'class':'as-card as-sms'},[E('h3',{},'发短信'),compose])};}
  return root;
 }
});
