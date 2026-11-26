package diff

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"excel-diff/internal/reader"
)

const (
	gapSimilarity  = 0.5     // 같은 구간 안에서 짝지을 최소 유사도
	moveSimilarity = 0.6     // 값이 바뀐 이동으로 볼 최소 유사도
	rareLimit      = 4       // 최신 행에 이 횟수 이하로 나오는 값은 드문 값
	candidateLimit = 96      // 행 하나가 살펴보는 후보 수
	postingWindow  = 32      // 값 하나에서 가져오는 후보 수
	candidateKeep  = 4       // 행 하나가 남기는 후보 수
	lcsLimit       = 4 << 20 // 정확한 LCS 를 쓰는 일치 조합 수의 상한
	dpLimit        = 40000   // 위치 짝짓기 DP 가 전체 조합을 보는 상한
	bandMargin     = 64      // 큰 구간에서 대각선 양옆으로 더 보는 행 수
	bandLimit      = 6 << 20 // 큰 구간 하나에서 DP 가 보는 조합 수의 상한
	identMin       = 20      // 식별 열로 볼 최소 공통 값 수(값이 적으면 우연히 고유할 수 있음)
)

// term 은 행의 짝지은 열 하나의 값이다.
type term struct{ col, val int32 }

type side struct {
	rows  []reader.Row
	vec   [][]term // 짝지은 열의 값, 열 짝 순서
	sig   []int32  // vec 가 같으면 같은 번호. 0 은 짝지은 열에 값이 없음
	mate  []int    // 짝의 첨자, -1 은 없음
	fixed []bool   // 키 열 때문에 자동 정렬에서 제외
}

func (s *side) free(i int) bool { return s.mate[i] < 0 && !s.fixed[i] }

type aligner struct {
	old, new side
	cols     []ColPair
	count    map[term]int // 최신 행 전체에서 (열, 값)이 나온 횟수
}

// shareRare 는 두 행이 드문 값을 하나 이상 공유하는지 알려준다.
// Y/N 같은 흔한 값만 겹치는 행은 우연히 비슷한 것일 수 있다.
func (a *aligner) shareRare(x, y []term) bool {
	return slices.ContainsFunc(x, func(t term) bool { return a.count[t] <= rareLimit && slices.Contains(y, t) })
}

func newAligner(oldRows, newRows []reader.Row, cols []ColPair, in interner) *aligner {
	oldAt, newAt := map[int]int32{}, map[int]int32{}
	for i, c := range cols {
		if c.Old > 0 && c.New > 0 {
			oldAt[c.Old], newAt[c.New] = int32(i), int32(i)
		}
	}
	sigs := map[string]int32{}
	var buf []byte
	build := func(rows []reader.Row, at map[int]int32) side {
		s := side{rows: rows, vec: make([][]term, len(rows)), sig: make([]int32, len(rows)),
			mate: make([]int, len(rows)), fixed: make([]bool, len(rows))}
		for i := range rows {
			s.mate[i] = -1
			v := make([]term, 0, len(rows[i].Cells))
			for _, c := range rows[i].Cells {
				if k, ok := at[c.Col]; ok {
					v = append(v, term{k, in.id(c.Key)})
				}
			}
			slices.SortFunc(v, func(a, b term) int { return cmp.Compare(a.col, b.col) })
			s.vec[i] = v
			if len(v) == 0 {
				continue
			}
			buf = buf[:0]
			for _, t := range v {
				buf = binary.LittleEndian.AppendUint32(buf, uint32(t.col))
				buf = binary.LittleEndian.AppendUint32(buf, uint32(t.val))
			}
			id, ok := sigs[string(buf)]
			if !ok {
				id = int32(len(sigs) + 1)
				sigs[string(buf)] = id
			}
			s.sig[i] = id
		}
		return s
	}
	a := &aligner{old: build(oldRows, oldAt), new: build(newRows, newAt), cols: cols, count: map[term]int{}}
	for _, v := range a.new.vec {
		for _, t := range v {
			a.count[t]++
		}
	}
	return a
}

func (a *aligner) pair(o, n int) { a.old.mate[o], a.new.mate[n] = n, o }

