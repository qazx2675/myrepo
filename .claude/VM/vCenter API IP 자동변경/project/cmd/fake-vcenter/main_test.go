package main

import "testing"

func TestStageOf(t *testing.T) {
	// vsphere.CheckConnection 스크립트처럼 되돌리기 스크립트 본문(nmcli con up)을
	// 품고 있어도 "check" 로 판별해야 한다.
	check := `CONN=$(nmcli -t -f NAME,DEVICE con show --active | grep -v '^lo:' | head -n1 | cut -d: -f1)
cat > /tmp/vm-ip-change/x.revert.sh <<REVERTEOF
nmcli con mod "$CONN" ipv4.method "$ORIG_METHOD"
nmcli con up "$CONN"
REVERTEOF`
	cases := map[string]string{
		check: "check",
		`nmcli con mod "$CONN" ipv4.method manual ipv4.addresses 10.10.0.2/24 ipv4.gateway 10.10.0.1`: "apply",
		`CONN=$(cat /tmp/vm-ip-change/x.conn)
nmcli con up "$CONN"`: "up",
		`mkdir -p /tmp/vm-ip-change
uptime > /tmp/vm-ip-change/loadavg 2>&1`: "uptime",
	}
	for script, want := range cases {
		if got := stageOf(script); got != want {
			t.Errorf("stageOf(%.40q...) = %s, want %s", script, got, want)
		}
	}
}
