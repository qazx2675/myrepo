#!/usr/bin/env python3
# build_handbook.py — 이 폴더의 .md 문서들을 목차/검색이 들어간 단일 HTML 파일 하나로 합칩니다.
#
# 사용법:
#     cd .claude/VM/인수인계
#     python3 build_handbook.py
#
# 결과물: VM_인수인계_핸드북.html  (인터넷/서버 불필요, 브라우저로 열기만 하면 됨)
#
# 파이썬 표준 라이브러리만 사용합니다 (폐쇄망에서도 그대로 동작).
# 외부 마크다운 라이브러리를 쓰지 않으므로 pip install 이 필요 없습니다.
#
# ```mermaid 코드블록은 흐름도로 렌더합니다. 렌더러(vendor/mermaid.min.js)를 HTML 안에
# 그대로 넣으므로(CDN 미사용) 폐쇄망에서도 흐름도가 보입니다.

import html
import os
import re
import sys
from datetime import datetime

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "VM_인수인계_핸드북.html")
MERMAID_JS = os.path.join(HERE, "vendor", "mermaid.min.js")

# 문서를 이 순서로 붙입니다. 여기에 없는 .md 는 파일명 순서로 뒤에 붙습니다.
ORDER = [
    "README.md",
    "00_빠른시작.md",
    "01_기초지식.md",
    "02_공통_실행환경.md",
    "10_V2.md",
    "11_vm-param-check.md",
    "12_vm_verifier.md",
    "13_lpage_search.md",
    "14_Network_Change_Integration_Script.md",
    "15_vCenter_API_IP_자동변경.md",
    "17_VM_setup_잔여도구.md",
    "30_유지보수_AI_활용가이드.md",
    "31_변경요청서_양식.md",
    "40_폴더구조.md",
    "90_용어집.md",
    "91_트러블슈팅_FAQ.md",
    "99_인수인계_체크리스트.md",
]


# --------------------------------------------------------------------------
# 인라인 마크다운 (코드/굵게/기울임/링크/체크박스)
# --------------------------------------------------------------------------

CODE_TOKEN = "\x00CODE%d\x00"


def slug(name):
    """파일명 -> HTML id"""
    return "doc-" + re.sub(r"[^0-9A-Za-z가-힣_-]", "-", name[:-3] if name.endswith(".md") else name)


def heading_id(docslug, text, counter):
    base = re.sub(r"[^0-9A-Za-z가-힣]+", "-", text).strip("-").lower()
    if not base:
        base = "sec"
    key = "%s--%s" % (docslug, base)
    n = counter.get(key, 0)
    counter[key] = n + 1
    return key if n == 0 else "%s-%d" % (key, n)


def fix_link(url):
    """문서 간 상대 링크(./11_xxx.md#anchor)를 단일 HTML 내부 앵커로 바꿉니다."""
    if url.startswith("#"):
        return url
    m = re.match(r"^\.?/?([^/#]+\.md)(#.*)?$", url)
    if m:
        return "#" + slug(m.group(1))
    return url


def inline(text):
    codes = []

    def stash(m):
        codes.append(m.group(1))
        return CODE_TOKEN % (len(codes) - 1)

    # `code` 먼저 빼둔다 (안의 특수문자를 마크다운으로 해석하지 않기 위해)
    text = re.sub(r"`([^`]+)`", stash, text)

    text = html.escape(text, quote=False)

    # 링크 [텍스트](주소)
    def link(m):
        label, url = m.group(1), m.group(2)
        url = fix_link(url)
        ext = ' target="_blank" rel="noreferrer"' if url.startswith("http") else ""
        return '<a href="%s"%s>%s</a>' % (html.escape(url, quote=True), ext, label)

    text = re.sub(r"\[([^\]]*)\]\(([^)\s]+)\)", link, text)

    text = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", text)
    text = re.sub(r"(?<![\w*])\*([^*\n]+)\*(?![\w*])", r"<em>\1</em>", text)

    for i, c in enumerate(codes):
        text = text.replace(CODE_TOKEN % i, "<code>%s</code>" % html.escape(c, quote=False))
    return text


def cells(row):
    """표 한 줄을 셀 목록으로. \\| 는 리터럴 파이프."""
    row = row.strip()
    if row.startswith("|"):
        row = row[1:]
    if row.endswith("|") and not row.endswith("\\|"):
        row = row[:-1]
    parts = re.split(r"(?<!\\)\|", row)
    return [p.strip().replace("\\|", "|") for p in parts]


