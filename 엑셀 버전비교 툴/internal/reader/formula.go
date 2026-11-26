package reader

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xuri/efp"
	"github.com/xuri/excelize/v2"
)

// excelize 는 참조한 수식 셀의 계산 오류를 삼키고 빈 값으로 간주한다. 그래서 저장값 없는
// 수식은 참조를 전이적으로 따라가, 계산할 수 없는 셀에 의존하지 않을 때만 값을 쓴다.
// 참조 그래프를 Tarjan 알고리즘(재귀 없음)으로 훑으며 의존 대상부터 계산한다.

// maxAlias 를 넘는 배열 수식·병합 영역은 셀마다 등록하지 않고 사각형 겹침으로 검사한다.
const maxAlias = 10000

// absolutePart 는 $ 로 완전히 고정된 셀, 열, 행이다.
var absolutePart = regexp.MustCompile(`^(\$[A-Za-z]+(\$[0-9]+)?|\$[0-9]+)$`)

type cellPos struct{ row, col int }

// arrayRef 는 배열 수식의 기준 셀과 적용 범위이다.
type arrayRef struct {
	at  cellPos
	ref string
}

// formulaEntry 는 열 안의 수식 위치이다. 배열·병합 영역의 셀은 src(수식이 있는 셀)가 다르다.
type formulaEntry struct {
	row int
	src cellPos
}

// bigArea 는 maxAlias 를 넘는 배열 수식·병합 영역이다.
type bigArea struct {
	r1, c1, r2, c2 int
	src            cellPos
}

type formulaSheet struct {
	name   string
	cols   map[int][]formulaEntry // 열 번호 -> 행 번호 오름차순
	copies map[cellPos]bool       // 공유 수식을 이어받은 셀
	big    []bigArea
}

type nodeKind uint8

const (
	cellNode    nodeKind = iota // a=행, b=열
	segmentNode                 // a=열, [b,c)=그 열의 수식 목록 구간
	rectNode                    // a..b 행, c..d 열
)

type nodeKey struct {
	kind              nodeKind
	sheet, a, b, c, d int
}

type node struct {
	key        nodeKey
	edges      []int
	next       int // 다음에 방문할 edges 위치
	low, index int
	onStack    bool
	bad        bool
}

type analyzer struct {
	f       *excelize.File
	wb      *Workbook
	sheets  []formulaSheet
	byName  map[string]int // 소문자 시트 이름
	names   []excelize.DefinedName
	pending map[nodeKey]formulaRef
	funcs   map[string]bool // 함수 이름 -> excelize 지원 여부
	probe   *excelize.File
	ids     map[nodeKey]int
	nodes   []node
	visited int
	calced  []int // 시트별 자체 계산한 수식 수
	report  func(done, total int)
	total   int // 저장값 없는 수식 수
}

// calculateAll 은 저장값 없는 수식을 계산한다. 계산할 수 없으면 Unknown 으로 두고 경고를 남긴다.
func calculateAll(f *excelize.File, wb *Workbook, parsers []sheetParser, report func(done, total int)) error {
	a := &analyzer{
		f: f, wb: wb, byName: map[string]int{}, names: f.GetDefinedName(),
		pending: map[nodeKey]formulaRef{}, funcs: map[string]bool{}, probe: excelize.NewFile(),
		ids: map[nodeKey]int{}, calced: make([]int, len(parsers)), report: report,
	}
	defer a.probe.Close()
	var roots []nodeKey
	for i := range parsers {
		p := &parsers[i]
		s, err := a.formulaSheet(p)
		if err != nil {
			return err
		}
		a.sheets = append(a.sheets, s)
		a.byName[strings.ToLower(s.name)] = i
		for _, ref := range p.pending {
			r := &wb.Sheets[i].Rows[ref.row]
			k := nodeKey{kind: cellNode, sheet: i, a: r.Num, b: r.Cells[ref.cell].Col}
			a.pending[k] = ref
			roots = append(roots, k)
		}
	}
	a.total = len(roots)
	for _, k := range roots {
		a.walk(k)
	}
	for _, k := range roots {
		if a.nodes[a.ids[k]].bad {
			axis, _ := excelize.CoordinatesToCellName(k.b, k.a) // 읽은 셀 위치라 항상 범위 안이다
			wb.Warnings = append(wb.Warnings, Warning{Sheet: a.sheets[k.sheet].name, Cell: axis, Message: "저장된 계산값이 없고 수식을 계산할 수 없어 값 확인 불가"})
		}
	}
	for i, n := range a.calced {
		if n > 0 {
			wb.Warnings = append(wb.Warnings, Warning{Sheet: a.sheets[i].name,
				Message: "저장된 값이 없어 자체 계산한 수식 " + strconv.Itoa(n) + "개(Excel 과 다를 수 있음)"})
		}
	}
	return nil
}

