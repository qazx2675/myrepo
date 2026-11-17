package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type probeRow struct {
	Size    int
	Code    int
	Elapsed time.Duration
	OK      bool
	Note    string
}

func randB64(n int) string {
	raw := make([]byte, n*3/4+3)
	_, _ = rand.Read(raw)
	return base64.StdEncoding.EncodeToString(raw)[:n]
}

func fmtSize(n int) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n%1024 == 0:
		return fmt.Sprintf("%d KiB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// probe 는 base64 문자열 size 자를 variables 에 PATCH 하고 다시 읽어 비교한다.
func probe(ctx context.Context, c *Client, size int) probeRow {
	s := randB64(size)
	vars := "awx_copy_probe: \"" + s + "\"\n"
	t0 := time.Now()
	code, err := c.PatchVarsCode(ctx, vars)
	r := probeRow{Size: size, Code: code, Elapsed: time.Since(t0)}
	if err != nil {
		var he *HTTPError
		switch {
		case errors.As(err, &he):
			b := strings.Join(strings.Fields(he.Body), " ")
			if len(b) > 80 {
				b = b[:80] + "..."
			}
			r.Note = "거부: " + b
		case strings.Contains(err.Error(), "timeout"):
			r.Note = "timeout(응답 없음)"
		default:
			r.Note = "연결 오류: " + err.Error()
		}
		return r
	}
	got, err := c.GetVars(ctx)
	if err != nil {
		r.Note = "PATCH 성공, GET 실패: " + err.Error()
		return r
	}
	if got != vars {
		r.Note = fmt.Sprintf("저장값 불일치 (보낸 %d자, 받은 %d자)", len(vars), len(got))
		return r
	}
	r.OK = true
	r.Note = "저장 후 GET 일치"
	return r
}

// measureLimits 는 variables 에 넣을 수 있는 최대 크기를 잰다. 끝나면 원본으로 복원한다.
func measureLimits(ctx context.Context, c *Client, maxMiB int, w io.Writer) (err error) {
	orig, err := c.GetVars(ctx)
	if err != nil {
		return err
	}
	if hasBlock(orig) {
		return errBusy
	}
	if _, err := saveLocalBackup(c.inv, newSession()+"-measure", orig); err != nil {
		return err
	}
	defer func() {
		ctx2, cancel := bgCtx(120 * time.Second)
		defer cancel()
		if perr := c.PatchVars(ctx2, orig); perr != nil {
			fmt.Fprintf(w, "\n원복 실패: %v\n", perr)
			if err == nil {
				err = perr
			}
			return
		}
		after, gerr := c.GetVars(ctx2)
		fmt.Fprintf(w, "\n원복: 원본 %d자로 복원, 확인 결과 일치=%v (오류=%v)\n", len(orig), gerr == nil && after == orig, gerr)
	}()

	var rows []probeRow
	lastOK, firstFail := 0, 0
	for size := 64 << 10; size <= maxMiB<<20; size *= 2 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r := probe(ctx, c, size)
		rows = append(rows, r)
		fmt.Fprintf(w, "측정 %s: HTTP %d, %.2f초, %s\n", fmtSize(size), r.Code, r.Elapsed.Seconds(), r.Note)
		if !r.OK {
			firstFail = size
			break
		}
		lastOK = size
	}
	if firstFail > 0 && lastOK > 0 {
		lo, hi := lastOK, firstFail
		for hi-lo > 64<<10 && ctx.Err() == nil {
			mid := (lo + hi) / 2
			mid -= mid % 4
			r := probe(ctx, c, mid)
			r.Note = "(이진 탐색) " + r.Note
			rows = append(rows, r)
			fmt.Fprintf(w, "측정 %s: HTTP %d, %.2f초, %s\n", fmtSize(mid), r.Code, r.Elapsed.Seconds(), r.Note)
			if r.OK {
				lo = mid
			} else {
				hi = mid
			}
		}
		lastOK = lo
	}
	fmt.Fprintln(w, "\n| 크기(base64 문자) | 바이트 | HTTP | 응답 시간(초) | 결과 | 비고 |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---|---|")
	for _, r := range rows {
		res := "성공"
		if !r.OK {
			res = "실패"
		}
		code := fmt.Sprint(r.Code)
		if r.Code == 0 {
			code = "-"
		}
		fmt.Fprintf(w, "| %s | %d | %s | %.2f | %s | %s |\n", fmtSize(r.Size), r.Size, code, r.Elapsed.Seconds(), res, r.Note)
	}
	fmt.Fprintf(w, "\n최대 성공 크기: %s (%d 문자)\n", fmtSize(lastOK), lastOK)
	if firstFail > 0 {
		fmt.Fprintf(w, "첫 실패 크기: %s\n", fmtSize(firstFail))
	}
	return nil
}

// snapshot 은 작업 템플릿과 인벤토리 소스의 modified 값을 GET 으로만 읽는다.
func snapshot(ctx context.Context, c *Client, w io.Writer) error {
	for _, p := range []string{"/api/v2/job_templates/", "/api/v2/inventory_sources/"} {
		lines, err := c.listModified(ctx, p)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Fprintf(w, "%s %s\n", p, l)
		}
		fmt.Fprintf(w, "# %s count=%d\n", p, len(lines))
	}
	return nil
}
