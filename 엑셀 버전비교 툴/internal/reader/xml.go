package reader

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// excelize 의 행 반복자는 셀 형식·수식 유무·열 정보를 알려주지 않고,
// 셀 단위 API 는 시트 전체를 메모리에 올리므로 XML 을 직접 한 번 훑는다.

type pkgFiles map[string]*zip.File

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/"))
}

func newPackage(zr *zip.Reader) pkgFiles {
	p := pkgFiles{}
	for _, f := range zr.File {
		p[normalizeName(f.Name)] = f
	}
	return p
}

func (p pkgFiles) size(name string) int64 {
	if f := p[normalizeName(name)]; f != nil {
		return int64(f.UncompressedSize64)
	}
	return 0
}

func (p pkgFiles) open(name string) (io.ReadCloser, error) {
	f := p[normalizeName(name)]
	if f == nil {
		return nil, fmt.Errorf("%s 항목이 없습니다", name)
	}
	return f.Open()
}

func (p pkgFiles) unmarshal(name string, v any) error {
	r, err := p.open(name)
	if err != nil {
		return err
	}
	defer r.Close()
	return xml.NewDecoder(r).Decode(v)
}

type relationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

// rels 는 part 의 관계 목록을 대상 경로를 풀어서 돌려준다.
func (p pkgFiles) rels(part string) ([]relationship, error) {
	dir, base := path.Split(part)
	var v struct {
		Rels []relationship `xml:"Relationship"`
	}
	if err := p.unmarshal(dir+"_rels/"+base+".rels", &v); err != nil {
		return nil, err
	}
	for i, r := range v.Rels {
		if strings.HasPrefix(r.Target, "/") {
			v.Rels[i].Target = strings.TrimPrefix(r.Target, "/")
		} else {
			v.Rels[i].Target = path.Join(dir, r.Target)
		}
	}
	return v.Rels, nil
}

type sheetInfo struct {
	name, path        string
	hidden, worksheet bool
}

type workbookInfo struct {
	sheets        []sheetInfo
	sharedStrings string
	date1904      bool
}

func (p pkgFiles) workbook() (info workbookInfo, err error) {
	root, err := p.rels("")
	if err != nil {
		return info, err
	}
	main := ""
	for _, r := range root {
		if strings.HasSuffix(r.Type, "/officeDocument") {
			main = r.Target
		}
	}
	var v struct {
		Pr struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name  string `xml:"name,attr"`
			State string `xml:"state,attr"`
			RID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := p.unmarshal(main, &v); err != nil {
		return info, err
	}
	rels, err := p.rels(main)
	if err != nil {
		return info, err
	}
	byID := map[string]relationship{}
	for _, r := range rels {
		byID[r.ID] = r
		if strings.HasSuffix(r.Type, "/sharedStrings") {
			info.sharedStrings = r.Target
		}
	}
	info.date1904 = isTrue(v.Pr.Date1904)
	for _, s := range v.Sheets {
		r, ok := byID[s.RID]
		if !ok {
			return info, fmt.Errorf("시트 '%s' 의 위치 정보가 없습니다", s.Name)
		}
		info.sheets = append(info.sheets, sheetInfo{
			name:      s.Name,
			path:      r.Target,
			hidden:    s.State == "hidden" || s.State == "veryHidden",
			worksheet: strings.HasSuffix(r.Type, "/worksheet"),
		})
	}
	if len(info.sheets) == 0 {
		return info, errors.New("시트가 없습니다")
	}
	return info, nil
}

// textContent 는 현재 요소가 끝날 때까지 읽으며 <t> 의 글자를 모은다.
// 윗주(rPh)는 셀 값이 아니므로 건너뛴다.
func textContent(d *xml.Decoder) (string, error) {
	var b strings.Builder
	depth, inText := 0, false
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "rPh" {
				if err := d.Skip(); err != nil {
					return "", err
				}
				continue
			}
			depth++
			inText = t.Name.Local == "t"
		case xml.EndElement:
			if depth == 0 {
				return unescape(b.String()), nil
			}
			depth--
			inText = false
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
}

// charData 는 자식 요소가 없는 현재 요소의 글자를 읽는다.
func charData(d *xml.Decoder) (string, error) {
	s := ""
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			s += string(t)
		case xml.StartElement:
			if err := d.Skip(); err != nil {
				return "", err
			}
		case xml.EndElement:
			return s, nil
		}
	}
}

