package render

// 수정계획서 6.1 의 시험표 T1 ~ T17 과 보조 시험.
// 모든 시험은 ROOT 임시 디렉토리와 가짜 ip/hostname/nmcli 로만 실행합니다 (harness_test.go).

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// ---- 시험 자료 ----

// Rocky 8 고정 IP ifcfg (T1)
const ifcfgStatic = "TYPE=Ethernet\nBOOTPROTO=none\nNAME=ens160\nUUID=1b8c1a2e-5d4f-4c53-9b7a-0f3d2e8a6c11\nDEVICE=ens160\nONBOOT=yes\nIPADDR=192.168.0.58\nPREFIX=24\nGATEWAY=192.168.0.1\nDNS1=8.8.8.8\n"

const ifcfgStaticWant = "TYPE=Ethernet\nBOOTPROTO=none\nNAME=ens160\nUUID=1b8c1a2e-5d4f-4c53-9b7a-0f3d2e8a6c11\nDEVICE=ens160\nONBOOT=yes\nIPADDR=172.16.5.58\nPREFIX=24\nGATEWAY=172.16.5.1\nDNS1=8.8.8.8\n"

// RHEL 8 DHCP ifcfg (T2). IPADDR 가 없고 IPv6/DHCP 전용 줄이 있다.
const ifcfgDHCP = "TYPE=Ethernet\nPROXY_METHOD=none\nBROWSER_ONLY=no\nBOOTPROTO=dhcp\nDEFROUTE=yes\nIPV4_FAILURE_FATAL=no\nIPV6INIT=yes\nIPV6_AUTOCONF=yes\nIPV6_DEFROUTE=yes\nIPV6_FAILURE_FATAL=no\nNAME=ens160\nUUID=1b8c1a2e-5d4f-4c53-9b7a-0f3d2e8a6c11\nDEVICE=ens160\nONBOOT=yes\n"

const ifcfgDHCPWant = "TYPE=Ethernet\nPROXY_METHOD=none\nBROWSER_ONLY=no\nBOOTPROTO=none\nIPADDR=172.16.5.58\nNETMASK=255.255.255.0\nGATEWAY=172.16.5.1\nDEFROUTE=yes\nIPV4_FAILURE_FATAL=no\nIPV6INIT=yes\nIPV6_AUTOCONF=yes\nIPV6_DEFROUTE=yes\nIPV6_FAILURE_FATAL=no\nNAME=ens160\nUUID=1b8c1a2e-5d4f-4c53-9b7a-0f3d2e8a6c11\nDEVICE=ens160\nONBOOT=yes\n"

const kfHead = "[connection]\nid=ens160\nuuid=0d1e2f3a-4b5c-4d6e-8f70-112233445566\ntype=ethernet\ninterface-name=ens160\n\n[ethernet]\nmac-address=00:0C:29:AA:BB:CC\n\n"
const kfTail = "\n[ipv6]\naddr-gen-mode=eui64\nmethod=auto\n\n[proxy]\n"

// 고정 keyfile: address1=IP/24,GW (T7)
const kfStatic = kfHead + "[ipv4]\naddress1=192.168.0.58/24,192.168.0.1\ndns=8.8.8.8;\nmethod=manual\n" + kfTail
const kfStaticWant = kfHead + "[ipv4]\naddress1=172.16.5.58/24,172.16.5.1\ndns=8.8.8.8;\nmethod=manual\n" + kfTail

// 고정 keyfile: address1 과 gateway 가 따로 (T7 변형)
const kfStaticSep = kfHead + "[ipv4]\naddress1=192.168.0.58/24\ndns=8.8.8.8;\ngateway=192.168.0.1\nmethod=manual\n" + kfTail
const kfStaticSepWant = kfHead + "[ipv4]\naddress1=172.16.5.58/24\ndns=8.8.8.8;\ngateway=172.16.5.1\nmethod=manual\n" + kfTail

// DHCP keyfile (T8)
const kfDHCP = kfHead + "[ipv4]\nmethod=auto\n" + kfTail
const kfDHCPWant = kfHead + "[ipv4]\nmethod=manual\naddress1=172.16.5.58/24\ngateway=172.16.5.1\n" + kfTail

const nmUUID = "1b8c1a2e-5d4f-4c53-9b7a-0f3d2e8a6c11"

func backupPath(stamp, nodePath string) string { return backupBase + "/" + stamp + nodePath }

func manifestOf(l *lab, stamp string) string { return l.read(backupBase + "/" + stamp + "/manifest") }

func backupDirs(t *testing.T, l *lab) []string {
	t.Helper()
	ents, err := os.ReadDir(l.abs(backupBase))
	if err != nil {
		return nil
	}
	var d []string
	for _, e := range ents {
		if e.IsDir() {
			d = append(d, e.Name())
		}
	}
	return d
}

func mustOK(t *testing.T, code int, out string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("스크립트가 실패했습니다 (종료 코드 %d):\n%s", code, out)
	}
}

// ---- T1 ----

