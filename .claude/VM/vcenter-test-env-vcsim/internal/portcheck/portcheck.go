// Package portcheck는 vcsim이 뜰 고정 포트를 이미 쓰고 있는 프로세스가 있는지 확인하고,
// 있으면 사용자에게 보여준 뒤 종료할지 물어본다. vcsim은 항상 같은 포트(builder.Port)로
// 뜨게 고정돼 있어서, 이전 실행이 백그라운드에 남아있으면 "address already in use" panic이
// 난다(실제로 겪음: 6일 전에 nohup으로 띄워두고 잊어버린 vc-test-env 프로세스가 포트를 물고 있었음).
package portcheck

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var listenerRegex = regexp.MustCompile(`pid=(\d+)`)
var nameRegex = regexp.MustCompile(`\(\("([^"]+)"`)

// owner는 "ss -ltnp"로 지정한 포트를 점유 중인 프로세스를 찾는다.
// pid가 0이면 포트가 비어있거나(또는 ss 명령을 찾지 못해 확인 불가) 확인할 수 없었다는 뜻이다.
func owner(port string) (pid int, name string) {
	out, err := exec.Command("ss", "-ltnp").Output()
	if err != nil {
		return 0, ""
	}
	suffix := ":" + port
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[3], suffix) {
			continue
		}
		m := listenerRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		procName := "?"
		if nm := nameRegex.FindStringSubmatch(line); nm != nil {
			procName = nm[1]
		}
		return p, procName
	}
	return 0, ""
}

// EnsureFree는 port를 점유 중인 프로세스가 있으면 화면에 보여주고, 종료할지 사용자에게
// (y/N) 확인받는다. 이미 비어있으면 아무 것도 묻지 않고 즉시 반환한다.
func EnsureFree(port string) error {
	pid, name := owner(port)
	if pid == 0 {
		return nil
	}

	fmt.Printf("포트 %s를 이미 사용 중인 프로세스가 있습니다: PID %d (%s)\n", port, pid, name)
	fmt.Print("이 프로세스를 종료하고 계속하시겠습니까? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	resp, _ := reader.ReadString('\n')
	resp = strings.ToLower(strings.TrimSpace(resp))
	if resp != "y" && resp != "yes" {
		return fmt.Errorf("포트 %s가 사용 중이라 vcsim을 기동할 수 없습니다. 직접 종료하거나(kill %d) 나중에 다시 시도하세요", port, pid)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("PID %d 종료 실패: %w", pid, err)
	}

	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		if p, _ := owner(port); p == 0 {
			fmt.Printf("PID %d를 종료했습니다.\n", pid)
			return nil
		}
	}
	return fmt.Errorf("PID %d를 종료했지만 포트 %s가 아직 해제되지 않았습니다", pid, port)
}
