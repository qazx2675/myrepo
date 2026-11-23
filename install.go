// install.go - 호스트별 사전점검, 드라이버 설치(mount/scp), 모듈 언로드 후 1회 재시도
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 대상 서버에서 scp 로 받은 파일을 두는 곳
const remoteTmp = "/tmp/gpu_install"

// 원격 명령 제한시간
const (
	shortTimeout   = 5 * time.Minute
	installTimeout = 40 * time.Minute
)

// 원격 스크립트 끝에 찍는 종료코드 표식
const mkRC = "GI_RC="

// 테스트에서 바꿔 끼운다.
var (
	remoteRun = runOnHost
	copyFile  = scpFile
)

// 화면 출력은 여러 호스트가 동시에 쓰므로 줄 단위로 잠근다.
var (
	screen   io.Writer = os.Stdout
	screenMu sync.Mutex
)

// hostLog 는 호스트 하나의 로그(logs/<날짜시각>/<host>.log)다. 파일을 못 열면 화면에만 쓴다.
type hostLog struct {
	host string
	f    *os.File
}

func openHostLog(dir, host string) *hostLog {
	l := &hostLog{host: host}
	if dir != "" {
		f, err := os.OpenFile(filepath.Join(dir, host+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			l.say("로그 파일을 열지 못했습니다: %v", err)
		} else {
			l.f = f
		}
	}
	return l
}

func (l *hostLog) close() {
	if l.f != nil {
		l.f.Close()
	}
}

// say 는 화면과 로그 양쪽에 한 줄을 쓴다.
func (l *hostLog) say(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	screenMu.Lock()
	fmt.Fprintf(screen, "[%s] %s\n", l.host, msg)
	screenMu.Unlock()
	l.write("## " + msg + "\n")
}

// write 는 로그 파일에만 쓴다(원격 출력 전체).
func (l *hostLog) write(s string) {
	if l.f != nil {
		fmt.Fprintf(l.f, "%s %s", time.Now().Format("15:04:05"), s)
		if !strings.HasSuffix(s, "\n") {
			l.f.WriteString("\n")
		}
	}
}

// run 은 원격 스크립트를 실행하고 명령·출력·종료코드를 로그에 남긴다.
// 접속 실패 등으로 종료코드를 못 받으면 rc=-1 이다.
func (l *hostLog) run(cfg *Config, title, script string, timeout time.Duration) (string, int) {
	l.write("$ " + title + "\n" + script + "\n")
	out, rc, err := remoteRun(cfg, l.host, script, timeout)
	if out != "" {
		l.write(out + "\n")
	}
	if err != nil {
		l.write("오류: " + err.Error() + "\n")
	}
	l.write("종료코드 " + strconv.Itoa(rc) + "\n")
	return out, rc
}

// runOnHost 는 bash 스크립트를 호스트 하나에서 gossh 로 실행한다. stderr 는 stdout 에 합치고
// 마지막에 종료코드 표식을 찍어 읽는다. 래퍼는 표식 뒤 항상 exit 0 으로 끝나 gossh 가
// "ERROR: 종료코드 N" 줄을 만들지 않게 하고, 판정은 오직 표식으로 한다.
func runOnHost(cfg *Config, host, script string, timeout time.Duration) (string, int, error) {
	full := "{\n" + script + "\n} 2>&1\necho \"" + mkRC + "$?\"\nexit 0\n"
	so, se, err := runGossh(cfg, []string{host}, remoteBash(full), timeout)
	if err != nil {
		return "", -1, err
	}
	return parseRemote(so, se, host)
}

// gosshExitLine 은 gossh 가 원격 종료코드 != 0 일 때 덧붙이는 줄이다.
var gosshExitLine = regexp.MustCompile(`^ERROR: 종료코드 \d+$`)

// parseRemote 는 gossh -script 출력에서 host 의 줄만 모으고 종료코드 표식을 읽는다.
func parseRemote(stdout, stderr, host string) (string, int, error) {
	var lines []string
	rc := -1
	for _, l := range strings.Split(stdout, "\n") {
		h, v, ok := splitHostLine(l)
		if !ok || h != host {
			continue
		}
		if gosshExitLine.MatchString(v) {
			continue // gossh 가 덧붙인 종료코드 줄은 본문·판정에 쓰지 않는다
		}
		if strings.HasPrefix(v, mkRC) {
			if n, err := strconv.Atoi(strings.TrimPrefix(v, mkRC)); err == nil {
				rc = n
			}
			continue
		}
		lines = append(lines, v)
	}
	out := strings.Join(lines, "\n")
	if rc < 0 {
		r := hostReasons(stderr)[host]
		if r == "" {
			r = "종료코드를 받지 못함"
		}
		return out, rc, fmt.Errorf("%s", r)
	}
	return out, rc, nil
}

// scpFile 은 관리 서버의 local 파일을 root@host:dir/ 로 보낸다.
func scpFile(local, host, dir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "scp", "-q", "-o", "BatchMode=yes", local, "root@"+host+":"+dir+"/")
	c.WaitDelay = 5 * time.Second
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// precheck 는 사전점검이다. 진행하면 true. 건너뛰거나 fail 이면 t.Skip/t.Fail 을 채우고 false.
func precheck(cfg *Config, t *Target, l *hostLog) bool {
	st := t.Status
	if st.GPUs == 0 {
		t.Skip = "GPU 없음"
		l.say("건너뜀: lspci 에 NVIDIA GPU 가 없습니다")
		return false
	}
	var miss []string
	if !st.KernelDevel {
		miss = append(miss, "no kernel-devel")
	}
	if !st.GCC {
		miss = append(miss, "no gcc")
	}
	if len(miss) > 0 {
		t.Fail = strings.Join(miss, ", ")
		l.say("fail: %s (현재 커널 %s)", t.Fail, st.Kernel)
		return false
	}
	if st.NVSwitch || st.FabricManager != "" {
		l.say("경고: NVSwitch/fabricmanager 장비입니다. nvidia-fabricmanager 버전을 드라이버 %s 와 맞춰야 합니다(자동 설치 안 함, 현재: %s)",
			t.Version, dash(st.FabricManager))
	}
	if st.Nouveau {
		l.say("nouveau 가 로드되어 있어 언로드합니다")
		if _, rc := l.run(cfg, "nouveau 언로드", "for v in /sys/class/vtconsole/vtcon*; do grep -qi frame \"$v/name\" 2>/dev/null && echo 0 > \"$v/bind\"; done; rmmod nouveau && ! lsmod | grep -q '^nouveau '", shortTimeout); rc != 0 {
			t.Fail = "nouveau unload failed"
			l.say("fail: nouveau 언로드 실패")
			return false
		}
	}
	return true
}

// unloadScript 는 nvidia 관련 서비스를 멈추고 모듈을 내린다. 프로세스는 kill 하지 않는다.
// 결과는 다음 설치 시도로 판정하므로 여기서는 실패해도 끝까지 진행한다.
const unloadScript = `for s in nvidia-dcgm nvidia-persistenced nvidia-fabricmanager; do
  systemctl stop "$s" 2>/dev/null
done
for m in nvidia_peermem nv_peer_mem nvidia_fs gdrdrv nvidia_vgpu_vfio nvidia_drm nvidia_modeset nvidia_uvm nvidia nouveau; do
  if lsmod | grep -q "^$m "; then rmmod "$m"; fi
done
lsmod | grep -E '^(nvidia|nouveau)' || true`

// diagScript 는 재시도도 실패했을 때 남길 점유 프로세스와 설치 로그 끝부분이다.
const diagScript = `echo '--- lsof /dev/nvidia*'
shopt -s nullglob
set -- /dev/nvidia* /dev/nvidia-caps/*
if [ $# -gt 0 ]; then timeout 60 lsof -b -nP "$@" 2>&1; fi
echo '--- tail -n 50 /var/log/nvidia-installer.log'
tail -n 50 /var/log/nvidia-installer.log 2>&1
true`

// verifyScript 는 설치 후 nvidia-smi 의 드라이버·CUDA 버전을 찍는다.
const verifyScript = `echo "GI_DRIVER=$(timeout 60 nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -n 1)"
echo "GI_CUDA=$(timeout 60 nvidia-smi 2>/dev/null | sed -n 's/.*CUDA[ A-Za-z]*Version: *\([0-9][0-9.]*\).*/\1/p' | head -n 1)"`

// parseVerify 는 verifyScript 출력에서 드라이버·CUDA 버전을 읽는다.
func parseVerify(out string) (driver, cuda string) {
	for _, line := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "GI_DRIVER":
			if versionRe.MatchString(v) {
				driver = v
			}
		case "GI_CUDA":
			cuda = v
		}
	}
	return
}