// matchKeys 는 양쪽에서 각각 하나뿐인 키를 가진 행을 키로 짝짓는다.
func (a *aligner) matchKeys(oldCol, newCol int, oldName, newName string) []reader.Warning {
	var warnings []reader.Warning
	index := func(s *side, col int) ([]string, map[string]int) {
		keys := make([]string, len(s.rows))
		count := map[string]int{}
		for i := range s.rows {
			cells := s.rows[i].Cells
			k, ok := slices.BinarySearchFunc(cells, col, func(c reader.Cell, n int) int { return cmp.Compare(c.Col, n) })
			if ok && !cells[k].Unknown {
				keys[i] = cells[k].Key
				count[keys[i]]++
			}
		}
		return keys, count
	}
	oldAll, oldCount := index(&a.old, oldCol)
	newAll, newCount := index(&a.new, newCol)
	// 이 파일에서 하나뿐인 키를 돌려주고, 키로 비교하지 않는 행은 경고한다.
	pick := func(s *side, keys []string, count, other map[string]int, label, sheet string) map[string]int {
		unique := map[string]int{}
		var bad []string
		total := 0
		for i, k := range keys {
			if k != "" && count[k] == 1 {
				unique[k] = i
				if other[k] <= 1 {
					continue
				}
			}
			total++
			if len(bad) < 10 {
				bad = append(bad, strconv.Itoa(s.rows[i].Num))
			}
		}
		if total > 0 {
			warnings = append(warnings, reader.Warning{Sheet: sheet, Message: fmt.Sprintf(
				"[%s 파일] 키가 비었거나 확인 불가이거나 어느 한쪽 파일에서 중복인 %d개 행은 자동 정렬로 비교했습니다(행: %s)",
				label, total, strings.Join(bad, ", "))})
		}
		return unique
	}
	oldKeys := pick(&a.old, oldAll, oldCount, newCount, "이전", oldName)
	newKeys := pick(&a.new, newAll, newCount, oldCount, "최신", newName)
	// 상대 파일에서 중복인 키의 행은 자동 정렬에 맡긴다.
	for k, i := range oldKeys {
		a.old.fixed[i] = newCount[k] <= 1
	}
	for k, i := range newKeys {
		a.new.fixed[i] = oldCount[k] <= 1
	}
	for k, o := range oldKeys {
		if n, ok := newKeys[k]; ok {
			a.pair(o, n)
		}
	}
	// 키 열의 머리글이 바뀐 경우: 첫 행끼리는 짝짓는다.
	if len(a.old.rows) > 0 && len(a.new.rows) > 0 && a.old.mate[0] < 0 && a.new.mate[0] < 0 &&
		a.old.fixed[0] && a.new.fixed[0] {
		a.pair(0, 0)
	}
	return warnings
}

func (a *aligner) align(step func(percent int)) {
	a.exact()
	step(30)
	a.similarInGaps()
	step(55)
	a.moves()
	ids := a.idents()
	a.identities(ids)
	step(90)
	a.positional(ids)
	step(100)
}

// exact 는 완전히 같은 행의 최장 공통 부분열을 기준점으로 잡는다.
func (a *aligner) exact() {
	collect := func(s *side) (sig []int32, at []int) {
		for i := range s.rows {
			if s.free(i) && s.sig[i] != 0 {
				sig = append(sig, s.sig[i])
				at = append(at, i)
			}
		}
		return
	}
	x, xi := collect(&a.old)
	y, yi := collect(&a.new)
	lcs(x, y, func(i, j int) { a.pair(xi[i], yi[j]) })
}

