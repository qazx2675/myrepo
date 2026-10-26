package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// rfenc.go 는 2순위(벤더 admin) 계정 비밀번호를 암호화 파일 rf.enc 로 다룹니다.
//
// rf.conf 에는 계정명(<vendor>_user)만 두고, 비밀번호는 bash encrypt.sh rf [벤더] 로
// pass.enc 와 같은 방식(AES-256-GCM, 같은 key.bin)으로 암호화해 rf.enc 에 저장합니다.
//   - 형식: 한 줄에 벤더 하나, "<vendor>=<base64(nonce||암호문)>". # 주석과 빈 줄은 무시합니다.
//     벤더마다 따로 암호화하므로 한 벤더를 갱신해도 다른 벤더 줄은 그대로 남습니다.
//   - 위치: bios.conf 의 rf_enc_file (기본 rf.enc, pass.enc 와 같은 폴더). 권한 0600.
//   - rf.conf 에 평문 <vendor>_pass 가 남아 있으면 경고 한 줄 후 그대로 씁니다(과도기).
//     같은 벤더가 rf.enc 에도 있으면 rf.enc 가 우선입니다.
// 비밀번호는 어떤 출력·로그·오류 메시지에도 싣지 않습니다.

// rfEncMaxBytes 는 rf.enc 크기 상한입니다 (벤더 6개에는 충분).
const rfEncMaxBytes = 64 * 1024

// validRFVendor 는 v 가 rf.conf/rf.enc 벤더 키(소문자)인지 봅니다.
func validRFVendor(v string) bool {
	for _, k := range fallbackVendors {
		if v == k {
			return true
		}
	}
	return false
}

// rfEncLine 은 rf.enc 의 한 줄입니다. vendor 가 비면 주석·빈 줄(raw 그대로 보존)입니다.
type rfEncLine struct {
	raw    string
	vendor string
	ct     []byte
}

// parseRFEnc 는 rf.enc 텍스트를 줄 단위로 해석합니다 (복호화하지 않음). 오류 메시지에 값은 싣지 않습니다.
func parseRFEnc(text, name string) ([]rfEncLine, error) {
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil, nil
	}
	var out []rfEncLine
	seen := map[string]bool{}
	for i, raw := range strings.Split(text, "\n") {
		raw = strings.TrimRight(raw, "\r")
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			out = append(out, rfEncLine{raw: raw})
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("%s:%d: <vendor>=<암호문> 형식이 아닙니다", name, i+1)
		}
		v := strings.ToLower(strings.TrimSpace(line[:eq]))
		if !validRFVendor(v) {
			return nil, fmt.Errorf("%s:%d: 알 수 없는 벤더 (vendor=%s)", name, i+1, strings.Join(fallbackVendors, ","))
		}
		if seen[v] {
			return nil, fmt.Errorf("%s:%d: 벤더 %s 가 두 번 있습니다", name, i+1, v)
		}
		seen[v] = true
		ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[eq+1:]))
		if err != nil || len(ct) == 0 {
			return nil, fmt.Errorf("%s:%d: %s 암호문이 올바르지 않습니다 (bash encrypt.sh rf %s 로 다시 저장)", name, i+1, v, v)
		}
		out = append(out, rfEncLine{raw: raw, vendor: v, ct: ct})
	}
	return out, nil
}

// readRFEncFile 은 rf.enc 를 읽어 줄 목록을 돌려줍니다. 없으면 (nil, false, nil).
func readRFEncFile(path string) (lines []rfEncLine, exists bool, err error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("2순위 계정 암호 파일 %s: %w", path, err)
	}
	if !st.Mode().IsRegular() {
		return nil, true, fmt.Errorf("2순위 계정 암호 파일 %s 은(는) 일반 파일이 아닙니다", path)
	}
	if st.Size() > rfEncMaxBytes {
		return nil, true, fmt.Errorf("2순위 계정 암호 파일 %s 이(가) 너무 큽니다 (%d 바이트 초과)", path, rfEncMaxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, true, fmt.Errorf("2순위 계정 암호 파일 %s 을(를) 읽을 수 없습니다: %w", path, err)
	}
	lines, err = parseRFEnc(string(data), path)
	return lines, true, err
}

// loadRFEnc 는 rf.enc 를 keyFile 로 복호화해 벤더 → 비밀번호를 돌려줍니다. 파일이 없으면 (nil, "", nil).
// warn 은 그룹/기타 권한이 열려 있을 때의 경고 한 줄입니다 (비밀번호 없음).
func loadRFEnc(path, keyFile string) (m map[string]string, warn string, err error) {
	lines, exists, err := readRFEncFile(path)
	if err != nil || !exists {
		return nil, "", err
	}
	if st, serr := os.Stat(path); serr == nil && runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		warn = fmt.Sprintf("2순위 계정 암호 파일 %s 권한이 그룹/기타에 열려 있습니다 (%04o). chmod 600 을 권장합니다", path, st.Mode().Perm())
	}
	m = map[string]string{}
	var key []byte
	for _, l := range lines {
		if l.vendor == "" {
			continue
		}
		if key == nil {
			if key, err = loadKey(keyFile); err != nil {
				return nil, warn, fmt.Errorf("2순위 계정 암호 파일 %s 의 키 파일: %w", path, err)
			}
		}
		plain, derr := decrypt(l.ct, key)
		if derr != nil {
			return nil, warn, fmt.Errorf("2순위 계정 암호 파일 %s: %s 복호화 실패 (rf.enc 와 %s 이 짝이 맞는지 확인)", path, l.vendor, keyFile)
		}
		m[l.vendor] = string(plain)
	}
	return m, warn, nil
}

