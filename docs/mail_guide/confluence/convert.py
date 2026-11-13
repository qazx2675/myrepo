# -*- coding: utf-8 -*-
"""Convert docs/mail_guide/mail_draft_outlook.html into Confluence Storage Format XHTML
and a standalone preview XHTML document.

Usage:
    python convert.py ../mail_draft_outlook.html . [--extract-attachments]
"""
import argparse
import base64
import html
import re
import sys
from pathlib import Path
from bs4 import BeautifulSoup

CIRC = "①②③④⑤⑥⑦⑧⑨"
DEFAULT_FILES = [
    "fig1_header_settings.png",
    "fig2_awx_settings.png",
    "fig3_tab1_selfcheck.png",
    "fig4_tab2_worker.png",
    "fig5_tab3_checker.png",
    "fig6_help_popover.png",
]


def xesc(text: str) -> str:
    """Escape text for XML/XHTML (UTF-8 safe, no named HTML entities)."""
    return html.escape(text, quote=False)


def xattr(val: str) -> str:
    """Escape attribute value for XML/XHTML."""
    return html.escape(val, quote=True)


def extract_sections(soup: BeautifulSoup):
    """Parse setup table, image sections (h2 + img + legend items), and tip list from HTML."""
    setup_title = ""
    setup_rows = []
    setup_div = soup.find("div", id="setup-section")
    if setup_div:
        h2 = setup_div.find("h2")
        setup_title = h2.get_text(strip=True) if h2 else "0. 설치·실행 및 초기 설정"
        for tr in setup_div.find_all("tr"):
            tds = tr.find_all(["td", "th"])
            if len(tds) >= 2:
                setup_rows.append((tds[0].get_text(strip=True), tds[1].get_text(strip=True)))

    sections = []
    fallback_idx = 0
    for h2 in soup.find_all("h2"):
        box = h2.parent
        if setup_div and box is setup_div:
            continue
        img = box.find("img")
        ul = box.find("ul")
        if not img or not ul:
            continue

        title = h2.get_text(strip=True)
        alt = img.get("alt", title)
        src_val = img.get("src", "")
        fname = img.get("data-filename", "").strip()
        if not fname:
            fname = DEFAULT_FILES[fallback_idx] if fallback_idx < len(DEFAULT_FILES) else f"fig{fallback_idx + 1}.png"
        fallback_idx += 1

        b64_data = ""
        m = re.match(r"^data:image/png;base64,(.+)$", src_val.strip())
        if m:
            b64_data = m.group(1)

        cap_div = img.find_next_sibling("div")
        caption = cap_div.get_text(strip=True) if cap_div else ""

        items = []
        for idx_li, li in enumerate(ul.find_all("li"), start=1):
            strong = li.find("strong")
            name = re.sub(r"\s+", " ", strong.get_text("")).strip() if strong else f"항목 {idx_li}"
            full_text = re.sub(r"\s+", " ", li.get_text("")).strip()
            # Strip leading badge digit + space + name + colon
            desc = full_text
            if strong:
                pos = full_text.find(name)
                if pos >= 0:
                    desc = full_text[pos + len(name):].lstrip(" :\u00a0")
            items.append((idx_li, name, desc))

        sections.append({
            "title": title,
            "alt": alt,
            "filename": fname,
            "b64": b64_data,
            "caption": caption,
            "items": items,
        })

    # Find flow block ("한눈에 보는 전체 사용 흐름") and tip block ("사용 시 참고 팁")
    flow_lines = []
    tip_items = []
    for div in soup.find_all("div"):
        st = div.find("strong")
        if not st:
            continue
        label = st.get_text(strip=True)
        if "한눈에 보는 전체 사용 흐름" in label and not div.find("div"):
            clone = BeautifulSoup(str(div), "html.parser")
            for br in clone.find_all("br"):
                br.replace_with("\n")
            raw_lines = [re.sub(r"[ \t]+", " ", ln).strip() for ln in clone.get_text("").splitlines() if ln.strip()]
            for ln in raw_lines:
                if "한눈에 보는 전체 사용 흐름" in ln:
                    continue
                ln_clean = re.sub(r"^\d+\)\s*", "", ln)
                if ln_clean:
                    flow_lines.append(ln_clean)
        elif "사용 시 참고 팁" in label and not div.find("div"):
            ul = div.find("ul")
            if ul:
                for li in ul.find_all("li"):
                    s_tag = li.find("strong")
                    key = re.sub(r"\s+", " ", s_tag.get_text("")).strip() if s_tag else ""
                    txt = re.sub(r"\s+", " ", li.get_text("")).strip()
                    val = txt[len(key):].lstrip(" :\u00a0") if key and txt.startswith(key) else txt
                    tip_items.append((key, val))

    return {
        "setup_title": setup_title,
        "setup_rows": setup_rows,
        "flow_lines": flow_lines,
        "sections": sections,
        "tip_items": tip_items,
    }