func (a *analyzer) formulaSheet(p *sheetParser) (formulaSheet, error) {
	s := formulaSheet{name: p.sheet.Name, cols: map[int][]formulaEntry{}, copies: p.copies}
	seen := map[cellPos]bool{}
	add := func(at, src cellPos) {
		if !seen[at] {
			seen[at] = true
			s.cols[at.col] = append(s.cols[at.col], formulaEntry{at.row, src})
		}
	}
	for _, at := range p.formulas {
		add(at, at)
	}
	// 영역의 각 셀을 기준 셀의 수식으로 등록한다.
	alias := func(ref string, src cellPos) {
		r1, c1, r2, c2, ok := parseArea(ref)
		if !ok {
			return
		}
		if (r2-r1+1)*(c2-c1+1) > maxAlias {
			s.big = append(s.big, bigArea{r1, c1, r2, c2, src})
			return
		}
		for r := r1; r <= r2; r++ {
			for c := c1; c <= c2; c++ {
				add(cellPos{r, c}, src)
			}
		}
	}
	for _, ar := range p.arrays {
		alias(ar.ref, ar.at)
	}
	// excelize 는 병합 영역 안의 참조를 첫 셀로 바꿔 읽는다.
	merged, err := a.f.GetMergeCells(s.name, true)
	if err != nil {
		return s, err
	}
	formulas := map[cellPos]bool{}
	for _, at := range p.formulas {
		formulas[at] = true
	}
	for _, m := range merged {
		col, row, err := excelize.CellNameToCoordinates(m.GetStartAxis())
		if at := (cellPos{row, col}); err == nil && formulas[at] {
			alias(m.GetStartAxis()+":"+m.GetEndAxis(), at)
		}
	}
	for _, list := range s.cols {
		slices.SortFunc(list, func(x, y formulaEntry) int { return cmp.Compare(x.row, y.row) })
	}
	return s, nil
}

func (a *analyzer) id(k nodeKey) int {
	if i, ok := a.ids[k]; ok {
		return i
	}
	a.ids[k] = len(a.nodes)
	a.nodes = append(a.nodes, node{key: k, index: -1})
	return len(a.nodes) - 1
}

// walk 는 k 에서 닿는 노드를 강결합 요소 단위로, 의존 대상부터 판정한다.
func (a *analyzer) walk(k nodeKey) {
	root := a.id(k)
	if a.nodes[root].index >= 0 {
		return
	}
	var stack, path []int
	visit := func(v int) {
		edges, bad := a.expand(a.nodes[v].key) // a.nodes 가 늘어날 수 있다
		n := &a.nodes[v]
		n.edges, n.bad, n.onStack = edges, bad, true
		n.index, n.low = a.visited, a.visited
		a.visited++
		stack, path = append(stack, v), append(path, v)
	}
	visit(root)
	for len(path) > 0 {
		v := path[len(path)-1]
		if n := &a.nodes[v]; n.next < len(n.edges) {
			w := n.edges[n.next]
			n.next++
			if m := &a.nodes[w]; m.index < 0 {
				visit(w) // m 은 더 쓰지 않는다
			} else if m.onStack {
				n.low = min(n.low, m.index)
			}
			continue
		}
		path = path[:len(path)-1]
		low := a.nodes[v].low
		if len(path) > 0 {
			parent := &a.nodes[path[len(path)-1]]
			parent.low = min(parent.low, low)
		}
		if low != a.nodes[v].index {
			continue
		}
		at := len(stack) - 1
		for stack[at] != v {
			at--
		}
		component := stack[at:]
		stack = stack[:at]
		cyclic := len(component) > 1 || slices.Contains(a.nodes[v].edges, v)
		for _, w := range component {
			n := &a.nodes[w]
			n.onStack = false
			n.bad = n.bad || cyclic
			for _, e := range n.edges {
				n.bad = n.bad || a.nodes[e].bad
			}
		}
		for _, w := range component {
			if a.nodes[w].key.kind == cellNode {
				a.evaluate(w)
			}
			a.nodes[w].edges = nil
		}
	}
}