func (p pkgFiles) sharedStrings(name string) ([]string, error) {
	r, err := p.open(name)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	d := xml.NewDecoder(r)
	var strs []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return strs, nil
		}
		if err != nil {
			return nil, err
		}
		if t, ok := tok.(xml.StartElement); ok && t.Name.Local == "si" {
			s, err := textContent(d)
			if err != nil {
				return nil, err
			}
			strs = append(strs, s)
		}
	}
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}

func isTrue(s string) bool { return s == "1" || s == "true" }

type sheetParser struct {
	sheet    Sheet
	index    int // wb.Sheets 에서의 위치
	strs     []string
	date1904 bool
	progress func(offset int64)
	pending  []formulaRef

	row     Row
	lastCol int
}

func (p *sheetParser) parse(pkg pkgFiles, name string) error {
	r, err := pkg.open(name)
	if err != nil {
		return err
	}
	defer r.Close()
	d := xml.NewDecoder(r)
	rowCount := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "col":
				p.col(t)
			case "row":
				num := p.row.Num + 1
				if s := attr(t, "r"); s != "" {
					if num, err = strconv.Atoi(s); err != nil || num <= p.row.Num || num > excelize.TotalRows {
						return fmt.Errorf("행 번호가 올바르지 않습니다: %q", s)
					}
				}
				p.row = Row{Num: num, Hidden: isTrue(attr(t, "hidden")), Cells: make([]Cell, 0, p.lastCol)}
				p.lastCol = 0
			case "c":
				if err := p.cell(d, t); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "row" {
				if len(p.row.Cells) > 0 {
					p.sheet.Rows = append(p.sheet.Rows, p.row)
				}
				if rowCount++; rowCount%1000 == 0 {
					p.progress(d.InputOffset())
				}
			}
		}
	}
}

func (p *sheetParser) col(t xml.StartElement) {
	first, err1 := strconv.Atoi(attr(t, "min"))
	last, err2 := strconv.Atoi(attr(t, "max"))
	if err1 != nil || err2 != nil || first < 1 {
		return
	}
	width, _ := strconv.ParseFloat(attr(t, "width"), 64)
	c := Col{Width: width, Hidden: isTrue(attr(t, "hidden"))}
	for i := first; i <= min(last, excelize.MaxColumns); i++ {
		p.sheet.Cols[i] = c
	}
}

func (p *sheetParser) cell(d *xml.Decoder, start xml.StartElement) error {
	col := p.lastCol + 1
	if ref := attr(start, "r"); ref != "" {
		var err error
		if col, _, err = excelize.CellNameToCoordinates(ref); err != nil {
			return err
		}
	}
	if col <= p.lastCol {
		return fmt.Errorf("%d행의 셀 순서가 올바르지 않습니다", p.row.Num)
	}
	p.lastCol = col

	var value, inline string
	var hasValue, hasFormula bool
	for done := false; !done; {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				hasValue = true
				value, err = charData(d)
			case "is":
				inline, err = textContent(d)
			case "f":
				hasFormula = true
				err = d.Skip()
			default:
				err = d.Skip()
			}
			if err != nil {
				return err
			}
		case xml.EndElement:
			done = true
		}
	}

	typ := attr(start, "t")
	c := Cell{Col: col}
	// 빈 <v> 는 문자열 결과(t="str")일 때만 저장된 계산값이다.
	if hasFormula && inline == "" && (!hasValue || value == "" && typ != "str") {
		c.Key, c.Display, c.Unknown = unknownKey, unknownDisplay, true
		style := attr(start, "s")
		p.pending = append(p.pending, formulaRef{
			sheet: p.index, row: len(p.sheet.Rows), cell: len(p.row.Cells),
			styled: style != "" && style != "0",
		})
	} else {
		switch typ {
		case "inlineStr":
			c.Key = inline
		case "s":
			if value == "" {
				return nil
			}
			i, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || i < 0 || i >= len(p.strs) {
				return fmt.Errorf("%d행의 공유 문자열 번호가 올바르지 않습니다: %q", p.row.Num, value)
			}
			c.Key = p.strs[i]
		case "str":
			c.Key = unescape(value)
			c.Display = c.Key
		case "b":
			switch value {
			case "1", "true":
				c.Key = "TRUE"
			case "0", "false":
				c.Key = "FALSE"
			default:
				c.Key = value
			}
		case "e":
			c.Key = value
		case "d":
			c.Key = isoDateKey(value, p.date1904)
		default:
			c.Key = rawNumberKey(value)
		}
		if c.Key == "" {
			return nil
		}
	}
	p.row.Cells = append(p.row.Cells, c)
	return nil
}
