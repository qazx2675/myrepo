// newhost_tpl_test.go - 신규 설치 양식 템플릿(newhost_template_1~9) 파싱·검증, work_os6_over 해석, conf 키 로딩
package main

import (
	"reflect"
	"strings"
	"testing"
)

// setNewhostTpls: newhost_template_1~9 를 v... 로 채우고(나머지는 빈 값) 테스트가 끝나면 되돌린다
func setNewhostTpls(t *testing.T, v ...string) {
	t.Helper()
	ptrs := []*string{&newhost_template_1, &newhost_template_2, &newhost_template_3, &newhost_template_4, &newhost_template_5,
		&newhost_template_6, &newhost_template_7, &newhost_template_8, &newhost_template_9}
	old := make([]string, len(ptrs))
	for i, p := range ptrs {
		old[i] = *p
		*p = ""
	}
	t.Cleanup(func() {
		for i, p := range ptrs {
			*p = old[i]
		}
	})
	for i, s := range v {
		*ptrs[i] = s
	}
}

func TestParseNewhostTpl(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want NewhostTpl
		bad  string // 비면 성공, 아니면 오류 문구에 들어 있어야 함
	}{
		{"별칭 있음", "A|sec bond sda 500 8.10 uefi",
			NewhostTpl{Alias: "A", Infra: "sec", NIC: "bond", Disk: "sda", Size: "500", OS: "8.10", Boot: "uefi", Part: "sda5"}, ""},
		{"별칭 비어 있음 (| 만)", "|sec bond sda 500 8.10 uefi",
			NewhostTpl{Infra: "sec", NIC: "bond", Disk: "sda", Size: "500", OS: "8.10", Boot: "uefi", Part: "sda5"}, ""},
		{"| 생략", "mec eth0 nvme 400 9.6 uefi",
			NewhostTpl{Infra: "mec", NIC: "eth0", Disk: "nvme", Size: "400", OS: "9.6", Boot: "uefi", Part: "nvme0n1p8"}, ""},
		{"레거시 허용, 여러 칸 공백·탭", "  L |  tmp\teth0   sda 300 8.10 레거시 ",
			NewhostTpl{Alias: "L", Infra: "tmp", NIC: "eth0", Disk: "sda", Size: "300", OS: "8.10", Boot: "레거시", Part: "sda5"}, ""},
		{"disk·boot 대문자는 소문자로", "sec bond SDA 500 8.10 LEGACY",
			NewhostTpl{Infra: "sec", NIC: "bond", Disk: "sda", Size: "500", OS: "8.10", Boot: "legacy", Part: "sda5"}, ""},
		{"필드 부족", "A|sec bond sda 500 8.10", NewhostTpl{}, "6개"},
		{"필드 초과", "sec bond sda 500 8.10 uefi extra", NewhostTpl{}, "6개"},
		{"빈 값", "", NewhostTpl{}, "6개"},
		{"disk 불허", "sec bond sdb 500 8.10 uefi", NewhostTpl{}, "disk"},
		{"nvme0n1 같은 값도 불허 (nvme 만)", "sec bond nvme0n1 500 8.10 uefi", NewhostTpl{}, "disk"},
		{"용량 숫자 아님", "sec bond sda 5G 8.10 uefi", NewhostTpl{}, "용량"},
		{"용량 소수 불허", "sec bond sda 5.5 8.10 uefi", NewhostTpl{}, "용량"},
		{"boot 불허", "sec bond sda 500 8.10 bios", NewhostTpl{}, "boot"},
		{"값에 | 가 또 있음", "A|sec bo|nd sda 500 8.10 uefi", NewhostTpl{}, "|"},
		{"별칭에 공백", "my alias|sec bond sda 500 8.10 uefi", NewhostTpl{}, "별칭"},
		{"별칭이 숫자만", "3|sec bond sda 500 8.10 uefi", NewhostTpl{}, "별칭"},
	}
	for _, c := range cases {
		got, err := parseNewhostTpl(c.in)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%s: err=%v, %q 를 포함해야 함", c.name, err, c.bad)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: 오류 %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestNewhostTplNameAndSpec(t *testing.T) {
	tpl, err := parseNewhostTpl("A|Sec bond sda 500 8.10 uefi")
	if err != nil {
		t.Fatal(err)
	}
	if got := tpl.Name(); got != "sec-bond-sda-500-8.10-uefi" {
		t.Errorf("Name=%q", got)
	}
	if got := tpl.Spec(); got != "A|Sec bond sda 500 8.10 uefi" {
		t.Errorf("Spec=%q", got)
	}
	// Spec 은 다시 파싱하면 같은 값 (user 별 템플릿 파일 저장 → 읽기)
	back, err := parseNewhostTpl(tpl.Spec())
	if err != nil || back != tpl {
		t.Errorf("round trip: %+v err=%v", back, err)
	}
	noAlias, _ := parseNewhostTpl("mec eth0 nvme 400 9.6 uefi")
	if noAlias.Spec() != "mec eth0 nvme 400 9.6 uefi" || noAlias.Name() != "mec-eth0-nvme-400-9.6-uefi" {
		t.Errorf("별칭 없음: spec=%q name=%q", noAlias.Spec(), noAlias.Name())
	}
}