def checkbox(text):
    if text.startswith("[ ] "):
        return '<span class="cb">☐</span> ' + inline(text[4:])
    if text.lower().startswith("[x] "):
        return '<span class="cb done">☑</span> ' + inline(text[4:])
    return inline(text)


# --------------------------------------------------------------------------
# 블록 변환
# --------------------------------------------------------------------------

def render(md, docslug, counter, toc):
    lines = md.split("\n")
    out = []
    i = 0
    n = len(lines)

    while i < n:
        line = lines[i]

        # 코드 펜스
        m = re.match(r"^\s*```(\S*)\s*$", line)
        if m:
            lang = m.group(1)
            i += 1
            buf = []
            while i < n and not re.match(r"^\s*```\s*$", lines[i]):
                buf.append(lines[i])
                i += 1
            i += 1
            if lang == "mermaid":
                out.append('<div class="mermaid">%s</div>' % html.escape("\n".join(buf), quote=False))
                continue
            out.append('<pre class="lang-%s"><code>%s</code></pre>'
                       % (html.escape(lang, quote=True), html.escape("\n".join(buf), quote=False)))
            continue

        # 원본 HTML 블록 (<details> 등)
        if re.match(r"^\s*</?(details|summary|div|br|img|span|table|hr)\b", line):
            out.append(line)
            i += 1
            continue

        # 제목
        m = re.match(r"^(#{1,6})\s+(.*)$", line)
        if m:
            level = len(m.group(1))
            text = m.group(2).strip()
            hid = heading_id(docslug, text, counter)
            out.append('<h%d id="%s">%s</h%d>' % (level + 1 if level < 6 else 6, hid, inline(text), level + 1 if level < 6 else 6))
            if level == 2:
                toc.append((hid, text))
            i += 1
            continue

        # 수평선
        if re.match(r"^\s*(-{3,}|\*{3,})\s*$", line):
            out.append("<hr>")
            i += 1
            continue

        # 표
        if line.strip().startswith("|") and i + 1 < n and re.match(r"^\s*\|?[\s:\-|]+\|[\s:\-|]*$", lines[i + 1]):
            header = cells(line)
            i += 2
            rows = []
            while i < n and lines[i].strip().startswith("|"):
                rows.append(cells(lines[i]))
                i += 1
            t = ['<div class="tw"><table><thead><tr>']
            t += ["<th>%s</th>" % inline(c) for c in header]
            t.append("</tr></thead><tbody>")
            for r in rows:
                t.append("<tr>" + "".join("<td>%s</td>" % inline(c) for c in r) + "</tr>")
            t.append("</tbody></table></div>")
            out.append("".join(t))
            continue

        # 인용
        if line.strip().startswith(">"):
            buf = []
            while i < n and (lines[i].strip().startswith(">") or (buf and lines[i].strip() and not lines[i].strip().startswith(("#", "|", "-", "*", "1.")))):
                s = lines[i].strip()
                buf.append(re.sub(r"^>\s?", "", s))
                i += 1
            inner = render("\n".join(buf), docslug, counter, [])
            out.append("<blockquote>%s</blockquote>" % inner)
            continue

        # 목록 (2단계까지 들여쓰기 지원)
        m = re.match(r"^(\s*)([-*+]|\d+\.)\s+(.*)$", line)
        if m:
            ordered_root = bool(re.match(r"^\d+\.$", m.group(2)))
            root_tag = "ol" if ordered_root else "ul"
            out.append("<%s>" % root_tag)
            stack = [(len(m.group(1)), root_tag)]
            while i < n:
                mm = re.match(r"^(\s*)([-*+]|\d+\.)\s+(.*)$", lines[i])
                if not mm:
                    if lines[i].strip() == "":
                        # 다음 줄도 목록이면 계속, 아니면 종료
                        if i + 1 < n and re.match(r"^\s*([-*+]|\d+\.)\s+", lines[i + 1]):
                            i += 1
                            continue
                    break
                indent = len(mm.group(1))
                ordered = bool(re.match(r"^\d+\.$", mm.group(2)))
                text = mm.group(3)
                if indent > stack[-1][0]:
                    tag = "ol" if ordered else "ul"
                    out.append("<%s>" % tag)
                    stack.append((indent, tag))
                while indent < stack[-1][0] and len(stack) > 1:
                    out.append("</%s>" % stack[-1][1])
                    stack.pop()
                out.append("<li>%s</li>" % checkbox(text))
                i += 1
            while len(stack) > 1:
                out.append("</%s>" % stack[-1][1])
                stack.pop()
            out.append("</%s>" % root_tag)
            continue

        # 빈 줄
        if line.strip() == "":
            i += 1
            continue

        # 문단
        buf = [line]
        i += 1
        while i < n and lines[i].strip() != "" and not re.match(
                r"^\s*(#{1,6}\s|```|\||>|[-*+]\s|\d+\.\s|-{3,}\s*$|</?(details|summary|div)\b)", lines[i]):
            buf.append(lines[i])
            i += 1
        out.append("<p>%s</p>" % inline(" ".join(x.strip() for x in buf)))

    return "\n".join(out)


