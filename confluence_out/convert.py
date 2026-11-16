#!/usr/bin/env python3
"""mail_draft_outlook.html -> Confluence storage-format XHTML (+ standalone XHTML).

Usage: python convert.py src/orig.html out_dir
Outputs:
  out_dir/guide_confluence.xhtml   Confluence storage format (images = attachments)
  out_dir/guide_standalone.xhtml   well-formed XHTML, images embedded (browser preview)
  out_dir/attachments/figN_*.png   images to attach to the Confluence page
"""
import base64, html, re, sys, os
from bs4 import BeautifulSoup

src, out = sys.argv[1], sys.argv[2]
FILES = ["fig1_header_settings.png", "fig2_tab1_selfcheck.png", "fig3_tab2_worker.png",
         "fig4_tab3_checker.png", "fig5_help_popover.png"]
CIRC = "①②③④⑤⑥⑦⑧⑨"

soup = BeautifulSoup(open(src, encoding="utf-8").read(), "html.parser")
os.makedirs(os.path.join(out, "attachments"), exist_ok=True)
esc = lambda s: html.escape(s, quote=False)


def inline(node):
    """Inner content -> XHTML keeping only <strong>/<br/>."""
    r = ""
    for c in node.children:
        if isinstance(c, str):
            r += esc(c)
        elif c.name == "strong":
            r += "<strong>%s</strong>" % inline(c)
        elif c.name == "br":
            r += "<br/>"
        else:
            r += inline(c)
    return r


# ---- sections -------------------------------------------------------------
sections = []
for i, h2 in enumerate(soup.find_all("h2")):
    box = h2.parent
    img = box.find("img")
    data = img["src"].split("base64,")[1]
    open(os.path.join(out, "attachments", FILES[i]), "wb").write(base64.b64decode(data))
    cap = img.find_next_sibling("div").get_text(strip=True)
    items = []
    for li in box.find("ul").find_all("li", recursive=False):
        strong = li.find("strong")
        title = strong.get_text(strip=True)
        rest = li.get_text().split(strong.get_text(), 1)[1]
        rest = re.sub(r"^\s*:\s*", "", rest).strip()
        items.append((title, rest))
    sections.append(dict(title=h2.get_text(strip=True), img=FILES[i], data=data, cap=cap, items=items))

# ---- intro / flow / tips --------------------------------------------------
body = soup.body.find("div")
paras = [p for p in body.find_all("p", recursive=False)]
intro1, intro2 = inline(paras[0]), inline(paras[1])
outro = inline(paras[-1])
mail_subject = body.find("strong").next_sibling.strip()  # 메일 제목 text
flow_div = [d for d in body.find_all("div", recursive=False) if "한눈에" in d.get_text()][0]
flow = re.split(r"<br\s*/?>", "".join(str(c) for c in flow_div.children))[1:]
flow = [inline(BeautifulSoup(f, "html.parser")).strip() for f in flow if f.strip()]
tips_div = [d for d in body.find_all("div", recursive=False) if "사용 시 참고 팁" in d.get_text()][0]
tips = [inline(li) for li in tips_div.find_all("li")]


def macro(name, content, title=None):
    t = '<ac:parameter ac:name="title">%s</ac:parameter>' % esc(title) if title else ""
    return ('<ac:structured-macro ac:name="%s" ac:schema-version="1">%s'
            '<ac:rich-text-body>%s</ac:rich-text-body></ac:structured-macro>') % (name, t, content)


def items_table(items):
    rows = ""
    for n, (t, d) in enumerate(items):
        rows += ("<tr><td style=\"text-align: center;\">%s</td><td><strong>%s</strong></td><td>%s</td></tr>"
                 % (CIRC[n], esc(t), esc(d)))
    return ('<table><colgroup><col style="width: 50px;"/><col style="width: 260px;"/><col/></colgroup><tbody>'
            '<tr><th style="text-align: center;">번호</th><th>항목</th><th>설명</th></tr>%s</tbody></table>' % rows)


# ---- Confluence storage format -------------------------------------------
c = []
c.append(macro("info", "<p><strong>메일 제목(권장):</strong> %s</p>" % esc(mail_subject)))
c.append("<p>%s</p>" % intro1)
c.append("<p>%s</p>" % intro2)
c.append(macro("panel", "<ol>%s</ol>" % "".join("<li>%s</li>" % f.split(") ", 1)[-1] if re.match(r"^\d\) ", f) else "<li>%s</li>" % f for f in flow),
               title="한눈에 보는 전체 사용 흐름"))
for s in sections:
    c.append("<h2>%s</h2>" % esc(s["title"]))
    c.append('<p style="text-align: center;"><ac:image ac:width="624" ac:alt="%s"><ri:attachment ri:filename="%s"/></ac:image></p>'
             % (esc(s["title"]).replace('"', "&quot;"), s["img"]))
    c.append('<p style="text-align: center;"><em>%s</em></p>' % esc(s["cap"]))
    c.append(items_table(s["items"]))
c.append("<h2>사용 시 참고 팁</h2>")
c.append(macro("tip", "<ul>%s</ul>" % "".join("<li>%s</li>" % t for t in tips)))
c.append("<p>%s</p>" % outro)
open(os.path.join(out, "guide_confluence.xhtml"), "w", encoding="utf-8").write("\n".join(c) + "\n")

# ---- standalone XHTML -----------------------------------------------------
st = []
st.append('<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE html>\n'
          '<html xmlns="http://www.w3.org/1999/xhtml" lang="ko" xml:lang="ko">\n<head>\n'
          '<meta charset="UTF-8"/>\n<title>[안내] 작업대상 호스트 교차 검증기 사용 가이드</title>\n'
          '<style>body{font-family:"Malgun Gothic",Arial,sans-serif;max-width:800px;margin:24px auto;color:#1e293b;line-height:1.7}'
          'h2{border-left:4px solid #2563eb;padding-left:10px;font-size:18px}'
          'table{border-collapse:collapse;width:100%;margin:8px 0 24px}th,td{border:1px solid #cbd5e1;padding:6px 10px;vertical-align:top;font-size:14px}'
          'th{background:#f1f5f9}.c{text-align:center}img{max-width:624px;width:100%;border:1px solid #94a3b8}'
          '.box{background:#eff6ff;border:1px solid #bfdbfe;padding:10px 14px;margin:12px 0}</style>\n</head>\n<body>')
st.append('<div class="box"><strong>메일 제목(권장):</strong> %s</div>' % esc(mail_subject))
st.append("<p>%s</p><p>%s</p>" % (intro1, intro2))
st.append('<div class="box"><strong>한눈에 보는 전체 사용 흐름</strong><ol>%s</ol></div>'
          % "".join("<li>%s</li>" % (f.split(") ", 1)[-1] if re.match(r"^\d\) ", f) else f) for f in flow))
for s in sections:
    st.append("<h2>%s</h2>" % esc(s["title"]))
    st.append('<p class="c"><img src="data:image/png;base64,%s" alt="%s"/><br/><em>%s</em></p>'
              % (s["data"], esc(s["title"]).replace('"', "&quot;"), esc(s["cap"])))
    st.append(items_table(s["items"]).replace(' style="text-align: center;"', ' class="c"'))
st.append('<h2>사용 시 참고 팁</h2><ul>%s</ul><p>%s</p>' % ("".join("<li>%s</li>" % t for t in tips), outro))
st.append("</body>\n</html>\n")
open(os.path.join(out, "guide_standalone.xhtml"), "w", encoding="utf-8").write("\n".join(st))
print("sections", len(sections), "tips", len(tips), "flow", len(flow))
