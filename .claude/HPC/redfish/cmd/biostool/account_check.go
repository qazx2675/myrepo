package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// account_check.go 는 점검(biostool check, bios.sh 의 bios) 끝에서 빠진 공용 계정을 만들자고 제안합니다 (Cisco XML 대상은 뺌).
//
//  1. 1순위 계정이 없어 2순위(rf.conf)로 접속한 호스트 → rf.conf 계정으로 1순위 계정을 만듭니다.
//     bios.conf 에 user2 가 있으면 같은 호스트에 두 번째 계정도 함께 만듭니다 (이미 있으면 건너뜀).
//  2. 1순위 계정으로 접속했지만 user2 가 없는 호스트 (bios.conf 에 user2 가 있을 때, 점검 중 같은 세션의 계정 목록 GET 으로 확인)
//     → 1순위 계정으로 로그인해 user2 만 만듭니다. 목록을 읽지 못한 호스트는 추측하지 않고 뺍니다.
//     user2 는 있지만 권한(RoleId)이 설정(user2_role_<vendor>, 없으면 user2_role)과 다른 호스트도 같은 방식으로 권한만 바꿉니다.
//  3. 1순위·2순위 모두 인증 실패(AUTH_FAIL) → 목록을 보여 주고, 운영자가 관리자 ID·비밀번호를 직접 입력하면 그 계정으로
//     호스트마다 1회만 로그인해 없는 계정만 만듭니다. 다른 벤더의 rf.conf 계정은 돌려가며 시험하지 않습니다 (계정 잠금 위험).
//     입력한 계정으로도 로그인되지 않은 호스트는 다시 보여 주고 다른 계정을 받습니다 (빈 입력이면 끝, 최대 manualMaxRounds 번).
//
// 1·2 는 확인 한 번으로 함께 만들고, 3 은 계정을 입력받은 뒤 따로 묻습니다. 기본 응답은 Y 입니다 (빈 입력도 생성).
// 터미널이 아니면 -stdin-ok 없이는 묻지 않고, -no-prompt 면 안내만 합니다. dry-run 이면 1·2 는 계획만 보여 주고 3 은 묻지 않습니다.
// 생성·검증·롤백은 biostool account create 와 같은 코드(createHosts)이고 로그인할 계정만 다릅니다 (acctEnv.admin).
// 직접 입력한 비밀번호는 메모리에서만 쓰며 화면·로그·결과 파일 어디에도 남기지 않습니다.
//
// 결과 폴더에 대상 목록을 따로 남깁니다 (호스트 이름·IP 만, 0600):
//   - account_targets.txt       생성 대상 (2순위 계정으로만 로그인된 호스트)
//   - account_user2_targets.txt user2 생성 대상 (1순위 계정으로 로그인, user2 없음)
//   - account_unfixable.txt     rf.conf 로는 생성 불가 (1순위 인증 실패 + rf.conf 계정도 없거나 거부 → AUTH_FAIL)
//
// 생성한 뒤에는 1순위 계정을 만든(CREATED) 호스트만 1순위 계정으로 다시 점검하고(결과 폴더/recheck/<일시>/),
// 원래 결과와 합쳐 보여 줍니다. dry-run 이면 BMC 에 쓰지 않으므로 다시 점검하지 않습니다.

// 대상 목록 파일 이름 (결과 폴더 안).
const (
	acctTargetsFile   = "account_targets.txt"
	acctUser2File     = "account_user2_targets.txt"
	acctUnfixableFile = "account_unfixable.txt"
)

// credManual 은 운영자가 직접 입력한 관리자 계정으로 로그인했다는 표시입니다 (acctHost.Source).
const credManual = "manual"

// manualMaxRounds 는 관리자 계정을 직접 입력받는 최대 횟수입니다. 대상 호스트는 점검에서 이미 1~2회 인증 실패했으므로
// 입력이 틀릴 때마다 호스트마다 실패가 1회씩 늘어 계정 잠금에 가까워집니다.
const manualMaxRounds = 3

