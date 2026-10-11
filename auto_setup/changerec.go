// changerec.go - m 메뉴 실행 기록: <state>/changes/<user>/<YYYYmmddHHMMSS>_<kind>.json (디렉터리 0700, 파일 0600),
// 원복·auto.appl 원복 대상 자동 채움, 원복 성공 뒤 이전 기록 갱신(임시 파일 + rename), auto_setup.log 요약 한 줄.
// 기록에는 bindpw·gossh 명령줄·래퍼 내용이 들어가지 않는다 (detail 은 마스킹된 엔진 출력의 요약뿐).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// chgHost: 호스트별 결과 (기록 파일 hosts[] 항목)
type chgHost struct {
	Host           string `json:"host"`
	IPResult       string `json:"ip_result,omitempty"`
	LDAPResult     string `json:"ldap_result,omitempty"`
	ApplResult     string `json:"appl_result,omitempty"` // auto.appl 원복 결과
	OldIP          string `json:"old_ip,omitempty"`
	NewIP          string `json:"new_ip,omitempty"` // IP 원복이면 복원된 IP
	GW             string `json:"gw,omitempty"`
	Site           string `json:"site,omitempty"`
	Route          string `json:"route,omitempty"` // os8 | os6
	Detail         string `json:"detail,omitempty"`
	ApplPending    bool   `json:"appl_pending"`     // LDAP 적용 OK → auto.appl 대기 (auto.appl 원복·LDAP 원복 성공 시 false)
	IPRolledBack   bool   `json:"ip_rolled_back"`   // 이 IP 변경을 원복함
	LDAPRolledBack bool   `json:"ldap_rolled_back"` // 이 LDAP 변경을 원복함
}

func (x *chgHost) addDetail(tag, d string) {
	if d = strings.TrimSpace(d); d == "" {
		return
	}
	if x.Detail != "" {
		x.Detail += "; "
	}
	x.Detail += tag + ": " + d
}

type changeRecord struct {
	User      string     `json:"user"`
	Kind      string     `json:"kind"` // ip | ldap | both | rollback-ip | rollback-ldap | rollback-both | appl
	Infra     string     `json:"infra,omitempty"`
	Start     string     `json:"start"`
	End       string     `json:"end,omitempty"` // 비어 있으면 끝나지 않음 (실행 중 비정상 종료)
	Cancelled bool       `json:"cancelled"`
	Hosts     []*chgHost `json:"hosts"`
}

var changeRecRe = regexp.MustCompile(`^\d{14}_[a-z-]+(?:_\d+)?\.json$`)

// validChangeUser: 경로에 쓰이므로 영문·숫자·. _ - 만, "." ".." 금지
func validChangeUser(u string) bool { return userNameRe.MatchString(u) && u != "." && u != ".." }

func changeUserDir(stateDir, user string) (string, error) {
	if !validChangeUser(user) {
		return "", errors.New("user 이름이 올바르지 않음")
	}
	return filepath.Join(stateDir, "changes", user), nil
}

func mkdir0700(dirs ...string) error {
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return err
		}
		if err := os.Chmod(d, 0700); err != nil {
			return err
		}
	}
	return nil
}

// createChangeRecord: 새 기록 파일 (같은 초·종류가 있으면 _2, _3 …). 경로 반환.
func createChangeRecord(stateDir string, rec *changeRecord, at time.Time) (string, error) {
	dir, err := changeUserDir(stateDir, rec.User)
	if err != nil {
		return "", err
	}
	if err := mkdir0700(filepath.Join(stateDir, "changes"), dir); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return "", err
	}
	base := at.Format("20060102150405") + "_" + rec.Kind
	for n := 1; n < 100; n++ {
		name := base + ".json"
		if n > 1 {
			name = fmt.Sprintf("%s_%d.json", base, n)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(append(b, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(p, 0600)
		}
		if err != nil {
			os.Remove(p)
			return "", err
		}
		return p, nil
	}
	return "", errors.New("기록 파일 이름을 만들 수 없음")
}

// saveChangeRecord: 기존 기록을 원자적으로 덮어쓴다 (같은 디렉터리 임시 파일 0600 → rename)
func saveChangeRecord(path string, rec *changeRecord) error {
	if path == "" {
		return errors.New("기록 경로 없음")
	}
	b, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".rec_")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0600)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

type recFile struct {
	path string
	rec  *changeRecord
}

// loadChangeRecords: user 의 기록 (최신순). 읽을 수 없거나 깨진 파일은 건너뛴다.
func loadChangeRecords(stateDir, user string) []recFile {
	dir, err := changeUserDir(stateDir, user)
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && changeRecRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var out []recFile
	for _, n := range names {
		p := filepath.Join(dir, n)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rec := &changeRecord{}
		if json.Unmarshal(b, rec) != nil {
			continue
		}
		out = append(out, recFile{p, rec})
	}
	return out
}

func isApplyKind(k string) bool { return k == "ip" || k == "ldap" || k == "both" }

