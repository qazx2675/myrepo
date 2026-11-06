package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"account-mount/internal/info"
)

func TestApplyScriptGeneration(t *testing.T) {
	accts := []info.Account{
		{User: "asdf", ID: "1234", Storage: "qwer12345:/asdf/asdf"},
	}
	script, err := ApplyScript(accts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(script, accountsPlaceholder) {
		t.Errorf("placeholder was not replaced")
	}
	if !strings.Contains(script, "asdf 1234 qwer12345:/asdf/asdf") {
		t.Errorf("account data missing in script:\n%s", script)
	}
}

func TestApplyAndRollbackEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 가 없는 환경이라 건너뜁니다")
	}

	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 초기 파일 준비
	autoMaster := filepath.Join(etcDir, "auto.master")
	passwd := filepath.Join(etcDir, "passwd")
	group := filepath.Join(etcDir, "group")
	autoHome := filepath.Join(etcDir, "auto.home")

	if err := os.WriteFile(autoMaster, []byte("/misc\t/etc/auto.misc\n+auto.master\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwd, []byte("root:x:0:0:root:/root:/bin/bash\nexisting:x:999:999:old:/home/existing:/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte("root:x:0:\nexisting:x:999:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	accts := []info.Account{
		{User: "asdf", ID: "1234", Storage: "qwer12345:/asdf/asdf"},
		{User: "existing", ID: "2000", Storage: "nas02:/vol/users"},
	}

	script, err := ApplyScript(accts)
	if err != nil {
		t.Fatal(err)
	}
	scriptFile := filepath.Join(root, "apply.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// 1차 실행 (실제 반영)
	cmd := exec.Command("bash", scriptFile)
	cmd.Env = append(os.Environ(), "ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("1st apply failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "|OK|accounts=2|") {
		t.Fatalf("unexpected 1st apply output:\n%s", out)
	}

	// 파일 내용 검증
	amBytes, _ := os.ReadFile(autoMaster)
	if !strings.HasSuffix(string(amBytes), "/user /etc/auto.home\n") {
		t.Errorf("auto.master should end with '/user /etc/auto.home':\n%s", amBytes)
	}

	pwBytes, _ := os.ReadFile(passwd)
	if !strings.Contains(string(pwBytes), "asdf:x:1234:1234:rasdf:/user/asdf:/bin/csh\n") {
		t.Errorf("passwd missing asdf:\n%s", pwBytes)
	}
	if !strings.Contains(string(pwBytes), "existing:x:2000:2000:rexisting:/user/existing:/bin/csh\n") {
		t.Errorf("passwd existing not updated:\n%s", pwBytes)
	}
	if strings.Contains(string(pwBytes), "existing:x:999:999") {
		t.Errorf("passwd old existing entry still remains:\n%s", pwBytes)
	}

	grBytes, _ := os.ReadFile(group)
	if !strings.Contains(string(grBytes), "asdf:x:1234:\n") || !strings.Contains(string(grBytes), "existing:x:2000:\n") {
		t.Errorf("group entries invalid:\n%s", grBytes)
	}

	ahBytes, _ := os.ReadFile(autoHome)
	if !strings.Contains(string(ahBytes), "asdf -rw,hard,actimeo=1,proto=tcp,vers=3 qwer12345:/asdf/asdf/asdf\n") {
		t.Errorf("auto.home missing asdf:\n%s", ahBytes)
	}
	if !strings.Contains(string(ahBytes), "existing -rw,hard,actimeo=1,proto=tcp,vers=3 nas02:/vol/users/existing\n") {
		t.Errorf("auto.home missing existing:\n%s", ahBytes)
	}

	// 2차 실행 (멱등성: NOCHANGE 및 auto.master 공백 무관 중복 제외 검증)
	// auto.master 의 /user 줄을 탭과 공백이 섞인 형태로 바꿔도 중복 추가되지 않아야 함
	if err := os.WriteFile(autoMaster, []byte("/misc\t/etc/auto.misc\n  /user \t\t /etc/auto.home   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd2 := exec.Command("bash", scriptFile)
	cmd2.Env = append(os.Environ(), "ROOT="+root)
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("2nd apply failed: %v\n%s", err, out2)
	}
	if !strings.Contains(string(out2), "|NOCHANGE|accounts=2") {
		t.Fatalf("expected NOCHANGE on 2nd apply, got:\n%s", out2)
	}
	amBytes2, _ := os.ReadFile(autoMaster)
	if strings.Count(string(amBytes2), "/etc/auto.home") != 1 {
		t.Errorf("auto.master should not duplicate /user /etc/auto.home:\n%s", amBytes2)
	}

	// 롤백 실행 검증
	rbScript, err := RollbackScript(RollbackLatest, "")
	if err != nil {
		t.Fatal(err)
	}
	rbFile := filepath.Join(root, "rollback.sh")
	if err := os.WriteFile(rbFile, []byte(rbScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cmdRb := exec.Command("bash", rbFile)
	cmdRb.Env = append(os.Environ(), "ROOT="+root)
	outRb, err := cmdRb.CombinedOutput()
	if err != nil {
		t.Fatalf("rollback failed: %v\n%s", err, outRb)
	}
	if !strings.Contains(string(outRb), "|OK|") {
		t.Fatalf("unexpected rollback output:\n%s", outRb)
	}
	pwRestored, _ := os.ReadFile(passwd)
	if !strings.Contains(string(pwRestored), "existing:x:999:999:old:/home/existing:/bin/bash\n") || strings.Contains(string(pwRestored), "asdf:") {
		t.Errorf("passwd not restored properly:\n%s", pwRestored)
	}
}