// offerAccountCreate 는 check 결과에서 계정이 빠진 호스트를 모아 계정 생성을 제안합니다 (위 1·2·3).
// 대상이 없거나 덤프 읽기면 아무것도 하지 않습니다.
func offerAccountCreate(rc *runContext, run *checkRun, dryRun, noPrompt, stdinOK bool, in *os.File, w io.Writer, o reportOpts) error {
	if run == nil || run.FromDump != "" {
		return nil
	}
	var fb, u2, unfix []*hostCheck
	for _, h := range run.Hosts {
		switch {
		case h == nil || h.Target.XML:
		case h.Status == "" && h.CredSource == credFallback:
			if rc.Fallback != nil {
				fb = append(fb, h)
			}
		case h.Status == "" && h.CredSource == credPrimary && h.user2 == acctNeedCreate && rc.Conf.User2 != "":
			u2 = append(u2, h)
		case h.Status == "" && h.CredSource == credPrimary && h.user2 == acctPresent && rc.Conf.User2 != "" &&
			user2RoleDiffers(&acctMember{RoleID: h.user2Role}, rc.Conf.user2RoleFor(hostVendor(h))):
			u2 = append(u2, h) // user2 는 있지만 권한이 설정과 다름 → 권한만 바꿀 대상
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
		fmt.Fprintf(w, "\n%s\n", p.yellow(fmt.Sprintf("※ rf.conf 계정으로는 계정을 만들 수 없는 호스트 %d대 — 1순위 계정(%s) 인증 실패, 2순위(rf.conf) 계정도 없거나 거부됨 → %s", len(unfix), rc.Conf.User, filepath.ToSlash(path))))
		for i, h := range unfix {
			if i >= 10 {
				fmt.Fprintf(w, "  ... 외 %d대\n", len(unfix)-i)
				break
			}
			v := "-"
			if h.authVendor != "" {
				v = vendorLabel(h.authVendor)
			}
			fmt.Fprintf(w, "  %-28s %-15s %-10s %s\n", sanitizeDetail(h.Target.Hostname), orDefault(sanitizeDetail(h.Target.IP), "-"), v, orDefault(sanitizeDetail(h.Detail), "-"))
		}
	}
	if len(fb) > 0 {
		fmt.Fprintf(w, "\n%s\n", p.cyan(fmt.Sprintf("※ 1순위 계정(%s)으로 로그인되지 않아 2순위(rf.conf) 계정으로 점검한 호스트 %d대 — 1순위 계정을 만들 수 있습니다.", rc.Conf.User, len(fb))))
		if path, err := writeAcctHostList(run.Dir, acctTargetsFile, fb); err != nil {
			fmt.Fprintln(w, "경고: "+acctTargetsFile+" 기록 실패: "+err.Error())
		} else {
			fmt.Fprintf(w, "생성 대상 목록: %s\n", filepath.ToSlash(path))
		}
	}
	if len(u2) > 0 {
		miss := 0
		for _, h := range u2 {
			if h.user2 == acctNeedCreate {
				miss++
			}
		}
		if miss > 0 {
			fmt.Fprintf(w, "\n%s\n", p.cyan(fmt.Sprintf("※ 1순위 계정(%s)으로 점검했지만 두 번째 계정 %s 가 없는 호스트 %d대 — 1순위 계정으로 로그인해 %s 만 만들 수 있습니다.", rc.Conf.User, rc.Conf.User2, miss, rc.Conf.User2)))
		}
		if len(u2) > miss {
			fmt.Fprintf(w, "\n%s\n", p.cyan(fmt.Sprintf("※ 두 번째 계정 %s 의 권한이 설정(%s)과 다른 호스트 %d대 — 1순위 계정으로 로그인해 %s 의 권한(RoleId)만 바꿀 수 있습니다.", rc.Conf.User2, rc.Conf.user2RolesText(), len(u2)-miss, rc.Conf.User2)))
		}
		if path, err := writeAcctHostList(run.Dir, acctUser2File, u2); err != nil {
			fmt.Fprintln(w, "경고: "+acctUser2File+" 기록 실패: "+err.Error())
		} else {
			fmt.Fprintf(w, "user2 생성 대상 목록: %s\n", filepath.ToSlash(path))
		}
	}
	if len(fb)+len(u2)+len(unfix) == 0 {
		return nil
	}
	env, err := newAcctEnv(rc, dryRun)
	if err != nil {
		fmt.Fprintln(w, "계정을 만들지 않습니다: "+err.Error())
		return nil
	}
	specs := env.accounts()
	af := &accountFlags{noPrompt: noPrompt, stdinOK: stdinOK, listMax: 20}
	workers := acctWorkers(0, rc.Conf.Concurrency)
	var done [][]*acctHost // 실제로 처리한 호스트별 계정 칸 (결과 파일·재점검)

	// 1·2: 확인 한 번으로 함께 만든다.
	var needFB, needU2 [][]*acctHost
	for _, h := range fb {
		g := markNeed(newCreateHosts(h.Target, specs), h, credFallback)
		env.setRoles(g, hostVendor(h))
		needFB = append(needFB, g)
	}
	if len(specs) > 1 {
		for _, h := range u2 {
			g := markNeed(newCreateHosts(h.Target, specs[1:]), h, credPrimary)
			env.setRoles(g, hostVendor(h))
			if h.user2 == acctPresent {
				g[0].Status, g[0].curRole = acctNeedRoleUpdate, h.user2Role
			}
			needU2 = append(needU2, g)
		}
	}
	if both := append(append([][]*acctHost(nil), needFB...), needU2...); len(both) > 0 {
		if len(needFB) > 0 && len(specs) > 1 {
			fmt.Fprintf(w, "두 번째 계정 %s(%s)도 같은 호스트에 함께 만듭니다 (이미 있으면 건너뜀, 권한이 다르면 권한만 바꿈).\n", specs[1].User, rc.Conf.user2RolesText())
		}
		printAcctTargets(w, flattenAcct(both), 10)
		proceed := true
		switch {
		case dryRun:
			fmt.Fprintln(w, "dry-run: 아래는 만들 계획입니다 (BMC 에 쓰지 않음, 다시 점검하지 않음).")
		case noPrompt:
			fmt.Fprintln(w, "-no-prompt 이므로 묻지 않습니다. 만들려면: bash bios.sh account (또는 bash account_create.sh)")
			return nil
		default:
			if len(needFB) > 0 {
				fmt.Fprintln(w, "만든 뒤에는 1순위 계정을 만든 호스트만 1순위 계정으로 다시 점검해 결과를 합쳐 보여 줍니다.")
			}
			ok, err := confirmCreate(in, w, createWhat(both), af)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(w, "계정을 만들지 않습니다. 나중에: bash bios.sh account (또는 bash account_create.sh)")
				proceed = false
			}
		}
		if proceed {
			createHosts(env, needFB, workers)
			if len(needU2) > 0 {
				pe := *env
				pe.admin = &fallbackAcct{user: rc.Conf.User, pass: rc.Password}
				createHosts(&pe, needU2, workers)
			}
			fmt.Fprintln(w)
			writeAcctReport(w, flattenAcct(both), 20, p)
			done = append(done, both...)
		}
	}

	// 3: AUTH_FAIL 호스트는 운영자가 입력한 관리자 계정으로.
	if len(unfix) > 0 {
		done = append(done, manualCreate(env, unfix, af, dryRun, in, w, p, workers)...)
	}
	if len(done) == 0 {
		return nil
	}
	path, err := writeAcctResult(run.Dir, flattenAcct(done))
	fmt.Fprintf(w, "\n계정 생성 결과 파일: %s\n", path)
	printAcctDisclaimer(w, "create", dryRun)
	if !dryRun {
		if rerr := recheckCreated(rc, run, done, w, o.Color); rerr != nil {
			fmt.Fprintln(w, "경고: 재점검 결과 기록 중 오류: "+rerr.Error())
		}
	}
	return err
}

