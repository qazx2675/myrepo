// survey.go - gossh 1회로 호스트별 현황 수집, 표식 기반 출력 파싱
package main

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// 원격 스크립트가 찍는 표식. 한 줄에 "GI_키=값" 하나.
const (
	mkBegin = "GI_BEGIN"
	mkEnd   = "GI_END"
)

// HostStatus 는 호스트 하나의 현황이다. Err 가 비어 있지 않으면 현황을 읽지 못한 것이다.
type HostStatus struct {
	Hostname      string          // 원격 hostname
	Kernel        string          // uname -r
	Driver        string          // 현재 드라이버 버전(nvidia-smi). 없거나 읽지 못하면 ""
	CUDA          string          // nvidia-smi 의 CUDA Version (예: 12.4, 새 드라이버의 "CUDA UMD Version" 도 읽음)
	GPUs          int             // lspci 의 NVIDIA VGA/3D 장치 수
	NVSwitch      bool            // NVSwitch 장비
	KernelDevel   bool            // 현재 커널의 kernel-devel 설치
	GCC           bool            // gcc 있음
	Nouveau       bool            // nouveau 로드됨
	FabricManager string          // 설치된 fabricmanager 패키지(없으면 "")
	UserMount     bool            // /user/<MOUNT_USER> 접근 가능
	RunReadable   map[string]bool // 버전별 <autofs>/<run> 읽기 가능
	BaseURLs      []string        // /etc/yum.repos.d/rhel.repo 의 baseurl
	DCGM          DcgmState       // nvidia-dcgm·DCGM 패키지 현재 상태 (같은 버전 재실행 때 DCGM 만 맞출지 판단)
	Err           string
}

// remoteRunFile 은 대상 서버(리눅스) 기준 .run 경로다.
func remoteRunFile(dir, ver string) string {
	return path.Join(dir, runPrefix+ver+runSuffix)
}

// buildSurveyScript 는 현황 수집용 bash 스크립트다. versions 는 읽기 확인할 드라이버 버전들이다.
// autofs·nvidia-smi 가 멈춰도 끝나도록 timeout 을 건다.
func buildSurveyScript(autofs, mountUser string, versions []string) string {
	var b strings.Builder
	b.WriteString(`echo ` + mkBegin + `
echo "GI_HOSTNAME=$(hostname)"
echo "GI_KERNEL=$(uname -r)"
if command -v nvidia-smi >/dev/null 2>&1; then
  echo "GI_DRIVER=$(timeout 30 nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -n 1)"
  echo "GI_CUDA=$(timeout 30 nvidia-smi 2>/dev/null | sed -n 's/.*CUDA[ A-Za-z]*Version: *\([0-9][0-9.]*\).*/\1/p' | head -n 1)"
fi
nv=$(lspci 2>/dev/null | grep -i nvidia)
echo "GI_GPU=$(printf '%s\n' "$nv" | grep -ciE 'vga|3d controller')"
if printf '%s\n' "$nv" | grep -qiE 'nvswitch|bridge' || ls /dev/nvidia-nvswitch* >/dev/null 2>&1; then echo GI_NVSWITCH=1; fi
rpm -q "kernel-devel-$(uname -r)" >/dev/null 2>&1 && echo GI_KDEVEL=1
command -v gcc >/dev/null 2>&1 && echo GI_GCC=1
lsmod 2>/dev/null | grep -q '^nouveau ' && echo GI_NOUVEAU=1
echo "GI_FM=$(rpm -qa 2>/dev/null | grep -i fabricmanager | head -n 1)"
`)
	if mountUser != "" {
		b.WriteString("timeout 10 ls " + shQuote("/user/"+mountUser) + " >/dev/null 2>&1 && echo GI_USERMNT=1\n")
	}
	for _, v := range versions {
		b.WriteString("if timeout 10 head -c 1 " + shQuote(remoteRunFile(autofs, v)) +
			" >/dev/null 2>&1; then echo GI_RUN=" + v + ":1; else echo GI_RUN=" + v + ":0; fi\n")
	}
	b.WriteString("sed -n 's/^[[:space:]]*baseurl[[:space:]]*=[[:space:]]*//p' /etc/yum.repos.d/rhel.repo 2>/dev/null | sed 's/^/GI_BASEURL=/'\n")
	b.WriteString(dcgmStateScript)
	b.WriteString("echo " + mkEnd + "\n")
	return b.String()
}

