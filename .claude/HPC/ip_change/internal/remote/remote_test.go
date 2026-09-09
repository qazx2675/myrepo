package remote

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSpaceOutHasNoLongRun(t *testing.T) {
	// base64 로 감싼 실제 스크립트를 여러 개 넣어 우연히 위험어(3글자 이상)가
	// 이어지지 않는지 확인합니다.
	scripts := []string{
		strings.Repeat("echo reboot halt ddc test\n", 50),
		"짧은 스크립트",
		"",
	}
	for _, s := range scripts {
		spaced := spaceOut(s)
		run := 0
		for _, r := range spaced {
			if r == ' ' {
				run = 0
				continue
			}
			run++
			if run >= 3 {
				t.Fatalf("spaceOut(%q) 결과에 3글자 이상 이어진 조각이 있습니다: %q", s, spaced)
			}
		}
	}
}

// 실제 스크립트 내용(사용자 데이터가 될 수 있는 부분)은 base64 로 통째로
// 감싸므로 따옴표가 하나도 남지 않아야 합니다. 바깥쪽 "tr -d ' '" 의 작은따옴표는
// 정적 템플릿의 일부(ldap_setting 과 동일)라 검사 대상에서 제외합니다.
func TestBuildCommandHasNoQuotesInPayload(t *testing.T) {
	cmd := BuildCommand("#!/bin/bash\necho \"hello 'world'\"\n", Options{})
	before, after, ok := strings.Cut(cmd, "echo ")
	if !ok || before != "" {
		t.Fatalf("예상한 형태가 아닙니다: %q", cmd)
	}
	payload, _, ok := strings.Cut(after, " | tr -d")
	if !ok {
		t.Fatalf("예상한 형태가 아닙니다: %q", cmd)
	}
	if strings.ContainsAny(payload, `'"`) {
		t.Fatalf("base64 payload 에 따옴표가 남아 있습니다: %q", payload)
	}
}

// bash 가 있으면 실제로 디코딩까지 왕복시켜 원본 스크립트가 그대로 복원되는지 확인합니다.
func TestBuildCommandRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}
	script := "#!/bin/bash\necho 'RESULT|host1|OK'\n"
	cmdline := BuildCommand(script, Options{RemotePath: "/tmp/ip_change_test_apply.sh"})

	out, err := exec.Command("bash", "-c", cmdline).CombinedOutput()
	if err != nil {
		t.Fatalf("round-trip 실행 실패: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "RESULT|host1|OK") {
		t.Fatalf("복원된 스크립트 출력이 예상과 다릅니다: %q", out)
	}
	if _, err := os.Stat("/tmp/ip_change_test_apply.sh"); err == nil {
		t.Fatalf("실행 후 임시 스크립트가 지워지지 않았습니다")
	}
}

func TestParse(t *testing.T) {
	raw := "host1: line one\nhost1: line two\nhost2: only line\nSome banner without colon-space\n"
	results := parse(raw)
	if len(results) != 2 {
		t.Fatalf("want 2 hosts, got %d", len(results))
	}
	if results[0].Host != "host1" || len(results[0].Lines) != 2 {
		t.Fatalf("unexpected host1 result: %+v", results[0])
	}
	if results[1].Host != "host2" || results[1].LastLine() != "only line" {
		t.Fatalf("unexpected host2 result: %+v", results[1])
	}
}
