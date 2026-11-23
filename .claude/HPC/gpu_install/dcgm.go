// dcgm.go - 드라이버 설치 뒤 CUDA major 에 맞는 DCGM 4 설치(mount: rhel.repo 토글 / scp: rpm 전송),
// nvidia-dcgm 기동과 판정
package main

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 대상 서버의 repo 파일. mount 서버는 enabled 를 잠시 켜고, scp 서버는 baseurl 만 읽는다(2단계 현황).
const rhelRepo = "/etc/yum.repos.d/rhel.repo"

const dcgmPrefix = "datacenter-gpu-manager-4-"

// cudaMajor 는 "12.4" 같은 CUDA 버전의 major 다. 읽지 못하면 0.
func cudaMajor(s string) int {
	m, _, _ := strings.Cut(strings.TrimSpace(s), ".")
	n, err := strconv.Atoi(m)
	if err != nil {
		return 0
	}
	return n
}

// dcgmSupported 는 DCGM 4 를 설치하는 CUDA major(12, 13) 인지 본다.
func dcgmSupported(major int) bool { return major == 12 || major == 13 }

// dcgmPackages 는 CUDA major 에 맞는 DCGM 패키지 4개다.
func dcgmPackages(major int) []string {
	c := "cuda" + strconv.Itoa(major)
	return []string{dcgmPrefix + "core", dcgmPrefix + "proprietary", dcgmPrefix + c, dcgmPrefix + "proprietary-" + c}
}

// dcgmOpposite 는 반대 CUDA(12↔13) 전용 패키지 2개다. 설치 전에 있으면 제거한다.
func dcgmOpposite(major int) []string {
	c := "cuda" + strconv.Itoa(25-major)
	return []string{dcgmPrefix + c, dcgmPrefix + "proprietary-" + c}
}

// DcgmState 는 nvidia-dcgm 서비스와 DCGM 패키지 상태다(현황 수집과 설치 뒤 확인에서 같이 쓴다).
type DcgmState struct {
	Active  string            // systemctl is-active nvidia-dcgm
	Enabled string            // systemctl is-enabled nvidia-dcgm
	GPUs    int               // dcgmi discovery -l 의 GPU 수
	Pkgs    map[string]string // 설치된 datacenter-gpu-manager-4-* 이름 → 버전
}

// dcgmStateScript 는 DcgmState 를 "GI_DCGM_*=값" 줄로 찍는다.
const dcgmStateScript = `echo "GI_DCGM_ACTIVE=$(systemctl is-active nvidia-dcgm 2>/dev/null)"
echo "GI_DCGM_ENABLED=$(systemctl is-enabled nvidia-dcgm 2>/dev/null)"
if command -v dcgmi >/dev/null 2>&1; then
  echo "GI_DCGM_FOUND=$(timeout 30 dcgmi discovery -l 2>&1 | grep -iE 'GPUs? found' | head -n 1)"
fi
rpm -qa --qf '%{NAME} %{VERSION}\n' 'datacenter-gpu-manager-4-*' 2>/dev/null | sort | sed 's/^/GI_DCGM_PKG=/'
`

// dcgmStartScript 는 nvidia-dcgm 을 enable·restart 하고 dcgmi 가 GPU 를 볼 때까지(최대 약 30초) 기다린 뒤 상태를 찍는다.
const dcgmStartScript = `systemctl enable nvidia-dcgm
systemctl restart nvidia-dcgm
for i in 1 2 3 4 5 6 7 8 9 10; do
  timeout 30 dcgmi discovery -l 2>/dev/null | grep -qiE '(^|[^0-9])[1-9][0-9]* GPUs? found' && break
  sleep 3
done
timeout 30 dcgmi discovery -l 2>&1
` + dcgmStateScript

var dcgmiFoundRe = regexp.MustCompile(`(?i)(\d+)\s+GPUs?\s+found`)

