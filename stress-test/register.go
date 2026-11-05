package main

// AWX 등록 (01 스크립트 방식): yaml → scp → custom_inventory.sh → svr_dir 새 yml 확인 → invsync

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ssh/scp 실행 파일 (테스트에서 가짜로 바꾼다)
var (
	sshBin = "ssh"
	scpBin = "scp"
)

// writeYAML 은 대상의 12필드를 한 줄씩 기록한다.
func writeYAML(path string, list []*target) error {
	var b strings.Builder
	for _, t := range list {
		b.WriteString(strings.Join(t.Fields, " ") + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// listYML 은 dir 안의 *.yml 파일 이름 목록(정렬)을 돌려준다.
func listYML(dir string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, "*.yml"))
	out := make([]string, len(m))
	for i, p := range m {
		out[i] = filepath.Base(p)
	}
	sort.Strings(out)
	return out
}

// stampYML 은 dir 안의 *.yml 수정 시각(ns)이다. 같은 이름으로 덮어쓴 yml 도 새로 만든 것으로 보기 위해 쓴다.
func stampYML(dir string) map[string]int64 {
	res := map[string]int64{}
	for _, n := range listYML(dir) {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil {
			res[n] = fi.ModTime().UnixNano()
		}
	}
	return res
}

// changedNames 는 before 에 없거나 수정 시각이 달라진 이름을 정렬해서 돌려준다.
func changedNames(before, after map[string]int64) []string {
	var out []string
	for n, t := range after {
		if bt, ok := before[n]; !ok || bt != t {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

var eaRe = regexp.MustCompile(`_([0-9]+)ea\.yml$`)

// eaMismatch 는 yml 이름의 _<N>ea 대수가 입력 줄 수와 다르면 N 과 true 를 돌려준다.
func eaMismatch(name string, lines int) (int, bool) {
	m := eaRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, n != lines
}

func runCmd(name string, args ...string) error {
	c := exec.Command(name, args...)
	out, err := c.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%v: %s", err, msg)
		}
		return err
	}
	return nil
}

// register 는 yaml 을 만들어 repohost 에 올리고 custom_inventory.sh 로 yml 을 만든 뒤 invsync 한다.
// 성공하면 새로 생긴 yml 이름을 돌려준다.
func (a *app) register(yamlName string, list []*target) (string, error) {
	cfg := a.cfg
	local := filepath.Join(a.workDir, yamlName)
	if err := writeYAML(local, list); err != nil {
		return "", fmt.Errorf("yaml 생성 실패: %w", err)
	}
	a.logf("%s yaml 생성: %s (%d대)", markInfo, yamlName, len(list))
	repoDir := strings.TrimRight(cfg.RepoDir, "/")
	if err := runCmd(scpBin, "-o", "BatchMode=yes", local, cfg.Repohost+":"+repoDir+"/"); err != nil {
		return "", fmt.Errorf("scp 실패: %v", err)
	}
	before := stampYML(cfg.SvrDir)
	remote := fmt.Sprintf("bash %s/custom_inventory.sh %s/%s", repoDir, repoDir, yamlName)
	if err := runCmd(sshBin, "-o", "BatchMode=yes", cfg.Repohost, remote); err != nil {
		return "", fmt.Errorf("custom_inventory.sh 실패: %v", err)
	}
	created := changedNames(before, stampYML(cfg.SvrDir))
	if len(created) != 1 {
		return "", fmt.Errorf("%s 에 새로 생긴 yml 이 %d개입니다 (1개여야 함): %s", cfg.SvrDir, len(created), strings.Join(created, " "))
	}
	yml := created[0]
	if n, bad := eaMismatch(yml, len(list)); bad {
		a.logf("%s 경고: %s 의 대수(%dea)가 입력 대수(%d)와 다릅니다", markWarn, yml, n, len(list))
	}
	a.logf("%s yml 생성: %s", markInfo, yml)
	if cfg.GitDir != "" {
		// 01 의 git_upload: svr_dir 의 yml 을 git_dir 로 복사하고 그 안에서 .git_upload.sh <user> <yml> 실행
		gitDir := strings.ReplaceAll(cfg.GitDir, "${user}", a.user)
		data, err := os.ReadFile(filepath.Join(cfg.SvrDir, yml))
		if err == nil {
			err = os.WriteFile(filepath.Join(gitDir, yml), data, 0644)
		}
		if err != nil {
			return "", fmt.Errorf("git_upload 복사 실패: %v", err)
		}
		c := exec.Command("bash", ".git_upload.sh", a.user, yml)
		c.Dir = gitDir
		if out, err := c.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git_upload 실패: %v: %s", err, strings.TrimSpace(string(out)))
		}
		a.logf("%s git 업로드 완료: %s", markInfo, yml)
	}
	if err := a.client.invsync(cfg, yml, a.pollInterval(), a.logf); err != nil {
		return "", fmt.Errorf("invsync 실패: %v", err)
	}
	a.logf("%s 인벤토리 동기화 완료: %s", markOK, yml)
	return yml, nil
}