def build_confluence_xhtml(parsed: dict) -> str:
    """Build Confluence Storage Format XHTML XML."""
    parts = [
        f"<!-- Confluence 첨부 필요 이미지({len(parsed['sections'])}개): "
        + ", ".join(s["filename"] for s in parsed["sections"])
        + " -->",
        "<p>안녕하세요.</p>",
        "<p>작업 전 AWX 호스트 대상 오류를 사전에 방지하고, 작업자와 확인자가 서로 교차 검증(Cross-Check)을 진행할 수 있도록 개선된 "
        "<strong>작업대상 호스트 교차 검증기(host-cross-checker.exe)</strong>의 초기 설정 방법과 실제 실행 화면 기준 항목별 사용법을 안내드립니다.</p>",
    ]

    if parsed["flow_lines"]:
        flow_html = "".join(f"<li>{xesc(line)}</li>" for line in parsed["flow_lines"])
        parts.append(
            '<ac:structured-macro ac:name="info">'
            '<ac:parameter ac:name="title">한눈에 보는 전체 사용 흐름</ac:parameter>'
            f'<ac:rich-text-body><ol>{flow_html}</ol></ac:rich-text-body>'
            '</ac:structured-macro>'
        )

    if parsed["setup_rows"]:
        rows_xml = "".join(
            f"<tr><td><strong>{xesc(k)}</strong></td><td>{xesc(v)}</td></tr>"
            for k, v in parsed["setup_rows"]
        )
        parts.append(f"<h2>{xesc(parsed['setup_title'])}</h2>")
        parts.append(
            '<table class="wrapped">'
            '<colgroup><col style="width: 240px;"/><col/></colgroup>'
            f"<tbody><tr><th>구성 항목</th><th>설정 및 동작 설명</th></tr>{rows_xml}</tbody>"
            "</table>"
        )
        parts.append(
            '<ac:structured-macro ac:name="note">'
            '<ac:parameter ac:name="title">자격증명 보안 유지 (Windows DPAPI)</ac:parameter>'
            '<ac:rich-text-body><p>AWX 비밀번호와 API Token은 평문으로 저장되지 않고 '
            '현재 PC/사용자 전용 Windows DPAPI로 암호화(<code>awxPasswordEnc</code>, <code>awxTokenEnc</code>)되어 '
            '<code>config.json</code>에 저장됩니다. 다른 PC로 <code>config.json</code>을 복사하면 복호화가 차단되며 재입력 안내가 표시됩니다.</p></ac:rich-text-body>'
            '</ac:structured-macro>'
        )

    for sec in parsed["sections"]:
        rows_xml = []
        for idx, name, desc in sec["items"]:
            circ = CIRC[idx - 1] if 1 <= idx <= len(CIRC) else f"{idx}."
            rows_xml.append(
                f"<tr><td><strong>{xesc(circ)}</strong></td>"
                f"<td><strong>{xesc(name)}</strong></td>"
                f"<td>{xesc(desc)}</td></tr>"
            )
        cap_xml = f"<p><em>{xesc(sec['caption'])}</em></p>" if sec["caption"] else ""
        parts.append(f"<h2>{xesc(sec['title'])}</h2>")
        parts.append(
            '<ac:structured-macro ac:name="panel">'
            '<ac:parameter ac:name="borderStyle">solid</ac:parameter>'
            '<ac:parameter ac:name="borderColor">#cbd5e1</ac:parameter>'
            '<ac:parameter ac:name="bgColor">#f8fafc</ac:parameter>'
            '<ac:rich-text-body>'
            f'<p style="text-align: center;"><ac:image ac:width="624" ac:alt="{xattr(sec["alt"])}">'
            f'<ri:attachment ri:filename="{xattr(sec["filename"])}"/></ac:image></p>'
            f"{cap_xml}"
            "</ac:rich-text-body>"
            "</ac:structured-macro>"
        )
        parts.append(
            '<table class="wrapped">'
            '<colgroup><col style="width: 48px;"/><col style="width: 230px;"/><col/></colgroup>'
            f"<tbody><tr><th>번호</th><th>항목</th><th>설명</th></tr>{''.join(rows_xml)}</tbody>"
            "</table>"
        )

    if parsed["tip_items"]:
        tips_xml = "".join(
            f"<li><strong>{xesc(k)}</strong>: {xesc(v)}</li>" if k else f"<li>{xesc(v)}</li>"
            for k, v in parsed["tip_items"]
        )
        parts.append(
            '<ac:structured-macro ac:name="tip">'
            '<ac:parameter ac:name="title">사용 시 참고 팁</ac:parameter>'
            f'<ac:rich-text-body><ul>{tips_xml}</ul></ac:rich-text-body>'
            '</ac:structured-macro>'
        )

    parts.append("<p>감사합니다.</p>")
    return "\n".join(parts) + "\n"