// parseSurvey 는 gossh -script 출력을 호스트별 HostStatus 로 읽는다(결과에는 hosts 의 모든 호스트).
// GI_BEGIN ~ GI_END 사이 줄만 본다. GI_END 가 없으면 Err 에 stderr 첫 줄(없으면 "응답 없음")을 넣는다.
func parseSurvey(stdout, stderr string, hosts []string) map[string]*HostStatus {
	res := map[string]*HostStatus{}
	state := map[string]int{} // 0: 표식 전, 1: 수집 중, 2: 끝
	for _, h := range hosts {
		res[h] = &HostStatus{RunReadable: map[string]bool{}}
	}
	for _, l := range strings.Split(stdout, "\n") {
		h, v, ok := splitHostLine(l)
		st := res[h]
		if !ok || st == nil {
			continue
		}
		v = strings.TrimSpace(strings.TrimPrefix(v, "ERROR: "))
		switch {
		case v == mkBegin:
			*st = HostStatus{RunReadable: map[string]bool{}}
			state[h] = 1
			continue
		case v == mkEnd && state[h] == 1:
			state[h] = 2
			continue
		case state[h] != 1:
			continue
		}
		k, val, _ := strings.Cut(v, "=")
		val = strings.TrimSpace(val)
		switch k {
		case "GI_HOSTNAME":
			st.Hostname = val
		case "GI_KERNEL":
			st.Kernel = val
		case "GI_DRIVER":
			if versionRe.MatchString(val) { // "NVIDIA-SMI has failed ..." 같은 문구는 버전으로 보지 않는다
				st.Driver = val
			}
		case "GI_CUDA":
			st.CUDA = val
		case "GI_GPU":
			fmt.Sscan(val, &st.GPUs)
		case "GI_NVSWITCH":
			st.NVSwitch = val == "1"
		case "GI_KDEVEL":
			st.KernelDevel = val == "1"
		case "GI_GCC":
			st.GCC = val == "1"
		case "GI_NOUVEAU":
			st.Nouveau = val == "1"
		case "GI_FM":
			st.FabricManager = val
		case "GI_USERMNT":
			st.UserMount = val == "1"
		case "GI_RUN":
			if ver, r, ok := strings.Cut(val, ":"); ok {
				st.RunReadable[ver] = r == "1"
			}
		case "GI_BASEURL":
			if val != "" {
				st.BaseURLs = append(st.BaseURLs, val)
			}
		default:
			st.DCGM.set(k, val)
		}
	}
	reason := hostReasons(stderr)
	for _, h := range hosts {
		if state[h] == 2 {
			continue
		}
		r := reason[h]
		switch {
		case r != "":
		case state[h] == 1:
			r = "현황 출력이 중간에 끊김"
		default:
			r = "응답 없음"
		}
		res[h] = &HostStatus{RunReadable: map[string]bool{}, Err: r}
	}
	return res
}

// Survey 는 entries 의 모든 호스트에 gossh 를 한 번 실행해 현황을 모은다.
func Survey(cfg *Config, es []Entry) (map[string]*HostStatus, error) {
	var hosts, vers []string
	seen := map[string]bool{}
	for _, e := range es {
		hosts = append(hosts, e.Host)
		if !seen[e.Version] {
			seen[e.Version] = true
			vers = append(vers, e.Version)
		}
	}
	cmd := remoteBash(buildSurveyScript(cfg.AutofsPath, cfg.MountUser, vers))
	so, se, err := runGossh(cfg, hosts, cmd, 10*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("gossh 실행 실패: %w", err)
	}
	return parseSurvey(so, se, hosts), nil
}
