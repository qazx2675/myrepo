package main

import (
	"errors"
	"fmt"
	"os"

	"biostool/internal/ucsmxml"
)

// account_ucsm.go 는 Cisco UCSM(XML API) 로컬 사용자 생성·비밀번호 변경입니다 (biostool account -ucsm).
// 대상은 UCSM 주소이며 블레이드 BMC 가 아닙니다. 요청은 ucsmxml 의 계정 모드(configConfMo 로 aaaUser
// 하나의 생성·비밀번호 변경·삭제만)를 지납니다. UCSM 은 단일 엔드포인트라 대상을 한 대씩 처리합니다.
// 생성 계정의 역할은 admin 입니다 (UCSM 의 관리자 역할).

func (e *acctEnv) ucsmClient(t Target, user, pass string) *ucsmxml.Client {
	return ucsmxml.New(ucsmxml.Options{
		BaseURL: t.BaseURL(), User: user, Pass: pass, Insecure: e.conf.Insecure,
		Timeout: e.conf.Timeout, Account: true,
	})
}

func (h *acctHost) setUcsmErr(err error) {
	var xe *ucsmxml.Error
	if errors.As(err, &xe) {
		h.Status, h.Detail = xe.Status, xe.Detail
		return
	}
	h.Status, h.Detail = ucsmxml.StatusXMLError, ucsmErrBrief(err)
}

func isUcsmAuthFail(err error) bool {
	var xe *ucsmxml.Error
	return errors.As(err, &xe) && xe.Status == ucsmxml.StatusAuthFail
}

// ucsmProbe 는 1순위 계정으로 로그인해 봅니다 (읽기). AUTH_FAIL 이면 생성 후보입니다.
func (e *acctEnv) ucsmProbe(t Target) *acctHost {
	h := newAcctHost(t, "create")
	h.Vendor = "Cisco"
	h.Model = "UCSM"
	if t.Err != "" {
		h.Status = t.Err
		return h
	}
	c := e.ucsmClient(t, e.conf.User, e.password)
	defer func() { _ = c.Logout() }()
	switch err := c.Login(); {
	case err == nil:
		h.Status, h.Source = acctPrimaryOK, credPrimary
	case isUcsmAuthFail(err):
		h.Status, h.Source = acctNeedCreate, credFallback
	default:
		h.setUcsmErr(err)
	}
	return h
}

// ucsmCreate 는 cisco admin 계정으로 로그인해 1순위 계정을 만들고 검증·롤백합니다.
func (e *acctEnv) ucsmCreate(h *acctHost) {
	t := h.Target
	user, pass, ok := e.fb.lookup("cisco")
	if !ok {
		h.Status, h.Detail = acctNoAdmin, "rf.conf 에 Cisco admin 계정(cisco_user/cisco_pass) 없음"
		return
	}
	c := e.ucsmClient(t, user, pass)
	defer func() { _ = c.Logout() }()
	if err := c.Login(); err != nil {
		h.setUcsmErr(err)
		if h.Status == ucsmxml.StatusAuthFail {
			h.Detail = "Cisco admin 계정 로그인 거부 — " + h.Detail
		}
		return
	}
	c.AddSecret(e.password)
	users, err := c.ListUsers()
	if err != nil {
		h.setUcsmErr(err)
		return
	}
	for _, u := range users {
		if u == e.conf.User {
			h.Status, h.Detail = acctExists, "UCSM 에 사용자가 이미 있음 — 덮어쓰지 않음. 비밀번호가 다르면 change 또는 UCSM 에서 확인"
			return
		}
	}
	h.Method = "configConfMo sys/user-ext/user-" + e.conf.User + " (aaaUser created, role admin)"
	h.Warn = "UCSM XML 쓰기는 실장비 미확인 방식 (id_json 문서 근거)"
	if e.dryRun {
		h.Status, h.Detail = acctWouldCreate, `요청 본문(비밀번호 제외): configConfMo aaaUser name="`+e.conf.User+`" pwd="*****" status="created" + aaaUserRole admin`
		return
	}
	if err := c.CreateUser(e.conf.User, e.password); err != nil {
		h.setUcsmErr(err)
		if h.Status == ucsmxml.StatusXMLError || h.Status == "" {
			h.Status = acctRejected
		}
		return
	}
	verr := e.ucsmVerify(t, e.conf.User, e.password)
	if verr == nil {
		h.Status = acctCreated
		return
	}
	if !isUcsmAuthFail(verr) {
		h.Status = acctVerifyUnknown
		h.Detail = "사용자는 만들었으나 검증 로그인을 통신 오류로 하지 못함 — 롤백하지 않음, 수동 확인"
		return
	}
	if derr := c.DeleteUser(e.conf.User); derr != nil {
		h.Status, h.Rollback = acctRollbackFailed, "실패"
		h.Detail = "새 계정 로그인 검증 실패 + 삭제 실패 — UCSM 에서 " + e.conf.User + " 를 수동 확인/삭제하십시오"
		return
	}
	h.Status, h.Rollback = acctVerifyFailRB, "완료"
	h.Detail = "새 계정 로그인 검증 실패 — 만든 사용자를 삭제함"
}