// markNeed 는 점검 결과 h 의 계정 칸들을 생성 대상으로 표시합니다 (src: 로그인할 계정 출처, 벤더·모델은 점검에서 읽은 값).
func markNeed(g []*acctHost, h *hostCheck, src string) []*acctHost {
	for _, ah := range g {
		ah.Source, ah.Status = src, acctNeedCreate
		if h.Info != nil {
			ah.Vendor, ah.Model = h.Info.Vendor, h.Info.Model
		}
	}
	return g
}

// hostVendor 는 점검 결과 h 의 벤더입니다 (Info 가 없으면 AUTH_FAIL 때 식별한 벤더 키, 모르면 "").
func hostVendor(h *hostCheck) string {
	if h.Info != nil && h.Info.Vendor != "" {
		return h.Info.Vendor
	}
	return h.authVendor
}

// manualCreate 는 AUTH_FAIL 호스트에 운영자가 직접 입력한 관리자 계정으로 없는 계정을 만듭니다 (위 3).
// 입력한 계정으로 호스트마다 로그인 1회 (재시도 없음)이고, 처음 10대는 한 대씩 처리해 절반 이상 실패하면 남은 호스트는
// 시도하지 않습니다 (runAcctPool 웜업 — 틀린 계정으로 전체에 인증 실패를 남기지 않음). 로그인되지 않았거나 시도하지 않은
// 호스트만 다시 보여 주고 다른 계정을 받습니다. 돌려주는 값은 시도한 호스트별 계정 칸입니다 (호스트마다 마지막 시도).
func manualCreate(env *acctEnv, hs []*hostCheck, af *accountFlags, dryRun bool, in *os.File, w io.Writer, p painter, workers int) [][]*acctHost {
	switch {
	case dryRun:
		fmt.Fprintln(w, "\ndry-run 이므로 관리자 계정 직접 입력은 묻지 않습니다 (로그인 시도 없음).")
		return nil
	case af.noPrompt:
		fmt.Fprintln(w, "\n-no-prompt 이므로 관리자 계정 직접 입력은 묻지 않습니다.")
		return nil
	case !af.stdinOK && !stdinIsTerminal(in):
		fmt.Fprintf(w, "\n%s — 관리자 계정 직접 입력은 묻지 않습니다.\n", msgNonInteractive)
		return nil
	}
	specs := env.accounts()
	last := make([][]*acctHost, len(hs)) // 호스트마다 마지막 시도
	remain := make([]int, len(hs))
	for i := range hs {
		remain[i] = i
	}
	for round := 0; len(remain) > 0; round++ {
		if round > 0 {
			fmt.Fprintf(w, "\n%s\n", p.yellow(fmt.Sprintf("※ 입력한 관리자 계정으로 로그인되지 않았거나 시도하지 않은 호스트 %d대", len(remain))))
			for k, i := range remain {
				if k >= 10 {
					fmt.Fprintf(w, "  ... 외 %d대\n", len(remain)-k)
					break
				}
				g := last[i][0]
				fmt.Fprintf(w, "  %-28s %-15s %s %s\n", sanitizeDetail(g.Target.Hostname), orDefault(sanitizeDetail(g.Target.IP), "-"), g.Status, orDefault(sanitizeDetail(g.Detail), ""))
			}
			if round >= manualMaxRounds {
				fmt.Fprintf(w, "관리자 계정 입력은 %d번까지입니다 (호스트마다 인증 실패가 늘어 잠길 수 있음). BMC 에서 직접 확인하십시오.\n", manualMaxRounds)
				break
			}
		}
		fmt.Fprintf(w, "\n위 %d대는 관리자 ID·비밀번호를 직접 입력하면 그 계정으로 호스트마다 1회만 로그인해 없는 계정만 만듭니다.\n", len(remain))
		fmt.Fprintln(w, "틀리면 호스트마다 인증 실패가 1회 늘어납니다 (계정 잠금 주의). 다른 벤더의 rf.conf 계정은 시도하지 않습니다.")
		if stdinIsTerminal(in) {
			ttyFlushInput(int(in.Fd())) // 앞에서 눌러 둔 Enter 가 ID 로 읽히지 않게 비운다
		}
		id := readLine(in, w, "관리자 ID (빈 입력: 건너뜀): ")
		if id == "" {
			fmt.Fprintln(w, "관리자 계정 직접 입력을 건너뜁니다.")
			break
		}
		if err := validAcctText(id, acctNameMax, false); err != nil {
			fmt.Fprintln(w, "관리자 ID 가 올바르지 않아 건너뜁니다: "+err.Error())
			break
		}
		pw, err := readAdminPassword(in, w, "관리자 비밀번호: ")
		if err != nil || pw == "" {
			msg := "비어 있음"
			if err != nil {
				msg = err.Error()
			}
			fmt.Fprintln(w, "관리자 비밀번호를 입력받지 못해 건너뜁니다: "+msg)
			break
		}
		groups := make([][]*acctHost, len(remain))
		for k, i := range remain {
			groups[k] = markNeed(newCreateHosts(hs[i].Target, specs), hs[i], credManual)
			env.setRoles(groups[k], hostVendor(hs[i])) // 로그인 뒤 확인한 벤더로 다시 정함 (createGroup)
		}
		fmt.Fprintf(w, "관리자 계정 %s 로 %d대에 로그인해 없는 계정만 만듭니다 (이미 있는 계정은 건너뜀).\n", sanitizeDetail(id), len(remain))
		ok, err := confirmCreate(in, w, createWhat(groups), af)
		if err != nil || !ok {
			fmt.Fprintln(w, "관리자 계정으로 만들지 않습니다.")
			break
		}
		me := *env
		me.admin = &fallbackAcct{user: id, pass: pw}
		createHosts(&me, groups, workers)
		fmt.Fprintln(w)
		writeAcctReport(w, flattenAcct(groups), 20, p)
		var next []int
		for k, i := range remain {
			last[i] = groups[k]
			if st := groups[k][0].Status; st == StatusAuthFail || st == acctSkippedWarmup {
				next = append(next, i)
			}
		}
		remain = next
	}
	var out [][]*acctHost
	for _, g := range last {
		if g != nil {
			out = append(out, g)
		}
	}
	return out
}

