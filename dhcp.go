// dhcp.go - DHCP 조회 (d 키, 계획서 4.5). config_check 방식 그대로:
// ping X 서버 목록을 stdin 으로 dhcp_server 에 넘겨 /root/a.sh 를 한 번 실행하고 출력을 돌려준다.
// 로컬과 원격에 파일을 남기지 않는다(원격 임시 목록은 같은 명령 안에서 지운다).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strings"
	"time"
)

// sshBin 은 ssh 실행 파일이다(테스트에서 스텁으로 바꾼다).
var sshBin = "ssh"

const (
	dhcpNoServer = "dhcp_server 미설정"
	dhcpNoHosts  = "ping X 서버가 없습니다."
	dhcpTimeout  = 2 * time.Minute
)

// currentUser 는 원격 임시 파일 이름에 붙일 현재 사용자 이름이다(config_check 의 ${user} 에 해당).
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "root"
}

// dhcpRemoteCmd 는 dhcp_server 에서 실행할 명령이다(config_check 와 같음).
func dhcpRemoteCmd(userName string) string {
	f := "/root/off_server_list_" + userName
	return "cat > " + f + "; bash /root/a.sh; rm -f " + f
}

// DHCPQuery 는 hosts(ping X 서버)를 dhcp_server 로 보내 나온 출력을 돌려준다.
func DHCPQuery(cfg *Config, hosts []string) (string, error) {
	if cfg.DHCPServer == "" {
		return dhcpNoServer, nil
	}
	if len(hosts) == 0 {
		return dhcpNoHosts, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), dhcpTimeout)
	defer cancel()
	args := []string{"-o", "ConnectTimeout=10", cfg.DHCPServer, dhcpRemoteCmd(currentUser())}
	so, se, err := runNice(ctx, sshBin, args, strings.NewReader(strings.Join(hosts, "\n")+"\n"))
	out := strings.TrimRight(string(so), "\r\n")
	if e := strings.TrimRight(string(se), "\r\n"); e != "" {
		if out != "" {
			out += "\n"
		}
		out += e
	}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return "", err
		}
		if out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("(ssh 종료 코드 %d)", ee.ExitCode())
	}
	if out == "" {
		out = "(출력 없음)"
	}
	return out, nil
}

// pingXHosts 는 스냅샷에서 ping X 서버를 이름 순으로 돌려준다.
func pingXHosts(snap *Snapshot) []string {
	var out []string
	for _, r := range snap.Records {
		if r.Status == StatusPingX {
			out = append(out, r.Name)
		}
	}
	sort.Strings(out)
	return out
}

// DHCPReport 는 `d` 패널 본문이다(현재 스냅샷의 ping X 서버로 조회한다).
func (c *Collector) DHCPReport() string {
	out, err := DHCPQuery(c.cfg, pingXHosts(c.Snapshot()))
	if err != nil {
		return "DHCP 조회 실패: " + err.Error()
	}
	return out
}

// Report 는 d·l 패널 조회를 실행한다(reportSource).
func (c *Collector) Report(kind rune) string {
	switch kind {
	case panelDHCP:
		return c.DHCPReport()
	case panelLDAP:
		return c.LDAPReport()
	}
	return ""
}

// ReportBusy 는 조회 중에 패널에 보일 안내다(reportSource).
func (c *Collector) ReportBusy(kind rune) string {
	if kind == panelLDAP {
		return fmt.Sprintf("LDAP 점검중... (%d대)", c.LDAPPending())
	}
	return "DHCP 조회중..."
}