// evaluate 는 수식 셀 하나를 계산한다. 저장값 있는 수식도 excelize 가 다시 계산하므로 확인한다.
func (a *analyzer) evaluate(v int) {
	n := &a.nodes[v]
	k := n.key
	ref, isPending := a.pending[k]
	s := a.sheets[k.sheet]
	axis, err := excelize.CoordinatesToCellName(k.b, k.a)
	var raw, shown string
	if err == nil && !n.bad {
		// 소문자 주소로 계산한다. 계산 중인 셀 자신을 참조하면(ROW(A2) 등) excelize 가 빈 값을
		// 참조 캐시에 남기는데, 소문자 주소면 다른 수식이 읽는 항목("시트!A2")과 겹치지 않는다.
		raw, shown, err = calcValue(a.f, s.name, strings.ToLower(axis), isPending && ref.styled)
		n.bad = err != nil
	}
	if !isPending {
		return
	}
	if !n.bad {
		c := &a.wb.Sheets[ref.sheet].Rows[ref.row].Cells[ref.cell]
		c.Key, c.Display, c.Unknown = calcKey(raw), shown, false
		a.calced[k.sheet]++
	}
	delete(a.pending, k)
	if done := a.total - len(a.pending); done%100 == 0 {
		a.report(done, a.total)
	}
}

// expand 는 노드가 의존하는 노드와, 그 자체로 계산할 수 없는지를 돌려준다.
func (a *analyzer) expand(k nodeKey) (edges []int, bad bool) {
	switch k.kind {
	case rectNode:
		for col, list := range a.sheets[k.sheet].cols {
			if col < k.c || col > k.d {
				continue
			}
			byRow := func(e formulaEntry, row int) int { return cmp.Compare(e.row, row) }
			lo, _ := slices.BinarySearchFunc(list, k.a, byRow)
			hi, _ := slices.BinarySearchFunc(list, k.b+1, byRow)
			edges = a.cover(edges, k.sheet, col, 0, len(list), lo, hi)
		}
		for _, g := range a.sheets[k.sheet].big {
			if g.r1 <= k.b && k.a <= g.r2 && g.c1 <= k.d && k.c <= g.c2 {
				edges = append(edges, a.id(nodeKey{kind: cellNode, sheet: k.sheet, a: g.src.row, b: g.src.col}))
			}
		}
		return edges, false
	case segmentNode:
		mid := (k.b + k.c) / 2
		return []int{a.segment(k.sheet, k.a, k.b, mid), a.segment(k.sheet, k.a, mid, k.c)}, false
	}
	return a.formulaEdges(k)
}

// segment 는 한 열의 수식 구간 노드이다. 길이 1 이면 수식 셀 자체이다.
func (a *analyzer) segment(sheet, col, lo, hi int) int {
	if hi-lo == 1 {
		src := a.sheets[sheet].cols[col][lo].src
		return a.id(nodeKey{kind: cellNode, sheet: sheet, a: src.row, b: src.col})
	}
	return a.id(nodeKey{kind: segmentNode, sheet: sheet, a: col, b: lo, c: hi})
}

// cover 는 [from,to) 를 덮는 구간 노드를 [lo,hi) 를 반씩 나눠 가며 더한다.
func (a *analyzer) cover(edges []int, sheet, col, lo, hi, from, to int) []int {
	if to <= lo || hi <= from || from >= to {
		return edges
	}
	if from <= lo && hi <= to {
		return append(edges, a.segment(sheet, col, lo, hi))
	}
	mid := (lo + hi) / 2
	edges = a.cover(edges, sheet, col, lo, mid, from, to)
	return a.cover(edges, sheet, col, mid, hi, from, to)
}

