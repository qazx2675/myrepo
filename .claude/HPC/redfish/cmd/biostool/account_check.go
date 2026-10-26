package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// account_check.go 는 점검(biostool check, bios.sh 의 bios) 끝에서 "1순위 계정이 없어 2순위로 접속한 호스트"에
// 계정 생성을 제안합니다. bios.conf 에 user2 가 있으면 같은 호스트에 두 번째 계정도 함께 만듭니다 (이미 있으면 건너뜀).
// 기본 응답은 Y 입니다 (빈 입력도 생성). 터미널이 아니면 -stdin-ok 없이는 묻지 않고, -no-prompt 면 안내만 합니다.
// 생성·검증·롤백은 biostool account create 와 같은 코드(createHosts)를 씁니다.
//
// 결과 폴더에 대상 목록을 따로 남깁니다 (호스트 이름·IP 만, 0600):
//   - account_targets.txt   생성 대상 (2순위 계정으로만 로그인된 호스트)
//   - account_unfixable.txt 생성 불가 (1순위 인증 실패 + rf.conf 계정도 없거나 거부 → AUTH_FAIL)
//
// 생성한 뒤에는 1순위 계정을 만든(CREATED) 호스트만 1순위 계정으로 다시 점검하고(결과 폴더/recheck/<일시>/),
// 원래 결과와 합쳐 보여 줍니다. dry-run 이면 BMC 에 쓰지 않으므로 다시 점검하지 않습니다.

// 대상 목록 파일 이름 (결과 폴더 안).
const (
	acctTargetsFile   = "account_targets.txt"
	acctUnfixableFile = "account_unfixable.txt"
)

// offerAccountCreate 는 check 결과에서 2순위(rf.conf)로 로그인한 호스트를 모아 계정 생성을 제안합니다.
// 대상이 없거나 덤프 읽기면 아무것도 하지 않습니다 (생성 불가 호스트가 있으면 그 목록만 남김).
func offerAccountCreate(rc *runContext, run *checkRun, dryRun, noPrompt, stdinOK bool, in *os.File, w io.Writer, o reportOpts) error {
	if run == nil || run.FromDump != "" {
		return nil
	}
	var fb, unfix []*hostCheck
	for _, h := range run.Hosts {
		switch {
		case h == nil || h.Target.XML:
		case h.Status == "" && h.CredSource == credFallback:
			fb = append(fb, h)
		case h.Status == StatusAuthFail:
			unfix = append(unfix, h)
		}
	}
	p := painter{o.Color}
	if len(unfix) > 0 {
		path, err := writeAcctHostList(run.Dir, acctUnfixableFile, unfix)
		if err != nil {
			fmt.Fprintln(w, "경고: "+acctUnfixableFile+" 기록 실패: "+err.Error())
		}
		fmt.Fprintf(w, "\n%s\n", p.yellow(fmt.Sprintf("※ 계정을 만들 수 없는 호스트 %d대 — 1순위 계정(%s) 인증 실패, 2순위(rf.conf) 계정도 없거나 거부됨 → %s", len(unfix), rc.Conf.User, filepath.ToSlash(path))))
		for i, h := range unfix {
			if i >= 10 {
				fmt.Fprintf(w, "  ... 외 %d대\n", len(unfix)-i)
				break
			}
			fmt.Fprintf(w, "  %-28s %s\n", sanitizeDetail(h.Target.Hostname), orDefault(sanitizeDetail(h.Detail), "-"))
		}
	}
	if len(fb) == 0 || rc.Fallback == nil {
		return nil
	}
	fmt.Fprintf(w, "\n%s\n", p.cyan(fmt.Sprintf("※ 1순위 계정(%s)으로 로그인되지 않아 2순위(rf.conf) 계정으로 점검한 호스트 %d대 — 1순위 계정을 만들 수 있습니다.", rc.Conf.User, len(fb))))
	if path, err := writeAcctHostList(run.Dir, acctTargetsFile, fb); err != nil {
		fmt.Fprintln(w, "경고: "+acctTargetsFile+" 기록 실패: "+err.Error())
	} else {
		fmt.Fprintf(w, "생성 대상 목록: %s\n", filepath.ToSlash(path))
	}
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
		fmt.Fprintln(w, "dry-run: 아래는 만들 계획입니다 (BMC 에 쓰지 않음, 다시 점검하지 않음).")
	case noPrompt:
		fmt.Fprintln(w, "-no-prompt 이므로 묻지 않습니다. 만들려면: bash bios.sh account (또는 bash account_create.sh)")
		return nil
	default:
		fmt.Fprintln(w, "만든 뒤에는 1순위 계정을 만든 호스트만 1순위 계정으로 다시 점검해 결과를 합쳐 보여 줍니다.")
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
	if !dryRun {
		if rerr := recheckCreated(rc, run, need, w, o.Color); rerr != nil {
			fmt.Fprintln(w, "경고: 재점검 결과 기록 중 오류: "+rerr.Error())
		}
	}
	return err
}