def build_standalone_xhtml(parsed: dict) -> str:
    """Build valid standalone XHTML 1.0 Strict document with embedded base64 images."""
    body_parts = [
        '<div class="container">',
        '<div class="header-banner">',
        '<h1>작업대상 호스트 교차 검증기 (host-cross-checker.exe) 설정 및 사용 가이드</h1>',
        '<p class="subtitle">Confluence 게시용 / 로컬 미리보기 겸용 XHTML 1.0 Strict 문서</p>',
        '</div>',
        '<p>안녕하세요.</p>',
        '<p>작업 전 AWX 호스트 대상 오류를 사전에 방지하고, 작업자와 확인자가 서로 교차 검증(Cross-Check)을 진행할 수 있도록 개선된 '
        '<strong>작업대상 호스트 교차 검증기(host-cross-checker.exe)</strong>의 초기 설정 방법과 실제 실행 화면 기준 항목별 사용법을 안내드립니다.</p>',
    ]

    if parsed["flow_lines"]:
        li_flow = "".join(f"<li>{xesc(line)}</li>" for line in parsed["flow_lines"])
        body_parts.append(
            '<div class="macro info">'
            '<div class="macro-title">한눈에 보는 전체 사용 흐름</div>'
            f'<ol>{li_flow}</ol>'
            '</div>'
        )

    if parsed["setup_rows"]:
        rows_html = "".join(
            f'<tr><td class="name-col"><strong>{xesc(k)}</strong></td><td>{xesc(v)}</td></tr>'
            for k, v in parsed["setup_rows"]
        )
        body_parts.append(f"<h2>{xesc(parsed['setup_title'])}</h2>")
        body_parts.append(
            '<table class="guide-table">'
            '<thead><tr><th style="width:230px;">구성 항목</th><th>설정 및 동작 설명</th></tr></thead>'
            f"<tbody>{rows_html}</tbody>"
            "</table>"
        )
        body_parts.append(
            '<div class="macro note">'
            '<div class="macro-title">자격증명 보안 유지 (Windows DPAPI)</div>'
            '<p>AWX 비밀번호와 API Token은 평문으로 저장되지 않고 현재 PC/사용자 전용 Windows DPAPI로 암호화'
            '(<code>awxPasswordEnc</code>, <code>awxTokenEnc</code>)되어 <code>config.json</code>에 저장됩니다.</p>'
            '</div>'
        )

    for sec in parsed["sections"]:
        rows_html = []
        for idx, name, desc in sec["items"]:
            circ = CIRC[idx - 1] if 1 <= idx <= len(CIRC) else f"{idx}."
            rows_html.append(
                f'<tr><td class="num-col"><strong>{xesc(circ)}</strong></td>'
                f'<td class="name-col"><strong>{xesc(name)}</strong></td>'
                f'<td>{xesc(desc)}</td></tr>'
            )
        img_src = f"data:image/png;base64,{sec['b64']}" if sec["b64"] else f"../{sec['filename']}"
        cap_html = f'<div class="caption">{xesc(sec["caption"])}</div>' if sec["caption"] else ""
        body_parts.append(f'<div class="section-card">')
        body_parts.append(f"<h2>{xesc(sec['title'])}</h2>")
        body_parts.append(
            '<div class="img-box">'
            f'<img src="{xattr(img_src)}" alt="{xattr(sec["alt"])}" />'
            f"{cap_html}"
            '</div>'
        )
        body_parts.append(
            '<table class="guide-table">'
            '<thead><tr><th style="width:48px;">번호</th><th style="width:230px;">항목</th><th>설명</th></tr></thead>'
            f"<tbody>{''.join(rows_html)}</tbody>"
            "</table>"
        )
        body_parts.append('</div>')

    if parsed["tip_items"]:
        tips_html = "".join(
            f"<li><strong>{xesc(k)}</strong>: {xesc(v)}</li>" if k else f"<li>{xesc(v)}</li>"
            for k, v in parsed["tip_items"]
        )
        body_parts.append(
            '<div class="macro tip">'
            '<div class="macro-title">사용 시 참고 팁</div>'
            f'<ul>{tips_html}</ul>'
            '</div>'
        )

    body_parts.append('<p class="closing">감사합니다.</p></div>')

    return f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN"
  "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" lang="ko" xml:lang="ko">
