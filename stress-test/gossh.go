package main

// gossh 경로 결정·실행·출력 파싱 (setup_check 의 gossh 처리 규칙을 복사)

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// expandPath 는 값 양끝 따옴표를 벗기고 앞의 ~/ 를 홈 폴더로 바꾼다.
func expandPath(v string) string {
	v = unquote(strings.TrimSpace(v))
	if strings.HasPrefix(v, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(home, v[2:])
		}
	}
	return v
}

// unquote 는 값 전체를 감싼 따옴표("…" 또는 '…')를 뗀다.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

// resolveGossh 는 conf gossh → 환경변수 GOSSH → PATH 순으로 gossh 실행 경로를 정한다. 폴더면 그 안의 gossh.
func resolveGossh(confVal string) (string, error) {
	for _, src := range []struct{ name, v string }{{"설정 파일의 gossh", confVal}, {"환경변수 GOSSH", os.Getenv("GOSSH")}} {
		v := expandPath(src.v)
		if v == "" {
			continue
		}
		if fi, err := os.Stat(v); err == nil && fi.IsDir() {
			v = filepath.Join(v, "gossh")
		}
		if _, err := os.Stat(v); err != nil {
			return "", fmt.Errorf("%s 경로에 gossh 가 없습니다: %s", src.name, v)
		}
		return v, nil
	}
	p, err := exec.LookPath("gossh")
	if err != nil {
		return "", fmt.Errorf("gossh 를 찾을 수 없습니다. 설정 파일 gossh 키, stress_run.sh 의 GOSSH 변수, 또는 PATH 를 확인하세요")
	}
	return p, nil
}

// 접속불가로 보는 gossh 결과 파일 접미사와, 실행 뒤 지울 부수 파일 접미사
var (
	offSuffixes = []string{"_res_off", "_res_refsed", "_res_cancel"}
	resSuffixes = []string{"_res_off", "_res_refsed", "_res_cancel", "_os_install", "_nosvrauto"}
)

// gosshRunner 는 작업 폴더에 임시 호스트 파일을 만들어 gossh 를 실행한다.
type gosshRunner struct {
	path    string
	workDir string
	log     io.Writer // gossh stdout+stderr 를 이어 붙일 로그
	seq     int
}

// run 은 hosts 에 cmd 를 실행해 출력(stdout+stderr)과 접속불가 호스트 집합을 돌려준다. 임시 파일은 지운다.
func (g *gosshRunner) run(hosts []string, cmd string) (string, map[string]bool, error) {
	g.seq++
	hf := filepath.Join(g.workDir, fmt.Sprintf("hosts_%d", g.seq))
	if err := os.WriteFile(hf, []byte(strings.Join(hosts, "\n")+"\n"), 0o644); err != nil {
		return "", nil, err
	}
	defer func() {
		os.Remove(hf)
		for _, s := range resSuffixes {
			os.Remove(hf + s)
		}
	}()
	for _, s := range resSuffixes {
		os.Remove(hf + s)
	}
	// gossh 는 원격 stderr 와 "host: ERROR: 종료코드 N" 을 stderr 로 낸다. 응답한 호스트를 접속불가로
	// 잘못 보지 않도록 stderr 도 함께 파싱한다 (접속불가 판정은 결과 파일 기준).
	var stdout, stderr bytes.Buffer
	c := exec.Command(g.path, "-w", hf, "-script", cmd)
	c.Stdin = nil
	c.Stdout = io.MultiWriter(g.log, &stdout)
	c.Stderr = io.MultiWriter(g.log, &stderr)
	err := c.Run()
	if _, ok := err.(*exec.ExitError); ok {
		err = nil // 비정상 종료는 결과 파일·출력으로 판정
	}
	if err != nil {
		return "", nil, fmt.Errorf("gossh 실행 실패: %w", err)
	}
	off := map[string]bool{}
	for _, s := range offSuffixes {
		if b, e := os.ReadFile(hf + s); e == nil {
			for _, h := range strings.Fields(string(b)) {
				off[h] = true
			}
		}
	}
	return stdout.String() + "\n" + stderr.String(), off, nil
}

// parseGosshOutput 은 gossh stdout 의 "host: 내용" 줄을 호스트별 내용 목록으로 만든다.
func parseGosshOutput(out string) map[string][]string {
	res := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		host := line[:i]
		res[host] = append(res[host], strings.TrimSpace(line[i+2:]))
	}
	return res
}

const (
	vendorIntel   = "Intel"
	vendorAMD     = "AMD"
	vendorUnknown = "unknown"
)

// vendorCmd 는 접속체크 겸 CPU 벤더 확인 명령이다.
const vendorCmd = "grep -m1 vendor_id /proc/cpuinfo"

// parseVendors 는 접속체크 결과로 호스트별 벤더를 정한다.
// 접속불가 결과 파일에 있거나 아무 줄도 없으면 "" (접속불가), vendor_id 값으로 Intel/AMD, 그 외 응답은 unknown.
func parseVendors(out string, off map[string]bool, hosts []string) map[string]string {
	lines := parseGosshOutput(out)
	res := map[string]string{}
	for _, h := range hosts {
		ls := lines[h]
		if off[h] || len(ls) == 0 {
			res[h] = ""
			continue
		}
		res[h] = vendorUnknown
		for _, l := range ls {
			if !strings.HasPrefix(l, "vendor_id") {
				continue
			}
			if j := strings.Index(l, ":"); j >= 0 {
				switch strings.TrimSpace(l[j+1:]) {
				case "GenuineIntel":
					res[h] = vendorIntel
				case "AuthenticAMD":
					res[h] = vendorAMD
				}
			}
		}
	}
	return res
}

// loadCmd 는 감시 명령이다.
const loadCmd = "cat /proc/loadavg"

// parseLoads 는 "host: 0.52 0.40 0.30 1/200 1234" 줄에서 호스트별 1분 로드를 뽑는다. 못 읽은 호스트는 빠진다.
func parseLoads(out string) map[string]float64 {
	res := map[string]float64{}
	for h, ls := range parseGosshOutput(out) {
		for _, l := range ls {
			f := strings.Fields(l)
			if len(f) < 3 {
				continue
			}
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				res[h] = v
				break
			}
		}
	}
	return res
}