// lcs 는 x 와 y 의 공통 부분열을 emit 한다. 일치 조합이 너무 많으면 근사한다.
func lcs(x, y []int32, emit func(i, j int)) {
	type span struct{ x0, x1, y0, y1 int }
	stack := []span{{0, len(x), 0, len(y)}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for s.x0 < s.x1 && s.y0 < s.y1 && x[s.x0] == y[s.y0] {
			emit(s.x0, s.y0)
			s.x0++
			s.y0++
		}
		for s.x0 < s.x1 && s.y0 < s.y1 && x[s.x1-1] == y[s.y1-1] {
			s.x1--
			s.y1--
			emit(s.x1, s.y1)
		}
		if s.x0 == s.x1 || s.y0 == s.y1 {
			continue
		}
		pos := map[int32][]int{}
		for j := s.y0; j < s.y1; j++ {
			pos[y[j]] = append(pos[y[j]], j)
		}
		count := map[int32]int{}
		matches := 0
		for i := s.x0; i < s.x1; i++ {
			count[x[i]]++
			matches += len(pos[x[i]])
		}
		// diagonal 은 구간 길이 비율로 본 i 의 예상 위치이다.
		diagonal := func(i int) int { return s.y0 + (i-s.x0)*(s.y1-s.y0)/(s.x1-s.x0) }
		// hunt 는 예상 위치에서 band 안의 일치 조합으로 LCS 를 구한다(Hunt-Szymanski).
		// 길이가 같은 해가 여럿이면 예상 위치에 가까운 것을 택한다.
		hunt := func(band int) {
			var c []weighted
			for i := s.x0; i < s.x1; i++ {
				p, d := pos[x[i]], diagonal(i)
				from, _ := slices.BinarySearch(p, d-band)
				for _, j := range p[from:] {
					if j-d > band {
						break
					}
					c = append(c, weighted{i - s.x0, j - s.y0, 1 + 1e-7/float64(1+abs(j-d))})
				}
			}
			for _, k := range chain(c) {
				emit(c[k].o+s.x0, c[k].n+s.y0)
			}
		}
		if matches <= lcsLimit {
			hunt(s.y1 - s.y0)
			continue
		}
		var unique []weighted // patience: 양쪽에서 하나뿐인 행
		for i := s.x0; i < s.x1; i++ {
			if p := pos[x[i]]; count[x[i]] == 1 && len(p) == 1 {
				unique = append(unique, weighted{i, p[0], 1})
			}
		}
		if len(unique) > 0 {
			from := span{x0: s.x0, y0: s.y0}
			for _, k := range chain(unique) {
				u := unique[k]
				emit(u.o, u.n)
				stack = append(stack, span{from.x0, u.o, from.y0, u.n})
				from = span{x0: u.o + 1, y0: u.n + 1}
			}
			stack = append(stack, span{from.x0, s.x1, from.y0, s.y1})
			continue
		}
		hunt(max(1, lcsLimit/(2*(s.x1-s.x0))))
	}
}

// chained 는 현재 짝 중 상대 순서가 유지되는 것(이전 첨자 순)을 돌려준다.
// 같은 길이면 완전히 같은 행을 우선한다.
func (a *aligner) chained() []link {
	var c []weighted
	for o, n := range a.old.mate {
		if n >= 0 {
			w := 1000.0
			if a.old.sig[o] == a.new.sig[n] {
				w++
			}
			c = append(c, weighted{o, n, w})
		}
	}
	idx := chain(c)
	out := make([]link, len(idx))
	for i, k := range idx {
		out[i] = link{c[k].o, c[k].n}
	}
	return out
}

// gaps 는 기준점 사이 구간마다 아직 짝이 없는 행을 넘겨준다.
func (a *aligner) gaps(f func(from, to link, olds, news []int)) {
	from := link{-1, -1}
	anchors := append(a.chained(), link{len(a.old.rows), len(a.new.rows)})
	for _, to := range anchors {
		var olds, news []int
		for i := from.o + 1; i < to.o; i++ {
			if a.old.free(i) {
				olds = append(olds, i)
			}
		}
		for j := from.n + 1; j < to.n; j++ {
			if a.new.free(j) {
				news = append(news, j)
			}
		}
		if len(olds) > 0 && len(news) > 0 {
			f(from, to, olds, news)
		}
		from = to
	}
}

func matching(x, y []term) (same int) {
	for i, j := 0, 0; i < len(x) && j < len(y); {
		switch {
		case x[i].col < y[j].col:
			i++
		case x[i].col > y[j].col:
			j++
		default:
			if x[i].val == y[j].val {
				same++
			}
			i++
			j++
		}
	}
	return
}

func similarity(x, y []term) (float64, int) {
	same := matching(x, y)
	if same == 0 {
		return 0, 0
	}
	return float64(same) / float64(max(len(x), len(y))), same
}

// valueIndex 는 (열, 값) -> 최신 행 첨자(오름차순) 역인덱스이다.
type valueIndex map[term][]int

func (a *aligner) index() valueIndex {
	ix := valueIndex{}
	for j := range a.new.rows {
		if a.new.free(j) {
			for _, t := range a.new.vec[j] {
				ix[t] = append(ix[t], j)
			}
		}
	}
	return ix
}

type candidate struct {
	n    int
	sim  float64
	same int
}