<head>
<meta http-equiv="Content-Type" content="application/xhtml+xml; charset=UTF-8" />
<title>[안내] 작업대상 호스트 교차 검증기 설정 및 사용 가이드</title>
<style type="text/css">
/* <![CDATA[ */
body {{ margin: 0; padding: 24px; background: #f1f5f9; font-family: 'Malgun Gothic', '맑은 고딕', Arial, sans-serif; color: #1e293b; font-size: 14px; line-height: 1.65; }}
.container {{ max-width: 780px; margin: 0 auto; background: #ffffff; border: 1px solid #cbd5e1; border-radius: 10px; padding: 28px 32px; }}
.header-banner {{ background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 6px; padding: 14px 18px; margin-bottom: 18px; }}
.header-banner h1 {{ margin: 0 0 4px 0; font-size: 18px; color: #1e3a8a; }}
.subtitle {{ margin: 0; font-size: 12px; color: #475569; }}
h2 {{ margin: 22px 0 12px 0; font-size: 16px; color: #0f172a; border-left: 4px solid #2563eb; padding-left: 10px; }}
.section-card {{ margin: 22px 0; padding: 16px 18px; border: 1px solid #cbd5e1; border-radius: 8px; background: #ffffff; }}
.img-box {{ text-align: center; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 6px; padding: 12px; margin-bottom: 14px; }}
.img-box img {{ max-width: 624px; width: 100%; height: auto; border: 1px solid #94a3b8; border-radius: 4px; }}
.caption {{ margin-top: 8px; font-size: 12px; color: #64748b; }}
.guide-table {{ width: 100%; border-collapse: collapse; font-size: 13px; margin: 8px 0 12px 0; }}
.guide-table th, .guide-table td {{ border: 1px solid #cbd5e1; padding: 8px 10px; text-align: left; vertical-align: top; }}
.guide-table th {{ background: #f8fafc; color: #0f172a; font-weight: 700; }}
.num-col {{ text-align: center; color: #2563eb; white-space: nowrap; }}
.name-col {{ background: #f8fafc; color: #0f172a; }}
.macro {{ border-radius: 6px; padding: 12px 16px; margin: 16px 0; font-size: 13.5px; }}
.macro-title {{ font-weight: 700; margin-bottom: 6px; }}
.macro.info {{ background: #eff6ff; border: 1px solid #bfdbfe; color: #1e3a8a; }}
.macro.note {{ background: #fffbeb; border: 1px solid #fde68a; color: #78350f; }}
.macro.tip {{ background: #f0fdf4; border: 1px solid #bbf7d0; color: #14532d; }}
.macro ul, .macro ol {{ margin: 4px 0 0 20px; padding: 0; }}
.macro li {{ margin: 4px 0; }}
code {{ background: rgba(15, 23, 42, 0.08); padding: 1px 5px; border-radius: 4px; font-family: Consolas, 'D2Coding', monospace; font-size: 12.5px; }}
.closing {{ margin-top: 20px; }}
/* ]]> */
</style>
</head>
<body>
{"".join(body_parts)}
</body>
</html>
"""


def main():
    parser = argparse.ArgumentParser(description="Convert mail_draft_outlook.html to Confluence & Standalone XHTML.")
    parser.add_argument("src", help="Source HTML path (e.g. ../mail_draft_outlook.html)")
    parser.add_argument("out", help="Output directory path (e.g. .)")
    parser.add_argument("--extract-attachments", action="store_true", help="Also extract PNG attachments into <out>/attachments/")
    args = parser.parse_args()

    src_path = Path(args.src)
    out_dir = Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)

    soup = BeautifulSoup(src_path.read_text(encoding="utf-8"), "html.parser")
    parsed = extract_sections(soup)

    if args.extract_attachments:
        att_dir = out_dir / "attachments"
        att_dir.mkdir(parents=True, exist_ok=True)
        for sec in parsed["sections"]:
            if sec["b64"]:
                (att_dir / sec["filename"]).write_bytes(base64.b64decode(sec["b64"]))

    conf_xhtml = build_confluence_xhtml(parsed)
    standalone_xhtml = build_standalone_xhtml(parsed)

    (out_dir / "guide_confluence.xhtml").write_text(conf_xhtml, encoding="utf-8")
    (out_dir / "guide_standalone.xhtml").write_text(standalone_xhtml, encoding="utf-8")
    print(f"CONVERTED {len(parsed['sections'])} sections -> guide_confluence.xhtml, guide_standalone.xhtml")


if __name__ == "__main__":
    main()
