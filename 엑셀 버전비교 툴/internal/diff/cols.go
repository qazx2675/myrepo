package diff

import (
	"cmp"
	"slices"

	"excel-diff/internal/reader"
)

const (
	colThreshold   = 0.3   // 내용·머리글 점수가 이 이상이면 위치와 무관하게 짝지을 수 있다
	colAllPairs    = 20000 // 열 조합이 이보다 많으면 후보를 줄인다
	colNearBonus   = 0.1
	colSample      = 20 // 후보를 줄일 때 열마다 살펴보는 값의 수
	colMaxPostings = 50
)

// interner 는 셀 Key 를 1부터의 번호로 바꾼다.
type interner map[string]int32

func (in interner) id(key string) int32 {
	v, ok := in[key]
	if !ok {
		v = int32(len(in) + 1)
		in[key] = v
	}
	return v
}

// column 은 비어 있지 않은 열의 특징이다.
type column struct {
	num    int
	header int32   // 열의 첫 번째 값
	unique bool    // 첫 번째 값이 같은 열이 이 시트에 하나뿐
	values []int32 // 값 multiset(정렬됨). 행 순서의 영향을 받지 않는다
}

// columns 는 열 번호 오름차순으로 돌려준다.
func columns(rows []reader.Row, in interner) []column {
	at := map[int]int{}
	var cols []column
	for i := range rows {
		for _, c := range rows[i].Cells {
			k, ok := at[c.Col]
			v := in.id(c.Key)
			if !ok {
				k = len(cols)
				at[c.Col] = k
				cols = append(cols, column{num: c.Col, header: v})
			}
			cols[k].values = append(cols[k].values, v)
		}
	}
	headers := map[int32]int{}
	for i := range cols {
		slices.Sort(cols[i].values)
		headers[cols[i].header]++
	}
	for i := range cols {
		cols[i].unique = headers[cols[i].header] == 1
	}
	slices.SortFunc(cols, func(a, b column) int { return cmp.Compare(a.num, b.num) })
	return cols
}

func colScore(a, b column) float64 {
	same := 0
	for i, j := 0, 0; i < len(a.values) && j < len(b.values); {
		switch {
		case a.values[i] < b.values[j]:
			i++
		case a.values[i] > b.values[j]:
			j++
		default:
			same++
			i++
			j++
		}
	}
	score := 0.6 * 2 * float64(same) / float64(len(a.values)+len(b.values))
	switch {
	case a.header != b.header:
	case a.unique && b.unique:
		score += 0.4
	default: // 여러 열에 같은 머리글은 약한 근거
		score += 0.02
	}
	return score
}

func colCandidates(a, b []column) []link {
	var out []link
	if len(a)*len(b) <= colAllPairs {
		for i := range a {
			for j := range b {
				out = append(out, link{i, j})
			}
		}
		return out
	}
	index := map[int32][]int{}
	byNum := map[int]int{}
	sample := func(c column) []int32 {
		s := []int32{c.header}
		for k := 0; k < colSample; k++ {
			s = append(s, c.values[k*len(c.values)/colSample])
		}
		slices.Sort(s)
		return slices.Compact(s)
	}
	for j, c := range b {
		byNum[c.num] = j
		distinct := slices.Compact(slices.Clone(c.values))
		for _, v := range distinct {
			if len(index[v]) < colMaxPostings {
				index[v] = append(index[v], j)
			}
		}
	}
	for i, c := range a {
		seen := map[int]bool{}
		add := func(j int) {
			if !seen[j] {
				seen[j] = true
				out = append(out, link{i, j})
			}
		}
		if j, ok := byNum[c.num]; ok {
			add(j)
		}
		for _, v := range sample(c) {
			for _, j := range index[v] {
				add(j)
			}
		}
	}
	return out
}