// T1: 고정 IP ifcfg. IPADDR 와 GATEWAY 만 바뀌고 나머지 줄은 그대로. NetworkManager 실행 중이면
// nmcli 조회(읽기 전용)로 같은 파일을 찾는다. 백업과 manifest 가 남는다.
func TestT1StaticIfcfg(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.setNM("ens160", nmUUID, ifcfgPath)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "Rocky 8.10", ifcfgPath)
	stamp := stampOf(t, r)
	wantText(t, r.note(), "재부팅 전 nmcli 로 이 프로필을 건드리지 마십시오; BACKUP="+stamp, "비고")
	wantText(t, l.read(ifcfgPath), ifcfgStaticWant, "바뀐 ifcfg")
	if l.perm(ifcfgPath) != 0o644 {
		t.Errorf("권한이 바뀌었습니다: %v", l.perm(ifcfgPath))
	}
	wantText(t, l.read(backupPath(stamp, ifcfgPath)), ifcfgStatic, "백업 파일")
	wantText(t, manifestOf(l, stamp), "modified "+ifcfgPath+"\n", "manifest")
	if p := l.perm(backupBase + "/" + stamp); p != 0o700 {
		t.Errorf("백업 디렉토리 권한 = %v, 기대 0700", p)
	}
}

// ---- T2 ----

// T2: DHCP ifcfg (IPADDR 없음). BOOTPROTO=none, IPADDR, NETMASK(점 표기), GATEWAY 가
// BOOTPROTO 바로 아래에 생기고 IPv6 와 DHCP 전용 줄은 그대로 남는다.
func TestT2DHCPIfcfg(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgDHCP, 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "dhcp->static", "Rocky 8.10", ifcfgPath)
	wantText(t, l.read(ifcfgPath), ifcfgDHCPWant, "바뀐 ifcfg")
	stamp := stampOf(t, r)
	wantText(t, l.read(backupPath(stamp, ifcfgPath)), ifcfgDHCP, "백업 파일")
}

// ---- T3 ----

// T3: 줄 끝이 CRLF 인 ifcfg. 고쳐지고 결과는 LF 만 있다.
func TestT3CRLF(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, strings.ReplaceAll(ifcfgStatic, "\n", "\r\n"), 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "Rocky 8.10", ifcfgPath)
	got := l.read(ifcfgPath)
	if strings.Contains(got, "\r") {
		t.Errorf("결과 파일에 CR 이 남아 있습니다: %q", got)
	}
	wantText(t, got, ifcfgStaticWant, "바뀐 ifcfg")
}

// ---- T4 ----

// T4: bond0 (포트 있음, 고정). ifcfg-bond0 만 바뀌고 포트 파일은 그대로.
func TestT4BondStatic(t *testing.T) {
	l := newLab(t)
	l.setAddr(addrLine("bond0", oldIP, "24"))
	l.setRoute("default via 192.168.0.1 dev bond0 proto static metric 300")
	l.setLink("bond0", macEns, true, "bond mode 802.3ad")
	bond := "DEVICE=bond0\nNAME=bond0\nTYPE=Bond\nBONDING_MASTER=yes\nBONDING_OPTS=\"mode=802.3ad miimon=100\"\nBOOTPROTO=none\nONBOOT=yes\nIPADDR=192.168.0.58\nPREFIX=24\nGATEWAY=192.168.0.1\n"
	port := func(n string) string {
		return "DEVICE=" + n + "\nNAME=" + n + "\nTYPE=Ethernet\nONBOOT=yes\nBOOTPROTO=none\nMASTER=bond0\nSLAVE=yes\n"
	}
	l.put(legacyDir+"/ifcfg-bond0", bond, 0o644)
	l.put(legacyDir+"/ifcfg-ens1", port("ens1"), 0o644)
	l.put(legacyDir+"/ifcfg-ens2", port("ens2"), 0o644)
	before := l.snapshot(true, false)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "bond0", "static", "Rocky 8.10", legacyDir+"/ifcfg-bond0")
	wantText(t, l.read(legacyDir+"/ifcfg-bond0"),
		strings.NewReplacer("IPADDR=192.168.0.58", "IPADDR=172.16.5.58", "GATEWAY=192.168.0.1", "GATEWAY=172.16.5.1").Replace(bond), "bond0")

	after := l.snapshot(true, false)
	changed := diffSnap(before, after)
	if len(changed) != 1 || changed[0] != "바뀜: etc/sysconfig/network-scripts/ifcfg-bond0" {
		t.Errorf("ifcfg-bond0 만 바뀌어야 합니다: %v", changed)
	}
	wantText(t, manifestOf(l, stampOf(t, r)), "modified "+legacyDir+"/ifcfg-bond0\n", "manifest")
}

// ---- T5 ----

// T5: usb0 도 IPv4 를 가졌지만 SSH 접속 IP 는 ens160 에 있다. ens160 만 바뀐다.
func TestT5Usb0Ignored(t *testing.T) {
	l := newLab(t)
	l.setAddr("2: usb0    inet 192.168.7.2/24 brd 192.168.7.255 scope global noprefixroute usb0\\       valid_lft forever preferred_lft forever",
		addrLine("ens160", oldIP, "24"))
	usb := "DEVICE=usb0\nNAME=usb0\nTYPE=Ethernet\nONBOOT=yes\nBOOTPROTO=none\nIPADDR=192.168.7.2\nPREFIX=24\n"
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.put(legacyDir+"/ifcfg-usb0", usb, 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "Rocky 8.10", ifcfgPath)
	wantText(t, l.read(ifcfgPath), ifcfgStaticWant, "ens160")
	wantText(t, l.read(legacyDir+"/ifcfg-usb0"), usb, "usb0 은 그대로여야 합니다")
}

