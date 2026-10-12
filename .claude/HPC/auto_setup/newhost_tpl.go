// newhost_tpl.go - v0.8.0 신규 설치 양식 템플릿(conf 의 newhost_template_1~9) 파싱·검증, gossh 작업 실행 위치 기준(work_os6_over)
//
// 템플릿 형식: "별칭|infra nic disk 용량 os boot" (예 "A|sec bond sda 500 8.10 uefi").
// 별칭은 비워도 되고(`|sec bond …`) `|` 자체를 생략해도 된다(`sec bond …`).
// 신규 설치 양식(S5)이 같은 구조체·파서(parseNewhostTpl)로 user 별 템플릿 파일과 화면 입력도 처리한다.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// 기본 root 파티션 (disk 로 정해짐)
const (
	newhostPartSda  = "sda5"
	newhostPartNvme = "nvme0n1p8"
)

// workOS6OverDefault: work_os6_over 가 비었거나 잘못됐을 때 쓰는 값
const workOS6OverDefault = 1000

// NewhostTpl: 신규 설치 양식 템플릿 한 건
type NewhostTpl struct {
	No    int    // conf 번호 1~9 (user 별 템플릿·화면 입력은 0)
	Alias string // 짧은 별칭 (없으면 "")
	Infra string
	NIC   string
	Disk  string // sda | nvme
	Size  string // 용량 (숫자만)
	OS    string
	Boot  string // uefi | legacy | 레거시 (입력한 그대로, 소문자)
	Part  string // root 파티션 — Disk 로 유도: sda→sda5, nvme→nvme0n1p8
}

// newhostPart: disk 에서 root 파티션을 유도 (sda/nvme 가 아니면 "")
func newhostPart(disk string) string {
	switch disk {
	case "sda":
		return newhostPartSda
	case "nvme":
		return newhostPartNvme
	}
	return ""
}

// Name: 값으로 만든 자동 이름 `infra-nic-disk-용량-os-boot` (소문자) — 예 sec-bond-sda-500-8.10-uefi
func (t NewhostTpl) Name() string {
	return strings.ToLower(strings.Join([]string{t.Infra, t.NIC, t.Disk, t.Size, t.OS, t.Boot}, "-"))
}

// Spec: 저장·표시용 한 줄 `별칭|infra nic disk 용량 os boot` (별칭이 없으면 `|` 없이 값만). parseNewhostTpl 의 입력 형식과 같다.
func (t NewhostTpl) Spec() string {
	v := strings.Join([]string{t.Infra, t.NIC, t.Disk, t.Size, t.OS, t.Boot}, " ")
	if t.Alias == "" {
		return v
	}
	return t.Alias + "|" + v
}

// parseNewhostTpl: 템플릿 한 줄을 검사해 돌려준다 (No 는 호출자가 채움). 오류는 사유 문자열.
//   - 필드 6개(infra nic disk 용량 os boot), 각 토큰은 공백·`|` 없음
//   - disk 는 sda/nvme, 용량은 숫자, boot 는 uefi/legacy/레거시 (disk·boot 는 대소문자 무시, 소문자로 저장)
//   - 별칭은 공백 없음, 숫자만으로는 불가(행 번호와 구분되지 않음)
func parseNewhostTpl(raw string) (NewhostTpl, error) {
	var t NewhostTpl
	raw = strings.TrimSpace(raw)
	spec := raw
	if i := strings.IndexByte(raw, '|'); i >= 0 {
		t.Alias = strings.TrimSpace(raw[:i])
		spec = raw[i+1:]
	}
	if t.Alias != "" {
		if strings.ContainsAny(t.Alias, " \t") {
			return t, fmt.Errorf("별칭 %q 에 공백을 쓸 수 없음", t.Alias)
		}
		if _, err := strconv.Atoi(t.Alias); err == nil {
			return t, fmt.Errorf("별칭 %q 은 숫자만으로 쓸 수 없음 (행 번호와 구분되지 않음)", t.Alias)
		}
	}
	f := strings.Fields(spec)
	if len(f) != 6 {
		return t, fmt.Errorf("형식 오류 (별칭|infra nic disk 용량 os boot — 공백으로 나뉜 6개 값이 필요, 현재 %d개)", len(f))
	}
	for _, v := range f {
		if strings.Contains(v, "|") {
			return t, fmt.Errorf("값 %q 에 | 를 쓸 수 없음", v)
		}
	}
	t.Infra, t.NIC, t.Size, t.OS = f[0], f[1], f[3], f[4]
	t.Disk, t.Boot = strings.ToLower(f[2]), strings.ToLower(f[5])
	if t.Part = newhostPart(t.Disk); t.Part == "" {
		return t, fmt.Errorf("disk %q 은 허용되지 않음 (sda 또는 nvme)", f[2])
	}
	if _, err := strconv.ParseUint(t.Size, 10, 32); err != nil {
		return t, fmt.Errorf("용량 %q 은 숫자여야 함", t.Size)
	}
	switch t.Boot {
	case "uefi", "legacy", "레거시":
	default:
		return t, fmt.Errorf("boot %q 은 허용되지 않음 (uefi, legacy, 레거시)", f[5])
	}
	return t, nil
}

// parseNewhostTemplates: conf 의 newhost_template_1~9 중 비어 있지 않은 것을 검사한다.
// 빈 변수는 조용히 건너뛰고, 형식·값이 틀리거나 별칭이 앞 템플릿과 겹치는 것은 제외하며 사유를 problems 로 돌려준다 (parseAwxProfiles 와 같은 방식).
func parseNewhostTemplates() (valid []NewhostTpl, problems []string) {
	vals := []string{newhost_template_1, newhost_template_2, newhost_template_3, newhost_template_4, newhost_template_5,
		newhost_template_6, newhost_template_7, newhost_template_8, newhost_template_9}
	aliasNo := map[string]int{}
	for i, raw := range vals {
		no := i + 1
		if strings.TrimSpace(raw) == "" {
			continue
		}
		name := fmt.Sprintf("newhost_template_%d", no)
		t, err := parseNewhostTpl(raw)
		if err != nil {
			problems = append(problems, name+": "+err.Error())
			continue
		}
		if t.Alias != "" {
			key := strings.ToLower(t.Alias)
			if prev, dup := aliasNo[key]; dup {
				problems = append(problems, fmt.Sprintf("%s: 별칭 %s 이 newhost_template_%d 과 겹침", name, t.Alias, prev))
				continue
			}
			aliasNo[key] = no
		}
		t.No = no
		valid = append(valid, t)
	}
	return valid, problems
}

// parseWorkOS6Over: work_os6_over 해석 — 비면 1000, 1 이상의 정수가 아니면 1000 + 경고 사유 problems
func parseWorkOS6Over() (n int, problems []string) {
	raw := strings.TrimSpace(work_os6_over)
	if raw == "" {
		return workOS6OverDefault, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return workOS6OverDefault, []string{fmt.Sprintf("work_os6_over: %q 은 1 이상의 숫자가 아님 — %d 으로 처리", raw, workOS6OverDefault)}
	}
	return v, nil
}

// workOS6Over: gossh 작업(s)에서 os8 gossh 로 실행하는 최대 대수 (이 값 이하는 os8, 초과는 os6_mgmt 경유)
func workOS6Over() int {
	n, _ := parseWorkOS6Over()
	return n
}
