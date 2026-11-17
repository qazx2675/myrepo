package main

import (
	"bytes"
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

// fetchFile 은 다운로더다. 조각을 받아 합치고 SHA-256 을 검증해 저장한다.
// 마지막 조각(또는 실패한 조각)에서 state=done, result=ok|fail 을 남긴다.
// 원복은 업로더가 결과를 읽은 뒤 한다. 업로더가 cfg.Timeout 안에 원복하지 않으면 다운로더가 원복한다.
func fetchFile(ctx context.Context, c *Client, cfg *Config, outDir string, log io.Writer) (saved string, err error) {
	var (
		session  string
		orig     string
		haveOrig bool
		meta     Block
		n        int
		expected = 1
		parts    [][]byte
		last     Block
	)
	defer func() {
		if err != nil && haveOrig {
			if rerr := restoreIfOurs(c, session, orig, log); rerr != nil {
				fmt.Fprintln(log, rerr)
			}
		}
	}()
	// report 는 결과를 남기고 업로더의 원복을 기다린다. 쓰기에 실패하면 haveOrig 를 남겨 defer 가 원복한다.
	report := func(result, msg string) error {
		blk := last.clone()
		blk["state"], blk["result"], blk["message"] = stateDone, result, oneLine(msg, 300)
		blk["chunk_text"], blk["chunk_data"], blk["chunk_sha256"] = "", "", ""
		blk["ack"] = strconv.Itoa(expected)
		blk["updated_at"] = nowStamp()
		if perr := c.PatchVars(ctx, blk.Render()); perr != nil {
			return fmt.Errorf("수신 결과 기록 실패: %v", perr)
		}
		haveOrig = false
		waitRestored(ctx, c, cfg, session, orig, log)
		return nil
	}
	fail := func(e error) (string, error) {
		if rerr := report(recvFail, e.Error()); rerr != nil {
			fmt.Fprintln(log, rerr)
		}
		return "", e
	}

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
			if ok && session == "" && b["state"] == stateDone {
				ok = false // 이전 세션 결과가 아직 원복 전이다. 새 세션을 기다린다.
			}
			if ok && session == "" {
				if b["version"] != protoVersion && b["version"] != protoVersionV1 {
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
				last = b
				if b["version"] == protoVersion {
					if kerr := checkV2Keys(b); kerr != nil {
						return fail(fmt.Errorf("[%d/%d] %v", expected, n, kerr))
					}
					if b["encoding"] != encText {
						return fail(fmt.Errorf("[%d/%d] version %s 는 encoding %q 만 지원합니다: %q", expected, n, protoVersion, encText, b["encoding"]))
					}
				}
				var chunk []byte
				if meta["encoding"] == encText {
					chunk = decodeChunk(b["chunk_text"], meta["eol"])
				} else {
					chunk = []byte(b["chunk_data"]) // v1 base64
				}
				if sha256Hex(chunk) != b["chunk_sha256"] {
					return fail(fmt.Errorf("[%d/%d] 조각 SHA-256 불일치 (텍스트가 아닌 바이트나 섞인 줄바꿈은 복원되지 않습니다)", expected, n))
				}
				parts = append(parts, chunk)
				fmt.Fprintf(log, "[%d/%d] 수신 중 (%d bytes)\n", expected, n, len(chunk))
				if expected < n {
					ack := b.clone()
					ack["state"], ack["chunk_data"], ack["chunk_sha256"] = stateAck, "", ""
					ack["chunk_text"] = ""
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
				saved, err = finishFetch(meta, parts, outDir)
				if err != nil {
					return fail(err)
				}
				if rerr := report(recvOK, "SHA-256 일치"); rerr != nil {
					return saved, fmt.Errorf("파일은 저장했지만 %v (업로더는 실패로 봅니다)", rerr)
				}
				el := time.Since(start)
				fmt.Fprintf(log, "완료: %s 저장 (%s bytes, SHA-256 일치, %.2f초). 결과를 업로더에 전달했습니다.\n", saved, meta["total_bytes"], el.Seconds())
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

// waitRestored 는 업로더가 결과를 읽고 원복하기를 기다린다. cfg.Timeout 이 지나면 직접 원복한다.
func waitRestored(ctx context.Context, c *Client, cfg *Config, session, orig string, log io.Writer) {
	deadline := time.Now().Add(cfg.Timeout)
	d := pollMin
	for time.Now().Before(deadline) {
		if sleepCtx(ctx, d) != nil {
			break
		}
		cur, err := c.GetVars(ctx)
		if err == nil {
			if b, ok := parseBlock(cur); !ok || b["session"] != session {
				return // 업로더가 원복했다 (또는 다른 변경)
			}
		}
		d = nextPoll(d)
	}
	fmt.Fprintln(log, "업로더가 원복하지 않아 다운로더가 원복합니다.")
	if err := restoreIfOurs(c, session, orig, log); err != nil {
		fmt.Fprintln(log, err)
	}
}

// oneLine 은 메시지를 한 줄, 최대 n 문자로 줄인다.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "..."
	}
	return s
}

func finishFetch(meta Block, parts [][]byte, outDir string) (string, error) {
	var data []byte
	if meta["encoding"] == encText {
		data = assembleText(meta, parts)
	} else {
		var err error
		if data, err = decodePayload(string(bytes.Join(parts, nil)), meta["encoding"]); err != nil {
			return "", err
		}
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