// candidates 는 [lo, hi) 의 최신 행 중 x 와 비슷한 것을 유사도 순으로 돌려준다.
// 드문 값부터 보며, 흔한 값은 예상 위치(expect) 주변만 본다.
func (a *aligner) candidates(ix valueIndex, x []term, lo, hi, expect int, minSim float64, seen map[int]bool) []candidate {
	lists := make([][]int, 0, len(x))
	for _, t := range x {
		p := ix[t]
		from, _ := slices.BinarySearch(p, lo)
		to, _ := slices.BinarySearch(p, hi)
		if from < to {
			lists = append(lists, p[from:to])
		}
	}
	slices.SortStableFunc(lists, func(p, q []int) int { return cmp.Compare(len(p), len(q)) })
	clear(seen)
	var out []candidate
	for _, p := range lists {
		if len(seen) >= candidateLimit {
			break
		}
		mid, _ := slices.BinarySearch(p, expect)
		from := max(0, min(mid-postingWindow/2, len(p)-postingWindow))
		for _, j := range p[from:min(len(p), from+postingWindow)] {
			if seen[j] || !a.new.free(j) {
				continue
			}
			seen[j] = true
			if sim, same := similarity(x, a.new.vec[j]); sim >= minSim {
				out = append(out, candidate{j, sim, same})
			}
		}
	}
	slices.SortFunc(out, func(p, q candidate) int {
		return cmp.Or(cmp.Compare(q.sim, p.sim), cmp.Compare(abs(p.n-expect), abs(q.n-expect)), cmp.Compare(p.n, q.n))
	})
	return out[:min(len(out), candidateKeep)]
}

// similarInGaps 는 각 구간 안에서 순서를 지키며 비슷한 행끼리 짝짓는다.
func (a *aligner) similarInGaps() {
	ix := a.index()
	seen := map[int]bool{}
	var found []link
	a.gaps(func(from, to link, olds, news []int) {
		var c []weighted
		for k, o := range olds {
			expect := news[k*len(news)/len(olds)]
			for _, m := range a.candidates(ix, a.old.vec[o], from.n+1, to.n, expect, gapSimilarity, seen) {
				if a.shareRare(a.old.vec[o], a.new.vec[m.n]) {
					c = append(c, weighted{o - from.o - 1, m.n - from.n - 1, m.sim})
				}
			}
		}
		for _, k := range chain(c) {
			found = append(found, link{c[k].o + from.o + 1, c[k].n + from.n + 1})
		}
	})
	for _, l := range found {
		a.pair(l.o, l.n)
	}
}

// moves 는 구간을 넘어 같거나 매우 비슷한 행을 짝짓는다. 상대 순서가 바뀌면 이동이 된다.
func (a *aligner) moves() {
	same := map[int32][]int{}
	for j := range a.new.rows {
		if a.new.free(j) && a.new.sig[j] != 0 {
			same[a.new.sig[j]] = append(same[a.new.sig[j]], j)
		}
	}
	for i := range a.old.rows {
		if p := same[a.old.sig[i]]; a.old.free(i) && len(p) > 0 {
			a.pair(i, p[0])
			same[a.old.sig[i]] = p[1:]
		}
	}
	type move struct {
		o    int
		c    candidate
		dist int
	}
	seen := map[int]bool{}
	for round := 0; round < 3; round++ { // 후보를 빼앗긴 행은 다시 찾는다
		ix := a.index()
		var all []move
		for i := range a.old.rows {
			if !a.old.free(i) {
				continue
			}
			expect := i * len(a.new.rows) / len(a.old.rows)
			for _, c := range a.candidates(ix, a.old.vec[i], 0, len(a.new.rows), expect, moveSimilarity, seen) {
				if c.same >= 2 && a.shareRare(a.old.vec[i], a.new.vec[c.n]) {
					all = append(all, move{i, c, abs(c.n - expect)})
				}
			}
		}
		slices.SortFunc(all, func(x, y move) int {
			return cmp.Or(cmp.Compare(y.c.sim, x.c.sim), cmp.Compare(x.dist, y.dist), cmp.Compare(x.o, y.o), cmp.Compare(x.c.n, y.c.n))
		})
		paired := 0
		for _, m := range all {
			if a.old.free(m.o) && a.new.free(m.c.n) {
				a.pair(m.o, m.c.n)
				paired++
			}
		}
		if paired == 0 {
			break
		}
	}
}

