// oklist.go - oklist.txt(기준 설정표) 읽기, 조사값과의 비교, <호스트파일>.res 파일 쓰기(계획서 4.9).
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// cfgN 은 oklist 와 비교하는 설정 칸 수다: OS커널, appl, dns, ldap_URI, binddn, h/t (이 순서).
const cfgN = 6

// cmpOn 은 Snapshot.Cmp 에서 "oklist 에 있고 현재 형식 조사 결과가 있어 비교했다"는 표시다.
// 아래 6비트(1<<0 ~ 1<<5)는 위 순서의 칸이 oklist 와 다르다는 뜻이다.
const cmpOn uint8 = 0x80

// cmpDiffMask 는 다른 칸 비트 전체다.
const cmpDiffMask uint8 = 1<<cfgN - 1

// okCols 는 oklist 한 줄의 칸 수다: hostname + 설정 6칸.
const okCols = 1 + cfgN

// vals 는 비교하는 6칸 값이다.
func (c Check) vals() [cfgN]string {
	return [cfgN]string{c.Kernel, c.Appl, c.DNS, c.LdapURI, c.BindDN, c.HT}
}

// normCell 은 칸 값을 다듬는다: 앞뒤 공백을 떼고 안쪽 연속 공백을 하나로 줄인다.
func normCell(s string) string { return strings.Join(strings.Fields(s), " ") }

// oneLine 은 값 안의 탭·줄바꿈(CR 포함)을 공백으로 바꾼다(res 파일·복사용 표).
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
}

// Oklist 는 oklist.txt 내용이다.
type Oklist struct {
	Path  string
	Rows  map[string][cfgN]string // hostname -> 설정 6칸(다듬은 값)
	Count int                     // 쓸 수 있는 줄(서버) 수
	Warn  int                     // 무시한 줄 수(칸 부족·hostname 없음·hostname 중복)
}

// loadOklist 는 oklist 파일을 읽는다. 열 수 없으면 error 다.
func loadOklist(path string) (*Oklist, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.IsDir() {
		return nil, fmt.Errorf("%s 는 폴더입니다", path)
	}
	ok, err := parseOklist(f)
	if err != nil {
		return nil, err
	}
	ok.Path = path
	return ok, nil
}

// parseOklist 는 탭 구분 7칸(hostname OS커널 appl dns ldap_URI binddn h/t) 줄을 읽는다(엑셀에서 붙여 넣은 표).
// 칸마다 앞뒤 공백을 떼고 안쪽 연속 공백을 하나로 줄인다. 빈 줄·# 줄은 건너뛰고, 첫 칸이 hostname(대소문자 무시)이면
// 머리줄로 보고 건너뛴다. 칸이 7개보다 적거나 hostname 이 비었거나 앞에 나온 hostname 이면 무시하고 경고로 센다
// (처음 줄을 쓴다). 8번째 칸부터는 무시한다. CRLF 도 읽는다.
func parseOklist(r io.Reader) (*Oklist, error) {
	ok := &Oklist{Rows: map[string][cfgN]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		host := normCell(cells[0])
		if strings.EqualFold(host, "hostname") {
			continue
		}
		if len(cells) < okCols || host == "" {
			ok.Warn++
			continue
		}
		if _, dup := ok.Rows[host]; dup {
			ok.Warn++
			continue
		}
		var row [cfgN]string
		for i := range row {
			row[i] = normCell(cells[i+1])
		}
		ok.Rows[host] = row
		ok.Count++
	}
	return ok, sc.Err()
}

// cfgDiff 는 조사값과 oklist 줄의 다른 칸 비트다(대소문자 구분, 여러 값은 순서까지 같아야 같다).
// 조사값이 비었으면 oklist 칸이 비지 않은 한 다르다.
func cfgDiff(ck Check, row [cfgN]string) uint8 {
	var m uint8
	for i, v := range ck.vals() {
		if normCell(v) != row[i] {
			m |= 1 << i
		}
	}
	return m
}