// SSH 접속 IP 를 가진 장치가 없으면 기본 경로 장치를 쓰고 그 사실을 비고에 남긴다.
func TestSSHIPWithoutDeviceFallsBackToDefaultRoute(t *testing.T) {
	l := newLab(t)
	l.env["SSH_CONNECTION"] = "192.168.0.10 51234 10.99.99.99 22"
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "Rocky 8.10", ifcfgPath)
	if !strings.Contains(r.note(), "SSH 접속 IP(10.99.99.99)를 가진 장치가 없어 기본 경로 장치를 사용") {
		t.Errorf("비고: %q", r.note())
	}
}

// ---- T6 ----

// T6: 이미 바뀐 노드에서 다시 실행하면 "이미 바뀜" 이고 새 백업을 만들지 않는다.
func TestT6SecondRunAlreadyChanged(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	first := applyResult(t, out)
	stamp := stampOf(t, first)
	dirs1, man1 := backupDirs(t, l), manifestOf(l, stamp)
	file1 := l.read(ifcfgPath)

	out, code = l.apply(ApplyOptions{})
	mustOK(t, code, out)
	second := applyResult(t, out)
	wantResult(t, second, "OK", "ens160", "already", "Rocky 8.10", ifcfgPath)
	if strings.Contains(second.note(), "BACKUP=") {
		t.Errorf("두 번째 실행이 백업을 만들었습니다: %q", second.note())
	}
	if d2 := backupDirs(t, l); strings.Join(d2, ",") != strings.Join(dirs1, ",") {
		t.Errorf("백업 디렉토리가 늘었습니다: %v -> %v", dirs1, d2)
	}
	wantText(t, manifestOf(l, stamp), man1, "manifest 는 그대로여야 합니다")
	wantText(t, l.read(ifcfgPath), file1, "파일은 그대로여야 합니다")
}

// ---- T7 ----

// T7: 고정 keyfile (address1=IP/24,GW). address1 줄만 바뀌고 권한 600 이 유지된다.
func TestT7StaticKeyfile(t *testing.T) {
	l := newLab(t)
	l.setOS(osRHEL9)
	l.put(kfPath, kfStatic, 0o600)
	l.setNM("ens160", nmUUID, kfPath)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "RHEL 9.4", kfPath)
	wantText(t, l.read(kfPath), kfStaticWant, "바뀐 keyfile")
	if p := l.perm(kfPath); p != 0o600 {
		t.Errorf("keyfile 권한 = %v, 기대 0600", p)
	}
	stamp := stampOf(t, r)
	if p := l.perm(backupPath(stamp, kfPath)); p != 0o600 {
		t.Errorf("백업 keyfile 권한 = %v, 기대 0600", p)
	}
}

// T7 변형: address1 과 gateway 가 따로 있는 keyfile. 둘 다 제자리에서 바뀐다.
func TestT7StaticKeyfileSeparateGateway(t *testing.T) {
	l := newLab(t)
	l.setOS(osRHEL9)
	l.put(kfPath, kfStaticSep, 0o600)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "RHEL 9.4", kfPath)
	wantText(t, l.read(kfPath), kfStaticSepWant, "바뀐 keyfile")
	if p := l.perm(kfPath); p != 0o600 {
		t.Errorf("keyfile 권한 = %v, 기대 0600", p)
	}
}

// ---- T8 ----

// T8: DHCP keyfile (method=auto). method=manual, address1, 따로 gateway= 가 생긴다.
func TestT8DHCPKeyfile(t *testing.T) {
	l := newLab(t)
	l.setOS(osRHEL9)
	l.put(kfPath, kfDHCP, 0o600)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "dhcp->static", "RHEL 9.4", kfPath)
	wantText(t, l.read(kfPath), kfDHCPWant, "바뀐 keyfile")
	if p := l.perm(kfPath); p != 0o600 {
		t.Errorf("keyfile 권한 = %v, 기대 0600", p)
	}
}

// ---- T9 ----

// T9: 파일이 없고 Ethernet, RHEL 8. 새 ifcfg-ens160 을 만든다. 만든 파일은 manifest 에 created 로 남는다.
func TestT9NoFileEthernetRHEL8(t *testing.T) {
	l := newLab(t)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "new-file", "Rocky 8.10", ifcfgPath)
	want := join("DEVICE=ens160", "NAME=ens160", "TYPE=Ethernet", "ONBOOT=yes", "BOOTPROTO=none",
		"IPADDR=172.16.5.58", "NETMASK=255.255.255.0", "GATEWAY=172.16.5.1")
	wantText(t, l.read(ifcfgPath), want, "새 ifcfg")
	wantText(t, manifestOf(l, stampOf(t, r)), "created "+ifcfgPath+"\n", "manifest")
}

// ---- T10 ----

const bond0Main = "[connection]\nid=bond0\ntype=bond\ninterface-name=bond0\nautoconnect=true\n\n[bond]\nmode=802.3ad\nmiimon=100\n\n[ipv4]\nmethod=manual\naddress1=172.16.5.58/24\ngateway=172.16.5.1\n\n[ipv6]\nmethod=ignore\n"

func bond0Port(n string) string {
	return "[connection]\nid=bond0-port-" + n + "\ntype=ethernet\ninterface-name=" + n + "\ncontroller=bond0\nport-type=bond\nautoconnect=true\n"
}

