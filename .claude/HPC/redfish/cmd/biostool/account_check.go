package main

import (
	"fmt"
	"io"
	"os"
)

// account_check.go 는 점검(biostool check, bios.sh 의 bios) 끝에서 "1순위 계정이 없어 2순위로 접속한 호스트"에
// 계정 생성을 제안합니다. bios.conf 에 user2 가 있으면 같은 호스트에 두 번째 계정도 함께 만듭니다 (이미 있으면 건너뜀).
// 기본 응답은 Y 입니다 (빈 입력도 생성). 터미널이 아니면 -stdin-ok 없이는 묻지 않고, -no-prompt 면 안내만 합니다.
// 생성·검증·롤백은 biostool account create 와 같은 코드(createHosts)를 씁니다.

// offerAccountCreate 는 check 결과에서 2순위(rf.conf)로 로그인한 호스트를 모아 계정 생성을 제안합니다.
// 대상이 없거나 덤프 읽기·rf.conf 없음이면 아무것도 하지 않습니다.
func offerAccountCreate(rc *runContext, run *checkRun, dryRun, noPrompt, stdinOK bool, in *os.File, w io.Writer, o reportOpts) error {
	if run == nil || run.FromDump != "" || rc.Fallback == nil {
		return nil
	}
	var fb []*hostCheck
	for _, h := range run.Hosts {
		if h != nil && h.Status == "" && h.CredSource == credFallback {
			fb = append(fb, h)
		}
	}
	if len(fb) == 0 {
		return nil
	}
	p := painter{o.Color}
	fmt.Fprintf(w, "\n%s\n", p.cyan(fmt.Sprintf("※ 1순위 계정(%s)으로 로그인되지 않아 2순위(rf.conf) 계정으로 점검한 호스트 %d대 — 1순위 계정을 만들 수 있습니다.", rc.Conf.User, len(fb))))
	env, err := newAcctEnv(rc, dryRun)
	if err != nil {
		fmt.Fprintln(w, "계정을 만들지 않습니다: "+err.Error())
		return nil
	}
	specs := env.accounts()
	need := make([][]*acctHost, 0, len(fb))
	for _, h := range fb {
		g := newCreateHosts(h.Target, specs)
		for _, ah := range g {
			ah.Source, ah.Status = credFallback, acctNeedCreate
			if h.Info != nil {
				ah.Vendor, ah.Model = h.Info.Vendor, h.Info.Model
			}
		}
		need = append(need, g)
	}
	what := createWhat(need)
	if len(specs) > 1 {
		fmt.Fprintf(w, "두 번째 계정 %s(%s)도 같은 호스트에 함께 만듭니다 (이미 있으면 건너뜀).\n", specs[1].User, specs[1].Role)
	}
	firsts := make([]*acctHost, len(need))
	for i, g := range need {
		firsts[i] = g[0]
	}
	printAcctTargets(w, firsts, 10)

	af := &accountFlags{noPrompt: noPrompt, stdinOK: stdinOK, listMax: 20}
	switch {
	case dryRun:
		fmt.Fprintln(w, "dry-run: 아래는 만들 계획입니다 (BMC 에 쓰지 않음).")
	case noPrompt:
		fmt.Fprintln(w, "-no-prompt 이므로 묻지 않습니다. 만들려면: bash bios.sh account (또는 bash account_create.sh)")
		return nil
	default:
		ok, err := confirmCreate(in, w, what, af)
		if err != nil || !ok {
			fmt.Fprintln(w, "계정을 만들지 않습니다. 나중에: bash bios.sh account (또는 bash account_create.sh)")
			return err
		}
	}
	createHosts(env, need, acctWorkers(0, rc.Conf.Concurrency))
	fmt.Fprintln(w)
	all := flattenAcct(need)
	writeAcctReport(w, all, 20, p)
	path, err := writeAcctResult(run.Dir, all)
	fmt.Fprintf(w, "\n계정 생성 결과 파일: %s\n", path)
	printAcctDisclaimer(w, "create", dryRun)
	return err
}