// probeUser2 는 점검 세션 c(1순위 계정, 읽기 전용)로 계정 목록을 읽어 name 이 있는지 봅니다 (account create 의 점검과 같은
// accountsCollection + listAccounts: AccountService·컬렉션 GET 2회 + 멤버마다 GET 1회, 멤버 상한 acctMaxMembers).
// 있으면 acctPresent 와 그 계정의 RoleId (이름이 정확히 같을 때만, 권한 변경 판단용), 없으면 acctNeedCreate.
// 목록을 읽지 못했거나 읽지 못한 멤버가 있는데 찾지 못했으면 "" (추측하지 않음).
func probeUser2(c *Client, name string) (state, roleID string) {
	ms, err := listAccounts(c, accountsCollection(c))
	if err != nil {
		return "", ""
	}
	if findAccount(ms, name) != nil {
		if m := findAccountExact(ms, name); m != nil {
			roleID = m.RoleID
		}
		return acctPresent, roleID
	}
	for _, m := range ms {
		if m.Unknown {
			return "", ""
		}
	}
	return acctNeedCreate, ""
}

// readLine 은 한 줄을 읽어 앞뒤 공백을 지워 돌려줍니다 (askYesNo 와 같은 바이트 단위 읽기). 줄 끝 없이 EOF 면 "".
func readLine(in io.Reader, w io.Writer, prompt string) string {
	fmt.Fprint(w, prompt)
	line := readRawLine(in)
	if !strings.HasSuffix(line, "\n") {
		fmt.Fprintln(w)
		return ""
	}
	return strings.TrimSpace(line)
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
		// 첫 칸이 1순위 계정 (newCreateHosts). user2 만 만든 호스트(첫 칸이 user2)는 다시 점검하지 않는다.
		if len(g) > 0 && g[0].Status == acctCreated && (g[0].spec == nil || !g[0].spec.Second) {
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
