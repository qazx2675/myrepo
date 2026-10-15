package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
)

// allexport.go 는 allcheck -export-attrs 입니다: 읽은 BIOS Attributes 를 호스트마다 JSON 파일로 남겨,
// 사람이 diff·vimdiff 로 비교서버와 대상서버의 값을 눈으로 비교할 수 있게 합니다 (읽은 값을 저장할 뿐 추가 접속은 없음).
// 파일에는 호스트·모델·BIOS 버전·속성 값만 있고 비밀번호·토큰은 없습니다. 0600.

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// attrFileName 은 호스트 이름을 파일 이름으로 쓸 수 있게 바꿉니다 (127.0.0.1:19301 → 127.0.0.1_19301.json).
func attrFileName(host string) string {
	n := unsafeFileChars.ReplaceAllString(host, "_")
	if n == "" || n == "." || n == ".." {
		n = "host"
	}
	return n + ".json"
}

type attrExport struct {
	Host        string            `json:"host"`
	IP          string            `json:"ip,omitempty"`
	Vendor      string            `json:"vendor"`
	Model       string            `json:"model"`
	BiosVersion string            `json:"bios_version"`
	Attributes  map[string]string `json:"attributes"`
}

// exportAttrs 는 스냅샷의 속성을 dir/<호스트>.json 으로 씁니다. 읽기에 실패한 스냅샷은 건너뜁니다.
func exportAttrs(dir string, s *biosSnap, n *int32) error {
	if dir == "" || s == nil || !s.ok() || s.Attrs == nil {
		return nil
	}
	b, err := json.MarshalIndent(attrExport{
		Host: s.Target.Hostname, IP: s.Target.IP, Vendor: s.Vendor, Model: s.Model,
		BiosVersion: s.BiosVersion, Attributes: s.Attrs,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, attrFileName(s.Target.Hostname)), append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("속성 JSON 저장: %w", err)
	}
	atomic.AddInt32(n, 1)
	return nil
}
