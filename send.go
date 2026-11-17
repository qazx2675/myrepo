package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var errBusy = errors.New("인벤토리 variables 에 이미 awx_copy 블록이 있습니다. 다른 세션이 사용 중입니다. 남은 블록이면 --restore 로 정리하세요")

// nginxBodyLimit 은 192.168.0.58:30080 에서 무인증 PATCH 로 잰 nginx 본문 한도다 (docs/limits.md).
const nginxBodyLimit = 5 << 20

const (
	pollMin = 200 * time.Millisecond
	pollMax = 2 * time.Second
)

func nextPoll(d time.Duration) time.Duration {
	d *= 2
	if d > pollMax {
		d = pollMax
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// sendStats 는 기록용 업로드 정보다.
type sendStats struct {
	Size   int64
	Chunks int
}

// sendFile 은 업로더다. 인벤토리 variables 만 PATCH 한다.
func sendFile(ctx context.Context, c *Client, cfg *Config, path, typ string, log io.Writer) error {
	_, err := sendFileP(ctx, c, cfg, path, typ, log, nil)
	return err
}

// sendFileP 는 진행 알림(prog, nil 가능)을 받는 업로더다.
//
// 원복 책임:
//   - 정상: 다운로더가 마지막 조각(또는 실패한 조각)에서 state=done, result=ok|fail 을 남긴다.
//     업로더가 그것을 읽고 variables 를 원본으로 되돌린다.
//   - 업로더 오류/중단(Ctrl+C): 업로더가 자기 세션 블록일 때만 원복한다.
//   - 다운로더 중단: 다운로더가 원복한다. 업로더는 원본으로 돌아온 것을 보고 실패로 끝난다.
//   - done 을 남긴 뒤 업로더가 사라지면: 다운로더가 timeout 뒤 원복한다.
func sendFileP(ctx context.Context, c *Client, cfg *Config, path, typ string, log io.Writer, prog progressSink) (st sendStats, err error) {
	if typ != "txt" && typ != "csv" {
		return st, fmt.Errorf("type 은 csv 또는 txt 여야 합니다: %q", typ)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return st, fmt.Errorf("파일을 읽을 수 없습니다: %v", err)
	}
	eol := detectEOL(data)
	chunks := splitText(data, eol, cfg.ChunkSize, maxChunkBytes)
	n := len(chunks)
	st = sendStats{Size: int64(len(data)), Chunks: n}
	if eol == eolMixed {
		fmt.Fprintln(log, "경고: LF 와 CR 이 섞인 파일입니다(eol=mixed). CR 을 LF 로 정규화해 보내므로 수신 쪽 해시 검사에서 실패합니다.")
	}

	orig, err := c.GetVars(ctx)
	if err != nil {
		return st, err
	}
	if hasBlock(orig) {
		return st, errBusy
	}
	session := newSession()
	bp, err := saveLocalBackup(cfg.InventoryID, session, orig)
	if err != nil {
		return st, err
	}
	fmt.Fprintf(log, "파일 %s (%d bytes, %s, text eol=%s) 를 조각 %d개로 보냅니다. 세션 %s\n",
		filepath.Base(path), len(data), typ, eol, n, session)
	fmt.Fprintf(log, "원본 variables 백업: %s (%d자)\n", bp, len(orig))

	touched := false
	defer func() {
		if err != nil && touched {
			if rerr := restoreIfOurs(c, session, orig, log); rerr != nil {
				fmt.Fprintln(log, rerr)
			}
		}
	}()

	base := Block{
		"version": protoVersion, "session": session, "state": stateSending,
		"file_name": filepath.Base(path), "type": typ, "encoding": encText,
		"eol": eol, "final_newline": strconv.FormatBool(hasFinalNewline(data)),
		"total_bytes": strconv.Itoa(len(data)), "sha256": sha256Hex(data),
		"chunk_total": strconv.Itoa(n), "backup_b64": base64.StdEncoding.EncodeToString([]byte(orig)),
	}
	if prog != nil {
		prog.Begin(n, int64(len(data)))
	}
	start := time.Now()
	warned := false
	var sent int64
	for k := 1; k <= n; k++ {
		text, replaced := encodeChunk(chunks[k-1], eol)
		if replaced > 0 {
			fmt.Fprintf(log, "경고: [%d/%d] 텍스트가 아닌 바이트 %d곳을 U+FFFD 로 바꿨습니다. 원본과 달라 수신 쪽 해시 검사에서 실패합니다.\n", k, n, replaced)
		}
		blk := base.clone()
		blk["chunk_index"] = strconv.Itoa(k)
		blk["chunk_text"] = text
		blk["chunk_sha256"] = sha256Hex(chunks[k-1])
		blk["ack"] = strconv.Itoa(k - 1)
		blk["updated_at"] = nowStamp()
		vars := blk.Render()
		if body, _ := varsBody(vars); len(body) > nginxBodyLimit && !warned {
			warned = true
			fmt.Fprintf(log, "경고: PATCH 본문이 %d bytes 로 AWX nginx 한도(%d bytes, docs/limits.md)를 넘을 수 있습니다. chunk_size 를 줄이세요.\n", len(body), nginxBodyLimit)
		}
		if prog != nil {
			prog.Update(k, n, sent, "전송 중")
		} else {
			fmt.Fprintf(log, "[%d/%d] 전송 중 (%d bytes)\n", k, n, len(chunks[k-1]))
		}
		touched = true
		if err = c.PatchVars(ctx, vars); err != nil {
			return st, err
		}
		if prog != nil {
			prog.Update(k, n, sent, "수신 대기")
		}
		var done bool
		done, err = waitAck(ctx, c, cfg, session, orig, k, n, log)
		if err != nil {
			if done { // 상대가 이미 원복했다
				touched = false
			}
			return st, err
		}
		sent += int64(len(chunks[k-1]))
		if prog != nil {
			prog.Update(k, n, sent, "확인")
		}
	}
	// 다운로더가 result=ok 를 남겼다. 업로더가 원복한다.
	ctx2, cancel := bgCtx(60 * time.Second)
	defer cancel()
	if rerr := restoreIfOurs(c, session, orig, io.Discard); rerr != nil {
		fmt.Fprintf(log, "경고: 마지막 원복 실패: %v (다운로더가 timeout 뒤 원복하거나 --restore 로 복구)\n", rerr)
	} else if cur, gerr := c.GetVars(ctx2); gerr == nil && cur != orig {
		fmt.Fprintln(log, "경고: 원복 뒤 variables 가 원본과 다릅니다. --status 로 확인하세요.")
	}
	el := time.Since(start)
	fmt.Fprintf(log, "완료: %d bytes, 조각 %d개, %.2f초 (%.1f KiB/s). 다운로더 수신 확인(SHA-256 일치), variables 원복.\n",
		len(data), n, el.Seconds(), float64(len(data))/1024/maxf(el.Seconds(), 0.001))
	return st, nil
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// waitAck 는 다운로더의 ack 또는 결과를 기다린다. 두 번째 값이 true 이면 variables 가 이미 원본이다.
func waitAck(ctx context.Context, c *Client, cfg *Config, session, orig string, k, n int, log io.Writer) (bool, error) {
	deadline := time.Now().Add(cfg.Timeout)
	d := pollMin
	for {
		if err := sleepCtx(ctx, d); err != nil {
			return false, err
		}
		cur, err := c.GetVars(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			fmt.Fprintf(log, "경고: 조회 실패, 재시도: %v\n", err)
		} else {
			b, ok := parseBlock(cur)
			switch {
			case !ok && cur == orig:
				// 결과(done) 없이 원본으로 돌아왔다: 다운로더 중단 또는 구버전 다운로더. 성공으로 보지 않는다.
				return true, fmt.Errorf("다운로더가 수신 결과 없이 원복했습니다 (%d/%d 조각에서). 수신 성공을 확인할 수 없습니다", k, n)
			case !ok:
				return true, errors.New("variables 가 다른 값으로 바뀌었습니다. 중단합니다")
			case b["session"] != session:
				return true, fmt.Errorf("다른 세션(%s)이 variables 를 덮어썼습니다", b["session"])
			case b["state"] == stateDone && b["result"] == recvOK && k == n && b.int("ack") >= k:
				return false, nil
			case b["state"] == stateDone:
				msg := b["message"]
				if msg == "" {
					msg = "result=" + b["result"]
				}
				return false, fmt.Errorf("다운로더 수신 실패 (%s/%d 조각): %s", b["ack"], n, msg)
			case k < n && b["state"] == stateAck && b.int("ack") >= k:
				return false, nil
			}
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("[%d/%d] 다운로더 응답 대기 시간 초과(%s). 다운로더(awx_fetch.exe)를 실행했는지 확인하세요", k, n, cfg.Timeout)
		}
		d = nextPoll(d)
	}
}