// installDriver 는 .run 을 -s 로 설치하고, 실패하면 서비스 중지·모듈 언로드 후 1회 재시도한다.
// 그래도 실패하면 lsof·설치 로그를 호스트 로그에 남기고 t.Fail 을 채운다.
func installDriver(cfg *Config, t *Target, l *hostLog) bool {
	run := remoteRunFile(cfg.AutofsPath, t.Version)
	if t.Method == methodScp {
		local := RunFile(cfg.AutofsPath, t.Version)
		l.say("scp: %s → %s/", local, remoteTmp)
		if _, rc := l.run(cfg, "임시 디렉토리", "mkdir -p "+remoteTmp, shortTimeout); rc != 0 {
			t.Fail = "mkdir " + remoteTmp + " failed"
			l.say("fail: %s", t.Fail)
			return false
		}
		if err := copyFile(local, t.Host, remoteTmp, installTimeout); err != nil {
			l.write("scp 오류: " + err.Error() + "\n")
			t.Fail = "scp failed"
			l.say("fail: scp 실패 (%v)", err)
			return false
		}
		run = path.Join(remoteTmp, path.Base(run))
		defer l.run(cfg, "설치파일 삭제", "rm -f "+shQuote(run), shortTimeout)
	}
	cmd := "sh " + shQuote(run) + " -s"
	l.say("드라이버 %s 설치 중 (%s)", t.Version, t.Method)
	if _, rc := l.run(cfg, "드라이버 설치", cmd, installTimeout); rc == 0 {
		return true
	}
	l.say("설치 실패 → nvidia 서비스 중지·모듈 언로드 후 1회 재시도")
	l.run(cfg, "서비스 중지·모듈 언로드", unloadScript, shortTimeout)
	if _, rc := l.run(cfg, "드라이버 설치(재시도)", cmd, installTimeout); rc == 0 {
		return true
	}
	l.run(cfg, "실패 진단", diagScript, shortTimeout)
	t.Fail = "install failed(retry after unload)"
	l.say("fail: 재시도도 실패했습니다. 점유 프로세스(lsof)와 설치 로그는 호스트 로그를 보세요")
	return false
}