# --------------------------------------------------------------------------
# CSS / JS
# --------------------------------------------------------------------------

CSS = """
:root{
  --bg:#ffffff; --fg:#1c2024; --muted:#5b6570; --line:#e2e6ea; --line2:#eef1f4;
  --side:#f7f9fa; --accent:#1f6feb; --accent-soft:#e8f0fe;
  --code-bg:#f4f6f8; --pre-bg:#1e2430; --pre-fg:#e6edf3;
  --th:#eef2f5; --mark:#fff3b0;
}
@media (prefers-color-scheme: dark){
  :root:not([data-theme="light"]){
    --bg:#12161c; --fg:#dfe5ec; --muted:#98a3b0; --line:#2a323d; --line2:#222932;
    --side:#171d25; --accent:#6ea8ff; --accent-soft:#1d2a3f;
    --code-bg:#1c232c; --pre-bg:#0d1117; --pre-fg:#e6edf3;
    --th:#1c232c; --mark:#5a4a00;
  }
}
:root[data-theme="dark"]{
  --bg:#12161c; --fg:#dfe5ec; --muted:#98a3b0; --line:#2a323d; --line2:#222932;
  --side:#171d25; --accent:#6ea8ff; --accent-soft:#1d2a3f;
  --code-bg:#1c232c; --pre-bg:#0d1117; --pre-fg:#e6edf3;
  --th:#1c232c; --mark:#5a4a00;
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Apple SD Gothic Neo","Noto Sans KR","Malgun Gothic",sans-serif;
  font-size:15px;line-height:1.75;-webkit-text-size-adjust:100%}
.layout{display:flex;min-height:100vh}

/* 사이드바 */
#side{width:300px;flex:0 0 300px;background:var(--side);border-right:1px solid var(--line);
  height:100vh;position:sticky;top:0;overflow-y:auto;padding:18px 0 60px}
#side .brand{padding:0 18px 12px;border-bottom:1px solid var(--line);margin-bottom:10px}
#side .brand b{display:block;font-size:15px}
#side .brand span{color:var(--muted);font-size:12px}
#q{width:calc(100% - 36px);margin:10px 18px;padding:8px 10px;border:1px solid var(--line);
  border-radius:8px;background:var(--bg);color:var(--fg);font-size:13px;font-family:inherit}
#toc{list-style:none;margin:0;padding:0 8px}
#toc li{margin:0}
#toc a{display:block;padding:5px 10px;border-radius:6px;color:var(--fg);text-decoration:none;font-size:13.5px}
#toc a:hover{background:var(--accent-soft)}
#toc a.lv2{padding-left:26px;color:var(--muted);font-size:12.5px}
#toc a.doc{font-weight:600;margin-top:6px}
#toc a.active{background:var(--accent-soft);color:var(--accent)}
#toc .grp{padding:12px 12px 4px;font-size:11px;letter-spacing:.06em;color:var(--muted);text-transform:uppercase}

/* 본문 */
main{flex:1;min-width:0;padding:28px 44px 120px;max-width:1080px}
h1,h2,h3,h4,h5,h6{line-height:1.35;margin:1.8em 0 .6em;scroll-margin-top:18px}
h1{font-size:26px;border-bottom:2px solid var(--line);padding-bottom:.35em;margin-top:0}
h2{font-size:21px;border-bottom:1px solid var(--line2);padding-bottom:.3em}
h3{font-size:17px}
h4{font-size:15px;color:var(--muted)}
p{margin:.7em 0}
a{color:var(--accent)}
ul,ol{margin:.6em 0;padding-left:1.5em}
li{margin:.25em 0}
code{background:var(--code-bg);padding:.12em .38em;border-radius:4px;font-size:.88em;
  font-family:"SFMono-Regular",Consolas,"Liberation Mono",Menlo,monospace;word-break:break-word}
pre{background:var(--pre-bg);color:var(--pre-fg);padding:14px 16px;border-radius:8px;
  overflow-x:auto;font-size:13px;line-height:1.6}
pre code{background:none;padding:0;color:inherit;font-size:inherit;white-space:pre}
blockquote{margin:1em 0;padding:.6em 1em;border-left:4px solid var(--accent);
  background:var(--accent-soft);border-radius:0 6px 6px 0}
blockquote p:first-child{margin-top:0}
blockquote p:last-child{margin-bottom:0}
.tw{overflow-x:auto;margin:1em 0;border:1px solid var(--line);border-radius:8px}
table{border-collapse:collapse;width:100%;font-size:13.5px}
th,td{border-bottom:1px solid var(--line2);padding:8px 12px;text-align:left;vertical-align:top}
th{background:var(--th);font-weight:600;white-space:nowrap}
tr:last-child td{border-bottom:none}
hr{border:none;border-top:1px solid var(--line);margin:2em 0}
details{border:1px solid var(--line);border-radius:8px;padding:10px 14px;margin:1em 0;background:var(--side)}
summary{cursor:pointer;font-weight:600}
.cb{font-size:1.05em}
.cb.done{color:var(--accent)}
.doc{border-top:3px solid var(--line);padding-top:26px;margin-top:52px}
.doc:first-of-type{border-top:none;margin-top:0;padding-top:0}
.docmeta{font-size:11.5px;color:var(--muted);margin:-.4em 0 1.2em}
mark{background:var(--mark);color:inherit}
.mermaid{position:relative;margin:1em 0;padding:14px;border:1px solid var(--line);border-radius:8px;
  background:var(--bg);overflow-x:auto;text-align:center;white-space:pre}
.mermaid[data-done]{white-space:normal}
.mermaid svg{max-width:100%;height:auto}
.hb-legend{position:absolute;top:10px;left:12px;z-index:2;display:flex;flex-direction:column;
  gap:6px;font-size:15px;font-weight:700;line-height:1;color:var(--fg);
  background:var(--side);border:1.5px solid var(--fg);opacity:1;padding:8px 12px;
  border-radius:7px;pointer-events:none;white-space:nowrap;box-shadow:0 3px 10px rgba(0,0,0,.28)}
.hb-legend span{display:flex;align-items:center;gap:8px}
.hb-legend .ic{display:inline-block;flex:none;box-sizing:border-box}
.hb-legend .ic.rect{width:21px;height:15px;border:3px solid currentColor;border-radius:3px}
.hb-legend .ic.stadium{width:21px;height:15px;border:3px solid currentColor;border-radius:8px}
.hb-legend .ic.dashedbox{width:21px;height:15px;border:2.5px dashed currentColor;border-radius:3px}
.hb-legend .ic.diamond{width:15px;height:15px;border:3px solid currentColor;
  transform:rotate(45deg);margin:0 3px}
.hb-legend .ic.arrow,.hb-legend .ic.thickarrow,.hb-legend .ic.dashedarrow{
  width:22px;height:3px;background:currentColor;position:relative;margin-right:6px}
.hb-legend .ic.arrow::after,.hb-legend .ic.thickarrow::after,.hb-legend .ic.dashedarrow::after{
  content:'';position:absolute;right:-1px;top:-4px;
  border-left:8px solid currentColor;border-top:5px solid transparent;border-bottom:5px solid transparent}
.hb-legend .ic.thickarrow{height:5px}
.hb-legend .ic.thickarrow::after{top:-6px;border-left-width:10px;border-top-width:7px;border-bottom-width:7px}
.hb-legend .ic.dashedarrow{background:repeating-linear-gradient(90deg,currentColor 0 5px,transparent 5px 9px)}
@media print{.hb-legend{display:none}}
.mermaid .node rect,.mermaid .node polygon{rx:8px;ry:8px;filter:drop-shadow(0 1px 1.5px rgba(0,0,0,.14))}
.mermaid .cluster rect{rx:12px;ry:12px;stroke-dasharray:5 4}
.mermaid .cluster-label,.mermaid .cluster .nodeLabel{font-weight:700;font-size:15px}
.mermaid .edgeLabel{font-size:12.5px}

/* 상단 바 (모바일) */
#topbar{display:none;position:sticky;top:0;z-index:10;background:var(--side);
  border-bottom:1px solid var(--line);padding:10px 14px;align-items:center;gap:10px}
#topbar button{font:inherit;font-size:13px;padding:6px 10px;border:1px solid var(--line);
  background:var(--bg);color:var(--fg);border-radius:6px;cursor:pointer}
#theme{position:fixed;right:18px;bottom:18px;z-index:20;font:inherit;font-size:12px;
  padding:8px 12px;border:1px solid var(--line);background:var(--side);color:var(--fg);
  border-radius:20px;cursor:pointer;box-shadow:0 2px 8px rgba(0,0,0,.12)}
#top{position:fixed;right:18px;bottom:60px;z-index:20;font:inherit;font-size:12px;
  padding:8px 12px;border:1px solid var(--line);background:var(--side);color:var(--fg);
  border-radius:20px;cursor:pointer;box-shadow:0 2px 8px rgba(0,0,0,.12)}

@media (max-width:900px){
  .layout{display:block}
  #topbar{display:flex}
  #side{position:fixed;left:0;top:0;bottom:0;z-index:30;transform:translateX(-100%);
    transition:transform .18s ease;box-shadow:2px 0 12px rgba(0,0,0,.18);width:86%;max-width:320px}
  #side.open{transform:translateX(0)}
  main{padding:18px 16px 100px}
  h1{font-size:22px} h2{font-size:19px}
}
@media print{
  #side,#topbar,#theme,#top{display:none!important}
  main{max-width:none;padding:0}
  .doc{page-break-before:always}
  .doc:first-of-type{page-break-before:auto}
  pre,blockquote,.tw{page-break-inside:avoid}
  a{color:inherit;text-decoration:none}
}
"""

