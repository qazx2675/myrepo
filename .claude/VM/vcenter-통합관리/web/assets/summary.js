/*
 * summary.js - Step 4: 유형별 Summary 카드 + VMs / Hosts / Datastores / Networks 탭.
 * render.js 뒤, app.js 앞에서 로드되어 VCP_RENDER / VCP_RENDER_TAB / VCP_TABS 를 덮어쓴다.
 */
(function () {
  'use strict';
  var H = window.VCP_HELPERS, esc = H.esc, dash = H.dash;
  var MAX_ROWS = 500;

  window.VCP_TABS = {
    vCenter: [['summary', 'Summary'], ['vms', 'VMs'], ['hosts', 'Hosts'], ['datastores', 'Datastores'], ['networks', 'Networks']],
    Datacenter: [['summary', 'Summary'], ['vms', 'VMs'], ['hosts', 'Hosts']],
    Folder: [['summary', 'Summary'], ['vms', 'VMs'], ['hosts', 'Hosts']],
    ClusterComputeResource: [['summary', 'Summary'], ['vms', 'VMs'], ['hosts', 'Hosts'], ['datastores', 'Datastores'], ['networks', 'Networks']],
    HostSystem: [['summary', 'Summary'], ['vms', 'VMs'], ['datastores', 'Datastores'], ['networks', 'Networks']],
    VirtualMachine: [['summary', 'Summary'], ['datastores', 'Datastores'], ['networks', 'Networks']],
  };

  // ───────────────────────── 공용 ─────────────────────────
  var POWER = { poweredOn: 'Powered On', poweredOff: 'Powered Off', suspended: 'Suspended' };
  var STATUS = { green: 'Normal', yellow: 'Warning', red: 'Alert', gray: 'Unknown' };
  var TOOLS_ST = { guestToolsCurrent: 'Current', guestToolsUnmanaged: 'Guest Managed', guestToolsNeedUpgrade: 'Upgrade available',
    guestToolsSupportedNew: 'Newer than host', guestToolsSupportedOld: 'Supported, old', guestToolsTooOld: 'Too old', guestToolsBlacklisted: 'Blocked' };

  function pct(u, t) { return t > 0 && u >= 0 ? Math.min(100, Math.round(u / t * 100)) : null; }
  function num(n) { return typeof n === 'number' && isFinite(n); }

  function card(title, body, opt) {
    opt = opt || {};
    return '<div class="vcard' + (opt.w2 ? ' w2' : '') + '"><div class="vc-grip"></div><div class="vc-h">' + esc(title) +
      (opt.sub ? '<small>' + opt.sub + '</small>' : '') + '</div><div class="vc-b">' + body + '</div>' +
      (opt.foot ? '<div class="vc-f">' + opt.foot + '</div>' : '') + '</div>';
  }
  function kv(rows) {
    return '<table class="kv2">' + rows.filter(Boolean).map(function (r) {
      return '<tr><th>' + esc(r[0]) + '</th><td>' + r[1] + '</td></tr>';
    }).join('') + '</table>';
  }
  function statusDot(s) { s = s || 'gray'; return '<span class="dot ' + esc(s) + '"></span>'; }

  function svgWrap(inner) { return '<svg class="svg" viewBox="0 0 16 16">' + inner + '</svg>'; }
  var ICO = {
    cpu: '<svg viewBox="0 0 32 32"><rect x="8" y="8" width="16" height="16" rx="1"/><rect x="12" y="12" width="8" height="8"/><path d="M12 4v4M16 4v4M20 4v4M12 24v4M16 24v4M20 24v4M4 12h4M4 16h4M4 20h4M24 12h4M24 16h4M24 20h4"/></svg>',
    mem: '<svg viewBox="0 0 32 32"><rect x="3" y="9" width="26" height="12" rx="1"/><path d="M7 21v3M12 21v3M17 21v3M22 21v3M7 13v4M12 13v4M17 13v4M22 13v4"/></svg>',
    sto: '<svg viewBox="0 0 32 32"><ellipse cx="16" cy="8" rx="10" ry="4"/><path d="M6 8v16c0 2.2 4.5 4 10 4s10-1.800 10-4V8M6 16c0 2.200 4.500 4 10 4s10-1.800 10-4"/></svg>',
  };
  function big(lbl, icon, val, unit, sub) {
    return '<div class="use-lbl">' + esc(lbl) + '</div><div class="use-big">' + icon + '<b>' + esc(val) + '</b><span>' + esc(unit) + '</span></div>' +
      (sub ? '<div class="sub">' + sub + '</div>' : '');
  }

  // VM 이미지 (Summary 카드 좌측 큰 아이콘)
  var VM_IMG = '<svg viewBox="0 0 70 70" fill="none" stroke="#1d7fb0" stroke-width="2.4"><rect x="5" y="26" width="34" height="32" fill="#e3f1f8"/><path d="M14 26V10h48v36H39" fill="#eef7fb"/><rect x="14" y="10" width="48" height="36" fill="none"/><path d="M13 38h18"/></svg>';

  function osKind(o) {
    var s = ((o.guestId || '') + ' ' + (o.guestFullName || '')).toLowerCase();
    if (/win/.test(s)) return 'win';
    if (/linux|centos|rhel|red hat|ubuntu|debian|suse|sles|rocky|alma|fedora|photon|oracle|other 3|other 2|other 4|other 5/.test(s)) return 'linux';
    return 'other';
  }
  function osIcon(o, sz) {
    var k = osKind(o), st = sz ? ' style="width:' + sz + 'px;height:' + sz + 'px"' : '';
    if (k === 'win') return '<svg viewBox="0 0 24 24"' + st + ' fill="#1d7fb0"><path d="M3 5.500l7.500-1v7H3zM11.500 4.300L21 3v8.500h-9.500zM3 12.500h7.500v7L3 18.500zM11.500 12.500H21V21l-9.500-1.300z"/></svg>';
    if (k === 'linux') return '<svg viewBox="0 0 24 24"' + st + '><ellipse cx="12" cy="14" rx="6.500" ry="8" fill="#222"/><ellipse cx="12" cy="16" rx="3.800" ry="5.500" fill="#fff"/><circle cx="10" cy="7.500" r="1" fill="#fff"/><circle cx="14" cy="7.500" r="1" fill="#fff"/><path d="M10.500 9.500h3l-1.500 1.500z" fill="#f2a900"/><ellipse cx="8" cy="21.500" rx="2.500" ry="1.200" fill="#f2a900"/><ellipse cx="16" cy="21.500" rx="2.500" ry="1.200" fill="#f2a900"/></svg>';
    return '<svg viewBox="0 0 24 24"' + st + ' fill="none" stroke="#565656" stroke-width="1.400"><rect x="3" y="4" width="18" height="12" rx="1"/><path d="M8 20h8M12 16v4"/></svg>';
  }

  function powerHtml(o) {
    var t = POWER[o.powerState] || dash(o.powerState), c = o.powerState === 'poweredOn' ? 'st-on' : 'st-off';
    if (o.connectionState && o.connectionState !== 'connected') return '<span class="st-err">' + esc(o.connectionState) + '</span> <span class="sub">(' + esc(t) + ')</span>';
    return '<span class="' + c + '">' + esc(t) + '</span>';
  }
  function stateText(o) {
    if (o.type === 'HostSystem') {
      var a = [];
      a.push(o.connectionState || '-');
      if (o.powerState && o.powerState !== 'unknown') a.push(o.powerState);
      if (o.inMaintenance) a.push('maintenance');
      return a.join(' / ');
    }
    if (o.connectionState && o.connectionState !== 'connected') return o.connectionState;
    return POWER[o.powerState] || o.powerState || '-';
  }
  function statusText(s) { return STATUS[s] || s || '-'; }

  function dsName(d, id) { var x = d.datastores && d.datastores[id]; return x ? x.name : id; }
  function netName(d, id) { var x = d.networks && d.networks[id]; return x ? x.name : id; }

  // 하위 객체 수집 (자기 자신 제외). base: ctx.obj.children 또는 root children
  function descend(ctx) {
    var d = ctx.data, res = { vms: [], hosts: [], clusters: [] }, seen = {};
    var stack = ctx.moref ? (ctx.obj.children || []).slice() : (d.rootChildren || []).slice();
    while (stack.length) {
      var id = stack.pop();
      if (seen[id]) continue;
      seen[id] = 1;
      var o = d.objects[id];
      if (!o) continue;
      if (o.type === 'VirtualMachine') res.vms.push({ id: id, o: o });
      else if (o.type === 'HostSystem') res.hosts.push({ id: id, o: o });
      else if (o.type === 'ClusterComputeResource') res.clusters.push({ id: id, o: o });
      if (o.children) for (var i = 0; i < o.children.length; i++) stack.push(o.children[i]);
    }
    return res;
  }
  function byName(a, b) { return a.o.name < b.o.name ? -1 : a.o.name > b.o.name ? 1 : 0; }

  function clusterOf(d, moref) {
    var m = d._p[moref], g = 0;
    while (m && g++ < 64) { var o = d.objects[m]; if (o && o.type === 'ClusterComputeResource') return { id: m, o: o }; m = d._p[m]; }
    return null;
  }

  function dsTotals(d, ids) {
    var cap = 0, free = 0, n = 0;
    (ids || []).forEach(function (id) { var x = d.datastores[id]; if (x && num(x.capacity)) { cap += x.capacity; free += num(x.free) ? x.free : 0; n++; } });
    return { cap: cap, used: cap - free, n: n };
  }

  // Capacity and Usage 카드 (막대 3개)
  function barRow(label, used, total, fmt) {
    var p = pct(used, total);
    if (p === null) return '<div class="bar-row"><div class="bar-top"><b>' + esc(label) + '</b></div><div class="sub">-</div></div>';
    var cls = p >= 90 ? 'crit' : p >= 75 ? 'warn' : '';
    return '<div class="bar-row"><div class="bar-top"><b>' + esc(label) + '</b><span class="sub">' + esc(fmt(total)) + ' 중</span></div>' +
      '<div class="bar-big">' + esc(fmt(used)) + ' <small>사용 (' + p + '%)</small></div>' +
      '<div class="bar"><i class="' + cls + '" style="width:' + p + '%"></i></div>' +
      '<div class="bar-sub"><span>여유: ' + esc(fmt(total - used)) + '</span><span>용량: ' + esc(fmt(total)) + '</span></div></div>';
  }
  function capCard(ctx, cpuU, cpuT, memU, memT, ds) {
    var d = ctx.data;
    var body = '<div class="bars">' + barRow('CPU', cpuU, cpuT, H.fmtMHz) + barRow('Memory', memU, memT, H.fmtMB) +
      barRow('Storage', ds.used, ds.cap, H.fmtBytes) + '</div>';
    return card('Capacity and Usage', body, { sub: '마지막 수집: ' + esc(H.fmtDate(d.collectedAt)) });
  }

  function openFoot(ctx) {
    return '<a href="' + esc(H.openLink(ctx.vc, ctx.moref, ctx.moref ? ctx.obj : null)) + '">vCenter에서 열기</a>';
  }

  function relLink(ctx, moref, obj) {
    return '<div class="rel-i">' + H.icon(obj) + '<a href="' + H.linkTo(ctx.vcId, moref, 'summary') + '">' + esc(obj.name) + '</a></div>';
  }
  function relPlain(icoName, name) {
    return '<div class="rel-i"><span class="ico"><svg class="svg"><use href="#i-' + icoName + '"/></svg></span>' + esc(name) + '</div>';
  }
  function relGroup(title, items) {
    return '<div class="rel-h">' + esc(title) + '</div>' + (items.length ? items.join('') : '<div class="rel-i"><span class="none">-</span></div>');
  }
  function ancestorLinks(ctx) {
    return ctx.path.map(function (p) {
      return relLink(ctx, p.moref, p.obj);
    }).join('');
  }
  function dsNetGroups(ctx, o) {
    var d = ctx.data, out = '';
    var nets = (o.networks || []).map(function (id) { return relPlain('navnet', netName(d, id)); });
    var dss = (o.datastores || []).map(function (id) { return relPlain('navds', dsName(d, id)); });
    return relGroup('Networks', nets) + relGroup('Storage', dss);
  }

  // ───────────────────────── VM Summary ─────────────────────────
  function vmSummary(ctx) {
    var o = ctx.obj, d = ctx.data, vc = ctx.vc;
    var direct = H.deepLink(vc, ctx.moref, o);
    var guest = '<div class="guest-body"><div class="guest-screen">' + osIcon(o, 56) + '<span>' + powerHtml(o) + '</span></div>' +
      '<div class="guest-os-name">' + esc(dash(o.guestFullName)) + '</div>' +
      '<a class="btn pri block" href="' + esc(H.openLink(vc, ctx.moref, o)) + '">vCenter에서 열기</a>' +
      '<a class="lnk-small" href="' + esc(direct) + '" target="_blank" rel="noopener">브라우저로 직접 열기</a></div>';

    var tools;
    if (o.toolsRunning === 'guestToolsRunning') {
      tools = 'Running' + (o.toolsVersion ? ', version:' + esc(o.toolsVersion) : '') + (TOOLS_ST[o.toolsVersionStatus] ? ' (' + esc(TOOLS_ST[o.toolsVersionStatus]) + ')' : '');
    } else if (o.toolsVersionStatus === 'guestToolsNotInstalled') tools = 'Not installed';
    else tools = o.toolsRunning ? 'Not running' : '-';
    var ips = o.ips || [];
    var details = '<div class="vd-wrap"><div class="vd-img">' + VM_IMG + '</div>' + kv([
      ['Power Status', powerHtml(o)],
      ['Guest OS', '<span class="ico-big">' + osIcon(o, 18) + '</span> ' + esc(dash(o.guestFullName))],
      ['VMware Tools', tools],
      ['DNS Name', esc(dash(o.guestHostName))],
      ['IP Addresses (' + ips.length + ')', ips.length ? ips.map(esc).join('<br>') : '-'],
      ['Compatibility', esc(dash(o.hwVersion))],
      ['Status', statusDot(o.overallStatus) + esc(statusText(o.overallStatus))],
      o.template ? ['Template', 'Yes'] : null,
      o.annotation ? ['Notes', '<span class="notes-t">' + esc(o.annotation) + '</span>'] : null,
    ]) + '</div>';

    var committed = num(o.storageCommitted) ? o.storageCommitted : null;
    var usage = big('CPU', ICO.cpu, num(o.cpuUsageMhz) ? Math.round(o.cpuUsageMhz) : '-', 'MHz used') +
      big('Memory', ICO.mem, num(o.guestMemUsageMB) ? Math.round(o.guestMemUsageMB) : '-', 'MB used') +
      big('Storage', ICO.sto, committed === null ? '-' : H.fmtBytes(committed), 'used');

    var disks = o.disks || [], nics = o.nics || [];
    var hw = kv([
      ['CPU', esc((o.numCpu || '-') + ' CPU(s), ' + (num(o.cpuUsageMhz) ? Math.round(o.cpuUsageMhz) : 0) + ' MHz used')],
      ['Memory', esc(H.fmtMB(o.memMB) + ' 할당, ' + (num(o.guestMemUsageMB) ? H.fmtMB(o.guestMemUsageMB) : '-') + ' 게스트 사용 / 호스트 ' + (num(o.hostMemUsageMB) ? H.fmtMB(o.hostMemUsageMB) : '-'))],
    ].concat(disks.map(function (k, i) {
      return [k.label || ('Hard disk ' + (i + 1)), esc(H.fmtBytes(k.capacityBytes) + ' | ' + (k.thin ? 'Thin Provision' : 'Thick Provision')) +
        '<br><span class="sub">' + esc(dsName(d, k.datastore)) + (k.file ? ' &nbsp;' + esc(k.file) : '') + '</span>'];
    })).concat(nics.map(function (n, i) {
      return [n.label || ('Network adapter ' + (i + 1)), esc(dash(n.network)) + ' <span class="' + (n.connected ? 'st-on' : 'st-off') + '">(' + (n.connected ? 'Connected' : 'Disconnected') + ')</span>' +
        '<br><span class="sub">' + esc(dash(n.mac)) + '</span>'];
    })));

    var host = o.host && d.objects[o.host], cl = o.host ? clusterOf(d, o.host) : null;
    var rel = relGroup('Host', host ? [relLink(ctx, o.host, host)] : []);
    if (cl) rel += relGroup('Cluster', [relLink(ctx, cl.id, cl.o)]);
    rel += dsNetGroups(ctx, o);

    return '<div class="cgrid">' +
      card('Guest OS', guest) +
      card('Virtual Machine Details', details, { w2: true }) +
      card('Usage', usage, { sub: '마지막 수집: ' + esc(H.fmtDate(d.collectedAt)), foot: '<a href="' + esc(H.openLink(vc, ctx.moref, o)) + '">View Stats (vCenter)</a>' }) +
      card('VM Hardware', hw, { w2: true }) +
      card('Related Objects', rel) +
      '</div>';
  }

  // ───────────────────────── Host / Cluster / 집계 Summary ─────────────────────────
  function hostSummary(ctx) {
    var o = ctx.obj, d = ctx.data;
    var vms = (o.children || []).map(function (id) { return d.objects[id]; }).filter(function (x) { return x && x.type === 'VirtualMachine'; });
    var on = vms.filter(function (v) { return v.powerState === 'poweredOn'; }).length;
    var details = kv([
      ['Hypervisor', esc(dash(o.esxFullName))],
      ['Model', esc([o.vendor, o.model].filter(Boolean).join(' ') || '-')],
      ['Processor Type', esc(dash(o.cpuModel))],
      ['Logical Processors', esc(dash(o.cpuThreads))],
      ['Sockets / Cores', esc(dash(o.cpuSockets) + ' / ' + dash(o.cpuCores))],
      ['Virtual Machines', esc(vms.length + ' (실행 중 ' + on + ')')],
      ['State', esc(stateText(o))],
      ['Status', statusDot(o.overallStatus) + esc(statusText(o.overallStatus))],
      ['Uptime', esc(o.uptimeSec != null ? H.fmtUptime(o.uptimeSec) : '-')],
      ['Boot Time', esc(H.fmtDate(o.bootTime))],
    ]);
    var hw = kv([
      ['Manufacturer', esc(dash(o.vendor))], ['Model', esc(dash(o.model))], ['CPU', esc(dash(o.cpuModel))],
      ['CPU 합계', esc(H.fmtMHz(o.cpuMhzTotal))], ['Memory', esc(H.fmtMB(o.memTotalMB))],
    ]);
    var rel = relGroup('Parents', ctx.path.map(function (p) { return relLink(ctx, p.moref, p.obj); })) + dsNetGroups(ctx, o);
    return '<div class="cgrid">' + card('Host Details', details) +
      capCard(ctx, o.cpuMhzUsed, o.cpuMhzTotal, o.memUsedMB, o.memTotalMB, dsTotals(d, o.datastores)) +
      card('Hardware', hw) + card('Related Objects', rel, { foot: openFoot(ctx) }) + '</div>';
  }

  function clusterSummary(ctx) {
    var o = ctx.obj, d = ctx.data, sub = descend(ctx);
    var on = sub.vms.filter(function (v) { return v.o.powerState === 'poweredOn'; }).length;
    var details = kv([
      ['Hosts', esc(dash(o.numHosts != null ? o.numHosts : sub.hosts.length))],
      ['Effective Hosts', esc(dash(o.numEffectiveHosts))],
      ['Virtual Machines', esc(sub.vms.length + ' (실행 중 ' + on + ')')],
      ['vSphere DRS', o.drsEnabled ? '<span class="st-on">On</span>' : '<span class="st-off">Off</span>'],
      ['vSphere HA', o.haEnabled ? '<span class="st-on">On</span>' : '<span class="st-off">Off</span>'],
      ['Status', statusDot(o.overallStatus) + esc(statusText(o.overallStatus))],
    ]);
    var rel = relGroup('Parents', ctx.path.map(function (p) { return relLink(ctx, p.moref, p.obj); })) + relGroup('Hosts', sub.hosts.sort(byName).slice(0, 20).map(function (h) { return relLink(ctx, h.id, h.o); })) +
      (sub.hosts.length > 20 ? '<div class="sub">외 ' + (sub.hosts.length - 20) + '개 (Hosts 탭)</div>' : '') + dsNetGroups(ctx, o);
    return '<div class="cgrid">' + card('Cluster Details', details, { foot: openFoot(ctx) }) +
      capCard(ctx, o.usedCpuMhz, o.totalCpuMhz, o.usedMemMB, o.totalMemMB, dsTotals(d, o.datastores)) +
      card('Related Objects', rel) + '</div>';
  }

  // Datacenter / Folder / vCenter 루트
  function aggSummary(ctx) {
    var o = ctx.obj, d = ctx.data, vc = ctx.vc, root = !ctx.moref, sub = descend(ctx);
    var on = sub.vms.filter(function (v) { return v.o.powerState === 'poweredOn'; }).length;
    var cpuT = 0, cpuU = 0, memT = 0, memU = 0, dsIds = {};
    sub.hosts.forEach(function (h) {
      cpuT += h.o.cpuMhzTotal || 0; cpuU += h.o.cpuMhzUsed || 0; memT += h.o.memTotalMB || 0; memU += h.o.memUsedMB || 0;
      (h.o.datastores || []).forEach(function (x) { dsIds[x] = 1; });
    });
    var dsList = root ? Object.keys(d.datastores || {}) : Object.keys(dsIds);
    var rows = [];
    if (root) {
      rows.push(['Version', esc(dash(vc.version) + (vc.build ? ' (build ' + vc.build + ')' : ''))]);
      rows.push(['URL', '<a href="' + esc(H.deepLink(vc)) + '" target="_blank" rel="noopener">' + esc(dash(vc.url)) + '</a>']);
      rows.push(['Instance UUID', esc(dash(vc.instanceUuid))]);
      rows.push(['수집 상태', vc.status === 'fail' ? '<span class="b-fail">실패</span>' + (vc.error ? ' - ' + esc(vc.error) : '') : '<span class="b-ok">정상</span>']);
      rows.push(['마지막 수집', esc(H.fmtDate(vc.collectedAt || d.collectedAt)) + ' <span class="sub">(' + esc(H.fmtAgo(vc.collectedAt || d.collectedAt)) + ')</span>']);
    } else {
      rows.push(['유형', esc(H.TYPE_LABEL[o.type] || o.type)]);
    }
    rows.push(['Clusters', esc(sub.clusters.length)]);
    rows.push(['Hosts', esc(sub.hosts.length)]);
    rows.push(['Virtual Machines', esc(sub.vms.length + ' (실행 중 ' + on + ')')]);
    var details = kv(rows);
    var out = '<div class="cgrid">' + card(root ? 'vCenter Details' : (o.type === 'Datacenter' ? 'Datacenter Details' : 'Folder Details'), details, { foot: openFoot(ctx) }) +
      capCard(ctx, cpuU, cpuT, memU, memT, dsTotals(d, dsList));
    if (!root) out += card('Related Objects', relGroup('Parents', ctx.path.map(function (p) { return relLink(ctx, p.moref, p.obj); })));
    return out + '</div>';
  }

  // ───────────────────────── 표(정렬 / 필터 / 페이징 / CSV) ─────────────────────────
  // cols: [{label, get(row)->정렬값, cell(row)->HTML, csv(row)->문자열, num?}]
  function makeTable(ctx, cols, rows, opt) {
    opt = opt || {};
    var st = { col: opt.sort || 0, dir: 1, q: '', limit: MAX_ROWS };
    rows.forEach(function (r) { r._s = cols.map(function (c) { return String(c.csv(r)); }).join('\u0001').toLowerCase(); });
    var root = document.createElement('div');
    root.innerHTML = '<div class="tbl-bar"><input type="search" placeholder="필터 (이름, IP 등)" aria-label="필터"><span class="cnt"></span>' +
      (opt.csvName ? '<button class="btn" type="button">CSV 내보내기</button>' : '') + '</div>' +
      '<table class="grid"><thead><tr>' + cols.map(function (c, i) { return '<th class="srt' + (c.num ? ' num' : '') + '" data-i="' + i + '">' + esc(c.label) + '<span class="ar"></span></th>'; }).join('') +
      '</tr></thead><tbody></tbody></table><div class="morebar"></div>';
    var inp = root.querySelector('input'), cnt = root.querySelector('.cnt'), tb = root.querySelector('tbody'), more = root.querySelector('.morebar');
    var ths = root.querySelectorAll('th.srt');

    function view() {
      var q = st.q.toLowerCase(), c = cols[st.col], d = st.dir;
      var list = q ? rows.filter(function (r) { return r._s.indexOf(q) >= 0; }) : rows.slice();
      list.sort(function (a, b) {
        var x = c.get(a), y = c.get(b), xn = x == null || x === '', yn = y == null || y === '';
        if (xn || yn) return xn && yn ? 0 : xn ? 1 : -1;
        if (typeof x === 'number' && typeof y === 'number') return (x - y) * d;
        return String(x).localeCompare(String(y), 'ko', { numeric: true, sensitivity: 'base' }) * d;
      });
      return list;
    }
    function draw() {
      var list = view(), shown = list.slice(0, st.limit);
      tb.innerHTML = shown.map(function (r) {
        return '<tr>' + cols.map(function (c) { return '<td' + (c.num ? ' class="num"' : '') + '>' + c.cell(r) + '</td>'; }).join('') + '</tr>';
      }).join('') || '<tr><td colspan="' + cols.length + '" class="hint">표시할 항목이 없습니다.</td></tr>';
      cnt.textContent = (st.q ? list.length + ' / ' : '') + rows.length + '개' + (list.length > shown.length ? ' (' + shown.length + '개 표시)' : '');
      more.innerHTML = list.length > shown.length ? '<button class="btn" type="button">더 보기 (+' + Math.min(MAX_ROWS, list.length - shown.length) + ')</button>' : '';
      for (var i = 0; i < ths.length; i++) ths[i].querySelector('.ar').textContent = i === st.col ? (st.dir > 0 ? '▲' : '▼') : '';
      root._list = list;
    }
    root.querySelector('thead').addEventListener('click', function (e) {
      var th = e.target.closest('th.srt');
      if (!th) return;
      var i = +th.getAttribute('data-i');
      if (i === st.col) st.dir = -st.dir; else { st.col = i; st.dir = 1; }
      draw();
    });
    inp.addEventListener('input', function () { st.q = inp.value.trim(); st.limit = MAX_ROWS; draw(); });
    more.addEventListener('click', function (e) { if (e.target.closest('button')) { st.limit += MAX_ROWS; draw(); } });
    var btn = root.querySelector('.tbl-bar .btn');
    if (btn) btn.addEventListener('click', function () {
      var list = root._list || view();
      var lines = [cols.map(function (c) { return csvCell(c.label); }).join(',')];
      list.forEach(function (r) { lines.push(cols.map(function (c) { return csvCell(c.csv(r)); }).join(',')); });
      var blob = new Blob(['﻿' + lines.join('\r\n') + '\r\n'], { type: 'text/csv;charset=utf-8' });
      var url = URL.createObjectURL(blob), a = document.createElement('a');
      a.href = url; a.download = opt.csvName; document.body.appendChild(a); a.click(); a.remove();
      setTimeout(function () { URL.revokeObjectURL(url); }, 4000);
    });
    draw();
    return root;
  }
  function csvCell(v) {
    var s = String(v == null ? '' : v);
    if (/^[=+\-@]/.test(s) && !/^-?\d+(\.\d+)?$/.test(s)) s = "'" + s; // CSV 수식 주입 방지
    return /[",\r\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  }
  function fname(ctx, tab) {
    return (ctx.vc.name + '_' + (ctx.moref ? ctx.obj.name : 'root') + '_' + tab).replace(/[\\\/:*?"<>|\s]+/g, '_') + '.csv';
  }
  function link(ctx, id, o) {
    return H.icon(o) + '<a href="' + H.linkTo(ctx.vcId, id, 'summary') + '">' + esc(o.name) + '</a>';
  }
  function stCell(s) { return statusDot(s) + esc(statusText(s)); }
  function mb(v) { return num(v) ? Math.round(v) : null; }
  function bytes(v) { return num(v) ? v : null; }

  // ───────────────────────── VMs 탭 ─────────────────────────
  function vmsTab(ctx) {
    var d = ctx.data, vms = descend(ctx).vms;
    var cols = [
      { label: 'Name', get: function (r) { return r.o.name; }, cell: function (r) { return link(ctx, r.id, r.o); }, csv: function (r) { return r.o.name; } },
      { label: 'State', get: function (r) { return stateText(r.o); }, cell: function (r) { return esc(stateText(r.o)); }, csv: function (r) { return stateText(r.o); } },
      { label: 'Status', get: function (r) { return statusText(r.o.overallStatus); }, cell: function (r) { return stCell(r.o.overallStatus); }, csv: function (r) { return statusText(r.o.overallStatus); } },
      { label: 'Provisioned Space', num: 1, get: function (r) { return num(r.o.storageCommitted) ? r.o.storageCommitted + (r.o.storageUncommitted || 0) : null; },
        cell: function (r) { return num(r.o.storageCommitted) ? esc(H.fmtBytes(r.o.storageCommitted + (r.o.storageUncommitted || 0))) : '-'; },
        csv: function (r) { return num(r.o.storageCommitted) ? r.o.storageCommitted + (r.o.storageUncommitted || 0) : ''; } },
      { label: 'Used Space', num: 1, get: function (r) { return bytes(r.o.storageCommitted); }, cell: function (r) { return esc(H.fmtBytes(r.o.storageCommitted)); }, csv: function (r) { return bytes(r.o.storageCommitted) == null ? '' : r.o.storageCommitted; } },
      { label: 'Host CPU (MHz)', num: 1, get: function (r) { return mb(r.o.cpuUsageMhz); }, cell: function (r) { return num(r.o.cpuUsageMhz) ? esc(Math.round(r.o.cpuUsageMhz)) : '-'; }, csv: function (r) { return mb(r.o.cpuUsageMhz) == null ? '' : mb(r.o.cpuUsageMhz); } },
      { label: 'Host Mem (MB)', num: 1, get: function (r) { return mb(r.o.hostMemUsageMB); }, cell: function (r) { return num(r.o.hostMemUsageMB) ? esc(Math.round(r.o.hostMemUsageMB)) : '-'; }, csv: function (r) { return mb(r.o.hostMemUsageMB) == null ? '' : mb(r.o.hostMemUsageMB); } },
      { label: 'Guest OS', get: function (r) { return r.o.guestFullName; }, cell: function (r) { return esc(dash(r.o.guestFullName)); }, csv: function (r) { return r.o.guestFullName || ''; } },
      { label: 'IP', get: function (r) { return (r.o.ips || [])[0]; }, cell: function (r) { return esc((r.o.ips || []).join(', ') || '-'); }, csv: function (r) { return (r.o.ips || []).join(' '); } },
    ];
    return makeTable(ctx, cols, vms, { csvName: fname(ctx, 'vms') });
  }

  // ───────────────────────── Hosts 탭 ─────────────────────────
  function hostsTab(ctx) {
    var d = ctx.data, hosts = descend(ctx).hosts;
    hosts.forEach(function (r) { var c = clusterOf(d, r.id); r.cl = c ? c.o.name : ''; });
    function cpuP(r) { return pct(r.o.cpuMhzUsed, r.o.cpuMhzTotal); }
    function memP(r) { return pct(r.o.memUsedMB, r.o.memTotalMB); }
    function pc(v) { return v == null ? '-' : v + '%'; }
    var cols = [
      { label: 'Name', get: function (r) { return r.o.name; }, cell: function (r) { return link(ctx, r.id, r.o); }, csv: function (r) { return r.o.name; } },
      { label: 'State', get: function (r) { return stateText(r.o); }, cell: function (r) { return esc(stateText(r.o)); }, csv: function (r) { return stateText(r.o); } },
      { label: 'Status', get: function (r) { return statusText(r.o.overallStatus); }, cell: function (r) { return stCell(r.o.overallStatus); }, csv: function (r) { return statusText(r.o.overallStatus); } },
      { label: 'Cluster', get: function (r) { return r.cl; }, cell: function (r) { return esc(dash(r.cl)); }, csv: function (r) { return r.cl; } },
      { label: 'CPU %', num: 1, get: cpuP, cell: function (r) { return pc(cpuP(r)); }, csv: function (r) { return cpuP(r) == null ? '' : cpuP(r); } },
      { label: 'Mem %', num: 1, get: memP, cell: function (r) { return pc(memP(r)); }, csv: function (r) { return memP(r) == null ? '' : memP(r); } },
      { label: 'Uptime', num: 1, get: function (r) { return num(r.o.uptimeSec) ? r.o.uptimeSec : null; }, cell: function (r) { return esc(r.o.uptimeSec != null ? H.fmtUptime(r.o.uptimeSec) : '-'); }, csv: function (r) { return r.o.uptimeSec == null ? '' : H.fmtUptime(r.o.uptimeSec); } },
      { label: 'ESXi Version', get: function (r) { return r.o.esxVersion; }, cell: function (r) { return esc(dash(r.o.esxVersion)); }, csv: function (r) { return r.o.esxVersion || ''; } },
    ];
    return makeTable(ctx, cols, hosts, { csvName: fname(ctx, 'hosts') });
  }

  // ───────────────────────── Datastores / Networks 탭 ─────────────────────────
  function idsFor(ctx, key) {
    var d = ctx.data;
    if (!ctx.moref) return Object.keys(d[key] || {});
    return ctx.obj[key] || [];
  }
  function datastoresTab(ctx) {
    var d = ctx.data;
    var rows = idsFor(ctx, 'datastores').map(function (id) { return { id: id, x: d.datastores[id] || { name: id } }; });
    var used = function (r) { return num(r.x.capacity) ? r.x.capacity - (num(r.x.free) ? r.x.free : 0) : null; };
    var up = function (r) { return pct(used(r), r.x.capacity); };
    var cols = [
      { label: 'Name', get: function (r) { return r.x.name; }, cell: function (r) { return '<span class="ico"><svg class="svg"><use href="#i-navds"/></svg></span>' + esc(r.x.name); }, csv: function (r) { return r.x.name; } },
      { label: 'Type', get: function (r) { return r.x.type; }, cell: function (r) { return esc(dash(r.x.type)); }, csv: function (r) { return r.x.type || ''; } },
      { label: 'Capacity', num: 1, get: function (r) { return bytes(r.x.capacity); }, cell: function (r) { return esc(H.fmtBytes(r.x.capacity)); }, csv: function (r) { return r.x.capacity == null ? '' : r.x.capacity; } },
      { label: 'Free', num: 1, get: function (r) { return bytes(r.x.free); }, cell: function (r) { return esc(H.fmtBytes(r.x.free)); }, csv: function (r) { return r.x.free == null ? '' : r.x.free; } },
      { label: '% Used', num: 1, get: up, cell: function (r) { var p = up(r); return p == null ? '-' : '<span style="display:inline-block;width:70px;vertical-align:middle" class="bar"><i class="' + (p >= 90 ? 'crit' : p >= 75 ? 'warn' : '') + '" style="width:' + p + '%"></i></span> ' + p + '%'; }, csv: function (r) { return up(r) == null ? '' : up(r); } },
    ];
    return makeTable(ctx, cols, rows, {});
  }
  function networksTab(ctx) {
    var d = ctx.data;
    var rows = idsFor(ctx, 'networks').map(function (id) { return { id: id, x: d.networks[id] || { name: id } }; });
    var cols = [
      { label: 'Name', get: function (r) { return r.x.name; }, cell: function (r) { return '<span class="ico"><svg class="svg"><use href="#i-navnet"/></svg></span>' + esc(r.x.name); }, csv: function (r) { return r.x.name; } },
      { label: 'MoRef', get: function (r) { return r.id; }, cell: function (r) { return esc(r.id); }, csv: function (r) { return r.id; } },
    ];
    return makeTable(ctx, cols, rows, {});
  }

  // ───────────────────────── 등록 ─────────────────────────
  var R = window.VCP_RENDER, T = window.VCP_RENDER_TAB;
  R.VirtualMachine = vmSummary;
  R.HostSystem = hostSummary;
  R.ClusterComputeResource = clusterSummary;
  R.Datacenter = aggSummary; R.Folder = aggSummary; R.vCenter = aggSummary;
  ['vCenter', 'Datacenter', 'Folder', 'ClusterComputeResource', 'HostSystem'].forEach(function (t) { T[t + ':vms'] = vmsTab; });
  ['vCenter', 'Datacenter', 'Folder', 'ClusterComputeResource'].forEach(function (t) { T[t + ':hosts'] = hostsTab; });
  ['vCenter', 'ClusterComputeResource', 'HostSystem', 'VirtualMachine'].forEach(function (t) {
    T[t + ':datastores'] = datastoresTab; T[t + ':networks'] = networksTab;
  });
})();
