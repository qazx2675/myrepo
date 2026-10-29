package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetKey는 conf 파일에서 key 줄의 값만 value로 바꾼다 (Load와 같은 'key = value' / '#' 주석 규칙).
// 다른 줄·주석·줄 끝 주석·줄바꿈 스타일(CRLF)은 그대로 두고, key가 없으면 파일 끝에 추가한다.
// 임시파일에 쓴 뒤 rename으로 교체하며 원래 파일 권한을 유지한다.
func SetKey(path, key, value string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("설정 파일을 열 수 없습니다 (%s): %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("설정 파일 읽기 오류 (%s): %w", path, err)
	}
	content := string(data)

	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}

	lines := strings.SplitAfter(content, "\n")
	found := false
	for i, raw := range lines {
		body := strings.TrimRight(raw, "\r\n")
		term := raw[len(body):]

		code, comment := body, ""
		if idx := strings.Index(body, "#"); idx >= 0 {
			code, comment = body[:idx], body[idx:]
		}
		eq := strings.Index(code, "=")
		if eq < 0 || strings.TrimSpace(code[:eq]) != key {
			continue
		}

		rest := code[eq+1:]
		trimmedLeft := strings.TrimLeft(rest, " \t")
		lead := rest[:len(rest)-len(trimmedLeft)]
		oldVal := strings.TrimRight(trimmedLeft, " \t")
		trail := trimmedLeft[len(oldVal):]
		if lead == "" && oldVal == "" {
			lead = " " // 'key =' 처럼 값이 빈 줄은 '= 값' 모양으로
		}
		lines[i] = code[:eq+1] + lead + value + trail + comment + term
		found = true
	}
	out := strings.Join(lines, "")
	if !found {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += eol
		}
		out += key + " = " + value + eol
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("임시 파일 생성 실패: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("임시 파일 쓰기 실패: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("임시 파일 닫기 실패: %w", err)
	}
	if err := os.Chmod(tmpName, st.Mode().Perm()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("파일 권한 설정 실패: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("설정 파일 교체 실패 (%s): %w", path, err)
	}
	return nil
}