// ucsmVerify 는 계정으로 aaaLogin 1회를 해 봅니다.
func (e *acctEnv) ucsmVerify(t Target, user, pass string) error {
	c := ucsmxml.New(ucsmxml.Options{BaseURL: t.BaseURL(), User: user, Pass: pass, Insecure: e.conf.Insecure, Timeout: e.conf.Timeout})
	err := c.Login()
	_ = c.Logout()
	return err
}

// ucsmChange 는 1순위 사용자의 비밀번호를 바꾸고 검증·롤백합니다 (1순위 → 안 되면 cisco admin 으로 로그인).
func (e *acctEnv) ucsmChange(h *acctHost) {
	t := h.Target
	h.Vendor, h.Model, h.Op = "Cisco", "UCSM", "change"
	if t.Err != "" {
		h.Status = t.Err
		return
	}
	// 1순위 계정으로 로그인된 UCSM 만 변경한다 (admin 세션으로 남의 비밀번호를 바꾸지 않음).
	c := e.ucsmClient(t, e.conf.User, e.password)
	defer func() { _ = c.Logout() }()
	if err := c.Login(); err != nil {
		h.setUcsmErr(err)
		if isUcsmAuthFail(err) {
			h.Detail = "1순위 계정으로 로그인되지 않아 변경하지 않음 (계정이 없거나 비밀번호 불일치 — account create -ucsm 또는 UCSM 에서 확인)"
		}
		return
	}
	c.AddSecret(e.password)
	c.AddSecret(e.newPass)
	users, err := c.ListUsers()
	if err != nil {
		h.setUcsmErr(err)
		return
	}
	found := false
	for _, u := range users {
		if u == e.conf.User {
			found = true
		}
	}
	if !found {
		h.Status, h.Detail = acctNoAccount, "UCSM 에 사용자가 없음 — account create -ucsm 로 먼저 만드십시오"
		return
	}
	h.Method = "configConfMo sys/user-ext/user-" + e.conf.User + " (aaaUser modified, pwd)"
	if e.dryRun {
		h.Status, h.Detail = acctWouldChange, `요청 본문(비밀번호 제외): configConfMo aaaUser name="`+e.conf.User+`" pwd="*****" status="modified"`
		return
	}
	if err := c.AllowUserTarget(e.conf.User); err != nil {
		h.Status, h.Detail = acctRejected, "내부 오류: "+err.Error()
		return
	}
	if err := c.SetUserPassword(e.conf.User, e.newPass); err != nil {
		h.setUcsmErr(err)
		if h.Status == ucsmxml.StatusXMLError {
			h.Status = acctRejected
		}
		return
	}
	verr := e.ucsmVerify(t, e.conf.User, e.newPass)
	if verr == nil {
		h.Status = acctChanged
		return
	}
	if !isUcsmAuthFail(verr) {
		h.Status = acctVerifyUnknown
		h.Detail = "변경은 했으나 새 비밀번호 검증 로그인을 통신 오류로 하지 못함 — 롤백하지 않음, 수동 확인"
		return
	}
	if rerr := c.SetUserPassword(e.conf.User, e.password); rerr == nil && e.ucsmVerify(t, e.conf.User, e.password) == nil {
		h.Status, h.Rollback = acctVerifyFailRB, "완료"
		h.Detail = "새 비밀번호 로그인 검증 실패 — 이전 비밀번호로 되돌림"
		return
	}
	h.Status, h.Rollback = acctRollbackFailed, "실패"
	h.Detail = "새 비밀번호 로그인 검증 실패 + 되돌리기 실패 — UCSM 에서 " + e.conf.User + " 상태를 수동 확인하십시오"
}

