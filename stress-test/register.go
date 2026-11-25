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
	"time"
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

// inventoryDeleteCmd 는 인벤토리 삭제 때 원격에서 실행할 명령이다 (AWX 01 스크립트의 inventory_delete 와 같다).
func inventoryDeleteCmd(user string) string {
	return "bash /root/server/delhost_" + user
}

// 삭제 확인의 대기 시간. AWX 가 호스트를 뒤에서 지우는 동안(수백 대면 몇 분) 삭제 전 호스트가 줄어드는 한 기다린다.
const (
	deleteStallWait = 60 * time.Second // 남은 호스트(삭제 전에 있던 것)가 이 시간 동안 줄지 않으면 판정(중지)
	deleteMaxWait   = 10 * time.Minute // 줄어드는 중이어도 이 시간을 넘으면 중지
)

// 등록·동기화 뒤 완전일치 확인이 어긋나면 poll_interval 간격으로 다시 조회하는 횟수(반영이 늦은 경우).
const matchRetries = 6

// inventoryHostSet 은 인벤토리 호스트 목록에서 exclude 를 빼고 소문자 키 → 원래 이름 맵으로 만든다.
func inventoryHostSet(list, exclude []string) map[string]string {
	ex := map[string]bool{}
	for _, h := range exclude {
		ex[strings.ToLower(strings.TrimSpace(h))] = true
	}
	m := map[string]string{}
	for _, h := range list {
		if k := strings.ToLower(strings.TrimSpace(h)); k != "" && !ex[k] {
			m[k] = h
		}
	}
	return m
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, h := range m {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// readInventoryHosts 는 등록(register)·yamlState 와 같은 인벤토리의 호스트 목록을 읽는다.
func (a *app) readInventoryHosts() ([]string, error) {
	st, err := a.client.yamlState(a.cfg)
	if err != nil {
		return nil, err
	}
	return st.Hosts, nil
}

// inventoryDelete 는 conf 의 inventory_delete_host 에서 삭제 스크립트를 실행하고, 인벤토리가 실제로 비었는지 확인한다.
// 값이 비면 아무것도 하지 않는다(조회도 안 함). 실행 한 번에 한 번, 첫 yaml 등록 전에만 부른다.
// 확인은 삭제와 첫 등록 사이에서만 한다 (등록 뒤에는 같은 호스트가 다시 들어와 삭제 전과 같을 수 있으므로 register 는 쓰지 않는다).
func (a *app) inventoryDelete() error {
	host := strings.TrimSpace(a.cfg.InventoryDeleteHost)
	if host == "" {
		return nil
	}
	unverified := func(when string, err error) error {
		appendHistory(a.cfg, fmt.Sprintf("user=%s action=inventory_delete_check status=unverified error=%q", a.user, when+": "+err.Error()))
		return fmt.Errorf("인벤토리 삭제 확인 불가 (%s 호스트 조회 실패): %v", when, err)
	}
	beforeRaw, err := a.readInventoryHosts()
	if err != nil {
		return unverified("삭제 전", err)
	}
	before := inventoryHostSet(beforeRaw, a.cfg.InventoryMatchExclude)

	remote := inventoryDeleteCmd(a.user)
	a.logf("%s 인벤토리 삭제: ssh %s \"%s\"", markInfo, host, remote)
	if err := runCmd(sshBin, "-o", "BatchMode=yes", host, remote); err != nil {
		appendHistory(a.cfg, fmt.Sprintf("user=%s action=inventory_delete host=%s status=fail error=%q", a.user, host, err.Error()))
		return fmt.Errorf("inventory_delete 실패 (ssh %s \"%s\"): %v", host, remote, err)
	}
	appendHistory(a.cfg, fmt.Sprintf("user=%s action=inventory_delete host=%s status=ok", a.user, host))
	a.logf("%s 인벤토리 삭제 완료", markOK)

	pass := func(after int) {
		appendHistory(a.cfg, fmt.Sprintf("user=%s action=inventory_delete_check status=ok before=%d after=%d", a.user, len(before), after))
		a.logf("%s 인벤토리 삭제 확인: 삭제 전 %d대 → 삭제 뒤 %d대", markOK, len(before), after)
		a.emitDeleteCheck(len(before), after)
	}
	if len(before) == 0 {
		pass(0) // 지울 것이 없었다
		return nil
	}
	interval := a.pollInterval()
	if interval <= 0 {
		interval = time.Second
	}
	// 삭제 전에 있던 호스트가 모두 사라질 때까지 기다린다. 줄어드는 동안은 deleteMaxWait 까지, 줄지 않으면 deleteStallWait 뒤 중지.
	// 조회 오류(삭제 중 AWX 부하 등)는 기한 안에서는 다시 조회하고, 기한까지 계속 실패하면 확인 불가로 중지한다.
	start := nowFn()
	lastProgress, prevLeft := start, len(before)
	var left map[string]string
	var lastErr error
	for {
		afterRaw, err := a.readInventoryHosts()
		lastErr = err
		if err != nil {
			a.logf("%s 인벤토리 삭제 확인 조회 실패(다시 조회합니다): %v", markWarn, err)
		} else {
			after := inventoryHostSet(afterRaw, a.cfg.InventoryMatchExclude)
			left = map[string]string{}
			for k, h := range after {
				if _, ok := before[k]; ok {
					left[k] = h
				}
			}
			if len(left) == 0 {
				if len(after) > 0 { // 삭제 전에 없던 호스트(다른 사람이 넣은 것 등)는 등록 뒤 완전일치 확인이 거른다
					a.logf("%s 삭제 전에 없던 호스트 %d대가 인벤토리에 있습니다: %s — 등록 뒤 완전일치 확인에서 다시 걸러집니다", markWarn, len(after), strings.Join(sortedNames(after), " "))
				}
				pass(len(after))
				return nil
			}
			if len(left) < prevLeft {
				prevLeft, lastProgress = len(left), nowFn()
				a.logf("%s 인벤토리 삭제 진행 중: 삭제 전 %d대 중 %d대 남음", markInfo, len(before), len(left))
			}
		}
		now := nowFn()
		stalled, overdue := now.Sub(lastProgress) >= deleteStallWait, now.Sub(start) >= deleteMaxWait
		if !stalled && !overdue {
			sleepFn(interval)
			continue
		}
		if lastErr != nil {
			return unverified("삭제 뒤", lastErr)
		}
		names := strings.Join(sortedNames(left), " ")
		appendHistory(a.cfg, fmt.Sprintf("user=%s action=inventory_delete_check status=fail before=%d after=%d hosts=%q", a.user, len(before), len(left), names))
		why := fmt.Sprintf("%d초 동안 더 줄지 않았습니다", int(deleteStallWait/time.Second))
		if !stalled {
			why = fmt.Sprintf("최대 대기 %d분이 지났습니다", int(deleteMaxWait/time.Minute))
		}
		if len(left) == len(before) {
			return fmt.Errorf("인벤토리가 삭제되지 않았습니다 — 삭제 스크립트(ssh %s \"%s\")는 끝났지만 삭제 전과 같은 %d대가 남아 있습니다(%s): %s",
				host, remote, len(left), why, names)
		}
		return fmt.Errorf("인벤토리 삭제가 끝나지 않았습니다 — 삭제 전 %d대 중 %d대가 남아 있습니다(%s): %s", len(before), len(left), why, names)
	}
}

// needRegister 는 등록(launch)할 대상이 하나라도 있는지 알려준다.
func (a *app) needRegister() bool {
	for _, t := range a.targets {
		for _, r := range t.Res {
			if r == resNotRun {
				return true
			}
		}
	}
	return false
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
	// 안전장치: 인벤토리 호스트가 이번 묶음 대상과 완전히 같아야 launch 한다 (inventory_match_exclude 호스트는 비교에서 뺌).
	// 반영이 늦을 수 있으므로(삭제·동기화 직후) 어긋나면 poll_interval 간격으로 matchRetries 번 다시 조회한 뒤 판정한다.
	want := make([]string, len(list))
	for i, t := range list {
		want[i] = t.Host
	}
	interval := a.pollInterval()
	if interval <= 0 {
		interval = time.Second
	}
	var seen, extra, missing []string
	var err error
	for try := 0; ; try++ {
		var st yamlState
		if st, err = a.client.yamlState(cfg); err == nil {
			seen = st.Hosts
			if seen == nil {
				seen = []string{}
			}
			if extra, missing = inventoryDiff(seen, want, cfg.InventoryMatchExclude); len(extra)+len(missing) == 0 {
				break
			}
		}
		if try == matchRetries {
			break
		}
		if err != nil {
			a.logf("%s 등록 직후 인벤토리 호스트 조회 실패(다시 조회합니다): %v", markWarn, err)
		} else {
			a.logf("%s 인벤토리가 아직 대상과 다릅니다(인벤토리에만 있음 %d대, 인벤토리에 없음 %d대) — 다시 확인합니다", markWarn, len(extra), len(missing))
		}
		sleepFn(interval)
	}
	// new_setup -auto 가 읽는 줄: 방금 등록한 yml 과 그 직후의 인벤토리 호스트(launch 직전 변경 감시의 기준값).
	// 완전일치가 확인되지 않았으면 호스트는 싣지 않는다(지우는 중이던 옛 호스트 등이 기준값으로 남지 않게).
	if err != nil || len(extra)+len(missing) > 0 {
		a.logf("%s", registeredLine(yml, nil))
	} else {
		a.logf("%s", registeredLine(yml, seen))
	}
	if err != nil {
		return "", fmt.Errorf("인벤토리 완전일치 확인 불가 (호스트 조회 실패): %v", err)
	}
	if len(extra)+len(missing) > 0 {
		return "", fmt.Errorf("인벤토리가 대상과 완전히 일치하지 않습니다 — 인벤토리에만 있음 %d대 [%s], 인벤토리에 없음 %d대 [%s]",
			len(extra), strings.Join(extra, " "), len(missing), strings.Join(missing, " "))
	}
	_, onlyWant := inventoryDiff(nil, want, cfg.InventoryMatchExclude)
	a.logf("%s 인벤토리 완전일치 확인: %d대 (비교 제외 %d대)", markOK, len(onlyWant), len(want)-len(onlyWant))
	return yml, nil
}

// inventoryDiff 는 인벤토리 호스트(inv)와 대상(want)을 대소문자 무시로 비교해
// 인벤토리에만 있는 호스트와 인벤토리에 없는 대상을 돌려준다. exclude 호스트는 양쪽 모두에서 뺀다.
func inventoryDiff(inv, want, exclude []string) (extra, missing []string) {
	ex := map[string]bool{}
	for _, h := range exclude {
		ex[strings.ToLower(h)] = true
	}
	set := func(l []string) map[string]string {
		m := map[string]string{}
		for _, h := range l {
			if k := strings.ToLower(strings.TrimSpace(h)); k != "" && !ex[k] {
				m[k] = h
			}
		}
		return m
	}
	im, wm := set(inv), set(want)
	for k, h := range im {
		if _, ok := wm[k]; !ok {
			extra = append(extra, h)
		}
	}
	for k, h := range wm {
		if _, ok := im[k]; !ok {
			missing = append(missing, h)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return extra, missing
}