func setupBondNoFile(l *lab) {
	l.setOS(osRHEL10)
	l.setAddr(addrLine("bond0", oldIP, "24"))
	l.setRoute("default via 192.168.0.1 dev bond0 proto static metric 300")
	l.setLink("bond0", macEns, true, "bond mode 802.3ad")
	l.put("/sys/class/net/bond0/bonding/slaves", "ens1 ens2\n", 0o644)
	l.put("/sys/class/net/bond0/bonding/mode", "802.3ad 4\n", 0o644)
}

// T10: 파일이 없고 bond0, RHEL 10, -bond-mode lacp. 새 bond0 keyfile 과 포트 keyfile 이 802.3ad 로 생긴다.
func TestT10NoFileBondRHEL10LACP(t *testing.T) {
	l := newLab(t)
	setupBondNoFile(l)

	out, code := l.apply(ApplyOptions{BondMode: "lacp"})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "bond0", "new-file", "RHEL 10.0", keyfileDir+"/bond0.nmconnection")
	stamp := stampOf(t, r)
	wantText(t, r.note(), "포트 파일 생성: ens1 ens2; BACKUP="+stamp, "비고")
	wantText(t, l.read(keyfileDir+"/bond0.nmconnection"), bond0Main, "bond0 keyfile")
	for _, p := range []string{"ens1", "ens2"} {
		f := keyfileDir + "/bond0-port-" + p + ".nmconnection"
		wantText(t, l.read(f), bond0Port(p), p+" 포트 keyfile")
		if m := l.perm(f); m != 0o600 {
			t.Errorf("%s 권한 = %v, 기대 0600", f, m)
		}
	}
	if m := l.perm(keyfileDir + "/bond0.nmconnection"); m != 0o600 {
		t.Errorf("bond0 keyfile 권한 = %v, 기대 0600", m)
	}
	wantText(t, manifestOf(l, stamp),
		"created "+keyfileDir+"/bond0.nmconnection\ncreated "+keyfileDir+"/bond0-port-ens1.nmconnection\ncreated "+keyfileDir+"/bond0-port-ens2.nmconnection\n", "manifest")
}

// 새 bond 프로필의 모드(기본 active-backup)가 지금 bond 모드(802.3ad)와 다르면 WARN.
func TestNewBondModeMismatchWarns(t *testing.T) {
	l := newLab(t)
	setupBondNoFile(l)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	if r.status() != "WARN" || !strings.Contains(r.note(), "bond 모드 불일치 (현재 802.3ad, 파일 active-backup)") {
		t.Errorf("WARN 이어야 합니다: %v", r.f)
	}
	if got := l.read(keyfileDir + "/bond0.nmconnection"); !strings.Contains(got, "mode=active-backup\n") {
		t.Errorf("active-backup 모드가 아닙니다:\n%s", got)
	}
}

// ---- T11 / T12 ----

