/*
 * app.js - vCenter 통합 관리 포털 UI (트리 / 검색 / 라우팅 / 레이아웃)
 *
 * ── 확장 지점 (Step 4 가 사용) ────────────────────────────────────────────────
 * 1) 렌더러 레지스트리 (render.js 가 기본값을 채우고, 별도 파일이 덮어쓴다.
 *    index.html 에서 render.js 뒤, app.js 앞에 <script> 로 추가)
 *      window.VCP_RENDER[type] = function (ctx) {...}            // Summary 탭
 *      window.VCP_RENDER_TAB['HostSystem:vms'] = function (ctx)  // 그 외 탭 ("<type>:<tabId>")
 *    type: vCenter | Datacenter | Folder | ClusterComputeResource | HostSystem | VirtualMachine
 *    반환값: HTML 문자열 또는 DOM Node. (문자열 안 값은 ctx.helpers.esc 로 이스케이프)
 *    ctx = {
 *      vc:     manifest 의 vCenter 항목 {id,name,url,instanceUuid,version,status,error,collectedAt,counts}
 *      vcId:   vc.id
 *      obj:    선택 객체 (data.objects[moref]; vCenter 루트면 {type:'vCenter',name,children:rootChildren,status})
 *      moref:  선택 객체 MoRef ('' = vCenter 루트)
 *      data:   window.VCP_DATA[vcId] 전체 (objects/datastores/networks..., data._p = {moref: 부모moref('' = 루트)})
 *      tab:    현재 탭 id
 *      path:   조상 목록 [{moref,obj}] (vCenter 루트부터, 자기 자신 제외)
 *      helpers: window.VCP_HELPERS (fmtBytes/fmtMB/fmtMHz/fmtDate/fmtUptime/deepLink/launcherLink/linkTo/icon/esc ...)
 *      goto(vcId, moref, tab): 포털 내부 이동 (또는 <a href="+helpers.linkTo(...)+"> 사용)
 *      loadVc(vcId): Promise<data> - 다른 vCenter 데이터 지연 로드
 *    }
 * 2) 탭 목록: window.VCP_TABS[type] = [[tabId, 라벨], ...]  (render.js)
 * 3) 링크: 포털 내부는 해시 라우팅  #/<vcId>/<moref>/<tab>  (vCenter 루트: #/<vcId>/<tab>, 검색: #/search/<q>/<page>)
 *    vCenter 열기: helpers.openLink(vc, moref, obj) -> vcportal://open?url=<딥링크>
 * 4) window.VCP (디버그/테스트용): {S: 상태, loadVc, route, search}
 * ────────────────────────────────────────────────────────────────────────────
 */
