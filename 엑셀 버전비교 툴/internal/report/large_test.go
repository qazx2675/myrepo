package report

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func streamFile(t testing.TB, path string, rows, cols int, row func(r int) []any) {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sw, err := f.NewStreamWriter("Sheet1")
	must(t, err)
	for r := range rows {
		v := row(r)[:cols]
		cell, _ := excelize.CoordinatesToCellName(1, r+1)
		must(t, sw.SetRow(cell, v))
	}
	must(t, sw.Flush())
	must(t, f.SaveAs(path))
}

// TestLarge 는 읽기부터 저장까지의 시간과 최대 메모리를 잰다. go test -run Large -v
// 환경변수 EXCELDIFF_KEEP 에 폴더를 주면 결과 파일을 남긴다.
func TestLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("대용량 시험")
	}
	grid := func(rows, cols int, prefix string) [][]any {
		g := make([][]any, rows)
		for r := range g {
			g[r] = make([]any, cols)
			for c := range g[r] {
				g[r][c] = fmt.Sprintf("%s%d-%d", prefix, r, c)
			}
		}
		return g
	}
	// 알고 있는 변경: 값 299개 수정, 10행 삭제, 5행 삽입, 1행 이동, 열 1개 삭제·1개 삽입
	base := grid(10000, 30, "")
	planted := make([][]any, len(base))
	for r := range planted {
		planted[r] = slices.Clone(base[r])
		if r > 0 && r%100 == 50 {
			planted[r][7] = "changed"
		}
	}
	for r := 1; r <= 199; r++ {
		planted[r*50+1][9] = "changed2"
	}
	moved := planted[20]
	planted = slices.Delete(planted, 20, 21)
	planted = slices.Insert(planted, 7000, moved)
	planted = slices.Delete(planted, 3010, 3020)
	planted = slices.Insert(planted, 8000, grid(5, 30, "new")...)
	for r := range planted {
		planted[r] = slices.Insert(slices.Delete(planted[r], 3, 4), 15, any(fmt.Sprintf("col%d", r)))
	}

	for _, c := range []struct {
		name     string
		old, new [][]any
		cols     int
		check    func(o *Output) bool
	}{
		{"30x10000_planted", base, planted, 30, func(o *Output) bool {
			s := o.Summary
			// 삽입·삭제 행의 셀과 열 변경을 뺀 값 수정은 100 + 199 개
			return s.RowsDeleted == 10 && s.RowsAdded == 5 && s.RowsMoved == 1 && s.ColsAdded == 1 && s.ColsDeleted == 1 && s.ModifiedCells == 299
		}},
		{"30x10000_allchanged", base, grid(10000, 30, "n"), 30, func(o *Output) bool { return o.Summary.ModifiedCells == 300000 }},
		{"40x30000_allchanged", grid(30000, 40, "o"), grid(30000, 40, "n"), 40, func(o *Output) bool { return o.Summary.ModifiedCells == 1200000 }},
		// 항목이 링크 한도를 넘는 경우
		{"3x70000_links", grid(70000, 3, ""), func() [][]any {
			g := grid(70000, 3, "")
			for r := range g {
				g[r][2] = fmt.Sprint("m", r)
			}
			return g
		}(), 3, func(o *Output) bool { return o.Items == 70000 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			out := dir
			if keep := os.Getenv("EXCELDIFF_KEEP"); keep != "" {
				out = keep + "/" + c.name
			}
			o, n := dir+"/old.xlsx", dir+"/new.xlsx"
			streamFile(t, o, len(c.old), c.cols, func(r int) []any { return c.old[r] })
			streamFile(t, n, len(c.new), c.cols, func(r int) []any { return c.new[r] })
			runtime.GC()
			stop, peak := make(chan struct{}), make(chan [2]uint64)
			go func() {
				var top [2]uint64
				for {
					var m runtime.MemStats
					runtime.ReadMemStats(&m)
					top = [2]uint64{max(top[0], m.HeapAlloc), max(top[1], m.Sys-m.HeapReleased)}
					select {
					case <-stop:
						peak <- top
						return
					case <-time.After(50 * time.Millisecond):
					}
				}
			}()
			start := time.Now()
			var stages []string
			at := start
			res, err := Run(Options{OldPath: o, NewPath: n, OutDir: out, Progress: func(_ int, s string) {
				if len(stages) == 0 || !strings.HasPrefix(stages[len(stages)-1], s+"@") {
					stages = append(stages, fmt.Sprintf("%s@%.1fs", s, time.Since(at).Seconds()))
				}
			}})
			elapsed := time.Since(start)
			close(stop)
			top := <-peak
			must(t, err)
			info, err := os.Stat(res.Path)
			must(t, err)
			t.Logf("%v, 최대 힙 %d MB (Go 전체 %d MB), 결과 %d MB, 항목 %d\n%v\n%s",
				elapsed.Round(time.Millisecond), top[0]>>20, top[1]>>20, info.Size()>>20, res.Items, stages, res.Path)
			if !c.check(res) {
				t.Errorf("예상과 다른 결과: %+v", res.Summary)
			}
		})
	}
}