JS = """
(function(){
  var side=document.getElementById('side');
  var q=document.getElementById('q');
  var toc=document.getElementById('toc');
  var links=[].slice.call(toc.querySelectorAll('a'));

  document.getElementById('menu').onclick=function(){side.classList.toggle('open');};
  document.getElementById('top').onclick=function(){window.scrollTo({top:0,behavior:'smooth'});};

  var t=document.getElementById('theme');
  function cur(){try{return localStorage.getItem('hb-theme')||'';}catch(e){return '';}}
  function apply(v){
    if(v){document.documentElement.setAttribute('data-theme',v);}
    else{document.documentElement.removeAttribute('data-theme');}
    t.textContent = v==='dark' ? '🌙 어둡게' : (v==='light' ? '☀️ 밝게' : '🖥 시스템');
  }
  apply(cur());
  t.onclick=function(){
    var v=cur(); var nx = v==='' ? 'light' : (v==='light' ? 'dark' : '');
    try{localStorage.setItem('hb-theme',nx);}catch(e){}
    apply(nx);
    if(window.renderMermaid) window.renderMermaid();
  };

  // 목차 검색
  q.addEventListener('input',function(){
    var s=q.value.trim().toLowerCase();
    links.forEach(function(a){
      var hit = !s || a.textContent.toLowerCase().indexOf(s)>=0;
      a.style.display = hit ? '' : 'none';
    });
    [].forEach.call(toc.querySelectorAll('.grp'),function(g){
      g.style.display = s ? 'none' : '';
    });
  });
  q.addEventListener('keydown',function(e){ if(e.key==='Escape'){q.value='';q.dispatchEvent(new Event('input'));} });

  // 스크롤 위치에 따라 목차 강조
  var heads=[].slice.call(document.querySelectorAll('main h2, main h3.docttl, main .doc > h2'));
  var targets=[].slice.call(document.querySelectorAll('main [id]'));
  var map={}; links.forEach(function(a){ map[a.getAttribute('href').slice(1)]=a; });
  function onScroll(){
    var y=window.scrollY+120, act=null;
    for(var i=0;i<targets.length;i++){ if(targets[i].offsetTop<=y) act=targets[i].id; else break; }
    links.forEach(function(a){a.classList.remove('active');});
    if(act && map[act]) map[act].classList.add('active');
  }
  window.addEventListener('scroll',onScroll,{passive:true});
  onScroll();

  // 모바일에서 목차 클릭하면 닫기
  toc.addEventListener('click',function(e){
    if(e.target.tagName==='A' && window.innerWidth<=900) side.classList.remove('open');
  });
})();
"""