// positional 은 같은 구간에 남은 행을 위치 순서대로 "수정"으로 짝짓는다.
func (a *aligner) positional(ids []ident) {
	var found []link
	a.gaps(func(_, _ link, olds, news []int) {
		k, l := len(olds), len(news)
		if k == l {
			for i := range k {
				if sim, _ := similarity(a.old.vec[olds[i]], a.new.vec[news[i]]); !a.apart(ids, olds[i], news[i], sim) {
					found = append(found, link{olds[i], news[i]})
				}
			}
			return
		}
		// 짝의 수를 최대로 하면서 유사도 합이 큰 짝짓기. 큰 구간은 대각선 주변 band 만 본다.
		lo, hi := band(k, l)
		score := make([][]float64, k)
		pairScore := func(i, j int) float64 {
			sim, _ := similarity(a.old.vec[olds[i]], a.new.vec[news[j]])
			if a.apart(ids, olds[i], news[j], sim) {
				return math.Inf(-1) // 짝짓지 않음
			}
			return 1000 + sim
		}
		at := func(i, j int) float64 {
			switch {
			case i == k || j == l:
				return 0
			case j < lo(i) || j > hi(i):
				return math.Inf(-1)
			}
			return score[i][j-lo(i)]
		}
		for i := k - 1; i >= 0; i-- {
			score[i] = make([]float64, hi(i)-lo(i)+1)
			for j := hi(i); j >= lo(i); j-- {
				score[i][j-lo(i)] = max(at(i+1, j), at(i, j+1), at(i+1, j+1)+pairScore(i, j))
			}
		}
		for i, j := 0, 0; i < k && j < l; {
			switch at(i, j) {
			case at(i+1, j+1) + pairScore(i, j):
				found = append(found, link{olds[i], news[j]})
				i++
				j++
			case at(i+1, j):
				i++
			default:
				j++
			}
		}
	})
	for _, l := range found {
		a.pair(l.o, l.n)
	}
}

// band 는 위치 짝짓기 DP 에서 이전 행 i 가 살펴보는 최신 행 범위 [lo(i), hi(i)] 를 돌려준다.
// 작은 구간은 전체를, 큰 구간은 두 길이의 차이만큼 밀린 대각선 주변을 본다.
// 차이가 너무 커서 상한을 넘으면 길이 비율 대각선 주변만 본다.
func band(k, l int) (lo, hi func(i int) int) {
	if k*l <= dpLimit {
		return func(int) int { return 0 }, func(int) int { return l - 1 }
	}
	below, above := min(0, l-k)-bandMargin, max(0, l-k)+bandMargin
	if k*(above-below+1) > bandLimit {
		h := max(bandLimit/(2*k), l/k+1)
		return func(i int) int { return max(0, min(i*l/k-h, l-1)) },
			func(i int) int { return min(l-1, i*l/k+h) }
	}
	return func(i int) int { return max(0, min(i+below, l-1)) },
		func(i int) int { return min(l-1, i+above) }
}

// ident 는 자동 식별 열이다. 양쪽에서 값이 거의 행마다 다르고, 그 값의 대부분이 양쪽에 다 있다.
type ident struct {
	col      int32
	old, new map[int32]int // 값 -> 그 값을 가진 하나뿐인 행 첨자, 여러 행이면 -1
}

func valueAt(v []term, col int32) (int32, bool) {
	k, ok := slices.BinarySearchFunc(v, col, func(t term, c int32) int { return cmp.Compare(t.col, c) })
	if !ok {
		return 0, false
	}
	return v[k].val, true
}

// sequence 는 열 col 의 숫자가 행 순서대로 1씩 늘어나는지(순번 열) 알려준다.
// 순번은 행을 넣고 빼면 다시 매겨지므로 행 위치일 뿐 식별 근거가 아니다.
func (s *side) sequence(col int) bool {
	prev, steps, total := math.NaN(), 0, 0
	for i := range s.rows {
		cells := s.rows[i].Cells
		k, ok := slices.BinarySearchFunc(cells, col, func(c reader.Cell, n int) int { return cmp.Compare(c.Col, n) })
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(cells[k].Key, 64)
		if err != nil {
			prev = math.NaN()
			continue
		}
		if !math.IsNaN(prev) {
			total++
			if v == prev+1 {
				steps++
			}
		}
		prev = v
	}
	return total > 0 && steps*10 >= total*9
}