// writeAcctHostList 는 대상 목록 파일을 씁니다 (0600). 한 줄에 "입력값<TAB># 호스트이름 IP" — 첫 열은 user.txt
// 원문이라 그대로 대상 목록(-user)으로 다시 쓸 수 있습니다 (# 뒤는 주석). 비밀번호·계정 정보는 없습니다.
func writeAcctHostList(dir, name string, hs []*hostCheck) (string, error) {
	lines := []string{"# 대상(user.txt 원문)\t# 호스트이름 IP"}
	for _, h := range hs {
		t := h.Target
		lines = append(lines, fmt.Sprintf("%s\t# %s %s", sanitizeDetail(t.Input), orDefault(sanitizeDetail(t.Hostname), "-"), orDefault(sanitizeDetail(t.IP), "-")))
	}
	p := filepath.Join(dir, name)
	return p, writeLines(p, lines)
}

// recheckCreated 는 1순위 계정을 만든(CREATED) 호스트만 1순위 계정으로 다시 점검하고, 원래 결과에서 그 호스트를
// 재점검 결과로 바꿔 합친 결과를 보여 줍니다. 만들지 못한 호스트(SKIP_EXISTS·REJECTED 등)는 다시 접속하지 않습니다
// (1순위 인증 실패 기록을 늘리지 않기 위함). 재점검 결과는 결과 폴더/recheck/<일시>/ 에 check 와 같은 파일 +
// 합친 결과 CSV(profile_merged_*) 로 남깁니다. 쓰기(PATCH/POST)는 없습니다 (check 와 같은 읽기 전용).
func recheckCreated(rc *runContext, run *checkRun, need [][]*acctHost, w io.Writer, color bool) error {
	var ts []Target
	for _, g := range need {
		if len(g) > 0 && g[0].Status == acctCreated { // 첫 칸이 1순위 계정 (newCreateHosts)
			ts = append(ts, g[0].Target)
		}
	}
	if len(ts) == 0 || run.prof == nil {
		return nil
	}
	fmt.Fprintf(w, "\n1순위 계정(%s)을 만든 호스트 %d대를 다시 점검합니다 ...\n", rc.Conf.User, len(ts))
	sub := *rc
	sub.Targets = ts
	re, err := doCheck(&sub, checkOpts{Profile: run.prof, ResultDir: filepath.Join(run.Dir, "recheck")})
	if re == nil {
		return err
	}
	byInput := map[string]*hostCheck{}
	for _, h := range re.Hosts {
		if h != nil {
			byInput[h.Target.Input] = h
		}
	}
	merged := make([]*hostCheck, len(run.Hosts))
	cnt := map[string]int{}
	for i, h := range run.Hosts {
		merged[i] = h
		if h == nil {
			continue
		}
		if r := byInput[h.Target.Input]; r != nil {
			merged[i] = r
		}
		cnt[hostProgTag(merged[i])]++
	}
	fmt.Fprintf(w, "\n== 재점검 (1순위 계정, %d대) ==\n", len(ts))
	writeResultLines(w, profileLines(re.Hosts), color)
	fmt.Fprintf(w, "\n합친 결과 (전체 %d대, 재점검 %d대 반영): %s %d대 / %s %d대 / %s %d대 / %s %d대\n",
		len(merged), len(ts), TagOK, cnt[TagOK], TagDiff, cnt[TagDiff], TagNA, cnt[TagNA], progTagErr, cnt[progTagErr])
	paths, cerr := writeResultCSVs(re.Dir, "profile_merged_", profileLines(merged))
	fmt.Fprintf(w, "재점검 결과 폴더: %s/ (result.tsv, 합친 결과 CSV %d개)\n", filepath.ToSlash(re.Dir), len(paths))
	if err == nil {
		err = cerr
	}
	return err
}