// saveRFEncVendor 는 vendor 의 비밀번호를 암호화해 rf.enc 의 그 벤더 줄만 바꿉니다 (없으면 추가).
// 다른 벤더 줄·주석은 그대로 둡니다. 기존 줄이 있으면 그 줄들이 keyFile 로 풀리는지 먼저 확인해
// 다른 키로 만든 파일에 섞어 쓰지 않습니다. 결과 파일 권한은 0600 (임시 파일에 쓴 뒤 rename).
func saveRFEncVendor(path, keyFile, vendor, pass string) error {
	if !validRFVendor(vendor) {
		return fmt.Errorf("알 수 없는 벤더입니다 (%s 중 하나)", strings.Join(fallbackVendors, ", "))
	}
	if pass == "" {
		return errors.New("비밀번호가 비어 있습니다")
	}
	lines, _, err := readRFEncFile(path)
	if err != nil {
		return err
	}
	hasEntry := false
	for _, l := range lines {
		if l.vendor != "" {
			hasEntry = true
			break
		}
	}
	var key []byte
	if hasEntry {
		// 기존 항목이 있는데 키를 새로 만들면 기존 항목을 못 풀게 되므로 기존 키만 쓴다.
		if key, err = loadKey(keyFile); err != nil {
			return fmt.Errorf("키 파일: %w (%s 의 기존 항목을 풀 키가 필요합니다)", err, path)
		}
		for _, l := range lines {
			if l.vendor == "" {
				continue
			}
			if _, derr := decrypt(l.ct, key); derr != nil {
				return fmt.Errorf("%s 의 %s 항목이 %s 로 풀리지 않습니다 (다른 키로 만든 파일). 바꾸지 않았습니다", path, l.vendor, keyFile)
			}
		}
	} else if key, err = loadOrCreateKey(keyFile); err != nil {
		return fmt.Errorf("키 파일: %w", err)
	}
	ct, err := encrypt([]byte(pass), key)
	if err != nil {
		return fmt.Errorf("암호화: %w", err)
	}
	entry := vendor + "=" + base64.StdEncoding.EncodeToString(ct)
	var b strings.Builder
	replaced := false
	for _, l := range lines {
		if l.vendor == vendor {
			b.WriteString(entry)
			replaced = true
		} else {
			b.WriteString(l.raw)
		}
		b.WriteByte('\n')
	}
	if len(lines) == 0 {
		b.WriteString("# rf.enc — 2순위(벤더 admin) 계정 비밀번호 (bash encrypt.sh rf [벤더] 로만 갱신, key.bin 과 짝)\n")
	}
	if !replaced {
		b.WriteString(entry)
		b.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rf.enc.tmp*")
	if err != nil {
		return fmt.Errorf("암호문 저장: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 성공 후에는 없는 파일이라 무시된다
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return fmt.Errorf("암호문 권한 설정: %w", err)
	}
	if _, err := io.WriteString(tmp, b.String()); err != nil {
		tmp.Close()
		return fmt.Errorf("암호문 저장: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("암호문 저장: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("암호문 저장: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("암호문 권한 설정: %w", err)
	}
	return nil
}

// encryptRFVendor 는 vendor 의 2순위 계정 비밀번호를 입력받아 rf.enc(outPath)의 그 벤더 항목만 갱신합니다.
// 표준입력이 터미널이면 account change 와 같은 입력 규칙(글자마다 * 표시, 붙여넣기 거부, 2번 입력 일치)으로 받고,
// 파이프면 그 내용(끝 개행 제거)을 씁니다. 비밀번호는 인자로 받지 않으며 어디에도 출력하지 않습니다.
func encryptRFVendor(in *os.File, prompt io.Writer, vendor, outPath, keyPath string) error {
	vendor = normRFVendor(vendor)
	if !validRFVendor(vendor) {
		return fmt.Errorf("알 수 없는 벤더입니다 (%s 중 하나)", strings.Join(fallbackVendors, ", "))
	}
	var pass string
	if fi, err := in.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintf(prompt, "[%s] 2순위 계정 비밀번호를 입력합니다 (rf.conf 의 %s_user 계정, 저장: %s)\n", vendorLabel(vendor), vendor, outPath)
		if pass, err = readNewPassword(in, prompt); err != nil {
			return err
		}
	} else {
		raw, err := io.ReadAll(in)
		if err != nil {
			return fmt.Errorf("표준입력: %w", err)
		}
		pass = strings.TrimRight(string(raw), "\r\n")
	}
	return saveRFEncVendor(outPath, keyPath, vendor, pass)
}

// normRFVendor 는 명령행 벤더 이름을 rf.conf 벤더 키 모양(소문자, 앞뒤 공백 제거)으로 바꿉니다.
func normRFVendor(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