func (a *analyzer) formulaEdges(k nodeKey) (edges []int, bad bool) {
	defer func() { // 손상된 수식에서 라이브러리가 panic 하면 계산 불가로 본다.
		if recover() != nil {
			edges, bad = nil, true
		}
	}()
	sheet := a.sheets[k.sheet].name
	axis, err := excelize.CoordinatesToCellName(k.b, k.a)
	if err != nil {
		return nil, true
	}
	formula, err := a.f.GetCellFormula(sheet, axis)
	if err != nil {
		return nil, true
	}
	if formula == "" { // 배열 수식의 구성 셀 등, 수식을 알 수 없다
		return nil, true
	}
	ps := efp.ExcelParser()
	tokens := ps.Parse(formula)
	if len(tokens) == 0 {
		return nil, true
	}
	shared := a.sheets[k.sheet].copies[cellPos{k.a, k.b}]
	for i, t := range tokens {
		switch {
		case t.TType == efp.TokenTypeFunction && t.TSubType == efp.TokenSubTypeStart:
			if !a.supported(t.TValue) {
				return nil, true
			}
		case t.TType == efp.TokenTypeOperand && t.TSubType == efp.TokenSubTypeRange:
			if shared && !a.sharedSafe(t.TValue) {
				return nil, true
			}
			target, ok := a.resolve(k.sheet, t.TValue)
			if !ok {
				return nil, true
			}
			targets := []nodeKey{target}
			if positionArg(tokens, i) {
				targets = withoutCell(target, k)
			}
			for _, t := range targets {
				edges = append(edges, a.id(t))
			}
		}
	}
	return edges, false
}

// sharedSafe 는 공유 수식 복사 셀의 참조를 excelize 가 바르게 읽는지 본다. excelize 는 시트 지정
// 참조에서 시트가 붙은 부분을 이동시키지 못하므로 모든 부분이 절대 참조여야 한다. 시트 이름의
// 따옴표도 사라져 'Q1-Data'!$A$1 이 Q1-Data!$A$1 이 되므로, 잘린 이름일 수 있으면 거부한다.
func (a *analyzer) sharedSafe(ref string) bool {
	if !strings.Contains(ref, "!") {
		return true
	}
	for _, part := range strings.Split(ref, ":") {
		if bang := strings.LastIndex(part, "!"); bang >= 0 {
			name := strings.ToLower(part[:bang])
			for _, s := range a.sheets {
				head, ok := strings.CutSuffix(strings.ToLower(s.name), name)
				last, _ := utf8.DecodeLastRuneInString(head)
				if ok && head != "" && last != '_' && !unicode.IsLetter(last) && !unicode.IsDigit(last) {
					return false
				}
			}
			part = part[bang+1:]
		}
		if !absolutePart.MatchString(part) {
			return false
		}
	}
	return true
}

// withoutCell 은 사각형 r 에서 수식 셀 k 자신을 뺀 사각형들이다. 위치만 쓰는 자기 참조는 순환이
// 아니다. 다른 셀은 의존으로 남긴다. 빼면 excelize 가 되돌아오는 참조를 빈 값으로 계산해 둔다.
func withoutCell(r, k nodeKey) []nodeKey {
	if r.sheet != k.sheet || k.a < r.a || k.a > r.b || k.b < r.c || k.b > r.d {
		return []nodeKey{r}
	}
	above, below, left, right := r, r, r, r
	above.b, below.a = k.a-1, k.a+1
	left.a, left.b, left.d = k.a, k.a, k.b-1
	right.a, right.b, right.c = k.a, k.a, k.b+1
	return slices.DeleteFunc([]nodeKey{above, below, left, right}, func(x nodeKey) bool { return x.a > x.b || x.c > x.d })
}

// positionArg 는 tokens[i] 가 ROW 류 함수의 인수 그 자체인지 본다. 이때는 위치만 쓰고 값은 쓰지 않는다.
func positionArg(tokens []efp.Token, i int) bool {
	if i == 0 || i+1 >= len(tokens) {
		return false
	}
	open, end := tokens[i-1], tokens[i+1]
	if open.TType != efp.TokenTypeFunction || open.TSubType != efp.TokenSubTypeStart ||
		end.TType != efp.TokenTypeFunction || end.TSubType != efp.TokenSubTypeStop {
		return false
	}
	switch strings.ToUpper(open.TValue) {
	case "ROW", "COLUMN", "ROWS", "COLUMNS":
		return true
	}
	return false
}