// T11: 한 장치에 파일이 둘. 부팅 때 시작하는 다른 파일은 "경쟁 프로필" WARN.
func TestT11CompetingProfile(t *testing.T) {
	l := newLab(t)
	other := legacyDir + "/ifcfg-Wired_connection_1"
	otherBody := "TYPE=Ethernet\nBOOTPROTO=dhcp\nNAME=\"Wired connection 1\"\nDEVICE=ens160\nONBOOT=yes\n"
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.put(other, otherBody, 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	if r.status() != "WARN" || r.file() != ifcfgPath {
		t.Fatalf("WARN 이고 대상은 IP 가 든 파일이어야 합니다: %v", r.f)
	}
	if !strings.HasPrefix(r.note(), "경쟁 프로필: "+other) {
		t.Errorf("비고: %q", r.note())
	}
	wantText(t, l.read(ifcfgPath), ifcfgStaticWant, "대상 파일")
	wantText(t, l.read(other), otherBody, "경쟁 파일은 건드리지 않습니다")
}

// 옛 ".bak.<STAMP>" 가 설정 디렉토리에 ONBOOT=yes 로 남아 있어도 경쟁 프로필로 경고한다.
func TestCompetingProfileOldBakInConfigDir(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.put(ifcfgPath+".bak.20250101000000", ifcfgStatic, 0o644)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	if r.status() != "WARN" || !strings.Contains(r.note(), "경쟁 프로필: "+ifcfgPath+".bak.20250101000000") {
		t.Errorf("옛 백업이 경쟁 프로필로 보고되어야 합니다: %v", r.f)
	}
}

// T12: ONBOOT=no 이면 고치되 WARN.
func TestT12OnbootNo(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, strings.Replace(ifcfgStatic, "ONBOOT=yes", "ONBOOT=no", 1), 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	if r.status() != "WARN" || !strings.HasPrefix(r.note(), "부팅 시 시작하지 않는 프로필 (ONBOOT=no 또는 autoconnect=false)") {
		t.Errorf("WARN 이어야 합니다: %v", r.f)
	}
	wantText(t, l.read(ifcfgPath), strings.Replace(ifcfgStaticWant, "ONBOOT=yes", "ONBOOT=no", 1), "바뀐 ifcfg")
}

// keyfile 의 autoconnect=false 도 같은 WARN.
func TestAutoconnectFalseKeyfileWarns(t *testing.T) {
	l := newLab(t)
	l.setOS(osRHEL9)
	l.put(kfPath, strings.Replace(kfStatic, "interface-name=ens160\n", "interface-name=ens160\nautoconnect=false\n", 1), 0o600)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	if r := applyResult(t, out); r.status() != "WARN" || !strings.Contains(r.note(), "부팅 시 시작하지 않는 프로필") {
		t.Errorf("WARN 이어야 합니다: %v", r.f)
	}
}

// 링크가 내려가 있으면 WARN.
func TestLinkDownWarns(t *testing.T) {
	l := newLab(t)
	l.setLink("ens160", macEns, false)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	if r := applyResult(t, out); r.status() != "WARN" || !strings.Contains(r.note(), "link down") {
		t.Errorf("WARN(link down) 이어야 합니다: %v", r.f)
	}
}

// ---- T13 ----

const sysconfigNetwork = "NETWORKING=yes\nHOSTNAME=node01\nGATEWAY=192.168.0.1\n"
const routeEns160 = "10.20.0.0/16 via 192.168.0.1 dev ens160\n"

// T13: /etc/sysconfig/network 과 route-ens160 에 예전 게이트웨이가 남아 있으면 WARN 과 GWFIX 표시.
// 이어서 FixGatewayScript 가 apply 와 같은 STAMP 백업에 넣고 고친다.
func TestT13OldGatewayWarnAndFix(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.put("/etc/sysconfig/network", sysconfigNetwork, 0o644)
	l.put(legacyDir+"/route-ens160", routeEns160, 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	stamp := stampOf(t, r)
	gwfix := "/etc/sysconfig/network," + legacyDir + "/route-ens160"
	wantText(t, r.note(),
		"예전 게이트웨이(192.168.0.1)가 남아 있음; OLDGW=192.168.0.1; GWFIX="+gwfix+"; BACKUP="+stamp, "비고")
	if r.status() != "WARN" {
		t.Fatalf("WARN 이어야 합니다: %v", r.f)
	}
	// apply 는 이 두 파일을 건드리지 않는다
	wantText(t, l.read("/etc/sysconfig/network"), sysconfigNetwork, "apply 가 network 를 바꿨습니다")

	fixOut, code := l.fixGateway(GatewayFix{Host: testHost, OldGW: "192.168.0.1", NewGW: newGW,
		Files: strings.Split(gwfix, ","), Stamp: stamp})
	mustOK(t, code, fixOut)
	fr := parseResult(t, fixOut)
	wantText(t, strings.Join(fr.f, "|"), "RESULT|OK|"+testHost+"|"+gwfix+"|"+stamp, "고침 결과")
	wantText(t, l.read("/etc/sysconfig/network"), "NETWORKING=yes\nHOSTNAME=node01\nGATEWAY=172.16.5.1\n", "network")
	wantText(t, l.read(legacyDir+"/route-ens160"), "10.20.0.0/16 via 172.16.5.1 dev ens160\n", "route")
	// 같은 STAMP 디렉토리에 백업과 manifest
	wantText(t, l.read(backupPath(stamp, "/etc/sysconfig/network")), sysconfigNetwork, "network 백업")
	wantText(t, l.read(backupPath(stamp, legacyDir+"/route-ens160")), routeEns160, "route 백업")
	wantText(t, manifestOf(l, stamp),
		"modified "+ifcfgPath+"\nmodified /etc/sysconfig/network\nmodified "+legacyDir+"/route-ens160\n", "manifest")

	// 다시 고치면 바꿀 것이 없다: 파일과 STAMP 가 빈 칸, 백업도 그대로
	again, code := l.fixGateway(GatewayFix{Host: testHost, OldGW: "192.168.0.1", NewGW: newGW,
		Files: strings.Split(gwfix, ","), Stamp: stamp})
	mustOK(t, code, again)
	wantText(t, strings.Join(parseResult(t, again).f, "|"), "RESULT|OK|"+testHost+"||", "두 번째 고침 결과")
}

// 고칠 수 없는 파일을 넘기면 아무것도 쓰기 전에 거절한다.
func TestFixGatewayRefusesOtherFiles(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.put("/etc/sysconfig/network", sysconfigNetwork, 0o644)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	stamp := stampOf(t, applyResult(t, out))
	before := l.snapshot(false, false)

	fo, code := l.fixGateway(GatewayFix{Host: testHost, OldGW: "192.168.0.1", NewGW: newGW,
		Files: []string{"/etc/sysconfig/network", ifcfgPath}, Stamp: stamp})
	if code == 0 || !strings.HasPrefix(fo, "RESULT|FAIL|"+testHost+"|고칠 수 없는 파일입니다") {
		t.Fatalf("거절해야 합니다 (코드 %d): %s", code, fo)
	}
	wantSameTree(t, before, l.snapshot(false, false), "거절한 뒤")

	fo, code = l.fixGateway(GatewayFix{Host: testHost, OldGW: "192.168.0.1", NewGW: newGW,
		Files: []string{"/etc/sysconfig/network"}, Stamp: "20200101000000"})
	if code == 0 || !strings.Contains(fo, "apply 백업 STAMP 디렉토리가 없습니다") {
		t.Fatalf("STAMP 디렉토리가 없으면 FAIL 이어야 합니다 (코드 %d): %s", code, fo)
	}
}

// ---- T14 ----

// T14: -check 는 PLAN 과 CHECK 줄만 내고 아무 파일도 쓰지 않는다 (디렉토리 트리 비교).
func TestT14CheckWritesNothing(t *testing.T) {
	cases := []struct {
		name  string
		setup func(l *lab)
		mode  string
		plan  []string
		file  string
	}{
		{"DHCP ifcfg", func(l *lab) { l.put(ifcfgPath, ifcfgDHCP, 0o644) }, "dhcp->static",
			[]string{"PLAN|" + ifcfgPath + "|BOOTPROTO=none", "PLAN|" + ifcfgPath + "|IPADDR=172.16.5.58",
				"PLAN|" + ifcfgPath + "|NETMASK=255.255.255.0", "PLAN|" + ifcfgPath + "|GATEWAY=172.16.5.1"}, ifcfgPath},
		{"고정 keyfile", func(l *lab) { l.setOS(osRHEL9); l.put(kfPath, kfStatic, 0o600) }, "static",
			[]string{"PLAN|" + kfPath + "|address1=172.16.5.58/24,172.16.5.1"}, kfPath},
		{"파일 없음", func(l *lab) {}, "new-file",
			[]string{"PLAN|" + ifcfgPath + "|DEVICE=ens160", "PLAN|" + ifcfgPath + "|IPADDR=172.16.5.58"}, ifcfgPath},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newLab(t)
			c.setup(l)
			before := l.snapshot(false, true)
			out, code := l.apply(ApplyOptions{CheckOnly: true})
			mustOK(t, code, out)
			r := applyResult(t, out)
			wantResult(t, r, "CHECK", "ens160", c.mode, r.osLabel(), c.file)
			for _, p := range c.plan {
				if !strings.Contains("\n"+out, "\n"+p+"\n") {
					t.Errorf("PLAN 줄이 없습니다: %s\n%s", p, out)
				}
			}
			if strings.Contains(r.note(), "BACKUP=") {
				t.Errorf("-check 가 백업을 만들었다고 보고했습니다: %q", r.note())
			}
			wantSameTree(t, before, l.snapshot(false, true), "-check 뒤")
			if l.exists("/var/lib/ip_change") {
				t.Errorf("-check 가 /var/lib/ip_change 를 만들었습니다")
			}
		})
	}
}

