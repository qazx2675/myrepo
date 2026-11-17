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
	"strings"
	"time"
)

// fetchFile 은 다운로더다. 조각을 받아 합치고 SHA-256 을 검증해 저장한 뒤 variables 를 원복한다.
func fetchFile(ctx context.Context, c *Client, cfg *Config, outDir string, log io.Writer) (saved string, err error) {
	var (
		session  string
		orig     string
		haveOrig bool
		meta     Block
		n        int
		expected = 1
		parts    []string
	)
	defer func() {
		if err != nil && haveOrig {
			if rerr := restoreIfOurs(c, session, orig, log); rerr != nil {
				fmt.Fprintln(log, rerr)
			}
		}
	}()

	fmt.Fprintln(log, "업로드를 기다립니다...")
	deadline := time.Now().Add(cfg.Timeout)
	d := pollMin
	start := time.Now()
	for {
		cur, gerr := c.GetVars(ctx)
		if gerr != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if session == "" {
				return "", gerr
			}
			fmt.Fprintf(log, "경고: 조회 실패, 재시도: %v\n", gerr)
		} else {
			b, ok := parseBlock(cur)
			if !ok && session != "" {
				haveOrig = false
				return "", fmt.Errorf("업로더가 세션을 중단했습니다 (%d/%d 조각 수신 후)", expected-1, n)
			}
			if ok && session == "" {
				if b["version"] != protoVersion {
					return "", fmt.Errorf("지원하지 않는 양식 version %q", b["version"])
				}
				if b["state"] == stateSending && b.int("chunk_index") != 1 {
					return "", fmt.Errorf("진행 중인 세션 %s 이(가) %s번 조각에 있습니다. 업로더를 다시 실행하세요 (필요하면 --restore)", b["session"], b["chunk_index"])
				}
				raw, derr := base64.StdEncoding.DecodeString(b["backup_b64"])
				if derr != nil {
					return "", fmt.Errorf("backup_b64 해독 실패: %v", derr)
				}
				session, orig, haveOrig, meta, n = b["session"], string(raw), true, b.clone(), b.int("chunk_total")
				if n < 1 {
					return "", fmt.Errorf("chunk_total 이 잘못됐습니다: %q", b["chunk_total"])
				}
				start = time.Now()
				fmt.Fprintf(log, "세션 %s: %s (%s bytes, %s, 조각 %d개)\n", session, b["file_name"], b["total_bytes"], b["encoding"], n)
			}
			if ok && b["session"] != session {
				return "", fmt.Errorf("다른 세션(%s)이 끼어들었습니다", b["session"])
			}
			if ok && b["state"] == stateSending && b.int("chunk_index") == expected {
				chunk := b["chunk_data"]
				if sha256Hex([]byte(chunk)) != b["chunk_sha256"] {
					return "", fmt.Errorf("[%d/%d] 조각 SHA-256 불일치", expected, n)
				}
				parts = append(parts, chunk)
				fmt.Fprintf(log, "[%d/%d] 수신 중 (%d자)\n", expected, n, len(chunk))
				if expected < n {
					ack := b.clone()
					ack["state"], ack["chunk_data"], ack["chunk_sha256"] = stateAck, "", ""
					ack["ack"] = strconv.Itoa(expected)
					ack["updated_at"] = nowStamp()
					if err := c.PatchVars(ctx, ack.Render()); err != nil {
						return "", err
					}
					expected++
					d = pollMin
					deadline = time.Now().Add(cfg.Timeout)
					continue
				}
				saved, err = finishFetch(meta, strings.Join(parts, ""), outDir)
				if err != nil {
					return "", err
				}
				ctx2, cancel := bgCtx(60 * time.Second)
				defer cancel()
				if err := c.PatchVars(ctx2, orig); err != nil {
					return saved, fmt.Errorf("파일은 저장했지만 원복 실패: %v (--restore 를 실행하세요)", err)
				}
				haveOrig = false
				el := time.Since(start)
				fmt.Fprintf(log, "완료: %s 저장 (%s bytes, SHA-256 일치, %.2f초). variables 원복 완료.\n", saved, meta["total_bytes"], el.Seconds())
				return saved, nil
			}
		}
		if time.Now().After(deadline) {
			if session == "" {
				return "", fmt.Errorf("업로드 대기 시간 초과(%s)", cfg.Timeout)
			}
			return "", fmt.Errorf("[%d/%d] 다음 조각 대기 시간 초과(%s)", expected, n, cfg.Timeout)
		}
		if err := sleepCtx(ctx, d); err != nil {
			return "", err
		}
		d = nextPoll(d)
	}
}

func finishFetch(meta Block, b64, outDir string) (string, error) {
	data, err := decodePayload(b64, meta["encoding"])
	if err != nil {
		return "", err
	}
	if strconv.Itoa(len(data)) != meta["total_bytes"] {
		return "", fmt.Errorf("크기 불일치: 받은 %d bytes, 기대 %s bytes", len(data), meta["total_bytes"])
	}
	if sha256Hex(data) != meta["sha256"] {
		return "", errors.New("전체 SHA-256 불일치")
	}
	if outDir == "" {
		outDir = filepath.Join(exeDir(), "downloads")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("저장 폴더 생성 실패: %v", err)
	}
	p := uniquePath(outDir, sanitizeFileName(meta["file_name"]))
	tmp := p + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("저장 실패: %v", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("저장 실패: %v", err)
	}
	return p, nil
}