// CfgSummary 는 `l` 패널 머리줄의 oklist 집계다. oklist 가 없으면 HasOK 가 false 이고 나머지는 0 이다.
type CfgSummary struct {
	HasOK   bool
	OK      int // oklist 의 서버(줄) 수
	Checked int // 현재 형식 조사 결과가 있는 서버 수(전체)
	Diff    int // oklist 에 있고 조사했으며 한 칸 이상 다른 서버 수
	Warn    int // oklist 에서 무시한 줄 수
}

// hostOrder 는 Records 를 호스트파일 순서로 가리키는 인덱스다(HostOrder 가 없으면 Records 순서).
func (s *Snapshot) hostOrder() []int {
	if len(s.HostOrder) == len(s.Records) {
		return s.HostOrder
	}
	out := make([]int, len(s.Records))
	for i := range out {
		out[i] = i
	}
	return out
}

// cmpOf 는 Records[i] 의 비교 비트다(oklist 가 없으면 0).
func (s *Snapshot) cmpOf(i int) uint8 {
	if i < len(s.Cmp) {
		return s.Cmp[i]
	}
	return 0
}

// resFileName 은 실행 디렉터리에 둘 res 파일 경로(<호스트파일 이름>.res)다. state 파일과 같은 규칙이다.
func resFileName(hostfile string) string {
	return filepath.Base(hostfile) + ".res"
}

// resHeader 는 res 파일 머리줄이다.
const resHeader = "hostname\tOS커널\tappl\tdns\tldap_URI\tbinddn\th/t\t설치/yumupdate여부\t설정동일여부"

// doneNow 는 지금 상태가 설치/yumupdate 완료로 치는 상태인지 알려준다(InstallDone 을 켜는 조건).
func doneNow(r Record) bool {
	if r.IsYum() {
		return r.Status == StatusYumDone
	}
	return r.Status == StatusDone
}

// resContent 는 res 파일 내용이다: 머리줄 + 모든 서버(호스트파일 순서) 한 줄씩, 탭 구분 UTF-8, LF, BOM 없음.
// 조사 전 서버의 설정 칸은 비운다. 설정동일여부는 oklist 에 있고 조사 결과가 있을 때만 O/X 다.
func resContent(s *Snapshot) []byte {
	var b bytes.Buffer
	b.WriteString(resHeader + "\n")
	for _, i := range s.hostOrder() {
		r := s.Records[i]
		var v [cfgN]string
		if r.Check.current() {
			v = r.Check.vals()
		}
		b.WriteString(oneLine(r.Name))
		for _, x := range v {
			b.WriteString("\t" + oneLine(x))
		}
		done := "X"
		if r.InstallDone || doneNow(r) {
			done = "O"
		}
		same := ""
		if m := s.cmpOf(i); m&cmpOn != 0 {
			same = "O"
			if m&cmpDiffMask != 0 {
				same = "X"
			}
		}
		b.WriteString("\t" + done + "\t" + same + "\n")
	}
	return b.Bytes()
}

// writeRes 는 스냅샷으로 res 파일을 쓴다. 내용이 지난번과 같으면 쓰지 않는다. 임시 파일에 쓰고 이름을 바꾼다(0644).
// 실패해도 수집은 계속한다: 이유를 resErr 에 두어 다음 스냅샷의 오류 줄에 보이고, 데몬 로그에는 한 번만 남긴다.
func (c *Collector) writeRes(s *Snapshot) {
	if c.resPath == "" {
		return
	}
	c.resMu.Lock()
	defer c.resMu.Unlock()
	b := resContent(s)
	if c.resLast != nil && bytes.Equal(b, c.resLast) {
		return
	}
	err := writeFileAtomicMode(c.resPath, b, 0o644)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.resErr = "res 저장 실패: " + err.Error()
		if !c.resLogged && c.resLog != nil {
			c.resLog(c.resErr)
			c.resLogged = true
		}
		return
	}
	c.resErr = ""
	c.resLast = b
}
