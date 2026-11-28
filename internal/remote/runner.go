package remote

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GosshNotFound 는 gossh 를 어디서도 찾지 못했을 때의 안내입니다.
const GosshNotFound = "gossh 를 찾지 못했습니다. -gossh <경로> 또는 환경변수 IP_CHANGE_GOSSH=<경로> 를 지정하십시오."

// gosshNames 는 찾을 실행 파일 이름입니다 (앞이 우선).
var gosshNames = []string{"gossh_os8", "gossh"}

// FindGossh 는 gossh 실행 파일을 찾습니다. 순서:
//  1. flagVal (-gossh)   2. 환경변수 IP_CHANGE_GOSSH
//  3. 이 실행 파일이 있는 디렉토리의 gossh_os8, gossh   4. PATH 의 gossh_os8, gossh
//
// 1, 2 로 지정한 경로가 틀렸으면 다음 단계로 넘어가지 않고 그 경로를 오류로 알립니다.
func FindGossh(flagVal string) (string, error) {
	if flagVal != "" {
		return checkGossh(flagVal)
	}
	if env := os.Getenv("IP_CHANGE_GOSSH"); env != "" {
		return checkGossh(env)
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		dir := filepath.Dir(exe)
		for _, n := range gosshNames {
			p := filepath.Join(dir, n)
			if isExecFile(p) {
				return p, nil
			}
		}
	}
	for _, n := range gosshNames {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", errors.New(GosshNotFound)
}

func checkGossh(p string) (string, error) {
	if strings.ContainsRune(p, os.PathSeparator) {
		if !isExecFile(p) {
			return "", fmt.Errorf("gossh 실행 파일이 없거나 실행할 수 없습니다: %s", p)
		}
		return p, nil
	}
	found, err := exec.LookPath(p)
	if err != nil {
		return "", fmt.Errorf("gossh 를 PATH 에서 찾지 못했습니다: %s", p)
	}
	return found, nil
}

func isExecFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// Runner 는 키 인증으로 먼저 실행하고, 키 인증이 실패한 호스트만
// (-p 일 때) 패스워드를 한 번 물어 다시 실행합니다 (수정계획서 4.11, D8-2).
type Runner struct {
	Opts        Options
	AskPassword bool                   // -p
	Prompt      func() (string, error) // 비어 있으면 /dev/tty 에서 에코 없이 읽음

	password string
	asked    bool
	askErr   error
}

// Run 은 hosts 에 script 를 실행해 호스트별 결과를 돌려줍니다.
// 키 인증이 되는 호스트는 한 번만 실행됩니다. 두 번째 실행의 결과는 호스트별로
// 첫 번째 결과를 대체하므로 같은 호스트가 두 번 나오지 않습니다.
func (r *Runner) Run(hosts []string, script string) ([]Result, string, error) {
	hostFile, cleanup, err := WriteHostFile(hosts)
	if err != nil {
		return nil, "", fmt.Errorf("호스트 목록 임시파일: %w", err)
	}
	defer cleanup()

	cmdline := BuildCommand(script, r.Opts)
	keyOpts := r.Opts
	keyOpts.Password = ""
	results, raw, runErr := Run(hostFile, cmdline, keyOpts)

	var failed []string
	for _, res := range results {
		if res.AuthFailed() {
			failed = append(failed, res.Host)
		}
	}
	if len(failed) == 0 || !r.AskPassword {
		return results, raw, runErr
	}

	// 모니터 화면이 /dev/tty 를 쓰고 있으면 먼저 내려야 패스워드를 입력받을 수 있습니다.
	r.Opts.Monitor.Stop()
	if r.password == "" && !r.asked {
		r.asked = true
		prompt := r.Prompt
		if prompt == nil {
			prompt = func() (string, error) {
				return ReadPassword(fmt.Sprintf("키 인증이 실패한 %d대의 SSH 패스워드: ", len(failed)))
			}
		}
		r.password, r.askErr = prompt()
	}
	if r.password == "" {
		msg := "패스워드를 입력받지 못했습니다"
		if r.askErr != nil {
			msg += ": " + r.askErr.Error()
		}
		for i := range results {
			if results[i].AuthFailed() {
				results[i].Note = msg
			}
		}
		return results, raw, runErr
	}

	hostFile2, cleanup2, err := WriteHostFile(failed)
	if err != nil {
		return results, raw, fmt.Errorf("호스트 목록 임시파일: %w", err)
	}
	defer cleanup2()
	pwOpts := r.Opts
	pwOpts.Password = r.password
	pwOpts.Monitor = nil
	results2, raw2, runErr2 := Run(hostFile2, cmdline, pwOpts)

	replace := map[string]Result{}
	for _, res := range results2 {
		res.PasswordTried = true
		replace[res.Host] = res
	}
	merged := make([]Result, 0, len(results))
	for _, res := range results {
		if n, ok := replace[res.Host]; ok {
			merged = append(merged, n)
			delete(replace, res.Host)
		} else if contains(failed, res.Host) {
			// 두 번째 실행에서 아무 출력도 없었던 호스트
			res.PasswordTried = true
			merged = append(merged, res)
		} else {
			merged = append(merged, res)
		}
	}
	for _, n := range replace {
		merged = append(merged, n)
	}
	if raw2 != "" {
		raw += "\n" + raw2
	}
	if runErr == nil {
		runErr = runErr2
	}
	return merged, raw, runErr
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