MERMAID_INIT = """
(function(){
  var els=[].slice.call(document.querySelectorAll('.mermaid'));
  els.forEach(function(el){ el.setAttribute('data-src', el.textContent); });
  function dark(){
    var t=document.documentElement.getAttribute('data-theme');
    if(t) return t==='dark';
    return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
  }
  window.renderMermaid=function(){
    els.forEach(function(el){
      el.removeAttribute('data-processed'); el.removeAttribute('data-done');
      el.textContent=el.getAttribute('data-src');
    });
    var d=dark();
    // 문서 안에서 ":::이름" / "class X 이름" 으로 쓰는 공통 색 (밝게/어둡게 따로)
    var C = d ? {
      input:'#1c2a45,#5b8def', cmd:'#133236,#3fb6b0', change:'#3a2a14,#f0a04b',
      gate:'#2a2145,#a38bf0', warn:'#3d1d1d,#ef6b6b', safe:'#173019,#5cc25c', fg:'#e5e7eb'
    } : {
      input:'#e8f0fe,#3b6fd8', cmd:'#e3f5f4,#26918b', change:'#fff1e0,#dd8a2a',
      gate:'#f1ecff,#7c5cd6', warn:'#fdeaea,#c94242', safe:'#e8f6e8,#3c9a3c', fg:'#1f2937'
    };
    var defs=Object.keys(C).filter(function(k){return k!=='fg';}).map(function(k){
      var p=C[k].split(',');
      return '    classDef '+k+' fill:'+p[0]+',stroke:'+p[1]+',stroke-width:1.5px,color:'+C.fg;
    }).join('\\n');
    els.forEach(function(el){
      if(/^\\s*(flowchart|graph)\\b/.test(el.textContent)) el.textContent += '\\n'+defs;
    });
    mermaid.initialize({startOnLoad:false, securityLevel:'strict', theme:'base',
      fontFamily:'inherit',
      flowchart:{curve:'basis', nodeSpacing:36, rankSpacing:46, padding:14, htmlLabels:true},
      themeVariables: d ? {
        primaryColor:'#1e293b', primaryBorderColor:'#475569', primaryTextColor:'#e5e7eb',
        lineColor:'#94a3b8', textColor:'#e5e7eb', fontSize:'14px',
        clusterBkg:'#111a2e', clusterBorder:'#334155', edgeLabelBackground:'#0f172a'
      } : {
        primaryColor:'#f6f8fb', primaryBorderColor:'#94a3b8', primaryTextColor:'#1f2937',
        lineColor:'#64748b', textColor:'#1f2937', fontSize:'14px',
        clusterBkg:'#f8fafc', clusterBorder:'#cbd5e1', edgeLabelBackground:'#ffffff'
      }});
    mermaid.run({nodes: els}).then(function(){
      els.forEach(function(el){ el.setAttribute('data-done','1'); addLegend(el); });
    }).catch(function(e){ if(window.console) console.error(e); });
  };
  // 흐름도마다 실제로 쓰인 기호만 골라 11시 방향에 크고 또렷하게 범례 표시.
  // 유니코드 도형 문자는 글꼴에 따라 네모(tofu)로 깨지므로, 실제 모양과 같은
  // 작은 아이콘을 CSS로 직접 그린다(border/diamond/dashed box/화살표).
  function addLegend(el){
    var old=el.querySelector('.hb-legend'); if(old) old.remove();
    var src=el.getAttribute('data-src')||'';
    var items=[];
    if(/\\[\\(/.test(src) || /\\(\\(/.test(src)) items.push(['stadium','\\uc2dc\\uc791/\\uc885\\ub8cc']);
    if(/\\{"/.test(src)) items.push(['diamond','\\uc608/\\uc544\\ub2c8\\uc624']);
    items.push(['rect','\\ucc98\\ub9ac']);
    if(/-\\.->/.test(src)) items.push(['dashedarrow','\\ucc38\\uace0/\\uc0dd\\ub7b5 \\uac00\\ub2a5']);
    if(/==>/.test(src)) items.push(['thickarrow','\\ub2e8\\uacc4 \\uc804\\ud658']);
    else items.push(['arrow','\\ub2e4\\uc74c \\ub2e8\\uacc4']);
    if(/subgraph/.test(src)) items.push(['dashedbox','\\ub2e8\\uacc4 \\ubb36\\uc74c']);
    var leg=document.createElement('div');
    leg.className='hb-legend';
    leg.innerHTML=items.map(function(it){
      return '<span><i class="ic '+it[0]+'"></i>'+it[1]+'</span>';
    }).join('');
    el.insertBefore(leg, el.firstChild);
  }
  window.renderMermaid();
})();
"""