func ucsmAccountCreate(rc *runContext, env *acctEnv, af *accountFlags, dir string, p painter) error {
	n := len(rc.Targets)
	hosts := make([]*acctHost, n)
	fmt.Println("\n[1/2] UCSM 점검 중 (1순위 계정으로 aaaLogin 1회, 읽기)")
	for i, t := range rc.Targets {
		hosts[i] = env.ucsmProbe(t)
	}
	var need []*acctHost
	for _, h := range hosts {
		if h.Status == acctNeedCreate {
			need = append(need, h)
		}
	}
	fmt.Println()
	if len(need) == 0 {
		writeAcctReport(os.Stdout, hosts, af.listMax, p)
		path, werr := writeAcctResult(dir, hosts)
		fmt.Printf("\n생성할 UCSM 이 없습니다. 결과: %s\n", path)
		return werr
	}
	fmt.Printf("계정 %q 를 만들 UCSM (%d대):\n", rc.Conf.User, len(need))
	printAcctTargets(os.Stdout, need, af.listMax)
	if !env.dryRun {
		ok, err := confirmCreate(os.Stdin, os.Stdout, len(need), rc.Conf.User, af)
		if err != nil || !ok {
			path, _ := writeAcctResult(dir, hosts)
			fmt.Printf("생성하지 않고 종료합니다. 점검 결과: %s\n", path)
			return err
		}
	}
	fmt.Println("\n[2/2] UCSM 사용자 생성 · 로그인 검증 · 실패 시 삭제(롤백)")
	for _, h := range need {
		env.ucsmCreate(h)
	}
	fmt.Println()
	writeAcctReport(os.Stdout, need, af.listMax, p)
	path, werr := writeAcctResult(dir, hosts)
	fmt.Printf("\n결과 파일: %s\n", path)
	printAcctDisclaimer(os.Stdout, "create", env.dryRun)
	return werr
}

func ucsmAccountChange(rc *runContext, env *acctEnv, af *accountFlags, dir string, p painter) error {
	n := len(rc.Targets)
	hosts := make([]*acctHost, n)
	for i, t := range rc.Targets {
		hosts[i] = newAcctHost(t, "change")
	}
	if !env.dryRun {
		ok, err := acquireNewPassword(rc, env, af, n)
		if err != nil || !ok {
			return err
		}
	}
	fmt.Println("\nUCSM 사용자 비밀번호 변경 · 새 비밀번호 로그인 검증 · 실패 시 되돌리기")
	for _, h := range hosts {
		env.ucsmChange(h)
	}
	fmt.Println()
	writeAcctReport(os.Stdout, hosts, af.listMax, p)
	path, werr := writeAcctResult(dir, hosts)
	fmt.Printf("\n결과 파일: %s\n", path)
	if !env.dryRun {
		printEncryptGuide(os.Stdout, hosts, rc.Conf)
	}
	printAcctDisclaimer(os.Stdout, "change", env.dryRun)
	return werr
}
