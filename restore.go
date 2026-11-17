package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func backupDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".awx_copy", "backup")
}

// saveLocalBackup 은 시작 전 variables 원문을 로컬에 저장한다.
func saveLocalBackup(inv int, session, orig string) (string, error) {
	dir := backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("백업 폴더 생성 실패: %v", err)
	}
	p := filepath.Join(dir, fmt.Sprintf("inv%d_%s.vars", inv, session))
	if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
		return "", fmt.Errorf("로컬 백업 저장 실패: %v", err)
	}
	return p, nil
}

func latestLocalBackup(inv int) (string, error) {
	m, _ := filepath.Glob(filepath.Join(backupDir(), fmt.Sprintf("inv%d_*.vars", inv)))
	if len(m) == 0 {
		return "", errors.New("로컬 백업이 없습니다")
	}
	sort.Strings(m) // 세션 이름이 시각으로 시작하므로 마지막이 최신이다
	return m[len(m)-1], nil
}

func bgCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// restoreIfOurs 는 현재 블록이 우리 세션일 때만 원본으로 되돌린다.
func restoreIfOurs(c *Client, session, orig string, log io.Writer) error {
	ctx, cancel := bgCtx(60 * time.Second)
	defer cancel()
	cur, err := c.GetVars(ctx)
	if err == nil {
		b, ok := parseBlock(cur)
		if !ok {
			if cur == orig {
				return nil
			}
			fmt.Fprintln(log, "경고: variables 에 awx_copy 블록이 없고 원본과도 다릅니다. 다른 변경으로 보고 건드리지 않습니다.")
			return nil
		}
		if b["session"] != session {
			fmt.Fprintf(log, "경고: 다른 세션(%s)의 블록입니다. 건드리지 않습니다.\n", b["session"])
			return nil
		}
	}
	if err := c.PatchVars(ctx, orig); err != nil {
		return fmt.Errorf("원복 실패: %v (나중에 --restore 를 실행하세요)", err)
	}
	fmt.Fprintln(log, "variables 를 시작 전 원본으로 복원했습니다.")
	return nil
}

// runRestore 는 --restore 명령이다.
// 1) 블록이 있으면 블록의 backup_b64 로 복원한다.
// 2) 블록을 읽을 수 없으면 최신 로컬 백업으로 복원한다.
func runRestore(ctx context.Context, c *Client, cfg *Config, log io.Writer) error {
	cur, err := c.GetVars(ctx)
	if err != nil {
		return err
	}
	if !hasBlock(cur) {
		fmt.Fprintln(log, "awx_copy 블록이 없습니다. 복원할 것이 없습니다.")
		return nil
	}
	var orig string
	src := ""
	if b, ok := parseBlock(cur); ok {
		raw, derr := base64.StdEncoding.DecodeString(b["backup_b64"])
		if derr == nil {
			orig, src = string(raw), "블록의 backup_b64 (세션 "+b["session"]+")"
		}
	}
	if src == "" {
		p, lerr := latestLocalBackup(cfg.InventoryID)
		if lerr != nil {
			return fmt.Errorf("블록을 해석할 수 없고 %v", lerr)
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		orig, src = string(raw), "로컬 백업 "+p
	}
	if err := c.PatchVars(ctx, orig); err != nil {
		return err
	}
	after, err := c.GetVars(ctx)
	if err != nil {
		return err
	}
	if after != orig {
		return fmt.Errorf("복원 후 값이 다릅니다 (원본 %d자, 현재 %d자)", len(orig), len(after))
	}
	fmt.Fprintf(log, "복원 완료: %s 에서 %d자를 되돌렸습니다.\n", src, len(orig))
	return nil
}

func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var sb strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			sb.WriteRune('_')
		} else {
			sb.WriteRune(r)
		}
	}
	n := strings.TrimRight(sb.String(), ". ")
	if n == "" || n == "." || n == ".." {
		n = "awx_copy_file"
	}
	stem := strings.ToUpper(strings.SplitN(n, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		n = "_" + n
	}
	return n
}

// uniquePath 는 같은 이름이 있으면 _1, _2 ... 를 붙인다.
func uniquePath(dir, name string) string {
	p := filepath.Join(dir, name)
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p
		}
	}
}