// dcgmiGPUs 는 dcgmi discovery -l 의 "N GPUs found." 에서 N 을 읽는다. 없으면 0.
func dcgmiGPUs(s string) int {
	m := dcgmiFoundRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// set 은 "GI_DCGM_*" 키 하나를 반영한다. 모르는 키면 false.
func (d *DcgmState) set(k, v string) bool {
	switch k {
	case "GI_DCGM_ACTIVE":
		d.Active = v
	case "GI_DCGM_ENABLED":
		d.Enabled = v
	case "GI_DCGM_FOUND":
		d.GPUs = dcgmiGPUs(v)
	case "GI_DCGM_PKG":
		name, ver, _ := strings.Cut(v, " ")
		if name == "" {
			return true
		}
		if d.Pkgs == nil {
			d.Pkgs = map[string]string{}
		}
		d.Pkgs[name] = strings.TrimSpace(ver)
	default:
		return false
	}
	return true
}

// parseDcgmState 는 dcgmStateScript 출력을 읽는다.
func parseDcgmState(out string) DcgmState {
	var d DcgmState
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		d.set(k, strings.TrimSpace(v))
	}
	return d
}

// dcgmProblems 는 major 기준 DCGM 이 정상이 아닌 사유들이다(fail_list 에 쓰는 "dcgm ..." 형식). 비면 정상.
func dcgmProblems(d DcgmState, major int) []string {
	var p, miss, extra []string
	for _, n := range dcgmPackages(major) {
		if _, ok := d.Pkgs[n]; !ok {
			miss = append(miss, strings.TrimPrefix(n, dcgmPrefix))
		}
	}
	for _, n := range dcgmOpposite(major) {
		if _, ok := d.Pkgs[n]; ok {
			extra = append(extra, strings.TrimPrefix(n, dcgmPrefix))
		}
	}
	if len(miss) > 0 {
		p = append(p, "dcgm missing("+strings.Join(miss, ",")+")")
	}
	if len(extra) > 0 {
		p = append(p, "dcgm cuda mismatch("+strings.Join(extra, ",")+")")
	}
	if d.Active != "active" {
		p = append(p, "dcgm inactive")
	}
	if d.Enabled != "enabled" {
		p = append(p, "dcgm not enabled")
	}
	if d.GPUs < 1 {
		p = append(p, "dcgm no gpu")
	}
	return p
}

// summary 는 표에 쓸 "core버전 cudaN" 이다. 설치된 것이 없으면 "-".
func (d *DcgmState) summary() string {
	var s []string
	if v := d.Pkgs[dcgmPrefix+"core"]; v != "" {
		s = append(s, v)
	}
	for _, m := range []string{"12", "13"} {
		if _, ok := d.Pkgs[dcgmPrefix+"cuda"+m]; ok {
			s = append(s, "cuda"+m)
		}
	}
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, " ")
}

// state 는 표에 쓸 "active/enabled/GPU n" 이다.
func (d *DcgmState) state() string {
	return dash(d.Active) + "/" + dash(d.Enabled) + "/GPU " + strconv.Itoa(d.GPUs)
}

// dcgmRemoveSnippet 은 반대 CUDA 의 DCGM 패키지가 있으면 서비스를 멈추고 저장소 없이 제거한다.
func dcgmRemoveSnippet(major int) string {
	return `rm_pkgs=""
for p in ` + strings.Join(dcgmOpposite(major), " ") + ` datacenter-gpu-manager; do
  rpm -q "$p" >/dev/null 2>&1 && rm_pkgs="$rm_pkgs $p"
done
if [ -n "$rm_pkgs" ]; then
  echo "반대 CUDA 의 DCGM 패키지 제거:$rm_pkgs"
  systemctl stop nvidia-dcgm 2>/dev/null
  dnf remove -y --disablerepo='*' $rm_pkgs || { echo "GI_FAIL=dcgm remove cuda` + strconv.Itoa(25-major) + ` failed"; exit 1; }
fi
`
}