func TestParseNewhostTemplates(t *testing.T) {
	// 모두 비어 있으면 아무것도 없고 문제도 없음
	setNewhostTpls(t)
	if v, p := parseNewhostTemplates(); len(v) != 0 || len(p) != 0 {
		t.Fatalf("빈 변수: valid=%v problems=%v", v, p)
	}

	setNewhostTpls(t,
		"A|sec bond sda 500 8.10 uefi", // 1 ok
		"",                             // 2 비어 있음 — 건너뜀
		"sec bond sdz 500 8.10 uefi",   // 3 disk 오류
		"a|mec eth0 sda 500 9.6 uefi",  // 4 별칭이 1번(A)과 겹침 (대소문자 무시)
		"   ",                          // 5 공백만 — 비어 있음으로 본다
		"|mec eth0 nvme 400 9.6 uefi",  // 6 ok (별칭 없음)
	)
	valid, problems := parseNewhostTemplates()
	if len(valid) != 2 || valid[0].No != 1 || valid[1].No != 6 {
		t.Fatalf("valid=%+v", valid)
	}
	if valid[0].Part != "sda5" || valid[1].Part != "nvme0n1p8" {
		t.Errorf("Part 유도: %q %q", valid[0].Part, valid[1].Part)
	}
	if len(problems) != 2 || !strings.HasPrefix(problems[0], "newhost_template_3: ") || !strings.Contains(problems[0], "disk") ||
		!strings.HasPrefix(problems[1], "newhost_template_4: ") || !strings.Contains(problems[1], "newhost_template_1") {
		t.Errorf("problems=%q", problems)
	}
}

func TestParseWorkOS6Over(t *testing.T) {
	old := work_os6_over
	defer func() { work_os6_over = old }()
	cases := []struct {
		in      string
		want    int
		problem bool
	}{
		{"", 1000, false},
		{"  ", 1000, false},
		{"500", 500, false},
		{" 2000 ", 2000, false},
		{"abc", 1000, true},
		{"10.5", 1000, true},
		{"0", 1000, true},
		{"-5", 1000, true},
	}
	for _, c := range cases {
		work_os6_over = c.in
		n, p := parseWorkOS6Over()
		if n != c.want || (len(p) > 0) != c.problem {
			t.Errorf("work_os6_over=%q: n=%d problems=%v (want n=%d problem=%v)", c.in, n, p, c.want, c.problem)
		}
		if got := workOS6Over(); got != c.want {
			t.Errorf("workOS6Over(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestLoadConfNewhostKeys(t *testing.T) {
	saveConfVars(t)
	p := writeConf(t, "newhost_template_1=A|sec bond sda 500 8.10 uefi\nnewhost_template_9 = mec eth0 nvme 400 9.6 uefi\nwork_os6_over=1500\nnewhost_template_2=\n")
	loadConf([]string{p})
	cases := map[string]string{
		"newhost_template_1": "A|sec bond sda 500 8.10 uefi", "newhost_template_2": "",
		"newhost_template_9": "mec eth0 nvme 400 9.6 uefi", "work_os6_over": "1500",
	}
	for k, want := range cases {
		if got := *confVars()[k]; got != want {
			t.Errorf("%s = %q want %q", k, got, want)
		}
	}
	valid, problems := parseNewhostTemplates()
	if len(problems) != 0 || len(valid) != 2 || !reflect.DeepEqual([]int{valid[0].No, valid[1].No}, []int{1, 9}) {
		t.Errorf("valid=%+v problems=%v", valid, problems)
	}
	if workOS6Over() != 1500 {
		t.Errorf("workOS6Over=%d", workOS6Over())
	}
}
