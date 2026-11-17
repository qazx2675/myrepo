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

// sendFile 은 업로더다. 인벤토리 variables 만 PATCH 한다.
func sendFile(ctx context.Context, c *Client, cfg *Config, path, typ string, log io.Writer) (err error) {
	if typ != "txt" && typ != "csv" {
		return fmt.Errorf("type 은 csv 또는 txt 여야 합니다: %q", typ)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("파일을 읽을 수 없습니다: %v", err)
	}
	payload, enc := encodePayload(data)
	b64 := base64.StdEncoding.EncodeToString(payload)
	chunks := splitChunks(b64, cfg.ChunkSize)
	n := len(chunks)

	orig, err := c.GetVars(ctx)
	if err != nil {
		return err
	}
	if hasBlock(orig) {
		return errBusy
	}
	session := newSession()
	bp, err := saveLocalBackup(cfg.InventoryID, session, orig)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "파일 %s (%d bytes, %s, %s %d자) 를 조각 %d개로 보냅니다. 세션 %s\n",
		filepath.Base(path), len(data), typ, enc, len(b64), n, session)
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
		"file_name": filepath.Base(path), "type": typ, "encoding": enc,
		"total_bytes": strconv.Itoa(len(data)), "sha256": sha256Hex(data),
		"chunk_total": strconv.Itoa(n), "backup_b64": base64.StdEncoding.EncodeToString([]byte(orig)),
	}
	if est := len(base["backup_b64"]) + len(chunks[0]) + 2048; est > nginxBodyLimit {
		fmt.Fprintf(log, "경고: PATCH 본문이 약 %d bytes 로 AWX nginx 한도(%d bytes, docs/limits.md)를 넘을 수 있습니다. chunk_size 를 줄이세요.\n", est, nginxBodyLimit)
	}
	start := time.Now()
	for k := 1; k <= n; k++ {
		blk := base.clone()
		blk["chunk_index"] = strconv.Itoa(k)
		blk["chunk_data"] = chunks[k-1]
		blk["chunk_sha256"] = sha256Hex([]byte(chunks[k-1]))
		blk["ack"] = strconv.Itoa(k - 1)
		blk["updated_at"] = nowStamp()
		fmt.Fprintf(log, "[%d/%d] 전송 중 (%d자)\n", k, n, len(chunks[k-1]))
		touched = true
		if err = c.PatchVars(ctx, blk.Render()); err != nil {
			return err
		}
		var done bool
		done, err = waitAck(ctx, c, cfg, session, orig, k, n, log)
		if err != nil {
			if done { // 상대가 이미 원복했다
				touched = false
			}
			return err
		}
	}
	// 다운로더가 원복한다. 혹시 우리 블록이 남아 있으면 정리한다.
	ctx2, cancel := bgCtx(60 * time.Second)
	defer cancel()
	if cur, gerr := c.GetVars(ctx2); gerr == nil && cur != orig {
		if b, ok := parseBlock(cur); ok && b["session"] == session {
			if perr := c.PatchVars(ctx2, orig); perr != nil {
				fmt.Fprintf(log, "경고: 마지막 원복 실패: %v\n", perr)
			}
		}
	}
	el := time.Since(start)
	fmt.Fprintf(log, "완료: %d bytes, 조각 %d개, %.2f초 (%.1f KiB/s). variables 원복 확인.\n",
		len(data), n, el.Seconds(), float64(len(data))/1024/maxf(el.Seconds(), 0.001))
	return nil
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// waitAck 는 다운로더의 ack 를 기다린다. 두 번째 값이 true 이면 variables 가 이미 원본이다.
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
			case !ok && cur == orig && k == n:
				return true, nil // 다운로더가 받고 원복함
			case !ok && cur == orig:
				return true, fmt.Errorf("다운로더가 세션을 중단하고 원복했습니다 (%d/%d 조각에서)", k, n)
			case !ok:
				return true, errors.New("variables 가 다른 값으로 바뀌었습니다. 중단합니다")
			case b["session"] != session:
				return true, fmt.Errorf("다른 세션(%s)이 variables 를 덮어썼습니다", b["session"])
			case b.int("ack") >= k:
				return false, nil
			}
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("[%d/%d] 다운로더 ack 대기 시간 초과(%s). 다운로더(awx_fetch.exe)를 실행했는지 확인하세요", k, n, cfg.Timeout)
		}
		d = nextPoll(d)
	}
}