// supported 는 excelize 가 함수를 지원하는지 본다. 동적 참조 함수는 의존 대상을 알 수 없어 제외한다.
func (a *analyzer) supported(name string) bool {
	switch strings.ToUpper(strings.TrimPrefix(name, "_xlfn.")) {
	case "INDIRECT", "OFFSET":
		return false
	}
	ok, seen := a.funcs[name]
	if !seen {
		// 함수마다 다른 셀을 써서 계산 캐시를 피한다.
		axis := "A" + strconv.Itoa(len(a.funcs)+1)
		err := a.probe.SetCellFormula("Sheet1", axis, name+"()")
		if err == nil {
			_, _, err = calcValue(a.probe, "Sheet1", axis, false)
		}
		ok = err == nil || !strings.Contains(err.Error(), "not support")
		a.funcs[name] = ok
	}
	return ok
}

// resolve 는 참조 피연산자(셀, 범위, 정의된 이름)를 excelize 와 같은 순서로 해석한다.
// 외부 통합 문서, 구조적 참조, 3차원 참조 등 해석할 수 없으면 false 이다.
func (a *analyzer) resolve(sheet int, ref string) (nodeKey, bool) {
	refTo := ""
	for _, n := range a.names {
		if n.Name != ref {
			continue
		}
		if n.Scope == a.sheets[sheet].name {
			refTo = n.RefersTo
			break
		}
		if n.Scope == "Workbook" {
			refTo = n.RefersTo
		}
	}
	if refTo != "" {
		ref = strings.TrimPrefix(refTo, "=")
	}
	if strings.Contains(ref, "[") {
		return nodeKey{}, false
	}
	parts := strings.Split(strings.ReplaceAll(ref, "$", ""), ":")
	// 3차원 참조(Jan:Mar!B2)의 앞 시트 이름을 열 이름으로 읽지 않는다.
	if strings.Contains(ref, "!") && !strings.Contains(parts[0], "!") {
		return nodeKey{}, false
	}
	target := -1
	for i, part := range parts {
		if bang := strings.LastIndex(part, "!"); bang >= 0 {
			s, ok := a.byName[strings.ToLower(strings.Trim(part[:bang], "'"))]
			if !ok || i > 0 && s != target {
				return nodeKey{}, false
			}
			target, parts[i] = s, part[bang+1:]
		} else if i == 0 {
			target = sheet
		}
	}
	r1, c1, r2, c2, ok := parseArea(strings.Join(parts, ":"))
	if !ok {
		return nodeKey{}, false
	}
	return nodeKey{kind: rectNode, sheet: target, a: r1, b: r2, c: c1, d: c2}, true
}

// parseArea 는 시트 이름 없는 "A1", "A1:B2", "A:C", "1:3" 을 감싸는 사각형으로 바꾼다.
func parseArea(ref string) (r1, c1, r2, c2 int, ok bool) {
	parts := strings.Split(strings.ReplaceAll(ref, "$", ""), ":")
	r1, c1 = excelize.TotalRows, excelize.MaxColumns
	for _, part := range parts {
		top, bottom, left, right := 1, excelize.TotalRows, 1, excelize.MaxColumns
		if col, row, err := excelize.CellNameToCoordinates(part); err == nil {
			top, bottom, left, right = row, row, col, col
		} else if len(parts) == 1 {
			return 0, 0, 0, 0, false
		} else if col, err := excelize.ColumnNameToNumber(part); err == nil {
			left, right = col, col
		} else if row, err := strconv.Atoi(part); err == nil && row >= 1 && row <= excelize.TotalRows {
			top, bottom = row, row
		} else {
			return 0, 0, 0, 0, false
		}
		r1, c1, r2, c2 = min(r1, top), min(c1, left), max(r2, bottom), max(c2, right)
	}
	return r1, c1, r2, c2, true
}