// bond 새 프로필도 -check 에서는 PLAN 만 낸다.
func TestCheckNewBondWritesNothing(t *testing.T) {
	l := newLab(t)
	setupBondNoFile(l)
	before := l.snapshot(false, true)
	out, code := l.apply(ApplyOptions{CheckOnly: true, BondMode: "lacp"})
	mustOK(t, code, out)
	r := applyResult(t, out)
	if r.status() != "CHECK" {
		t.Fatalf("CHECK 이어야 합니다: %v", r.f)
	}
	for _, p := range []string{
		"PLAN|" + keyfileDir + "/bond0.nmconnection|mode=802.3ad",
		"PLAN|" + keyfileDir + "/bond0-port-ens1.nmconnection|controller=bond0",
		"PLAN|" + keyfileDir + "/bond0-port-ens2.nmconnection|controller=bond0",
	} {
		if !strings.Contains(out, p+"\n") {
			t.Errorf("PLAN 줄이 없습니다: %s", p)
		}
	}
	wantSameTree(t, before, l.snapshot(false, true), "-check 뒤")
}

// ---- T15 ----

// T15: 바꾼 파일(modified)과 고친 파일의 롤백. 변경 전 파일과 같아지고 ROLLED_BACK 표시가 남는다.
func TestT15RollbackModified(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	l.put("/etc/sysconfig/network", sysconfigNetwork, 0o644)
	l.put(legacyDir+"/route-ens160", routeEns160, 0o644)
	before := l.snapshot(true, false)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	stamp := stampOf(t, applyResult(t, out))
	fo, code := l.fixGateway(GatewayFix{Host: testHost, OldGW: "192.168.0.1", NewGW: newGW,
		Files: []string{"/etc/sysconfig/network", legacyDir + "/route-ens160"}, Stamp: stamp})
	mustOK(t, code, fo)
	if len(diffSnap(before, l.snapshot(true, false))) != 3 {
		t.Fatalf("세 파일이 바뀌어 있어야 합니다")
	}

	ro, code := l.rollback("")
	mustOK(t, code, ro)
	rr := parseResult(t, ro)
	if len(rr.f) != 7 || rr.f[1] != "OK" || rr.f[3] != stamp || rr.f[4] != "3" || rr.f[5] != "0" {
		t.Fatalf("롤백 결과: %v", rr.f)
	}
	wantSameTree(t, before, l.snapshot(true, false), "롤백 뒤")
	if !l.exists(backupBase + "/" + stamp + "/ROLLED_BACK") {
		t.Errorf("ROLLED_BACK 표시가 없습니다")
	}

	// 같은 STAMP 를 다시 되돌릴 수 없다
	ro, code = l.rollback(stamp)
	if code == 0 || !strings.Contains(ro, "이미 롤백했습니다") {
		t.Errorf("이미 롤백한 STAMP 는 FAIL 이어야 합니다 (코드 %d): %s", code, ro)
	}
	// 가장 최근 선택에서는 건너뛰어 "복원할 백업이 없습니다" 가 된다
	ro, code = l.rollback("")
	if code == 0 || !strings.HasPrefix(ro, "RESULT|FAIL|"+testHost+"|") {
		t.Errorf("롤백할 백업이 더 없으면 FAIL 이어야 합니다 (코드 %d): %s", code, ro)
	}
	wantSameTree(t, before, l.snapshot(true, false), "두 번째 롤백 시도 뒤")
}