// installHost 는 호스트 하나의 설치 작업이다. 결과는 t(Skip/Fail/NewDriver/NewCUDA/Dcgm)에 남긴다.
// t.DcgmOnly 면 드라이버는 건너뛰고 DCGM 단계만 한다.
func installHost(cfg *Config, t *Target) {
	l := openHostLog(cfg.LogDir, t.Host)
	defer l.close()
	if t.DcgmOnly {
		t.NewDriver, t.NewCUDA = t.Status.Driver, t.Status.CUDA
		l.say("드라이버 %s 는 같은 버전이라 건너뛰고 DCGM 만 맞춥니다", t.NewDriver)
		installDCGM(cfg, t, l)
		return
	}
	if !precheck(cfg, t, l) || !installDriver(cfg, t, l) {
		return
	}
	out, _ := l.run(cfg, "설치 확인", verifyScript, shortTimeout)
	t.NewDriver, t.NewCUDA = parseVerify(out)
	if t.NewDriver != t.Version {
		t.Fail = "version mismatch(" + dash(t.NewDriver) + ")"
		l.say("fail: 설치 후 nvidia-smi 버전이 %s 입니다 (요청 %s)", dash(t.NewDriver), t.Version)
		return
	}
	l.say("드라이버 %s 설치 완료 (CUDA %s)", t.NewDriver, dash(t.NewCUDA))
	installDCGM(cfg, t, l)
}