// idents 는 자동 식별 열을 찾는다.
func (a *aligner) idents() []ident {
	values := func(s *side, k int32) (map[int32]int, int) {
		m := map[int32]int{}
		for i, v := range s.vec {
			if x, ok := valueAt(v, k); ok {
				if _, dup := m[x]; dup {
					m[x] = -1
				} else {
					m[x] = i
				}
			}
		}
		unique := 0
		for _, i := range m {
			if i >= 0 {
				unique++
			}
		}
		return m, unique
	}
	var out []ident
	for k, c := range a.cols {
		if c.Old == 0 || c.New == 0 {
			continue
		}
		id := ident{col: int32(k)}
		var uo, un int
		id.old, uo = values(&a.old, id.col)
		id.new, un = values(&a.new, id.col)
		shared := 0
		for x, i := range id.old {
			if j, ok := id.new[x]; ok && i >= 0 && j >= 0 {
				shared++
			}
		}
		if uo*5 >= len(a.old.rows)*4 && un*5 >= len(a.new.rows)*4 && shared >= identMin &&
			shared*2 >= min(len(a.old.rows), len(a.new.rows)) && !a.old.sequence(c.Old) && !a.new.sequence(c.New) {
			out = append(out, id)
		}
	}
	return out
}

// votes 는 식별 열마다 o, n 의 값을 견준다. same 은 같은 값인 열 수, apart 는 두 값이 다르고
// 각각 상대 파일에 없는 열 수, conflict 는 어느 한쪽 값이 상대 파일의 다른 행을 가리키는지이다.
func (a *aligner) votes(ids []ident, o, n int) (same, apart int, conflict bool) {
	for _, id := range ids {
		x, ok1 := valueAt(a.old.vec[o], id.col)
		y, ok2 := valueAt(a.new.vec[n], id.col)
		j, inNew := id.new[x]
		i, inOld := id.old[y]
		if ok1 && inNew && j >= 0 && j != n || ok2 && inOld && i >= 0 && i != o {
			conflict = true
		}
		switch {
		case !ok1 || !ok2:
		case x == y:
			same++
		case !inNew && !inOld:
			apart++
		}
	}
	return
}

// identities 는 남은 행을 식별 열의 같은 값으로 짝짓는다. 다른 값이 모두 바뀐 행도 짝지어진다.
// 식별 열들의 합의가 있어야 한다: 다른 짝을 가리키는 열이 없고(순번 같은 가짜 식별 열 대비),
// 같은 값인 열이 다른 행이라는 근거의 열보다 많아야 한다(우연히 같은 값 대비).
func (a *aligner) identities(ids []ident) {
	for _, id := range ids {
		for x, o := range id.old {
			if n, ok := id.new[x]; ok && o >= 0 && n >= 0 && a.old.free(o) && a.new.free(n) {
				if same, apart, conflict := a.votes(ids, o, n); !conflict && same > apart {
					a.pair(o, n)
				}
			}
		}
	}
}

// apart 는 o, n 이 서로 다른 행이라는 근거가 있는지 알려준다. 내용이 비슷하지 않고,
// 같은 값의 식별 열이 없으며, 어느 식별 열의 두 값이 각각 상대 파일에 없으면 삭제 + 추가이다.
// 식별 값의 오타만 고친 행은 내용이 비슷하므로 해당하지 않는다.
func (a *aligner) apart(ids []ident, o, n int, sim float64) bool {
	if sim >= gapSimilarity {
		return false
	}
	same, apart, _ := a.votes(ids, o, n)
	return same == 0 && apart > 0
}

// result 는 좌우 배치 순서의 행 짝 목록을 만든다.
func (a *aligner) result() []RowPair {
	var links []link
	for o, n := range a.old.mate {
		links = append(links, link{o, n})
	}
	for n, o := range a.new.mate {
		if o < 0 {
			links = append(links, link{-1, n})
		}
	}
	inChain := make([]bool, len(links))
	for _, l := range a.chained() {
		inChain[l.o] = true // links 의 앞부분은 이전 행 첨자와 같다
	}
	out := make([]RowPair, 0, len(links))
	for _, i := range layout(links, inChain) {
		l := links[i]
		var p RowPair
		if l.o >= 0 {
			p.Old = &a.old.rows[l.o]
		}
		if l.n >= 0 {
			p.New = &a.new.rows[l.n]
		}
		switch {
		case l.o < 0:
			p.Status = StatusAdded
		case l.n < 0:
			p.Status = StatusDeleted
		case !inChain[i]:
			p.Status = StatusMoved
		}
		out = append(out, p)
	}
	return out
}