// T15: 새로 만든 파일(created)의 롤백. 만든 파일은 없어지고 백업 디렉토리에 ".removed" 로 남는다.
func TestT15RollbackCreated(t *testing.T) {
	l := newLab(t)
	setupBondNoFile(l)
	before := l.snapshot(true, false)

	out, code := l.apply(ApplyOptions{BondMode: "lacp"})
	mustOK(t, code, out)
	stamp := stampOf(t, applyResult(t, out))
	if !l.exists(keyfileDir + "/bond0.nmconnection") {
		t.Fatalf("새 파일이 만들어지지 않았습니다")
	}

	ro, code := l.rollback("")
	mustOK(t, code, ro)
	rr := parseResult(t, ro)
	if len(rr.f) != 7 || rr.f[1] != "OK" || rr.f[3] != stamp || rr.f[4] != "0" || rr.f[5] != "3" {
		t.Fatalf("롤백 결과: %v", rr.f)
	}
	wantSameTree(t, before, l.snapshot(true, false), "롤백 뒤")
	wantText(t, l.read(backupPath(stamp, keyfileDir+"/bond0.nmconnection")+".removed"), bond0Main, "치운 파일 보관본")
	if !l.exists(backupBase + "/" + stamp + "/ROLLED_BACK") {
		t.Errorf("ROLLED_BACK 표시가 없습니다")
	}
}

// 롤백 STAMP 를 지정하면 그 STAMP 만 되돌린다 (더 최근 STAMP 가 있어도).
func TestRollbackToSpecificStamp(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	old := "20250101000000"
	l.put(backupPath(old, ifcfgPath), "IPADDR=1.1.1.1\n", 0o644)
	l.put(backupBase+"/"+old+"/manifest", "modified "+ifcfgPath+"\n", 0o644)
	newer := "20260101000000"
	l.put(backupPath(newer, ifcfgPath), "IPADDR=2.2.2.2\n", 0o644)
	l.put(backupBase+"/"+newer+"/manifest", "modified "+ifcfgPath+"\n", 0o644)

	out, code := l.rollback(old)
	mustOK(t, code, out)
	wantText(t, l.read(ifcfgPath), "IPADDR=1.1.1.1\n", "지정한 STAMP 의 백업으로 복원")
	if l.exists(backupBase + "/" + newer + "/ROLLED_BACK") {
		t.Errorf("지정하지 않은 STAMP 에 표시가 생겼습니다")
	}
}

// ---- T16 ----

// T16: 옛 ".bak.<STAMP>" 백업만 있으면 옛 방식으로 되돌린다 (가장 최근 STAMP, 또는 지정한 STAMP).
func TestT16LegacyBakRollback(t *testing.T) {
	l := newLab(t)
	cur := ifcfgStaticWant
	l.put(ifcfgPath, cur, 0o644)
	l.put(ifcfgPath+".bak.20250101000000", strings.Replace(ifcfgStatic, "192.168.0.58", "10.1.1.58", 1), 0o644)
	l.put(ifcfgPath+".bak.20260101120000", ifcfgStatic, 0o644)

	out, code := l.rollback("")
	mustOK(t, code, out)
	want := "RESULT|OK|" + testHost + "|" + l.abs(ifcfgPath) + "|20260101120000|192.168.0.58"
	wantText(t, strings.TrimSpace(out), want, "옛 방식 롤백 결과")
	wantText(t, l.read(ifcfgPath), ifcfgStatic, "복원된 ifcfg")
	if !l.exists(ifcfgPath+".bak.20260101120000") || !l.exists(ifcfgPath+".bak.20250101000000") {
		t.Errorf("옛 백업 파일은 남아 있어야 합니다")
	}
	if l.exists("/var/lib/ip_change") {
		t.Errorf("옛 방식 롤백이 /var/lib/ip_change 를 만들었습니다")
	}

	// 지정한 STAMP
	l.put(ifcfgPath, cur, 0o644)
	out, code = l.rollback("20250101000000")
	mustOK(t, code, out)
	wantText(t, strings.TrimSpace(out), "RESULT|OK|"+testHost+"|"+l.abs(ifcfgPath)+"|20250101000000|10.1.1.58", "지정 STAMP 결과")
}

// 백업이 전혀 없으면 FAIL.
func TestRollbackNoBackup(t *testing.T) {
	l := newLab(t)
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	out, code := l.rollback("")
	if code == 0 || !strings.HasPrefix(strings.TrimSpace(out), "RESULT|FAIL|"+testHost+"|복원할 백업이 없습니다") {
		t.Fatalf("FAIL 이어야 합니다 (코드 %d): %s", code, out)
	}
}

// ---- T17 ----

// T17: SSH_CONNECTION 이 비어 있으면 기본 경로 장치를 쓴다.
func TestT17EmptySSHConnection(t *testing.T) {
	l := newLab(t)
	l.env["SSH_CONNECTION"] = ""
	l.setAddr("2: usb0    inet 192.168.7.2/24 brd 192.168.7.255 scope global noprefixroute usb0\\       valid_lft forever preferred_lft forever",
		addrLine("ens160", oldIP, "24"))
	l.put(ifcfgPath, ifcfgStatic, 0o644)

	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	r := applyResult(t, out)
	wantResult(t, r, "OK", "ens160", "static", "Rocky 8.10", ifcfgPath)
	if !strings.Contains(r.note(), "SSH_CONNECTION 이 없어 기본 경로 장치를 사용") {
		t.Errorf("비고: %q", r.note())
	}
}