(function () {
  'use strict';
  var H = window.VCP_HELPERS, esc = H.esc;
  var $ = function (id) { return document.getElementById(id); };
  var STALE_HOURS = 26;
  var PAGE_SIZE = 100, DROP_MAX = 50;

  var S = {
    manifest: null, vcs: [], vcMap: {},
    index: null, hay: null, indexPromise: null,
    loaded: {}, promises: {}, errors: {},
    expanded: {}, sel: null, tab: 'summary', rows: [],
    routeSeq: 0, lastSearch: null, drop: { items: [], act: -1, q: '' },
  };
  window.VCP = { S: S };
  window.VCP_PERF = {};

  // ───────────────────────── 데이터 로딩 ─────────────────────────
  function prepare(d) {
    if (d._p) return d;
    var p = {}, i, ch, o, id;
    (d.rootChildren || []).forEach(function (c) { p[c] = ''; });
    for (id in d.objects) {
      ch = d.objects[id].children;
      if (!ch) continue;
      for (i = 0; i < ch.length; i++) p[ch[i]] = id;
    }
    d._p = p;
    S.loaded[d.id] = d;
    return d;
  }

  function loadVc(id) {
    var vc = S.vcMap[id];
    if (!vc) return Promise.reject(new Error('알 수 없는 vCenter: ' + id));
    if (S.loaded[id]) return Promise.resolve(S.loaded[id]);
    if (S.promises[id]) return S.promises[id];
    var t0 = performance.now();
    var p = H.loadScript('data/' + id + '.js?v=' + encodeURIComponent(vc.collectedAt || S.manifest.generated || '')).then(function () {
      var d = window.VCP_DATA && window.VCP_DATA[id];
      if (!d) throw new Error('data/' + id + '.js 에 데이터가 없습니다');
      d.id = d.id || id;
      window.VCP_PERF['load_' + id] = Math.round(performance.now() - t0);
      return prepare(d);
    });
    p.catch(function () { delete S.promises[id]; });
    S.promises[id] = p;
    return p;
  }

  // ───────────────────────── 배너 / 하단 바 ─────────────────────────
  function renderBanner() {
    var lines = [], fails = S.vcs.filter(function (v) { return v.status === 'fail'; });
    if (fails.length) {
      var items = fails.slice(0, 5).map(function (v) {
        return '<b>' + esc(v.name || v.id) + '</b> (' + esc((v.error || '원인 불명').slice(0, 120)) + ')';
      });
      lines.push('수집에 실패한 vCenter ' + fails.length + '대: ' + items.join(', ') + (fails.length > 5 ? ' 외 ' + (fails.length - 5) + '대' : '') +
        '. 해당 vCenter 는 직전 수집 데이터로 표시됩니다.');
    }
    var g = new Date(S.manifest.generated).getTime();
    if (S.manifest.generated && !isNaN(g)) {
      var hrs = (Date.now() - g) / 3600e3;
      if (hrs > STALE_HOURS) lines.push('마지막 수집이 ' + Math.floor(hrs) + '시간 전(' + esc(H.fmtDate(S.manifest.generated)) + ')입니다. 수집기가 동작 중인지 확인하세요.');
    }
    var b = $('banner');
    if (!lines.length) { b.hidden = true; return; }
    b.innerHTML = lines.map(function (l) {
      return '<div class="bn-line"><svg class="svg"><use href="#i-warn"/></svg><span>' + l + '</span></div>';
    }).join('') + '<button class="bn-x" title="닫기"><svg class="svg"><use href="#i-close"/></svg></button>';
    b.hidden = false;
    b.querySelector('.bn-x').onclick = function () { b.hidden = true; };
  }

  function statusTable() {
    var h = '<table class="grid"><thead><tr><th>ID</th><th>vCenter</th><th>버전</th><th>상태</th><th>수집 시각</th><th>호스트</th><th>VM</th><th>클러스터</th><th>오류</th></tr></thead><tbody>';
    S.vcs.forEach(function (v) {
      var c = v.counts || {};
      h += '<tr><td>' + esc(v.id) + '</td><td>' + H.icon({ type: 'vCenter', status: v.status }) + ' <a href="' + H.linkTo(v.id, '') + '">' + esc(v.name || v.id) + '</a></td>' +
        '<td>' + esc(H.dash(v.version)) + '</td><td class="' + (v.status === 'fail' ? 'b-fail' : 'b-ok') + '">' + (v.status === 'fail' ? '실패' : '정상') + '</td>' +
        '<td>' + esc(H.fmtDate(v.collectedAt)) + '</td><td>' + esc(H.dash(c.hosts)) + '</td><td>' + esc(H.dash(c.vms)) + '</td><td>' + esc(H.dash(c.clusters)) + '</td>' +
        '<td class="wrap">' + esc(v.error || '') + '</td></tr>';
    });
    return h + '</tbody></table>';
  }

  function renderBottom() {
    var m = S.manifest, fails = S.vcs.filter(function (v) { return v.status === 'fail'; }).length;
    $('bb-sum').innerHTML = '마지막 수집: ' + esc(H.fmtDate(m.generated)) + ' (' + esc(H.fmtAgo(m.generated)) + ') · vCenter ' + S.vcs.length + '대' +
      (fails ? '<span class="b-fail">실패 ' + fails + '</span>' : '');
    $('bb-panel').innerHTML = '<div class="gen">수집 기준 시각(manifest.generated): <b>' + esc(H.fmtDate(m.generated)) + '</b></div>' + statusTable();
  }

  // ───────────────────────── 트리 ─────────────────────────
  function selKey() { return S.sel ? (S.sel.moref ? S.sel.vcId + '/' + S.sel.moref : S.sel.vcId) : ''; }

  function pushKids(rows, vcId, d, list, depth) {
    for (var i = 0; i < list.length; i++) {
      var m = list[i], o = d.objects[m];
      if (!o) continue;
      var key = vcId + '/' + m, has = !!(o.children && o.children.length), open = has && !!S.expanded[key];
      rows.push({ key: key, vcId: vcId, moref: m, depth: depth, kind: 'obj', obj: o, open: open, kids: has });
      if (open) pushKids(rows, vcId, d, o.children, depth + 1);
    }
  }

  function buildRows() {
    var rows = [];
    S.vcs.forEach(function (vc) {
      var open = !!S.expanded[vc.id];
      rows.push({ key: vc.id, vcId: vc.id, moref: '', depth: 0, kind: 'vc', vc: vc, open: open, kids: true });
      if (!open) return;
      var d = S.loaded[vc.id];
      if (d) pushKids(rows, vc.id, d, d.rootChildren || [], 1);
      else if (S.errors[vc.id]) rows.push({ kind: 'msg', depth: 1, text: '불러오기 실패: ' + S.errors[vc.id] });
      else rows.push({ kind: 'msg', depth: 1, text: '불러오는 중...' });
    });
    S.rows = rows;
  }

  function renderTree() {
    buildRows();
    var sk = selKey(), out = [];
    for (var i = 0; i < S.rows.length; i++) {
      var r = S.rows[i], ml = r.depth * 20;
      if (r.kind === 'msg') {
        out.push('<div class="row msg" style="margin-left:' + (ml + 20) + 'px">' + esc(r.text) + '</div>');
        continue;
      }
      var name, title, ico, dis = false, badge = '';
      if (r.kind === 'vc') {
        name = r.vc.name || r.vc.id;
        title = name + ' (' + r.vc.id + ')' + (r.vc.status === 'fail' ? ' - 수집 실패' : '');
        ico = H.icon({ type: 'vCenter', status: r.vc.status });
        if (r.vc.status === 'fail') badge = '<span class="vbadge">수집 실패</span>';
      } else {
        name = r.obj.name + H.nameSuffix(r.obj);
        title = name;
        ico = H.icon(r.obj);
        dis = H.isDisconnected(r.obj);
      }
      out.push('<div class="row' + (r.key === sk ? ' sel' : '') + (dis ? ' dis' : '') + (r.kids && !r.open ? ' closed' : '') + '" role="treeitem" data-i="' + i +
        '" aria-level="' + (r.depth + 1) + '"' + (r.kids ? ' aria-expanded="' + r.open + '"' : '') + ' style="margin-left:' + ml + 'px" title="' + esc(title) + '">' +
        '<span class="tw' + (r.kids ? '' : ' none') + '"><svg class="svg"><use href="#i-chev"/></svg></span>' + ico +
        '<span class="nm">' + esc(name) + '</span>' + badge + '</div>');
    }
    var t = $('tree'), st = t.scrollTop;
    t.innerHTML = out.join('');
    t.scrollTop = st;
  }

  function scrollSel() {
    var el = $('tree').querySelector('.row.sel');
    if (el) el.scrollIntoView({ block: 'nearest' });
  }

  function toggle(r) {
    if (r.open) delete S.expanded[r.key];
    else {
      S.expanded[r.key] = true;
      if (r.kind === 'vc' && !S.loaded[r.vcId]) {
        delete S.errors[r.vcId];
        loadVc(r.vcId).then(renderTree, function (e) { S.errors[r.vcId] = e.message; renderTree(); });
      }
    }
    renderTree();
  }

  function tabFor(type) {
    var tabs = window.VCP_TABS[type] || [];
    for (var i = 0; i < tabs.length; i++) if (tabs[i][0] === S.tab) return S.tab;
    return 'summary';
  }

  function gotoRow(r) {
    var type = r.kind === 'vc' ? 'vCenter' : r.obj.type;
    location.hash = H.linkTo(r.vcId, r.moref, tabFor(type));
  }

  function rowOf(e) {
    var el = e.target.closest ? e.target.closest('.row') : null;
    return el && el.getAttribute('data-i') != null ? S.rows[+el.getAttribute('data-i')] : null;
  }

  function initTree() {
    var t = $('tree');
    t.addEventListener('click', function (e) {
      var r = rowOf(e);
      if (!r || r.kind === 'msg') return;
      if (e.target.closest('.tw') && r.kids) { toggle(r); return; }
      gotoRow(r);
    });
    t.addEventListener('dblclick', function (e) {
      var r = rowOf(e);
      if (r && r.kind !== 'msg' && r.kids) toggle(r);
    });
    t.addEventListener('keydown', function (e) {
      var sk = selKey(), idx = -1, i, r;
      for (i = 0; i < S.rows.length; i++) if (S.rows[i].key === sk) { idx = i; break; }
      var cur = idx >= 0 ? S.rows[idx] : null;
      function move(from, dir) {
        for (var j = from + dir; j >= 0 && j < S.rows.length; j += dir) if (S.rows[j].kind !== 'msg') return S.rows[j];
        return null;
      }
      if (e.key === 'ArrowDown') r = idx < 0 ? move(-1, 1) : move(idx, 1);
      else if (e.key === 'ArrowUp') r = idx < 0 ? move(S.rows.length, -1) : move(idx, -1);
      else if (e.key === 'Home') r = move(-1, 1);
      else if (e.key === 'End') r = move(S.rows.length, -1);
      else if (e.key === 'ArrowRight' && cur) {
        if (cur.kids && !cur.open) { toggle(cur); e.preventDefault(); return; }
        if (cur.open) r = move(idx, 1);
      } else if (e.key === 'ArrowLeft' && cur) {
        if (cur.open) { toggle(cur); e.preventDefault(); return; }
        for (i = idx - 1; i >= 0; i--) if (S.rows[i].depth < cur.depth && S.rows[i].kind !== 'msg') { r = S.rows[i]; break; }
      } else if ((e.key === 'Enter' || e.key === ' ') && cur) { gotoRow(cur); e.preventDefault(); return; }
      else return;
      e.preventDefault();
      if (r && r.depth >= 0 && r.kind !== 'msg') gotoRow(r);
    });
  }

  // ───────────────────────── 검색 ─────────────────────────
  function buildHay() {
    var n = S.index.length, hay = new Array(n), e;
    for (var i = 0; i < n; i++) { e = S.index[i]; hay[i] = (e[3] + '\u0001' + e[4] + '\u0001' + e[6]).toLowerCase(); }
    S.hay = hay;
  }

  // 이름 / IP / guestHostName 부분 일치 (대소문자 무시). 정확한 이름·IP 일치 우선.
  function search(q) {
    q = (q || '').trim().toLowerCase();
    if (!q || !S.hay) return [];
    var out = [], hay = S.hay, idx = S.index;
    for (var i = 0; i < hay.length; i++) {
      if (hay[i].indexOf(q) < 0) continue;
      var e = idx[i], nm = e[3].toLowerCase(), rank;
      if (nm === q || (e[4] && (' ' + e[4] + ' ').indexOf(' ' + q + ' ') >= 0)) rank = 0;
      else {
        var p = nm.indexOf(q);
        rank = p === 0 ? 1 : p > 0 ? 2 : 3;
      }
      out.push({ e: e, rank: rank, i: i });
    }
    out.sort(function (a, b) {
      return (a.rank - b.rank) || (a.e[3] < b.e[3] ? -1 : a.e[3] > b.e[3] ? 1 : a.i - b.i);
    });
    return out;
  }
  S.search = search;
  window.VCP.search = search;

  var TYPE_SHORT = { H: '호스트', V: 'VM', C: '클러스터' };

  function subLine(e) {
    var vc = S.vcMap[e[0]], parts = [esc(vc ? vc.name : e[0])];
    if (e[1] === 'V') {
      if (e[5]) parts.push('호스트 ' + esc(e[5]));
      if (e[4]) parts.push(esc(e[4]));
    }
    return parts.join(' · ');
  }

  function iconFor(e) { return H.icon({ type: H.IDX_TYPE[e[1]] }); }

  function closeDrop() { $('sr-drop').hidden = true; S.drop.act = -1; }

  function showDrop() {
    var q = $('q').value.trim(), d = $('sr-drop');
    if (!q) { closeDrop(); return; }
    if (!S.hay) {
      d.innerHTML = '<div class="sr-empty">검색 인덱스를 불러오는 중...</div>';
      d.hidden = false;
      S.indexPromise && S.indexPromise.then(function () { if ($('q').value.trim() === q) showDrop(); }, function () {
        d.innerHTML = '<div class="sr-empty">검색 인덱스(data/index.js)를 불러올 수 없습니다.</div>';
      });
      return;
    }
    var t0 = performance.now(), res = search(q);
    window.VCP_PERF.lastSearchMs = +(performance.now() - t0).toFixed(1);
    var items = res.slice(0, DROP_MAX);
    S.drop = { items: items, act: -1, q: q, total: res.length };
    if (!items.length) { d.innerHTML = '<div class="sr-empty">"' + esc(q) + '" 검색 결과가 없습니다.</div>'; d.hidden = false; return; }
    d.innerHTML = items.map(function (it, k) {
      var e = it.e;
      return '<div class="sr-item" data-k="' + k + '">' + iconFor(e) + '<div class="sr-main"><div class="sr-name">' + H.highlight(e[3], q) + '</div>' +
        '<div class="sr-sub">' + subLine(e) + '</div></div><span class="sr-type">' + TYPE_SHORT[e[1]] + '</span></div>';
    }).join('') + '<div class="sr-foot"><span>총 ' + res.length + '건' + (res.length > items.length ? ' 중 ' + items.length + '건 표시' : '') +
      '</span><a href="#/search/' + encodeURIComponent(q) + '" data-all="1">모두 보기</a></div>';
    d.hidden = false;
  }

  function setAct(k) {
    var items = $('sr-drop').querySelectorAll('.sr-item');
    if (!items.length) return;
    S.drop.act = (k + items.length) % items.length;
    for (var i = 0; i < items.length; i++) items[i].classList.toggle('act', i === S.drop.act);
    items[S.drop.act].scrollIntoView({ block: 'nearest' });
  }

  function openResult(e) {
    closeDrop();
    location.hash = H.linkTo(e[0], e[2], 'summary');
  }

  function initSearch() {
    var q = $('q'), timer = null, box = $('search');
    q.addEventListener('input', function () { clearTimeout(timer); timer = setTimeout(showDrop, 120); });
    q.addEventListener('focus', function () { if (q.value.trim()) showDrop(); });
    q.addEventListener('keydown', function (e) {
      var drop = $('sr-drop');
      if (e.key === 'ArrowDown') { e.preventDefault(); if (drop.hidden) showDrop(); else setAct(S.drop.act + 1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); if (!drop.hidden) setAct(S.drop.act < 0 ? -1 : S.drop.act - 1); }
      else if (e.key === 'Escape') { closeDrop(); q.blur(); }
      else if (e.key === 'Enter') {
        e.preventDefault();
        clearTimeout(timer);
        var v = q.value.trim();
        if (!v) return;
        if (!drop.hidden && S.drop.act >= 0 && S.drop.items[S.drop.act]) openResult(S.drop.items[S.drop.act].e);
        else { closeDrop(); location.hash = '#/search/' + encodeURIComponent(v); }
      }
    });
    $('sr-drop').addEventListener('mousedown', function (e) {
      var a = e.target.closest('a[data-all]');
      if (a) { closeDrop(); return; } // 링크 기본 동작(해시 이동)
      var it = e.target.closest('.sr-item');
      if (it) { e.preventDefault(); openResult(S.drop.items[+it.getAttribute('data-k')].e); }
    });
    document.addEventListener('mousedown', function (e) { if (!box.contains(e.target)) closeDrop(); });
  }

  // ───────────────────────── 라우터 / 오른쪽 패널 ─────────────────────────
  var TAB_IDS = { summary: 1, vms: 1, hosts: 1, datastores: 1, networks: 1 };

  function parseHash() {
    var h = location.hash.replace(/^#\/?/, '');
    if (!h) return { kind: 'home' };
    var s = h.split('/').map(function (x) { try { return decodeURIComponent(x); } catch (e) { return x; } });
    if (s[0] === 'search') return { kind: 'search', q: s[1] || '', page: Math.max(1, parseInt(s[2], 10) || 1) };
    if (s.length <= 2 && (!s[1] || TAB_IDS[s[1]])) return { kind: 'obj', vcId: s[0], moref: '', tab: s[1] || 'summary' };
    return { kind: 'obj', vcId: s[0], moref: s[1], tab: s[2] || 'summary' };
  }

  function setBody(html, hdr, tabs) {
    $('objhdr').innerHTML = hdr || '';
    $('tabs').innerHTML = tabs || '';
    var b = $('body');
    if (typeof html === 'string') b.innerHTML = html; else { b.innerHTML = ''; b.appendChild(html); }
    b.scrollTop = 0;
  }

  function plainHdr(title, sub) {
    return '<h1>' + esc(title) + '</h1>' + (sub ? '<span class="oh-sub">' + esc(sub) + '</span>' : '');
  }

  function renderHome() {
    document.title = 'vSphere Client - 통합 포털';
    var hosts = 0, vms = 0, cl = 0;
    S.vcs.forEach(function (v) { var c = v.counts || {}; hosts += c.hosts || 0; vms += c.vms || 0; cl += c.clusters || 0; });
    setBody('<div class="home-cards"><div class="stat"><b>' + S.vcs.length + '</b>vCenter</div><div class="stat"><b>' + cl + '</b>클러스터</div>' +
      '<div class="stat"><b>' + hosts + '</b>호스트</div><div class="stat"><b>' + vms + '</b>가상 머신</div></div>' +
      '<p class="hint">왼쪽 트리에서 vCenter 를 펼치거나 위 검색창에서 호스트 / VM / IP 를 검색하세요.</p>' + statusTable(),
      plainHdr('vSphere Client 통합 포털', '전체 vCenter 요약'), '');
  }

  function renderSearchPage(q, page) {
    document.title = '검색: ' + q + ' - vSphere Client 통합 포털';
    if ($('q').value !== q) $('q').value = q;
    var hdr = plainHdr('검색 결과', '"' + q + '"');
    if (!S.hay) {
      setBody('<p class="hint">검색 인덱스를 불러오는 중...</p>', hdr, '');
      S.indexPromise && S.indexPromise.then(function () { var r = parseHash(); if (r.kind === 'search') renderSearchPage(r.q, r.page); },
        function () { setBody('<div class="warn-strip">검색 인덱스(data/index.js)를 불러올 수 없습니다.</div>', hdr, ''); });
      return;
    }
    if (!S.lastSearch || S.lastSearch.q !== q) S.lastSearch = { q: q, res: search(q) };
    var res = S.lastSearch.res, pages = Math.max(1, Math.ceil(res.length / PAGE_SIZE));
    page = Math.min(page, pages);
    var from = (page - 1) * PAGE_SIZE, part = res.slice(from, from + PAGE_SIZE);
    var base = '#/search/' + encodeURIComponent(q) + '/';
    var pager = '<div class="pager"><span>총 <b>' + res.length + '</b>건</span>' +
      (pages > 1 ? '<button ' + (page > 1 ? 'onclick="location.hash=\'' + base + (page - 1) + '\'"' : 'disabled') + '>이전</button><span>' + page + ' / ' + pages + '</span>' +
        '<button ' + (page < pages ? 'onclick="location.hash=\'' + base + (page + 1) + '\'"' : 'disabled') + '>다음</button>' : '') + '</div>';
    var h = pager;
    if (!res.length) h += '<p class="hint">검색 결과가 없습니다.</p>';
    else {
      h += '<table class="grid"><thead><tr><th>이름</th><th>유형</th><th>IP</th><th>호스트</th><th>vCenter</th></tr></thead><tbody>';
      part.forEach(function (it) {
        var e = it.e, vc = S.vcMap[e[0]];
        h += '<tr><td>' + iconFor(e) + '<a href="' + H.linkTo(e[0], e[2], 'summary') + '">' + H.highlight(e[3], q) + '</a></td><td>' + TYPE_SHORT[e[1]] + '</td><td>' + esc(e[4] || '-') +
          '</td><td>' + esc(e[5] || '-') + '</td><td>' + esc(vc ? vc.name : e[0]) + '</td></tr>';
      });
      h += '</tbody></table>' + pager;
    }
    setBody(h, hdr, '');
  }

  function ancestors(d, vc, moref) {
    var path = [], guard = 0, m = moref ? d._p[moref] : '';
    while (m && guard++ < 64) { path.unshift({ moref: m, obj: d.objects[m] }); m = d._p[m]; }
    path.unshift({ moref: '', obj: { type: 'vCenter', name: vc.name || vc.id } });
    return path;
  }

  function renderObject(r, d) {
    var vc = S.vcMap[r.vcId], moref = r.moref, obj;
    if (moref) {
      obj = d.objects[moref];
      if (!obj) {
        setBody('<div class="center-msg"><h2>객체를 찾을 수 없습니다</h2>' + esc(moref) + ' 은(는) ' + esc(vc.name) + ' 의 수집 데이터에 없습니다. (삭제되었거나 다음 수집 이후 생성됨)</div>',
          plainHdr(moref), '');
        return;
      }
    } else {
      obj = { type: 'vCenter', name: vc.name || vc.id, status: vc.status, children: d.rootChildren || [], parent: '' };
    }
    var tabs = window.VCP_TABS[obj.type] || [['summary', 'Summary']];
    var tab = 'summary';
    tabs.forEach(function (t) { if (t[0] === r.tab) tab = t[0]; });
    var ctx = { vc: vc, vcId: vc.id, obj: obj, moref: moref, data: d, tab: tab, helpers: H, loadVc: loadVc,
      goto: function (v, m, t) { location.hash = H.linkTo(v, m, t); }, path: ancestors(d, vc, moref) };
    var fn = tab === 'summary' ? window.VCP_RENDER[obj.type] : window.VCP_RENDER_TAB[obj.type + ':' + tab];
    if (!fn) fn = window.VCP_RENDER._tab;
    var res;
    try { res = fn(ctx); } catch (err) { console.error(err); res = '<div class="warn-strip">렌더링 오류: ' + esc(err.message) + '</div>'; }
    var warn = '';
    if (vc.status === 'fail') {
      warn = '<div class="warn-strip">이 vCenter 는 최근 수집에 실패했습니다' + (vc.error ? ' (' + esc(vc.error) + ')' : '') + '. 표시 데이터 기준 시각: ' +
        esc(H.fmtDate(d.collectedAt || vc.collectedAt)) + '</div>';
    }
    var body;
    if (typeof res === 'string') body = warn + res;
    else { body = document.createElement('div'); body.innerHTML = warn; body.appendChild(res); }
    var hdr = H.icon(obj) + '<h1>' + esc(obj.name) + '</h1><span class="oh-sub">' + esc(H.TYPE_LABEL[obj.type] || obj.type) + '</span>' +
      '<a class="act" href="' + esc(H.openLink(vc, moref, moref ? obj : null)) + '" title="원래 vCenter 화면을 로그인된 상태로 엽니다 (로그인 런처 필요)"><svg class="svg"><use href="#i-ext"/></svg>vCenter에서 열기</a>' +
      '<a class="act sub" href="' + esc(H.deepLink(vc, moref, moref ? obj : null)) + '" target="_blank" rel="noopener" title="브라우저에서 vCenter 로 직접 열기 (별도 로그인 필요)">직접 열기</a>';
    var tabsHtml = tabs.map(function (t) {
      return '<a role="tab" class="' + (t[0] === tab ? 'active' : '') + '" href="' + H.linkTo(vc.id, moref, t[0]) + '">' + esc(t[1]) + '</a>';
    }).join('');
    document.title = obj.name + ' - vSphere Client 통합 포털';
    setBody(body, hdr, tabsHtml);
  }

  function route() {
    var seq = ++S.routeSeq, r = parseHash();
    closeDrop();
    if (!S.manifest) return Promise.resolve();
    if (r.kind === 'home') { S.sel = null; S.tab = 'summary'; renderTree(); renderHome(); return Promise.resolve(); }
    if (r.kind === 'search') { S.sel = null; renderTree(); renderSearchPage(r.q, r.page); return Promise.resolve(); }
    var vc = S.vcMap[r.vcId];
    if (!vc) {
      S.sel = null; renderTree();
      setBody('<div class="center-msg"><h2>알 수 없는 vCenter</h2>' + esc(r.vcId) + '</div>', plainHdr(r.vcId), '');
      return Promise.resolve();
    }
    S.sel = { vcId: r.vcId, moref: r.moref };
    S.tab = r.tab;
    renderTree();
    setBody('<p class="hint">불러오는 중...</p>', plainHdr(vc.name || vc.id), '');
    return loadVc(r.vcId).then(function (d) {
      if (seq !== S.routeSeq) return;
      delete S.errors[r.vcId];
      if (r.moref) { // 트리 경로 펼치기
        S.expanded[r.vcId] = true;
        var m = d._p[r.moref], guard = 0;
        while (m && guard++ < 64) { S.expanded[r.vcId + '/' + m] = true; m = d._p[m]; }
      }
      renderTree();
      scrollSel();
      renderObject(r, d);
    }, function (e) {
      if (seq !== S.routeSeq) return;
      S.errors[r.vcId] = e.message;
      renderTree();
      setBody('<div class="center-msg"><h2>' + esc(vc.name || vc.id) + ' 의 데이터를 불러올 수 없습니다</h2>' +
        (vc.status === 'fail' ? '수집 실패: ' + esc(vc.error || '원인 불명') + '<br>' : '') + '마지막 성공 수집: ' + esc(H.fmtDate(vc.collectedAt)) + '<br><small>' + esc(e.message) + '</small></div>',
        H.icon({ type: 'vCenter', status: vc.status }) + plainHdr(vc.name || vc.id), '');
    });
  }
  window.VCP.route = route;
  window.VCP.loadVc = loadVc;

  // ───────────────────────── 레이아웃 ─────────────────────────
  function lsGet(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
  function lsSet(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* file:// 등에서 막힐 수 있음 */ } }

  function initLayout() {
    var w = parseInt(lsGet('vcp.navw'), 10);
    if (w >= 180 && w <= 900) document.documentElement.style.setProperty('--nav-w', w + 'px');
    var split = $('split');
    split.addEventListener('mousedown', function (e) {
      e.preventDefault();
      split.classList.add('drag');
      document.body.classList.add('resizing');
      var left = $('nav').getBoundingClientRect().left;
      function mv(ev) {
        var nw = Math.max(180, Math.min(900, ev.clientX - left));
        document.documentElement.style.setProperty('--nav-w', nw + 'px');
      }
      function up() {
        document.removeEventListener('mousemove', mv);
        document.removeEventListener('mouseup', up);
        split.classList.remove('drag');
        document.body.classList.remove('resizing');
        lsSet('vcp.navw', parseInt(getComputedStyle($('nav')).width, 10));
      }
      document.addEventListener('mousemove', mv);
      document.addEventListener('mouseup', up);
    });
    function toggleNav() { document.body.classList.toggle('nav-collapsed'); }
    $('nav-collapse').onclick = toggleNav;
    $('hdr-menu').onclick = toggleNav;
    $('hdr-refresh').onclick = function () { location.reload(); };
    $('bb-toggle').onclick = $('bottom').querySelector('.bb-tab').onclick = function () {
      var o = $('bottom').classList.toggle('open');
      $('bb-panel').hidden = !o;
    };
    // 런처 포털 모드(vcportal.exe 로 연 경우)에서는 window.vcpOpen 이 연결되어 있다.
    // 그때는 vcportal:// 프로토콜(Edge 확인 창) 대신 런처에 딥링크를 직접 전달한다.
    document.addEventListener('click', function (e) {
      var a = e.target.closest && e.target.closest('a[href^="vcportal:"]');
      if (!a || typeof window.vcpOpen !== 'function') return;
      e.preventDefault();
      window.vcpOpen(new URL(a.href).searchParams.get('url'));
    }, true);
  }

  // ───────────────────────── 시작 ─────────────────────────
  function fatal(msg) {
    setBody('<div class="center-msg"><h2>데이터를 불러올 수 없습니다</h2>' + esc(msg) + '<br><small>수집기가 실행되어 data\\manifest.js 가 생성되었는지 확인하세요.</small></div>', plainHdr('vSphere Client 통합 포털'), '');
  }

  function init() {
    initLayout();
    initTree();
    initSearch();
    window.addEventListener('hashchange', route);
    var t0 = performance.now();
    H.loadScript('data/manifest.js?t=' + Date.now()).then(function () {
      var m = window.VCP_MANIFEST;
      if (!m || !m.vcenters) throw new Error('manifest.js 형식이 올바르지 않습니다');
      S.manifest = m;
      S.vcs = m.vcenters;
      S.vcs.forEach(function (v) { S.vcMap[v.id] = v; });
      window.VCP_PERF.manifest = Math.round(performance.now() - t0);
      renderBanner();
      renderBottom();
      renderTree();
      route();
      // 검색 인덱스는 첫 화면 그린 뒤 로드
      var t1 = performance.now();
      S.indexPromise = H.loadScript('data/index.js?v=' + encodeURIComponent(m.generated || '')).then(function () {
        if (!window.VCP_INDEX) throw new Error('index.js 형식이 올바르지 않습니다');
        S.index = window.VCP_INDEX;
        buildHay();
        window.VCP_PERF.index = Math.round(performance.now() - t1);
        window.VCP_PERF.indexEntries = S.index.length;
      });
      S.indexPromise.catch(function (e) { console.error(e); });
      return S.indexPromise;
    }).catch(function (e) {
      console.error(e);
      if (!S.manifest) fatal(e.message);
    });
  }

  init();
})();
