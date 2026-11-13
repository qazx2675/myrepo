import os
import datetime
import zipfile
import openpyxl

BASE_DIR = r"C:\Users\qazx2\.gemini\antigravity\scratch\excel-workload-counter"
DATA_DIR = os.path.join(BASE_DIR, "test_data")


def make_date_wb(sheets_spec, out_path):
    wb = openpyxl.Workbook()
    first = True
    for sheet_name, rows_map in sheets_spec:
        if first:
            ws = wb.active
            ws.title = sheet_name
            first = False
        else:
            ws = wb.create_sheet(title=sheet_name)
        for coord, val in rows_map.items():
            ws[coord] = val
    wb.save(out_path)


def make_special_ooxml_xlsx(out_path):
    """
    Creates an .xlsx exercising:
    - sharedStrings rich text <r><t>...</t></r>
    - sharedStrings whitespace-only <t xml:space="preserve">   </t>
    - inlineStr
    - formula t="str" non-empty <v>2026-10-03</v>
    - formula t="str" empty <v></v>
    - numeric 0 (<v>0</v>)
    - column AA3 (must NOT be counted as column A)
    """
    content_types = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
  <Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>
</Types>'''

    root_rels = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>'''

    workbook_xml = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="업무량" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>'''

    wb_rels = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="/xl/worksheets/sheet1.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
</Relationships>'''

    shared_strings = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
  <si><t>헤더</t></si>
  <si><r><t>2026-</t></r><r><t>10-02</t></r></si>
  <si><t xml:space="preserve">   </t></si>
</sst>'''

    sheet_xml = '''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c></row>
    <row r="2"><c r="A2" t="inlineStr"><is><t>날짜</t></is></c></row>
    <row r="3">
      <c r="A3" t="inlineStr"><is><t>2026-10-01</t></is></c>
      <c r="AA3" t="inlineStr"><is><t>무시될열</t></is></c>
    </row>
    <row r="4"><c r="A4" t="s"><v>1</v></c></row>
    <row r="5"><c r="A5" t="str"><f>"2026-10-03"</f><v>2026-10-03</v></c></row>
    <row r="6"><c r="A6" t="str"><f>IF(1=1,"","")</f><v></v></c></row>
    <row r="7"><c r="A7" t="s"><v>2</v></c></row>
    <row r="8"><c r="A8"><v>0</v></c></row>
  </sheetData>
