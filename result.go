// result.go - 최종 결과 표
package main

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// PrintResults 는 HOST | 방식 | 이전→설치버전 | CUDA | DCGM | dcgm상태 | 결과 표를 출력한다.
// 건너뛴 호스트의 DCGM 칸은 현황 수집 때의 상태다.
func PrintResults(out io.Writer, ts []*Target) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HOST\t방식\t이전→설치버전\tCUDA\tDCGM\tdcgm상태\t결과")
	for _, t := range ts {
		st := t.Status
		if st == nil {
			st = &HostStatus{}
		}
		method := t.Method
		if st.Err != "" {
			method = "-"
		}
		cuda := t.NewCUDA
		if t.NewDriver == "" {
			cuda = st.CUDA
		}
		d := t.Dcgm
		if d == nil && t.Skip != "" {
			d = &st.DCGM
		}
		pkg, state := "-", "-"
		if d != nil {
			pkg = d.summary()
			if d.Active != "" {
				state = d.state()
			}
		}
		if t.DcgmNote != "" {
			pkg = t.DcgmNote
		}
		res := "ok"
		switch {
		case t.Fail != "":
			res = "fail: " + t.Fail
		case t.Skip != "":
			res = "skip: " + t.Skip
		}
		fmt.Fprintf(w, "%s\t%s\t%s → %s\t%s\t%s\t%s\t%s\n", t.Host, method, dash(st.Driver), dash(t.NewDriver),
			dash(cuda), pkg, state, res)
	}
	w.Flush()
}