// alignCols 는 열 짝 목록을 좌우 배치 순서로 만든다.
func alignCols(a, b []column, oldKey, newKey int) []ColPair {
	type scored struct {
		i, j, dist int
		score      float64
	}
	var cands []scored
	for _, c := range colCandidates(a, b) {
		if s := colScore(a[c.o], b[c.n]); s >= colThreshold {
			// 내용이 비슷비슷한 열(Y/N 등)끼리는 가까운 위치를 택한다.
			d := abs(a[c.o].num - b[c.n].num)
			cands = append(cands, scored{c.o, c.n, d, s + colNearBonus/float64(1+d)})
		}
	}
	slices.SortFunc(cands, func(x, y scored) int {
		return cmp.Or(cmp.Compare(y.score, x.score), cmp.Compare(x.dist, y.dist), cmp.Compare(x.i, y.i), cmp.Compare(x.j, y.j))
	})

	var links []link // 열 번호
	var scores []float64
	usedOld, usedNew := map[int]bool{}, map[int]bool{}
	pair := func(o, n int, score float64) {
		usedOld[o], usedNew[n] = true, true
		links = append(links, link{o, n})
		scores = append(scores, score)
	}
	if oldKey > 0 {
		hasOld := slices.ContainsFunc(a, func(c column) bool { return c.num == oldKey })
		hasNew := slices.ContainsFunc(b, func(c column) bool { return c.num == newKey })
		if hasOld && hasNew {
			pair(oldKey, newKey, 1)
		}
	}
	for _, c := range cands {
		if o, n := a[c.i].num, b[c.j].num; !usedOld[o] && !usedNew[n] {
			pair(o, n, c.score)
		}
	}
	reorderTwins(links, a, b)

	// 상대 순서가 유지된 짝을 기준으로 남은 열을 같은 위치의 수정으로 짝짓는다.
	w := make([]weighted, len(links))
	for i, l := range links {
		w[i] = weighted{l.o, l.n, 1000 + scores[i]}
	}
	anchors := []link{{0, 0}}
	for _, i := range chain(w) {
		anchors = append(anchors, links[i])
	}
	moved := map[link]bool{}
	for _, l := range links {
		moved[l] = true
	}
	for _, l := range anchors {
		delete(moved, l)
	}
	for g, from := range anchors {
		to := link{1 << 30, 1 << 30}
		if g+1 < len(anchors) {
			to = anchors[g+1]
		}
		var olds, news []int
		for _, c := range a {
			if c.num > from.o && c.num < to.o && !usedOld[c.num] {
				olds = append(olds, c.num)
			}
		}
		for _, c := range b {
			if c.num > from.n && c.num < to.n && !usedNew[c.num] {
				news = append(news, c.num)
			}
		}
		var near []scored
		huge := len(olds)*len(news) > colAllPairs*10
		for _, o := range olds {
			for _, n := range news {
				if d := abs((o - from.o) - (n - from.n)); !huge || d <= colSample {
					near = append(near, scored{i: o, j: n, dist: d})
				}
			}
		}
		slices.SortStableFunc(near, func(x, y scored) int { return cmp.Compare(x.dist, y.dist) })
		var here []link
		for _, c := range near {
			if usedOld[c.i] || usedNew[c.j] ||
				slices.ContainsFunc(here, func(h link) bool { return (h.o < c.i) != (h.n < c.j) }) {
				continue
			}
			here = append(here, link{c.i, c.j})
			pair(c.i, c.j, 0)
		}
		// 폭이 줄지 않은 구간의 빈 자리는 "모두 지워졌거나 새로 채워진 열"이다.
		if g+1 == len(anchors) {
			continue
		}
		for _, o := range olds {
			n := from.n + (o - from.o)
			if !usedOld[o] && to.n-from.n >= to.o-from.o && n < to.n && !usedNew[n] {
				pair(o, n, 0)
			}
		}
		for _, n := range news {
			o := from.o + (n - from.n)
			if !usedNew[n] && to.o-from.o >= to.n-from.n && o < to.o && !usedOld[o] {
				pair(o, n, 0)
			}
		}
	}
	for _, c := range a {
		if !usedOld[c.num] {
			links = append(links, link{c.num, -1})
		}
	}
	for _, c := range b {
		if !usedNew[c.num] {
			links = append(links, link{-1, c.num})
		}
	}

	w = w[:0]
	var at []int
	for i, l := range links {
		if l.o >= 0 && l.n >= 0 {
			weight := 1000.0
			if i < len(scores) {
				weight += scores[i]
			}
			w = append(w, weighted{l.o, l.n, weight})
			at = append(at, i)
		}
	}
	inChain := make([]bool, len(links))
	for _, i := range chain(w) {
		inChain[at[i]] = true
	}
	var out []ColPair
	for _, i := range layout(links, inChain) {
		l := links[i]
		p := ColPair{Old: max(l.o, 0), New: max(l.n, 0)}
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

// reorderTwins 는 내용이 똑같은 열끼리는 순서대로 짝지어 거짓 이동을 막는다.
func reorderTwins(links []link, a, b []column) {
	find := func(cols []column, num int) column {
		i, _ := slices.BinarySearchFunc(cols, num, func(c column, n int) int { return cmp.Compare(c.num, n) })
		return cols[i]
	}
	equal := func(x, y column) bool { return x.header == y.header && slices.Equal(x.values, y.values) }
	done := make([]bool, len(links))
	for i := range links {
		if done[i] {
			continue
		}
		group := []int{i}
		for j := i + 1; j < len(links); j++ {
			if !done[j] && equal(find(a, links[i].o), find(a, links[j].o)) && equal(find(b, links[i].n), find(b, links[j].n)) {
				group = append(group, j)
				done[j] = true
			}
		}
		if len(group) == 1 {
			continue
		}
		var olds, news []int
		for _, g := range group {
			olds = append(olds, links[g].o)
			news = append(news, links[g].n)
		}
		slices.Sort(olds)
		slices.Sort(news)
		for k, g := range group {
			links[g] = link{olds[k], news[k]}
		}
	}
}

func abs(v int) int { return max(v, -v) }
