/*
 * helpers.js - 공용 헬퍼 (app.js 와 Step 4 렌더러가 함께 사용). window.VCP_HELPERS 로 노출.
 * 순수 함수만 둔다. DOM/상태 의존 코드는 app.js.
 */
(function () {
  'use strict';

  var TYPE_LABEL = {
    vCenter: 'vCenter', Datacenter: '데이터센터', Folder: '폴더', ClusterComputeResource: '클러스터',
    HostSystem: '호스트', VirtualMachine: '가상 머신',
  };
  // 딥링크 경로 세그먼트 (DATA_SCHEMA "딥링크")
  var DEEP_SEG = { VirtualMachine: 'vm', HostSystem: 'host', ClusterComputeResource: 'cluster', Datacenter: 'datacenter', Folder: 'folder' };

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function isNum(n) { return typeof n === 'number' && isFinite(n); }

  function trimNum(n, d) { return String(+n.toFixed(d)); }

  // bytes -> "81.8 GB" (1024 기준, vSphere 표기와 동일)
  function fmtBytes(b) {
    if (!isNum(b)) return '-';
    var u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'], i = 0, v = b;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return trimNum(v, i === 0 ? 0 : v >= 100 ? 0 : v >= 10 ? 1 : 2) + ' ' + u[i];
  }

  // MB -> "8 GB"
  function fmtMB(mb) {
    return isNum(mb) ? fmtBytes(mb * 1048576) : '-';
  }

  // MHz -> "1.5 GHz" / "800 MHz"
  function fmtMHz(mhz) {
    if (!isNum(mhz)) return '-';
    return mhz >= 1000 ? trimNum(mhz / 1000, 2) + ' GHz' : Math.round(mhz) + ' MHz';
  }

  function p2(n) { return n < 10 ? '0' + n : '' + n; }

  // RFC3339 -> "2026-09-29 12:00:41" (뷰어 PC 로컬 시각)
  function fmtDate(iso) {
    if (!iso) return '-';
    var d = new Date(iso);
    if (isNaN(d.getTime())) return String(iso);
    return d.getFullYear() + '-' + p2(d.getMonth() + 1) + '-' + p2(d.getDate()) + ' ' + p2(d.getHours()) + ':' + p2(d.getMinutes()) + ':' + p2(d.getSeconds());
  }

  // 초 -> "28일 3시간 5분"
  function fmtUptime(sec) {
    if (!isNum(sec)) return '-';
    var d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
    return (d ? d + '일 ' : '') + (d || h ? h + '시간 ' : '') + m + '분';
  }

  // RFC3339 -> "3시간 전"
  function fmtAgo(iso) {
    var t = new Date(iso).getTime();
    if (!iso || isNaN(t)) return '-';
    var s = Math.max(0, (Date.now() - t) / 1000);
    if (s < 3600) return Math.max(1, Math.round(s / 60)) + '분 전';
    if (s < 86400) return Math.round(s / 3600) + '시간 전';
    return Math.round(s / 86400) + '일 전';
  }

  function dash(v) { return v === undefined || v === null || v === '' ? '-' : v; }

  // vSphere Client 8.0.3 딥링크. vc = {url, instanceUuid}, obj 없으면 vCenter 홈.
  function deepLink(vc, moref, obj) {
    var base = String(vc.url || '').replace(/\/+$/, '');
    if (!obj || !moref || !DEEP_SEG[obj.type]) return base + '/ui/';
    return base + '/ui/app/' + DEEP_SEG[obj.type] + ';nav=h/urn:vmomi:' + obj.type + ':' + moref + ':' + (vc.instanceUuid || '') + '/summary';
  }

  // 로그인 런처 링크
  function launcherLink(link) {
    return 'vcportal://open?url=' + encodeURIComponent(link);
  }

  function openLink(vc, moref, obj) {
    return launcherLink(deepLink(vc, moref, obj));
  }

  // 포털 내부 링크 해시: #/vc01/vm-3052/summary  (moref 없으면 vCenter 루트: #/vc01/summary)
  function linkTo(vcId, moref, tab) {
    return '#/' + encodeURIComponent(vcId) + (moref ? '/' + encodeURIComponent(moref) : '') + '/' + (tab || 'summary');
  }

  // 데이터 스크립트 주입 (fetch/XHR 불가: file://). Promise 반환.
  function loadScript(src) {
    return new Promise(function (resolve, reject) {
      var s = document.createElement('script');
      s.src = src;
      s.async = true;
      s.onload = function () { resolve(); };
      s.onerror = function () { s.remove(); reject(new Error(src + ' 을(를) 불러올 수 없습니다')); };
      document.head.appendChild(s);
    });
  }

  // 이름 안의 검색어 강조 (text 는 원문, 반환은 HTML)
  function highlight(text, q) {
    var t = String(text == null ? '' : text);
    if (!q) return esc(t);
    var i = t.toLowerCase().indexOf(q.toLowerCase());
    if (i < 0) return esc(t);
    return esc(t.slice(0, i)) + '<mark>' + esc(t.slice(i, i + q.length)) + '</mark>' + esc(t.slice(i + q.length));
  }

  // 트리에 표시하는 이름 접미사 (스크린샷: "(Disconnected)" 호스트, "(disconnected)" VM)
  function nameSuffix(obj) {
    if (!obj) return '';
    if (obj.type === 'HostSystem') {
      if (obj.connectionState === 'disconnected') return ' (Disconnected)';
      if (obj.connectionState === 'notResponding') return ' (Not Responding)';
      if (obj.inMaintenance) return ' (Maintenance Mode)';
    } else if (obj.type === 'VirtualMachine') {
      if (obj.connectionState && obj.connectionState !== 'connected') return ' (' + obj.connectionState + ')';
    }
    return '';
  }

  function isDisconnected(obj) {
    return !!obj && (obj.type === 'HostSystem' || obj.type === 'VirtualMachine') &&
      !!obj.connectionState && obj.connectionState !== 'connected';
  }

  // 인라인 SVG 아이콘 (index.html 의 <symbol> 스프라이트 참조) + 상태 오버레이
  // kind: vcenter|datacenter|folder|cluster|host|vm  (스프라이트 id = i-<kind>)
  function iconKind(obj) {
    switch (obj && obj.type) {
      case 'vCenter': return 'vcenter';
      case 'Datacenter': return 'datacenter';
      case 'Folder': return 'folder';
      case 'ClusterComputeResource': return 'cluster';
      case 'HostSystem': return 'host';
      case 'VirtualMachine': return 'vm';
    }
    return 'folder';
  }

  function icon(obj, extraCls) {
    var kind = iconKind(obj), ov = '';
    if (obj) {
      if (obj.type === 'VirtualMachine') {
        if (obj.connectionState && obj.connectionState !== 'connected') ov = ''; // 스크린샷: disconnected VM 은 일반 아이콘
        else if (obj.powerState === 'poweredOn') ov = 'ov-on';
        else if (obj.powerState === 'suspended') ov = 'ov-susp';
      } else if (obj.type === 'HostSystem') {
        if (obj.connectionState && obj.connectionState !== 'connected') ov = 'ov-err';
        else if (obj.inMaintenance) ov = 'ov-maint';
      } else if (obj.type === 'vCenter' && obj.status === 'fail') {
        ov = 'ov-err';
      }
    }
    return '<span class="ico ' + (extraCls || '') + '"><svg class="svg"><use href="#i-' + kind + '"/></svg>' +
      (ov ? '<i class="ov ' + ov + '"></i>' : '') + '</span>';
  }

  // 인덱스 유형 문자 -> 객체 형태 (아이콘/라벨용)
  var IDX_TYPE = { H: 'HostSystem', V: 'VirtualMachine', C: 'ClusterComputeResource' };

  window.VCP_HELPERS = {
    TYPE_LABEL: TYPE_LABEL, IDX_TYPE: IDX_TYPE,
    esc: esc, dash: dash, fmtBytes: fmtBytes, fmtMB: fmtMB, fmtMHz: fmtMHz, fmtDate: fmtDate, fmtUptime: fmtUptime, fmtAgo: fmtAgo,
    deepLink: deepLink, launcherLink: launcherLink, openLink: openLink, linkTo: linkTo,
    loadScript: loadScript, highlight: highlight, nameSuffix: nameSuffix, isDisconnected: isDisconnected, icon: icon,
  };
})();
