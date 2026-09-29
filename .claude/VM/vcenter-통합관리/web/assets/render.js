/*
 * render.js - 오른쪽 패널 기본(placeholder) 렌더러.
 * Step 4 는 이 파일을 고치지 말고 별도 파일(예: summary-host.js)을 index.html 에서 render.js 뒤,
 * app.js 앞에 추가해 아래 레지스트리에 덮어쓴다. (app.js 상단 주석의 "확장 지점" 참고)
 */
(function () {
  'use strict';
  var H = window.VCP_HELPERS, esc = H.esc;

  // 유형별 탭 (id, 라벨). 렌더러가 없는 탭은 placeholder 가 표시된다.
  window.VCP_TABS = window.VCP_TABS || {
    vCenter: [['summary', 'Summary'], ['vms', 'VMs'], ['datastores', 'Datastores'], ['networks', 'Networks']],
    Datacenter: [['summary', 'Summary'], ['vms', 'VMs']],
    Folder: [['summary', 'Summary'], ['vms', 'VMs']],
    ClusterComputeResource: [['summary', 'Summary'], ['vms', 'VMs'], ['datastores', 'Datastores'], ['networks', 'Networks']],
    HostSystem: [['summary', 'Summary'], ['vms', 'VMs'], ['datastores', 'Datastores'], ['networks', 'Networks']],
    VirtualMachine: [['summary', 'Summary'], ['datastores', 'Datastores'], ['networks', 'Networks']],
  };

  // Summary 렌더러: VCP_RENDER[type](ctx) -> HTML 문자열 | DOM Node
  window.VCP_RENDER = window.VCP_RENDER || {};
  // Summary 이외 탭 렌더러: VCP_RENDER_TAB['HostSystem:vms'](ctx)
  window.VCP_RENDER_TAB = window.VCP_RENDER_TAB || {};

  function row(k, v) { return '<tr><th>' + esc(k) + '</th><td>' + v + '</td></tr>'; }

  function stateText(o) {
    var s = [];
    if (o.powerState) s.push(o.powerState);
    if (o.connectionState) s.push(o.connectionState);
    if (o.inMaintenance) s.push('maintenance');
    return s.join(' / ');
  }

  // 기본 Summary: 유형/이름/경로/vCenter 등 기본 사실만
  function basicSummary(ctx) {
    var o = ctx.obj, vc = ctx.vc;
    var path = ctx.path.map(function (p) {
      return '<a href="' + H.linkTo(vc.id, p.moref) + '">' + esc(p.obj.name) + '</a>';
    }).join(' <span class="sep">&rsaquo;</span> ');
    var kids = (o.children || []).length;
    var h = '<div class="card"><div class="card-h">기본 정보</div><div class="card-b"><table class="kv">' +
      row('유형', esc(H.TYPE_LABEL[o.type] || o.type)) +
      row('이름', esc(o.name)) +
      row('MoRef', esc(ctx.moref || '-')) +
      row('위치', path || '-') +
      row('vCenter', '<a href="' + H.linkTo(vc.id, '') + '">' + esc(vc.name) + '</a> (' + esc(vc.id) + ')') +
      (stateText(o) ? row('상태', esc(stateText(o))) : '') +
      (o.type === 'VirtualMachine' ? row('IP', esc((o.ips || []).join(', ') || '-')) : '') +
      (kids ? row('하위 객체', kids + '개') : '') +
      row('수집 시각', esc(H.fmtDate(ctx.data.collectedAt))) +
      '</table></div></div>' +
      '<p class="hint">상세 Summary 카드는 다음 단계(Step 4)에서 추가됩니다.</p>';
    return h;
  }

  ['vCenter', 'Datacenter', 'Folder', 'ClusterComputeResource', 'HostSystem', 'VirtualMachine'].forEach(function (t) {
    if (!window.VCP_RENDER[t]) window.VCP_RENDER[t] = basicSummary;
  });

  // 폴백 (렌더러 없는 탭)
  window.VCP_RENDER._tab = function (ctx) {
    return '<p class="hint">"' + esc(ctx.tab) + '" 탭은 다음 단계(Step 4)에서 구현됩니다.</p>';
  };
})();
