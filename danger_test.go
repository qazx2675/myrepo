package main

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
)

func TestIsDangerousCommand(t *testing.T) {
	danger := []string{
		// 기존 7개(부분일치 유지)
		"reboot", "sudo reboot -f", "poweroff", "shutdown -h now", "halt", "init 0", "init 6", "ddc status",
		"REBOOT", "echo reboot-check", "cat /var/log/shutdown.log", "xhalt01",
		// A 즉시 재시작·정지
		"kexec -l /boot/vmlinuz", "sudo kexec -e",
		"systemctl kexec", "systemctl rescue", "systemctl emergency", "systemctl --no-block rescue",
		"systemctl isolate rescue.target", "systemctl isolate emergency.target", "systemctl isolate rescue",
		"init 1", "telinit 1", "sudo telinit 1",
		"echo b > /proc/sysrq-trigger", "echo 1 >/proc/sysrq-trigger",
		"ipmitool -H 10.0.0.1 -U a -P b power off", "ipmitool chassis power reset", "ipmitool -I lanplus power cycle",
		// B 디스크·데이터 파괴
		"mkfs.xfs /dev/sdb", "mkfs /dev/sdb", "mkfs.ext4 -F /dev/sdc1", "/usr/sbin/mkfs.xfs /dev/sdb",
		"sudo wipefs -a /dev/sdb", "fdisk /dev/sda", "parted /dev/sda mklabel gpt", "sgdisk -Z /dev/sdb", "shred -n3 /dev/sdb",
		"lvremove -f vg/lv", "vgremove vg", "pvremove /dev/sdb",
		"dd if=a of=/dev/sdb", "dd if=/dev/zero of=/dev/sda bs=1M", "sudo dd of=/dev/sdb if=x",
		"rm -rf /", "rm -rf /*", "sudo rm -rf /", "rm -r -f /", "rm -fr /", "rm --recursive --force /", "echo a; rm -rf /",
		// C 접속 끊김
		"ifdown eth0", "x; ifdown eth0", "ifdown eth0 && ifup eth0",
		"ip link set eth0 down", "ip -s link set dev eth0 down",
		"nmcli con down x", "nmcli device down eth0",
		"systemctl stop NetworkManager", "systemctl stop network", "systemctl stop sshd", "systemctl stop sshd.service",
		"systemctl --now stop sshd",
		// 명령 위치(구분자·인용 뒤·래퍼 뒤)
		"echo hi | xargs mkfs.xfs", "ls && (parted /dev/sda print)", "bash -c 'mkfs.xfs /dev/sdb'", `sh -c "wipefs -a /dev/sdb"`,
		"echo `fdisk -l`", "echo $(shred x)", "FOO=1 mkfs.xfs /dev/sdb", "a\nfdisk /dev/sda", "nohup sudo -n shred x",
	}
	safe := []string{
		"hostname", "ls -l /tmp", "df -h", "uptime", "cat /var/log/parted.log", "ls /sbin/parted", "echo mkfs",
		"departed01", "echo departed01", "cat /etc/fdisk.conf", "grep fdisk /var/log/messages", "echo shredder",
		"rm -rf /tmp/x", "rm -rf /var/tmp/a", "rm -rf /tmp/*", "rm -f /", "rm /", "rm -rf ./a",
		"dd if=/dev/zero of=/tmp/a bs=1M count=1", "dd if=/dev/sda of=/tmp/disk.img",
		"ip link show eth0", "ip addr", "ip link set eth0 up", "nmcli con show", "nmcli con up x",
		"systemctl status sshd", "systemctl stop httpd", "systemctl restart sshd", "systemctl start network",
		"systemctl isolate multi-user.target", "systemctl list-units",
		"ipmitool power status", "ipmitool chassis power status", "ipmitool sdr",
		"cat /proc/sysrq-trigger-x", "init 3", "telinit 3", "init 12x",
		"ifup eth0", "ifconfig", "sgdisk-doc", "mkfsx", "xmkfs.xfs",
		// base64/긴 영숫자 데이터에 글자가 우연히 섞인 경우
		"echo QUJDbWtmcy54ZnNmZGlza3BhcnRlZHdpcGVmcw== | base64 -d",
		"echo aGVsbG9ta2ZzYXNkZmRkcGFydGVkZmRpc2s9PQ+/mkfs/wipefs/parted/fdisk | base64 -d",
		"echo Zm9vbWtmcy54ZnM=mkfs.xfs",
	}
	for _, c := range danger {
		if !isDangerousCommand(c) {
			t.Errorf("위험이어야 하는데 정상 판정: %q", c)
		}
	}
	for _, c := range safe {
		if isDangerousCommand(c) {
			t.Errorf("정상이어야 하는데 위험 판정: %q", c)
		}
	}
}

func TestParseConfirmHosts(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []string
	}{
		{"구분자 혼합", []string{"a,b|c d\te\n"}, []string{"a", "b", "c", "d", "e"}},
		{"여러 줄", []string{"a b\n", "c\n"}, []string{"a", "b", "c"}},
		{"범위식", []string{"node[01-03],x\n"}, []string{"node01", "node02", "node03", "x"}},
		{"중복 제거", []string{"a,a|b a\n"}, []string{"a", "b"}},
		{"빈 입력", nil, nil},
		{"범위식 여러 개", []string{"h[1-2] g[08-10]\n"}, []string{"h1", "h2", "g08", "g09", "g10"}},
	}
	for _, c := range cases {
		got := parseConfirmHosts(c.lines)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestDiffHostSets(t *testing.T) {
	target := []string{"a", "b", "c"}
	cases := []struct {
		name               string
		input              []string
		wantIn, wantTarget []string
	}{
		{"완전일치(순서 다름)", []string{"c", "a", "b"}, nil, nil},
		{"하나 누락", []string{"a", "b"}, nil, []string{"c"}},
		{"하나 추가", []string{"a", "b", "c", "d"}, []string{"d"}, nil},
		{"교체", []string{"a", "b", "x"}, []string{"x"}, []string{"c"}},
		{"빈 입력", nil, nil, []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		in, tg := diffHostSets(target, c.input)
		if !reflect.DeepEqual(in, c.wantIn) || !reflect.DeepEqual(tg, c.wantTarget) {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.name, in, tg, c.wantIn, c.wantTarget)
		}
	}
	// 범위식 입력과 펼친 대상이 같으면 일치
	in, tg := diffHostSets([]string{"n01", "n02", "n03"}, parseConfirmHosts([]string{"n[01-03]\n", "n02\n"}))
	if in != nil || tg != nil {
		t.Errorf("범위식 일치 실패: %v %v", in, tg)
	}
}

func TestReadConfirmLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"빈 줄에서 종료", "a b\nc\n\nrest\n", []string{"a b\n", "c\n"}},
		{"공백만 있는 줄에서 종료", "a\n  \t\nrest\n", []string{"a\n"}},
		{"바로 빈 줄", "\ny\n", nil},
		{"EOF 종료", "a,b\nc\n", []string{"a,b\n", "c\n"}},
		{"개행 없는 EOF", "a,b", []string{"a,b"}},
		{"완전 빈 입력", "", nil},
	}
	for _, c := range cases {
		got := readConfirmLines(bufio.NewReader(strings.NewReader(c.in)))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	// 빈 줄 뒤의 입력(y/N 응답)은 남아 있어야 한다
	r := bufio.NewReader(strings.NewReader("a\n\ny\n"))
	readConfirmLines(r)
	if s, _ := r.ReadString('\n'); s != "y\n" {
		t.Errorf("대조 뒤 y/N 응답이 남아 있어야 함: %q", s)
	}
}