// SSH_CONNECTION 도 기본 경로도 없으면 아무것도 쓰지 않고 FAIL.
func TestNoDeviceFails(t *testing.T) {
	l := newLab(t)
	l.env["SSH_CONNECTION"] = ""
	l.setRoute("")
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	before := l.snapshot(false, true)
	out, code := l.apply(ApplyOptions{})
	r := applyResult(t, out)
	if code == 0 || r.status() != "FAIL" || !strings.HasPrefix(r.note(), "대상 장치를 찾지 못했습니다") {
		t.Errorf("FAIL 이어야 합니다 (코드 %d): %v", code, r.f)
	}
	wantSameTree(t, before, l.snapshot(false, true), "실패 뒤")
}

// ---- 검증 실패와 그 밖의 실패 ----

// 고친 뒤 다시 읽어 검증이 틀리면 백업으로 되돌리고 FAIL 한다. 가짜 awk 가 편집 결과를
// 바꾸지 않아 이 경로를 만든다.
func TestVerifyFailureRestoresBackup(t *testing.T) {
	l := newLab(t)
	l.breakAwk()
	l.put(ifcfgPath, ifcfgStatic, 0o640)
	before := l.snapshot(true, false)

	out, code := l.apply(ApplyOptions{})
	r := applyResult(t, out)
	if code == 0 || r.status() != "FAIL" || !strings.HasPrefix(r.note(), "검증 실패, 백업으로 되돌림") {
		t.Fatalf("FAIL(검증 실패) 이어야 합니다 (코드 %d): %v", code, r.f)
	}
	wantText(t, l.read(ifcfgPath), ifcfgStatic, "되돌려진 파일")
	if p := l.perm(ifcfgPath); p != 0o640 {
		t.Errorf("권한 = %v, 기대 0640", p)
	}
	wantSameTree(t, before, l.snapshot(true, false), "실패 뒤")
}

// 목록에 없는 hostname 이면 아무것도 쓰지 않고 FAIL.
func TestUnknownHostFails(t *testing.T) {
	l := newLab(t)
	l.env["FAKE_HOSTNAME"] = "othernode"
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	before := l.snapshot(false, true)
	out, code := l.apply(ApplyOptions{})
	r := applyResult(t, out)
	if code == 0 || r.status() != "FAIL" || !strings.Contains(r.note(), "대상 목록에 이 hostname(othernode)이 없습니다") {
		t.Errorf("FAIL 이어야 합니다 (코드 %d): %v", code, r.f)
	}
	wantSameTree(t, before, l.snapshot(false, true), "실패 뒤")
}

// hostname 이 FQDN 이어도 목록의 짧은 이름으로 찾는다.
func TestFQDNHostnameMatchesShortName(t *testing.T) {
	l := newLab(t)
	l.env["FAKE_HOSTNAME"] = "node01.lab.example"
	l.put(ifcfgPath, ifcfgStatic, 0o644)
	out, code := l.apply(ApplyOptions{})
	mustOK(t, code, out)
	wantResult(t, applyResult(t, out), "OK", "ens160", "static", "Rocky 8.10", ifcfgPath)
}

// 이미 있는 파일 이름과 부딪히면 새 파일을 만들지 않는다 (다른 장치용 파일이 있는 경우).
func TestNewFileRefusesExistingName(t *testing.T) {
	l := newLab(t)
	// DEVICE 가 다른 장치라서 ens160 의 파일로는 안 잡히지만 이름은 ifcfg-ens160 이다
	l.put(ifcfgPath, "DEVICE=other0\nNAME=ens160\nONBOOT=no\nBOOTPROTO=none\nIPADDR=10.0.0.1\nPREFIX=24\n", 0o644)
	before := l.snapshot(false, true)
	out, code := l.apply(ApplyOptions{})
	r := applyResult(t, out)
	if code == 0 || r.status() != "FAIL" || !strings.Contains(r.note(), "만들 파일이 이미 있습니다") {
		t.Errorf("FAIL 이어야 합니다 (코드 %d): %v", code, r.f)
	}
	wantSameTree(t, before, l.snapshot(false, true), "실패 뒤")
}

// 가짜 서비스 재시작류와 쓰기용 nmcli 는 호출을 기록하고 실패한다 (runScript 가 이 기록을 보면 시험을 실패시킨다).
func TestDangerousFakesRecordCalls(t *testing.T) {
	l := newLab(t)
	for _, c := range [][]string{{"systemctl", "restart", "NetworkManager"}, {"ifup", "ens160"}, {"reboot"}, {"nmcli", "connection", "up", "ens160"}, {"ip", "addr", "add", "1.2.3.4/24", "dev", "ens160"}} {
		cmd := exec.Command(l.bin+"/"+c[0], c[1:]...)
		cmd.Env = []string{"FAKE_DIR=" + l.fake, "PATH=/usr/bin:/bin"}
		if err := cmd.Run(); err == nil {
			t.Errorf("%v 가 성공했습니다", c)
		}
	}
	u, err := os.ReadFile(l.fake + "/unexpected")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"systemctl restart NetworkManager", "ifup ens160", "reboot", "nmcli connection up ens160", "ip addr add"} {
		if !strings.Contains(string(u), w) {
			t.Errorf("기록에 %q 가 없습니다:\n%s", w, u)
		}
	}
}