// dcgmMountScript 는 mount 서버용 스크립트다. 서브셸 하나 안에서
// repo 원본 저장 → trap 등록 → 반대 패키지 제거 → enabled=1 → 적용 확인 → dnf install 을 하고,
// 성공·실패·중단 어느 경우에도 trap 이 원본(enabled=0)으로 되돌린다.
// 앞선 실행이 강제 종료돼 원본 사본(.gpu_install.orig)이 남아 있으면 그것을 원본으로 쓴다.
// 토글이 어긋나도 설치가 실패하지 않게 rhel.repo 의 저장소 id 를 --enablerepo 로도 넘긴다.
// 폐쇄망에서는 다른 repo 파일이 켜져 있어도 닿지 않아 dnf 가 메타데이터 단계에서 실패하므로
// --disablerepo='*' 로 rhel.repo 의 저장소만 쓴다.
// SSH 가 끊겨 출력이 막히는 경우(PIPE)에도 복구되게 PIPE 도 잡는다.
func dcgmMountScript(repo string, major int) string {
	return `(
repo=` + shQuote(repo) + `
orig="$repo.gpu_install.orig"
if [ ! -f "$repo" ]; then echo "GI_FAIL=dcgm no rhel.repo"; exit 1; fi
if [ ! -f "$orig" ]; then
  cp -p "$repo" "$orig" || { echo "GI_FAIL=dcgm repo backup failed"; exit 1; }
fi
restore() {
  trap '' INT TERM HUP PIPE
  if cat "$orig" > "$repo" && rm -f "$orig"; then echo "GI_REPO_RESTORED=1"; else echo "GI_REPO_RESTORED=0"; fi
  grep -E '^[[:space:]]*enabled' "$repo" | sed 's/^/GI_REPO_AFTER=/'
}
trap restore EXIT
trap 'exit 130' INT TERM HUP PIPE
grep -E '^[[:space:]]*enabled' "$orig" | sed 's/^/GI_REPO_BEFORE=/'
` + dcgmRemoveSnippet(major) + `sed -i 's/^\([[:space:]]*enabled[[:space:]]*=[[:space:]]*\)0/\11/' "$repo"
if grep -Eq '^[[:space:]]*enabled[[:space:]]*=[[:space:]]*0' "$repo"; then echo "GI_FAIL=dcgm repo enable failed"; exit 1; fi
ids=$(sed -n 's/^[[:space:]]*\[\([^]]*\)\].*/\1/p' "$repo" | paste -sd, -)
echo "GI_REPO_IDS=$ids"
if [ -z "$ids" ]; then echo "GI_FAIL=dcgm no repo id in rhel.repo"; exit 1; fi
dnf install -y --setopt=skip_if_unavailable=True --disablerepo='*' --enablerepo="$ids" ` + strings.Join(dcgmPackages(major), " ") + ` || { echo "GI_FAIL=dcgm install failed"; exit 1; }
)`
}

// dcgmScpScript 는 scp 서버용 스크립트다. 반대 패키지 제거 뒤 보낸 rpm 을 저장소 없이 설치한다.
func dcgmScpScript(files []string, major int) string {
	var q []string
	for _, f := range files {
		q = append(q, shQuote(f))
	}
	return "(\n" + dcgmRemoveSnippet(major) +
		"dnf localinstall -y --disablerepo='*' " + strings.Join(q, " ") + ` || { echo "GI_FAIL=dcgm install failed"; exit 1; }
)`
}

// remoteFail 은 원격 출력의 "GI_FAIL=사유" 를 읽는다. 없으면 def.
func remoteFail(out, def string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "GI_FAIL="); ok && v != "" {
			return v
		}
	}
	return def
}

// repoDirs 는 baseurl 중 file:// 인 것을 관리 서버 경로로 바꾼다. 그 밖의 형식은 bad 로 돌려준다.
func repoDirs(urls []string) (dirs, bad []string) {
	for _, u := range urls {
		if p, ok := strings.CutPrefix(u, "file://"); ok && p != "" {
			dirs = append(dirs, p)
		} else {
			bad = append(bad, u)
		}
	}
	return
}