// changeAutofill: 원복·auto.appl 원복 대상 (최신 기록 순, 중복 제거) + 그때 os6 경유였던 호스트.
// want: ip  = ip_result OK 이고 IP 원복 안 됨 / ldap = ldap_result OK(CHANGED) 이고 LDAP 원복 안 됨 / appl = appl_pending
func changeAutofill(stateDir, user, want string) (hosts []string, os6 map[string]bool) {
	os6 = map[string]bool{}
	seen := map[string]bool{}
	for _, rf := range loadChangeRecords(stateDir, user) {
		if !isApplyKind(rf.rec.Kind) {
			continue
		}
		for _, h := range rf.rec.Hosts {
			if h == nil || seen[h.Host] || !hostRe.MatchString(h.Host) {
				continue
			}
			ok := false
			switch want {
			case "ip":
				ok = h.IPResult == rsOK && !h.IPRolledBack
			case "ldap":
				ok = (h.LDAPResult == rsOK || h.LDAPResult == rsChanged) && !h.LDAPRolledBack
			case "appl":
				ok = h.ApplPending
			}
			if ok {
				seen[h.Host] = true
				hosts = append(hosts, h.Host)
				if h.Route == "os6" {
					os6[h.Host] = true
				}
			}
		}
	}
	return
}

// markChangeRecords: user 의 적용 기록(ip·ldap·both)에서 hosts 항목에 fn 을 적용하고, 바뀐 파일만 원자적으로 다시 쓴다
func markChangeRecords(stateDir, user string, hosts map[string]bool, fn func(h *chgHost) bool) error {
	if len(hosts) == 0 {
		return nil
	}
	var first error
	for _, rf := range loadChangeRecords(stateDir, user) {
		if !isApplyKind(rf.rec.Kind) {
			continue
		}
		changed := false
		for _, h := range rf.rec.Hosts {
			if h != nil && hosts[h.Host] && fn(h) {
				changed = true
			}
		}
		if changed {
			if err := saveChangeRecord(rf.path, rf.rec); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// markAfterRun: 원복·auto.appl 원복이 성공한 호스트의 이전 적용 기록을 갱신한다
// (IP 원복 OK → ip_rolled_back, LDAP 원복 OK/NOCHANGE → ldap_rolled_back + appl_pending=false, auto.appl 원복 OK/대기 없음 → appl_pending=false)
func markAfterRun(stateDir string, p changePlan, res []*chgHost) error {
	if p.dry || (p.kind != kindRollback && p.kind != kindApplRestore) {
		return nil
	}
	ipOK, ldapOK, applOK := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, h := range res {
		if h.IPResult == rsOK {
			ipOK[h.Host] = true
		}
		if h.LDAPResult == rsOK || h.LDAPResult == rsNoChange {
			ldapOK[h.Host] = true
		}
		if h.ApplResult == rsOK || h.ApplResult == rsNoPending {
			applOK[h.Host] = true
		}
	}
	var errs []error
	if p.kind == kindRollback && p.rbIP {
		errs = append(errs, markChangeRecords(stateDir, p.user, ipOK, func(h *chgHost) bool {
			if h.IPResult == rsOK && !h.IPRolledBack {
				h.IPRolledBack = true
				return true
			}
			return false
		}))
	}
	if p.kind == kindRollback && p.rbLDAP {
		errs = append(errs, markChangeRecords(stateDir, p.user, ldapOK, func(h *chgHost) bool {
			if (h.LDAPResult == rsOK || h.LDAPResult == rsChanged) && (!h.LDAPRolledBack || h.ApplPending) {
				h.LDAPRolledBack, h.ApplPending = true, false // 원복 뒤 auto_appl_restore.sh 를 돌리면 새 설정이 다시 들어가므로 대기 해제
				return true
			}
			return false
		}))
	}
	if p.kind == kindApplRestore {
		errs = append(errs, markChangeRecords(stateDir, p.user, applOK, func(h *chgHost) bool {
			if h.ApplPending {
				h.ApplPending = false
				return true
			}
			return false
		}))
	}
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// hostOutcome: 호스트 한 대의 종합 — ok(모든 결과가 성공류) / fail / 미실행(결과 없음)
func hostOutcome(h *chgHost) string {
	any := false
	for _, s := range []string{h.IPResult, h.LDAPResult, h.ApplResult} {
		switch s {
		case "":
			continue
		case rsOK, rsNoChange, rsChanged, rsNoPending:
			any = true
		default:
			return "fail"
		}
	}
	if !any {
		return "none"
	}
	return "ok"
}

// changeLogSummary: auto_setup.log 한 줄 (종류·user·대상 수·OK/FAIL 수만 — 호스트 목록·값 없음)
func changeLogSummary(rec *changeRecord) string {
	ok, fail, none := 0, 0, 0
	for _, h := range rec.Hosts {
		switch hostOutcome(h) {
		case "ok":
			ok++
		case "fail":
			fail++
		default:
			none++
		}
	}
	s := fmt.Sprintf("변경 메뉴 %s: user %s 대상 %d대 OK %d FAIL %d", rec.Kind, rec.User, len(rec.Hosts), ok, fail)
	if none > 0 {
		s += fmt.Sprintf(" 미실행 %d", none)
	}
	if rec.Cancelled {
		s += " (취소됨)"
	}
	return s
}