def main():
    files =[f for f in os.listdir(HERE) if f.endswith(".md")]
    ordered = [f for f in ORDER if f in files] + sorted(f for f in files if f not in ORDER)
    if not ordered:
        print("[!] .md 파일이 없습니다.", file=sys.stderr)
        return 1

    counter = {}
    body = []
    toc_html = []
    groups = {
        "README.md": "시작",
        "10_V2.md": "도구별 문서",
        "30_유지보수_AI_활용가이드.md": "유지보수",
        "40_폴더구조.md": "찾아보기",
    }

    for fn in ordered:
        with open(os.path.join(HERE, fn), encoding="utf-8") as fp:
            md = fp.read()
        ds = slug(fn)
        toc2 = []
        rendered = render(md, ds, counter, toc2)

        m = re.search(r"^#\s+(.*)$", md, re.M)
        title = m.group(1).strip() if m else fn[:-3]

        body.append('<section class="doc" id="%s">' % ds)
        body.append('<div class="docmeta">%s</div>' % html.escape(fn, quote=False))
        body.append(rendered)
        body.append("</section>")

        if fn in groups:
            toc_html.append('<li class="grp">%s</li>' % html.escape(groups[fn], quote=False))
        toc_html.append('<li><a class="doc" href="#%s">%s</a></li>' % (ds, html.escape(title, quote=False)))
        for hid, text in toc2:
            toc_html.append('<li><a class="lv2" href="#%s">%s</a></li>' % (hid, html.escape(text, quote=False)))

    stamp = datetime.now().strftime("%Y-%m-%d %H:%M")

    mermaid_html = ""
    if any('class="mermaid"' in b for b in body):
        if not os.path.exists(MERMAID_JS):
            print("[!] %s 가 없습니다. 흐름도를 렌더할 수 없습니다." % MERMAID_JS, file=sys.stderr)
            return 1
        with open(MERMAID_JS, encoding="utf-8") as fp:
            mjs = fp.read()
        if "</script" in mjs.lower():
            print("[!] mermaid.min.js 에 </script 가 있어 인라인할 수 없습니다.", file=sys.stderr)
            return 1
        mermaid_html = "<script>%s</script>\n<script>%s</script>" % (mjs, MERMAID_INIT)
    page = """<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>VM 자동화 도구 인수인계 핸드북</title>
<style>%s</style>
</head>
<body>
<div id="topbar"><button id="menu">☰ 목차</button><b>VM 인수인계 핸드북</b></div>
<div class="layout">
  <nav id="side">
    <div class="brand"><b>VM 인수인계 핸드북</b><span>생성: %s</span></div>
    <input id="q" type="search" placeholder="목차 검색 (Esc 초기화)" autocomplete="off">
    <ul id="toc">%s</ul>
  </nav>
  <main>%s</main>
</div>
<button id="top">↑ 맨 위</button>
<button id="theme">🖥 시스템</button>
<script>%s</script>
%s
</body>
</html>
""" % (CSS, stamp, "\n".join(toc_html), "\n".join(body), JS, mermaid_html)

    with open(OUT, "w", encoding="utf-8") as fp:
        fp.write(page)

    size = os.path.getsize(OUT) / 1024.0
    print("생성 완료: %s (%d개 문서, %.0f KB)" % (OUT, len(ordered), size))
    return 0


if __name__ == "__main__":
    sys.exit(main())
