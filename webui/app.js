// AWX_copy 수신기 화면. 초기화 가드로 리스너는 한 번만 등록한다. 이벤트는 위임으로 처리한다.
(function () {
  'use strict';
  if (window.__awxFetchInit) return;
  window.__awxFetchInit = true;

  var params = new URLSearchParams(location.search);
  var noPing = params.get('noping') === '1'; // 스크린샷/시험용: 수명 관리에 참여하지 않는다
  var clientId = (function () {
    var a = new Uint8Array(8);
    (window.crypto || window.msCrypto).getRandomValues(a);
    return Array.prototype.map.call(a, function (b) { return ('0' + b.toString(16)).slice(-2); }).join('');
  })();

  var st = { state: null, histRev: -1, configs: [], pendingDelete: null, view: '' };
  var $ = function (id) { return document.getElementById(id); };

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }
  function human(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MiB';
    if (n >= 1024) return (n / 1024).toFixed(1) + ' KiB';
    return n + ' B';
  }
  function fmtTime(ts) {
    var d = new Date(ts);
    if (isNaN(d)) return ts;
    var p = function (x) { return ('0' + x).slice(-2); };
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
  }
  function setMsg(id, text, ok) {
    var el = $(id);
    el.textContent = text || '';
    el.className = 'msg' + (text ? (ok ? ' ok' : ' bad') : '');
  }

  // 요청은 모두 타임아웃을 둔다 (무기한 대기 방지).
  function api(path, body, timeoutMs) {
    var ctl = new AbortController();
    var t = setTimeout(function () { ctl.abort(); }, timeoutMs || 20000);
    var opt = { method: body === undefined ? 'GET' : 'POST', cache: 'no-store', signal: ctl.signal, headers: {} };
    if (body !== undefined) {
      opt.headers['Content-Type'] = 'application/json';
      opt.headers['X-AWX-Fetch'] = '1';
      opt.body = JSON.stringify(body || {});
    }
    return fetch(path, opt).then(function (r) {
      return r.json().catch(function () { return { ok: r.ok }; });
    }).finally(function () { clearTimeout(t); });
  }

  // ---- 수명 관리: ping 2초, 닫힐 때 bye 비콘 ----
  if (!noPing) {
    var ping = function () { fetch('/api/ping?id=' + clientId, { method: 'POST', cache: 'no-store', keepalive: true }).catch(function () {}); };
    ping();
    setInterval(ping, 2000);
    window.addEventListener('focus', ping);
    document.addEventListener('visibilitychange', function () { if (!document.hidden) ping(); });
    var bye = function () {
      try { navigator.sendBeacon('/api/bye?id=' + clientId, 'bye'); } catch (e) {}
    };
    window.addEventListener('pagehide', bye);
  }

  function show(view) {
    st.view = view;
    $('viewUsers').hidden = view !== 'users';
    $('viewNew').hidden = view !== 'new';
    $('viewMain').hidden = view !== 'main';
    $('viewStore').hidden = view !== 'main';
    if (view === 'users') loadConfigs(false);
    if (view === 'main') { st.histRev = -1; refresh(); }
  }

  function loadConfigs(autoPick) {
    return api('/api/configs').then(function (r) {
      st.configs = r.configs || [];
      $('confDir').textContent = '설정 폴더: ' + (r.conf_dir || '');
      var tb = $('userTable').querySelector('tbody');
      if (!st.configs.length) {
        tb.innerHTML = '<tr><td colspan="5" class="muted">설정이 없습니다. [새 설정 만들기]로 만드세요.</td></tr>';
      } else {
        tb.innerHTML = st.configs.map(function (c) {
          return '<tr><td class="mono">' + esc(c.user) + '</td><td class="mono">' + esc(c.url) + '</td><td>' + esc(c.inventory_id) +
            '</td><td>' + (c.encrypted ? '암호화(DPAPI)' : '평문 (선택하면 암호화로 바꿈)') +
            '</td><td><button type="button" class="btn btn-sm btn-primary" data-act="pick" data-user="' + esc(c.user) + '">선택</button></td></tr>';
        }).join('');
      }
      if (autoPick && st.configs.length === 1) pick(st.configs[0].user);
    });
  }

  function pick(user) {
    setMsg('userMsg', user + ' 연결 확인 중...', true);
    return api('/api/select', { user: user }, 30000).then(function (r) {
      if (!r.ok) { setMsg('userMsg', r.error || '선택 실패', false); return; }
      setMsg('userMsg', '', true);
      show('main');
    }).catch(function (e) { setMsg('userMsg', '요청 실패: ' + e, false); });
  }

  function renderState(s) {
    st.state = s;
    $('barUser').textContent = s.user ? (s.user + '  |  ' + s.url + '  |  inventory ' + s.inventory_id) : '사용자 없음';
    $('mainInfo').textContent = s.user ? ('변경 대상: 인벤토리 ' + s.inventory_id + ' variables') : '';
    $('btnFetch').disabled = !s.user || s.receiving;
    $('btnStop').disabled = !s.receiving;
    var wt = $('watchToggle');
    if (document.activeElement !== wt) wt.checked = !!s.watch;
    wt.disabled = !s.user;
    var pct = 0;
    if (s.receiving) {
      if (s.total > 0) pct = Math.min(100, s.bytes * 100 / s.total);
      $('progText').textContent = s.n ? ('[' + s.k + '/' + s.n + '] ' + (s.auto ? '자동 ' : '') + '수신 중  ' + human(s.bytes) + ' / ' + human(s.total) +
        '  ' + s.seconds.toFixed(1) + '초  ' + s.kib_per_sec.toFixed(1) + ' KiB/s  ' + s.file) : '업로드를 기다립니다...';
    } else {
      $('progText').textContent = s.watch ? '자동 감시 중 (새 업로드를 2초마다 확인)' : '대기';
    }
    $('progFill').style.width = pct.toFixed(1) + '%';
    var sl = $('statusLine');
    sl.textContent = s.message || '';
    sl.className = 'status' + (s.message ? (s.ok ? ' ok' : ' bad') : '');
    var sd = $('saveDir');
    if (document.activeElement !== sd && !sd.dataset.dirty) sd.value = s.save_dir || '';
    if (s.focus) { try { window.focus(); } catch (e) {} }
    if (st.view === 'main' && s.hist_rev !== st.histRev) loadHistory(s.hist_rev);
  }

  // 기록은 hist_rev 가 바뀔 때만 다시 읽는다. 최신 200건만 표시한다.
  function loadHistory(rev) {
    st.histRev = rev;
    return api('/api/history?limit=200').then(function (r) {
      var rs = r.records || [];
      $('histInfo').textContent = '최근 ' + rs.length + '건 표시 / 전체 ' + (r.total || 0) + '건  (' + (r.path || '') + ')';
      var tb = $('histTable').querySelector('tbody');
      if (!rs.length) { tb.innerHTML = '<tr><td colspan="8" class="muted">받은 파일이 없습니다.</td></tr>'; return; }
      tb.innerHTML = rs.map(function (x) {
        var tag = x.deleted ? '<span class="tag del">삭제됨</span> ' : '';
        tag += x.sha_ok ? '<span class="tag ok">정상</span>' : '<span class="tag bad">실패</span>';
        var acts = '';
        if (x.path && !x.deleted) {
          acts = '<button type="button" class="btn btn-sm" data-act="h-open" data-id="' + esc(x.id) + '">열기</button> ' +
            '<button type="button" class="btn btn-sm" data-act="h-folder" data-id="' + esc(x.id) + '">폴더 열기</button> ' +
            '<button type="button" class="btn btn-sm" data-act="h-copy" data-path="' + esc(x.path) + '">경로 복사</button> ' +
            '<button type="button" class="btn btn-sm btn-danger" data-act="h-delete" data-id="' + esc(x.id) + '" data-path="' + esc(x.path) + '">삭제</button>';
        }
        return '<tr class="' + (x.deleted ? 'deleted' : '') + '" title="' + esc(x.message) + '"><td class="mono">' + esc(fmtTime(x.ts)) + '</td><td>' + esc(x.file_name) +
          '</td><td>' + human(x.size) + '</td><td>' + esc(x.chunks) + '</td><td>' + tag + '</td><td class="path">' + esc(x.path || x.message) +
          '</td><td class="mono">' + esc(x.user) + '</td><td class="actions">' + acts + '</td></tr>';
      }).join('');
    }).catch(function () { st.histRev = -1; });
  }

  // 상태 폴링: 수신 중 0.5초, 아니면 2초. setTimeout 사슬로 겹치지 않게 한다.
  var timer = null;
  function refresh() {
    clearTimeout(timer);
    api('/api/state', undefined, 8000).then(function (s) {
      renderState(s);
      if (!s.user && st.view === 'main') show('users');
      timer = setTimeout(refresh, s.receiving ? 500 : 2000);
    }).catch(function () { timer = setTimeout(refresh, 3000); });
  }

  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
    var ta = document.createElement('textarea');
    ta.value = text; document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); } finally { document.body.removeChild(ta); }
    return Promise.resolve();
  }

  function act(name, el) {
    switch (name) {
      case 'show-users': return show('users');
      case 'show-new': return show('new');
      case 'pick': return pick(el.dataset.user);
      case 'fetch':
        return api('/api/fetch', {}).then(function (r) { if (!r.ok) setMsg('storeMsg', r.error, false); refresh(); });
      case 'stop': return api('/api/stop', {}).then(refresh);
      case 'save-dir':
        return api('/api/settings', { save_dir: $('saveDir').value }).then(function (r) {
          delete $('saveDir').dataset.dirty;
          setMsg('storeMsg', r.ok ? '저장 폴더를 바꿨습니다' : r.error, r.ok); refresh();
        });
      case 'open-save-dir':
        return api('/api/open-save-dir', {}).then(function (r) { if (!r.ok) setMsg('storeMsg', r.error, false); });
      case 'h-open': case 'h-folder':
        return api('/api/history/action', { id: el.dataset.id, action: name === 'h-open' ? 'open' : 'folder' }).then(function (r) {
          setMsg('storeMsg', r.ok ? '' : r.error, r.ok);
        });
      case 'h-copy':
        return copyText(el.dataset.path).then(function () { setMsg('storeMsg', '경로를 복사했습니다', true); });
      case 'h-delete':
        st.pendingDelete = el.dataset.id;
        $('confirmText').textContent = '파일을 삭제합니다. 기록은 "삭제됨"으로 남습니다.\n' + el.dataset.path;
        $('confirmBox').hidden = false;
        return;
      case 'confirm-yes':
        $('confirmBox').hidden = true;
        return api('/api/history/action', { id: st.pendingDelete, action: 'delete' }).then(function (r) {
          setMsg('storeMsg', r.ok ? '삭제했습니다' : r.error, r.ok); st.histRev = -1; refresh();
        });
      case 'confirm-no':
        $('confirmBox').hidden = true; st.pendingDelete = null; return;
      case 'new-test': return submitNew(true);
    }
  }

  function submitNew(testOnly) {
    var f = $('newForm');
    var body = { url: f.url.value, username: f.username.value, password: f.password.value, inventory_id: f.inventory_id.value, overwrite: f.overwrite.checked, test_only: testOnly };
    setMsg('newMsg', '연결 확인 중...', true);
    return api('/api/configs/save', body, 30000).then(function (r) {
      if (!r.ok) { setMsg('newMsg', r.error, false); return; }
      setMsg('newMsg', testOnly ? '연결 성공' : '저장했습니다', true);
      if (!testOnly) { f.password.value = ''; show('main'); }
    });
  }

  // 리스너 등록 (한 번만)
  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-act]');
    if (el && !el.disabled) act(el.dataset.act, el);
  });
  $('newForm').addEventListener('submit', function (e) { e.preventDefault(); submitNew(false); });
  $('watchToggle').addEventListener('change', function (e) {
    api('/api/watch', { on: e.target.checked }).then(function (r) { if (!r.ok) setMsg('storeMsg', r.error, false); refresh(); });
  });
  $('saveDir').addEventListener('input', function () { $('saveDir').dataset.dirty = '1'; });

  // 시작: 이미 고른 사용자가 있으면 수신 화면, 설정이 1개면 바로 선택
  var startView = params.get('view');
  api('/api/state').then(function (s) {
    renderState(s);
    if (startView === 'new') { show('new'); return; }
    if (startView === 'users') { show('users'); return; }
    if (s.user) { show('main'); return; }
    show('users');
    loadConfigs(true);
  }).catch(function () { show('users'); });
  refresh();
})();