</worksheet>'''

    with zipfile.ZipFile(out_path, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("[Content_Types].xml", content_types.encode("utf-8"))
        zf.writestr("_rels/.rels", root_rels.encode("utf-8"))
        zf.writestr("xl/workbook.xml", workbook_xml.encode("utf-8"))
        zf.writestr("xl/_rels/workbook.xml.rels", wb_rels.encode("utf-8"))
        zf.writestr("xl/sharedStrings.xml", shared_strings.encode("utf-8"))
        zf.writestr("xl/worksheets/sheet1.xml", sheet_xml.encode("utf-8"))


def main():
    os.makedirs(DATA_DIR, exist_ok=True)

    # TC-01: 김철수 26년 10월 ('업무량'이 2번째 시트, A3~A7 = 5건)
    make_date_wb(
        [
            ("개요", {"A1": "요약시트", "A3": "무시1", "A4": "무시2"}),
            (
                "업무량",
                {
                    "A1": "서버운영 업무량",
                    "A2": "일자",
                    "A3": datetime.date(2026, 10, 1),
                    "A4": datetime.date(2026, 10, 2),
                    "A5": datetime.date(2026, 10, 5),
                    "A6": datetime.date(2026, 10, 12),
                    "A7": datetime.date(2026, 10, 20),
                    "B3": "작업1",
                },
            ),
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_10월)_NEW_김철수.xlsx"),
    )

    # TC-02: 김철수 26년 11월 (A3~A5 = 3건)
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A3": datetime.date(2026, 11, 1),
                    "A4": datetime.date(2026, 11, 2),
                    "A5": datetime.date(2026, 11, 3),
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_11월)_NEW_김철수.xlsx"),
    )

    # TC-03: 김철수 26년 9월 (한 자리 월, A3~A4 = 2건)
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A3": datetime.date(2026, 9, 1),
                    "A4": datetime.date(2026, 9, 15),
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_9월)_NEW_김철수.xlsx"),
    )

    # TC-05: 박민수 26년 10월 (A3 비어있음, A4만 1건 = 1건)
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A4": datetime.date(2026, 10, 5),
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_10월)_NEW_박민수.xlsx"),
    )

    # TC-06 & TC-14: 이영희 26년 10월 (중간 빈행, 공백문자, 수식 "" 및 날짜수식, AA열 무시 -> 4건)
    # Excel COM으로 다시 저장해 실제 Excel 계산 캐시 및 Data Descriptor ZIP 생성
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "보고서",
                    "A2": "날짜",
                    "A3": datetime.date(2026, 10, 1),
                    "A5": datetime.date(2026, 10, 3),
                    "A6": "   ",
                    "A7": '=IF(1=1,"","")',
                    "A8": "2026-10-10",
                    "A10": "=DATE(2026,10,15)",
                    "B3": "비고1",
                    "AA3": "무시할열",
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_10월)_NEW_이영희.xlsx"),
    )

    # TC-07: OOXML 특수 유형 (inlineStr, rich-text sharedStrings, t="str", space sharedString, 0, AA3 -> 4건)
    make_special_ooxml_xlsx(
        os.path.join(DATA_DIR, "서버운영 업무량(26년_10월)_NEW_특수유형.xlsx")
    )

    # TC-09: 박민수 26년 11월 ('업무량' 시트 없음 -> 0건)
    make_date_wb(
        [
            (
                "Sheet1",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A3": datetime.date(2026, 11, 1),
                    "A4": datetime.date(2026, 11, 2),
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_11월)_NEW_박민수.xlsx"),
    )

    # TC-11: 김철수 25년 10월 (선택 연도 인자 검증 -> 2건)
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A3": datetime.date(2025, 10, 1),
                    "A4": datetime.date(2025, 10, 2),
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(25년_10월)_NEW_김철수.xlsx"),
    )

    # TC-15: A1:A2 헤더만 있고 A3 이하가 완전히 빈 시트 -> 0건
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "서버운영 업무량",
                    "A2": "일자",
                    "B3": "A열은 비어있고 B열만 있음",
                },
            )
        ],
        os.path.join(DATA_DIR, "서버운영 업무량(26년_10월)_NEW_빈데이터.xlsx"),
    )

    # TC-16: 공백이 포함된 한글 하위 폴더('공유 폴더') -> 3건
    sub_dir = os.path.join(DATA_DIR, "공유 폴더")
    os.makedirs(sub_dir, exist_ok=True)
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A3": datetime.date(2026, 10, 1),
                    "A4": datetime.date(2026, 10, 2),
                    "A5": datetime.date(2026, 10, 3),
                },
            )
        ],
        os.path.join(sub_dir, "서버운영 업무량(26년_10월)_NEW_홍길동.xlsx"),
    )

    # TC-18: 읽기 전용(Read-Only) 속성 파일 -> 2건
    ro_path = os.path.join(DATA_DIR, "서버운영 업무량(26년_10월)_NEW_읽기전용.xlsx")
    if os.path.exists(ro_path):
        os.chmod(ro_path, 0o666)
    make_date_wb(
        [
            (
                "업무량",
                {
                    "A1": "제목",
                    "A2": "날짜",
                    "A3": datetime.date(2026, 10, 1),
                    "A4": datetime.date(2026, 10, 2),
                },
            )
        ],
        ro_path,
    )
    os.chmod(ro_path, 0o444)

    print("Created all test fixtures in", DATA_DIR)


if __name__ == "__main__":
    main()
