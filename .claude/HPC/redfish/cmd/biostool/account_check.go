package main

import (
	"fmt"
	"io"
	"os"
)

// account_check.go 는 biostool check 끝에서 "1순위 계정이 없어 2순위로 접속한 호스트"에 계정 생성을 제안합니다.
// 기본 응답은 Y 입니다 (빈 입력도 생성). 터미널이 아니면 -stdin-ok 없이는 묻지 않고, -no-prompt 면 안내만 합니다.
// 생성·검증·롤백은 biostool account create 와 같은 코드(createHosts)를 씁니다.

// offerAccountCreate 는 check 결과에서 2순위(rf.conf)로 로그인한 호스트를 모아 계정 생성을 제안합니다.
// 대상이 없거나 덤프 읽기·rf.conf 없음이면 아무것도 하지 않습니다.
func offerAccountCreate(rc *runContext, run *checkRun, dryRun, noPrompt, stdinOK bool, in *os.File, w io.Writer, o reportOpts) error {
	if run.FromDump != "" || rc.Fallback == nil {
		return nil
	}
	var need []*acctHost
	for _, h := range run.Hosts {
		if h.Status != "" || h.CredSource != credFallback {
			continue
		}
		ah := newAcctHost(h.Target, "create")
		ah.Source, ah.Status = credFallback, acctNeedCreate
		if h.Info != nil {
			ah.Vendor, ah.Model = h.Info.Vendor, h.Info.Model
		}
		need = append(need, ah)
	}
	if len(need) == 0 {
		return nil
	}
	p := painter{o.Color}
	fmt.Fprintf(w, "\n%s\n", p.cyan(fmt.Sprintf("※ 1순위 계정(%s)으로 로그인되지 않아 2순위(rf.conf) 계정으로 점검한 호스트 %d대 — 1순위 계정을 만들 수 있습니다.", rc.Conf.User, len(need))))
	printAcctTargets(w, need, 10)

	af := &accountFlags{noPrompt: noPrompt, stdinOK: stdinOK, listMax: 20}
	switch {
	case dryRun:
		fmt.Fprintln(w, "dry-run: 아래는 만들 계획입니다 (BMC 에 쓰지 않음).")
	case noPrompt:
		fmt.Fprintln(w, "-no-prompt 이므로 묻지 않습니다. 만들려면: bash account_create.sh (또는 biostool account create)")
		return nil
	default:
		ok, err := confirmCreate(in, w, len(need), rc.Conf.User, af)
		if err != nil || !ok {
			fmt.Fprintln(w, "계정을 만들지 않습니다. 나중에: bash account_create.sh (또는 biostool account create)")
			return err
		}
	}
	env := &acctEnv{conf: rc.Conf, password: rc.Password, fb: rc.Fallback, dryRun: dryRun}
	createHosts(env, need, acctWorkers(0, rc.Conf.Concurrency))
	fmt.Fprintln(w)
	writeAcctReport(w, need, 20, p)
	path, err := writeAcctResult(run.Dir, need)
	fmt.Fprintf(w, "\n계정 생성 결과 파일: %s\n", path)
	printAcctDisclaimer(w, "create", dryRun)
	return err
}
