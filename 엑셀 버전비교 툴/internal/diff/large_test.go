package diff

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"
	"time"
)

// TestLarge 는 40열 × 30,000행의 시간과 메모리를 잰다. go test -run Large -v
func TestLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("대용량 시험")
	}
	const rows, cols = 30000, 40
	grid := func(f func(r, c int) string) [][]string {
		g := make([][]string, rows)
		for r := range g {
			g[r] = make([]string, cols)
			for c := range g[r] {
				g[r][c] = f(r, c)
			}
		}
		return g
	}
	unique := grid(func(r, c int) string { return fmt.Sprintf("%d-%d", r, c) })
	flags := func(seed uint64) [][]string {
		rng := rand.New(rand.NewPCG(seed, 3))
		return grid(func(int, int) string { return []string{"Y", "N"}[rng.IntN(2)] })
	}
	repeated := grid(func(_, c int) string { return fmt.Sprint("same", c) })
	shuffled := func(g [][]string) [][]string {
		g = slices.Clone(g)
		rand.New(rand.NewPCG(7, 7)).Shuffle(len(g), func(i, j int) { g[i], g[j] = g[j], g[i] })
		return g
	}
	planted := slices.Clone(unique)
	for r := 100; r < rows; r += 100 {
		planted[r] = slices.Clone(planted[r])
		planted[r][5] = "changed"
	}
	moved := planted[10]
	planted = slices.Delete(planted, 10, 11)
	planted = slices.Insert(planted, 20000, moved)
	planted = slices.Delete(planted, 5000, 5010)
	repeatedChanged := slices.Clone(repeated)
	for r := 0; r < rows; r += 3 {
		repeatedChanged[r] = slices.Clone(repeated[r])
		repeatedChanged[r][r%cols] = "x"
	}

	// 반복 데이터 중간에 행 블록을 넣고 뺀다(띠 모양 LCS 확인).
	repeatedShifted := slices.Insert(slices.Delete(slices.Clone(repeated), 100, 1100), 20000, unique[:500]...)

	cases := []struct {
		name     string
		old, new [][]string
		check    func(Summary) bool
	}{
		{"동일", unique, unique, func(s Summary) bool { return s.SameCells == rows*cols && s.RowsMoved == 0 }},
		{"심은 변경", unique, planted, func(s Summary) bool {
			return s.RowsDeleted == 10 && s.RowsMoved == 1 && s.ModifiedCells == 299 && s.RowsAdded == 0
		}},
		{"전부 변경", unique, grid(func(r, c int) string { return fmt.Sprintf("n%d-%d", r, c) }),
			func(s Summary) bool { return s.ModifiedCells == rows*cols && s.RowsModified == rows }},
		{"전부 순서 섞임", unique, shuffled(unique), func(s Summary) bool { return s.SameCells == rows*cols && s.RowsMoved > rows*9/10 }},
		{"섞고 전부 한 칸 수정", unique, func() [][]string {
			g := shuffled(unique)
			for r := range g {
				g[r] = slices.Clone(g[r])
				g[r][7] = "z"
			}
			return g
		}(), func(s Summary) bool { return s.ModifiedCells == rows && s.RowsAdded == 0 }},
		{"전부 동일 반복", repeated, repeated, func(s Summary) bool { return s.SameCells == rows*cols && s.RowsMoved == 0 }},
		{"반복 행 일부 수정", repeated, repeatedChanged, func(s Summary) bool { return s.ModifiedCells == rows/3 && s.RowsMoved == 0 }},
		{"반복 → 전부 다른 값", repeated, unique, func(s Summary) bool { return s.ModifiedCells == rows*cols }},
		{"반복 행 블록 삽입·삭제", repeated, repeatedShifted, func(s Summary) bool {
			return s.RowsDeleted+s.RowsModified+s.RowsMovedModified <= 1500 && s.RowsDeleted >= 500
		}},
		{"2값 전부 무작위", flags(1), flags(2), func(s Summary) bool {
			return s.RowsAdded < rows/100 && s.ColsMoved == 0 && s.RowsMovedModified == 0
		}},
	}
	for _, c := range cases {
		a, b := book(sheet("S", c.old)), book(sheet("S", c.new))
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		base := m.HeapAlloc
		stop, peak := make(chan struct{}), make(chan uint64)
		go func() {
			var top uint64
			for {
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				top = max(top, m.HeapAlloc)
				select {
				case <-stop:
					peak <- top
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
		}()
		start := time.Now()
		res, err := Compare(a, b, DefaultPairs(a, b), nil)
		elapsed := time.Since(start)
		close(stop)
		top := <-peak
		if err != nil {
			t.Fatal(c.name, err)
		}
		t.Logf("%s: %v, 입력 %d MB, 최대 %d MB, %+v", c.name, elapsed.Round(time.Millisecond), base>>20, top>>20, res.Summary)
		if !c.check(res.Summary) {
			t.Errorf("%s: 예상과 다른 결과", c.name)
		}
	}
}