// rpmNameVer 는 "이름-버전-릴리스.아키.rpm" 파일명이 name 의 rpm 이면 "버전-릴리스" 를 돌려준다.
// 이름 뒤가 숫자로 시작해야 같은 이름으로 본다(proprietary 와 proprietary-cuda12 구분). src.rpm 은 뺀다.
func rpmNameVer(file, name string) (string, bool) {
	base, ok := strings.CutSuffix(file, ".rpm")
	if !ok || strings.HasSuffix(base, ".src") {
		return "", false
	}
	rest, ok := strings.CutPrefix(base, name+"-")
	if !ok || rest == "" || rest[0] < '0' || rest[0] > '9' {
		return "", false
	}
	i := strings.LastIndexByte(rest, '.')
	if i < 0 || (rest[i+1:] != "x86_64" && rest[i+1:] != "noarch") { // 대상은 x86_64 서버
		return "", false
	}
	return rest[:i], true
}

// FindDcgmRPMs 는 대상 rhel.repo 의 baseurl(file://) 과 같은 경로를 관리 서버에서 뒤져
// names 각각의 최신 rpm 경로를 names 순서로 돌려준다.
// file:// 이 없거나 rpm 이 모자라면 fail 사유("dcgm ...")를 오류로 낸다.
func FindDcgmRPMs(baseURLs, names []string) ([]string, error) {
	dirs, bad := repoDirs(baseURLs)
	if len(dirs) == 0 {
		if len(bad) == 0 {
			return nil, fmt.Errorf("dcgm no baseurl in rhel.repo")
		}
		return nil, fmt.Errorf("dcgm baseurl not file://(%s)", strings.Join(bad, ","))
	}
	best := map[string]string{} // 이름 → 경로
	bestVer := map[string]string{}
	for _, d := range dirs {
		filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasPrefix(e.Name(), dcgmPrefix) {
				return nil
			}
			for _, n := range names {
				if v, ok := rpmNameVer(e.Name(), n); ok && (best[n] == "" || rpmEVRCmp(v, bestVer[n]) > 0) {
					best[n], bestVer[n] = p, v
				}
			}
			return nil
		})
	}
	var out, miss []string
	for _, n := range names {
		if best[n] == "" {
			miss = append(miss, strings.TrimPrefix(n, dcgmPrefix))
		}
		out = append(out, best[n])
	}
	if len(miss) > 0 {
		return nil, fmt.Errorf("dcgm rpm not found(%s)", strings.Join(miss, ","))
	}
	return out, nil
}

// rpmEVRCmp 는 "버전-릴리스" 를 버전 먼저, 다음 릴리스 순으로 비교한다.
func rpmEVRCmp(a, b string) int {
	av, ar := a, ""
	if i := strings.LastIndexByte(a, '-'); i >= 0 {
		av, ar = a[:i], a[i+1:]
	}
	bv, br := b, ""
	if i := strings.LastIndexByte(b, '-'); i >= 0 {
		bv, br = b[:i], b[i+1:]
	}
	if c := rpmVerCmp(av, bv); c != 0 {
		return c
	}
	return rpmVerCmp(ar, br)
}

// rpmVerCmp 는 rpm 의 rpmvercmp 처럼 숫자·문자 조각을 차례로 비교한다(종류가 다르면 숫자 조각이 새 것).
func rpmVerCmp(a, b string) int {
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	isAlnum := func(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	seg := func(s string, num bool) (string, string) {
		i := 0
		for i < len(s) && isAlnum(s[i]) && isDigit(s[i]) == num {
			i++
		}
		return s[:i], s[i:]
	}
	for {
		for a != "" && !isAlnum(a[0]) {
			a = a[1:]
		}
		for b != "" && !isAlnum(b[0]) {
			b = b[1:]
		}
		if a == "" || b == "" {
			break
		}
		num := isDigit(a[0])
		sa, ra := seg(a, num)
		sb, rb := seg(b, num)
		if sb == "" {
			if num {
				return 1
			}
			return -1
		}
		if num {
			sa, sb = strings.TrimLeft(sa, "0"), strings.TrimLeft(sb, "0")
			if len(sa) != len(sb) {
				if len(sa) > len(sb) {
					return 1
				}
				return -1
			}
		}
		if c := strings.Compare(sa, sb); c != 0 {
			return c
		}
		a, b = ra, rb
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	}
	return 1
}

// dcgmMount 는 mount 서버에서 rhel.repo 를 잠시 켜고 dnf install 한다. repo 복구를 확인하지 못하면 *restoreFail 을 켠다.
func dcgmMount(cfg *Config, t *Target, l *hostLog, major int, restoreFail *bool) bool {
	out, rc := l.run(cfg, "DCGM 설치(rhel.repo 토글)", dcgmMountScript(rhelRepo, major), installTimeout)
	if !strings.Contains(out, "GI_REPO_RESTORED=1") {
		*restoreFail = true
		l.say("경고: %s 를 원래 값으로 되돌렸는지 확인하지 못했습니다. 대상에서 enabled 값을 확인하세요", rhelRepo)
	}
	if rc != 0 {
		t.Fail = remoteFail(out, "dcgm install failed")
		l.say("fail: %s", t.Fail)
		return false
	}
	return true
}

// dcgmSendDeps 는 보낸 rpm 으로 rpm -U --test 를 해 부족한 의존성이 없어질 때까지,
// 관리 서버 repodata 에서 제공 rpm 을 찾아 send 로 보낸다(최대 maxDepRounds 번).
// 실패하면 t.Fail 에 "dcgm no repodata" 또는 "dcgm dep not found(...)" 를 남긴다.
func dcgmSendDeps(cfg *Config, t *Target, l *hostLog, send func(string) bool, remote *[]string, sent map[string]bool) bool {
	var idx repoIndex
	for round := 0; ; round++ {
		out, rc := l.run(cfg, "의존성 확인(rpm -U --test)", rpmTestScript(*remote), shortTimeout)
		if rc < 0 {
			t.Fail = "dcgm rpm test failed"
			l.say("fail: %s", t.Fail)
			return false
		}
		caps := parseMissingDeps(out)
		if len(caps) == 0 {
			return true
		}
		if idx == nil {
			var err error
			if idx, err = LoadRepoIndex(t.Status.BaseURLs); err != nil {
				t.Fail = err.Error()
				l.say("fail: %s (rhel.repo baseurl: %s)", t.Fail, dash(strings.Join(t.Status.BaseURLs, " ")))
				return false
			}
		}
		deps, missing := idx.resolveDeps(caps, sent)
		if len(missing) == 0 && round >= maxDepRounds {
			missing = caps
		}
		if len(missing) > 0 {
			t.Fail = "dcgm dep not found(" + strings.Join(missing, ",") + ")"
			l.say("fail: %s", t.Fail)
			return false
		}
		l.say("부족한 의존성 %s → rpm %d개 전송", strings.Join(caps, ","), len(deps))
		for _, f := range deps {
			if !send(f) {
				return false
			}
		}
	}
}

// dcgmScp 는 관리 서버에서 최신 DCGM rpm 을 찾아 보내고 부족한 의존성 rpm 도 보낸 뒤 localinstall 하고 지운다.
func dcgmScp(cfg *Config, t *Target, l *hostLog, major int) bool {
	files, err := FindDcgmRPMs(t.Status.BaseURLs, dcgmPackages(major))
	if err != nil {
		t.Fail = err.Error()
		l.say("fail: %s (rhel.repo baseurl: %s)", t.Fail, dash(strings.Join(t.Status.BaseURLs, " ")))
		return false
	}
	if _, rc := l.run(cfg, "임시 디렉토리", "mkdir -p "+remoteTmp, shortTimeout); rc != 0 {
		t.Fail = "dcgm mkdir " + remoteTmp + " failed"
		l.say("fail: %s", t.Fail)
		return false
	}
	var remote []string
	sent := map[string]bool{} // 보낸 관리 서버 rpm 경로
	defer func() {
		if len(remote) > 0 {
			var q []string
			for _, r := range remote {
				q = append(q, shQuote(r))
			}
			l.run(cfg, "DCGM rpm 삭제", "rm -f "+strings.Join(q, " "), shortTimeout)
		}
	}()
	send := func(f string) bool {
		l.say("scp: %s → %s/", f, remoteTmp)
		if err := copyFile(f, t.Host, remoteTmp, shortTimeout); err != nil {
			l.write("scp 오류: " + err.Error() + "\n")
			t.Fail = "dcgm scp failed"
			l.say("fail: DCGM rpm scp 실패 (%v)", err)
			return false
		}
		sent[f] = true
		remote = append(remote, path.Join(remoteTmp, filepath.Base(f)))
		return true
	}
	for _, f := range files {
		if !send(f) {
			return false
		}
	}
	if !dcgmSendDeps(cfg, t, l, send, &remote, sent) {
		return false
	}
	out, rc := l.run(cfg, "DCGM 설치(localinstall)", dcgmScpScript(remote, major), installTimeout)
	if rc != 0 {
		t.Fail = remoteFail(out, "dcgm install failed")
		l.say("fail: %s", t.Fail)
		return false
	}
	return true
}

// installDCGM 은 t.NewCUDA 의 major 에 맞춰 DCGM 을 설치하고 nvidia-dcgm 을 기동·판정한다.
// CUDA 12/13 이 아니면 경고만 하고 건너뛴다(드라이버는 성공). 실패하면 t.Fail 에 "dcgm ..." 사유를 남긴다.
func installDCGM(cfg *Config, t *Target, l *hostLog) {
	major := cudaMajor(t.NewCUDA)
	if !dcgmSupported(major) {
		t.DcgmNote = "skip(CUDA " + dash(t.NewCUDA) + ")"
		l.say("경고: CUDA %s 는 DCGM 4 설치 대상(12/13)이 아니라 DCGM 을 건너뜁니다(드라이버는 성공)", dash(t.NewCUDA))
		return
	}
	l.say("DCGM(cuda%d) 설치 중 (%s)", major, t.Method)
	restoreFail := false
	var ok bool
	if t.Method == methodMount {
		ok = dcgmMount(cfg, t, l, major, &restoreFail)
	} else {
		ok = dcgmScp(cfg, t, l, major)
	}
	if ok {
		out, _ := l.run(cfg, "nvidia-dcgm 기동·확인", dcgmStartScript, shortTimeout)
		d := parseDcgmState(out)
		t.Dcgm = &d
		if p := dcgmProblems(d, major); len(p) > 0 {
			t.Fail = strings.Join(p, ", ")
			l.say("fail: %s", t.Fail)
		} else {
			l.say("DCGM 정상 (%s, %s)", d.summary(), d.state())
		}
	}
	if restoreFail && t.Fail == "" {
		t.Fail = "dcgm repo restore failed"
	}
}

// MarkDcgmRepair 는 같은 버전이라 건너뛴 호스트 중 DCGM 이 정상이 아닌 호스트를 DCGM 단계만 하도록 바꾼다.
// DCGM 실패로 fail_list.retry 에 같은 버전으로 남은 호스트를 다시 돌리면 드라이버는 건너뛰고 DCGM 만 맞춘다.
func MarkDcgmRepair(out io.Writer, ts []*Target) {
	for _, t := range ts {
		if t.Skip != skipSameVersion || t.Status.GPUs == 0 {
			continue
		}
		major := cudaMajor(t.Status.CUDA)
		if !dcgmSupported(major) {
			continue
		}
		p := dcgmProblems(t.Status.DCGM, major)
		if len(p) == 0 {
			continue
		}
		t.Skip, t.DcgmOnly = "", true
		fmt.Fprintf(out, "%s: 드라이버 %s 는 같은 버전이라 건너뛰고 DCGM 만 다시 맞춥니다 (%s)\n",
			t.Host, t.Status.Driver, strings.Join(p, ", "))
	}
}
