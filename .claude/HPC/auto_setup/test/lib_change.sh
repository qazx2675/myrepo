#!/bin/bash
# lib_change.sh - test/demo_change.sh · test/run_change_os.sh 공용 (source 전용): 변경 메뉴(m) 시험용 가짜 노드(fixture)와 conf 생성.
#
# 노드 fixture: <nodes>/<호스트>/ 아래에 etc/ root/ 와 .fake/ (test/fake_gossh.sh 가 읽는 호스트별 상태).
# 실제 /etc 는 건드리지 않는다. 테스트용 bindpw 는 DEMO_BINDPW_ 로 시작하는 식별 가능한 값이다.
# shellcheck shell=bash

# chg_fx_host <nodes_dir> <host> <os 7|8|9> <ip> [플래그...]
#   플래그: os8down(os8 에서만 접속 불가) dead(어디서도 접속 불가) noifcfg(ifcfg-eth0 없음 → IP 변경 FAIL)
#           osrel(redhat-release 없이 os-release 만) s4(s4 접두 호스트: nslcd/ntp 가 선택됨) both(nslcd·ntp·sssd·chrony 파일을 전부 둠)
#           nofqdn(hostname 이 FQDN 이 아님)
chg_fx_host() {
	local nd=$1 h=$2 os=$3 ip=$4 f=" ${*:5} " r rel vid name
	r="$nd/$h"
	rm -rf "$r"
	mkdir -p "$r/etc/sysconfig/network-scripts" "$r/etc/openldap" "$r/etc/sssd" "$r/root" "$r/.fake"
	case $os in
		7) rel="Red Hat Enterprise Linux Server release 7.9 (Maipo)"; vid="7.9"; name="Red Hat Enterprise Linux Server" ;;
		8) rel="Rocky Linux release 8.10 (Green Obsidian)"; vid="8.10"; name="Rocky Linux" ;;
		9) rel="Red Hat Enterprise Linux release 9.4 (Plow)"; vid="9.4"; name="Red Hat Enterprise Linux" ;;
		*) echo "chg_fx_host: 알 수 없는 OS $os" >&2; return 1 ;;
	esac
	[[ $f == *" osrel "* ]] || printf '%s\n' "$rel" > "$r/etc/redhat-release"
	printf 'NAME="%s"\nVERSION_ID="%s"\nID="rhel"\n' "$name" "$vid" > "$r/etc/os-release"

	# ---- 네트워크 (ifcfg) ----
	if [[ $f != *" noifcfg "* ]]; then
		printf 'TYPE=Ethernet\nBOOTPROTO=none\nDEVICE=eth0\nONBOOT=yes\nIPADDR=%s\nPREFIX=24\nGATEWAY=%s.1\nDNS1=10.10.1.10\n' \
			"$ip" "${ip%.*}" > "$r/etc/sysconfig/network-scripts/ifcfg-eth0"
	fi
	printf 'DEVICE=lo\nIPADDR=127.0.0.1\nNETMASK=255.0.0.0\nONBOOT=yes\n' > "$r/etc/sysconfig/network-scripts/ifcfg-lo"

	# ---- LDAP 쪽 파일 ----
	printf '# ldap.conf\nBASE dc=example,dc=com\nURI ldap://old.example/\nBINDDN uid=old,ou=user\nBINDPW OLD_NODE_PW_DEMO\nTLS_CACERTDIR /etc/openldap/certs\n' \
		> "$r/etc/openldap/ldap.conf"
	printf '<?xml version="1.0" ?>\n<autofs_ldap_sasl_conf usetls="no" tlsrequired="no" authrequired="simple" user="uid=old" secret="OLD_NODE_PW_DEMO"/>\n' \
		> "$r/etc/autofs_ldap_auth.conf"
	printf '[ autofs ]\ntimeout = 300\nbrowse_mode = no\nldap_uri = "ldap://old.example/"\n' > "$r/etc/autofs.conf"
	printf 'search example.com\nnameserver 8.8.8.8\noptions timeout:2\n' > "$r/etc/resolv.conf"
	printf '/appl\t-rw,soft,intr\toldstore:/applX\n' > "$r/etc/auto.appl"
	local old=0 new=0
	if [[ $os == 7 || $f == *" s4 "* ]]; then old=1; else new=1; fi
	[[ $f == *" both "* ]] && { old=1; new=1; }
	if [[ $old == 1 ]]; then
		printf '# nslcd\nuid nslcd\ngid ldap\nuri ldap://old.example/\nbase dc=example,dc=com\nbinddn uid=old\nbindpw OLD_NODE_PW_DEMO\n' > "$r/etc/nslcd.conf"
		printf 'driftfile /var/lib/ntp/drift\nserver 0.rhel.pool.ntp.org iburst\nrestrict default nomodify\n' > "$r/etc/ntp.conf"
	fi
	if [[ $new == 1 ]]; then
		printf '[sssd]\nservices = nss, pam\ndomains = example.com\n\n[domain/example.com]\nid_provider = ldap\nldap_uri = ldap://old.example/\ncache_credentials = True\n' \
			> "$r/etc/sssd/sssd.conf"
		printf '# chrony\npool 2.pool.ntp.org iburst\ndriftfile /var/lib/chrony/drift\n' > "$r/etc/chrony.conf"
	fi

	# ---- fake_gossh.sh 용 호스트 상태 ----
	if [[ $f == *" nofqdn "* ]]; then printf '%s\n' "$h" > "$r/.fake/hostname"; else printf '%s.lab.local\n' "$h" > "$r/.fake/hostname"; fi
	printf '%s\n' "$ip" > "$r/.fake/ips"
	[[ $f == *" os8down "* ]] && : > "$r/.fake/os8_down"
	[[ $f == *" dead "* ]] && : > "$r/.fake/down"
	return 0
}

# chg_make_nodes <nodes_dir> [both]: 시나리오 호스트 9대 (OS 7/8/9 혼합).  인자 both 면 모든 호스트에 nslcd·ntp·sssd·chrony 파일을 둔다.
#   rh7-n01  RHEL7  정상                          rh7-n02   RHEL7  os8 접속 불가 → os6_mgmt 경유로 성공
#   rh8-n01  Rocky8 정상                          rh8-n02   Rocky8 ifcfg 없음 → IP 변경 FAIL
#   rh9-n01  RHEL9  정상                          rh9-n02   RHEL9  os-release 만 있음
#   s4rh8-n01 Rocky8 s4 접두 → nslcd/ntp          rh8-dead  Rocky8 어디서도 접속 불가
#   rh8-noasset Rocky8 정상 (assets.txt 에 없음 → LDAP 은 자산현황 없음으로 건너뜀)
chg_make_nodes() {
	local nd=$1 b=${2:-}
	mkdir -p "$nd"
	chg_fx_host "$nd" rh7-n01 7 10.20.7.11 $b
	chg_fx_host "$nd" rh7-n02 7 10.20.7.12 os8down $b
	chg_fx_host "$nd" rh8-n01 8 10.20.8.11 $b
	chg_fx_host "$nd" rh8-n02 8 10.20.8.12 noifcfg $b
	chg_fx_host "$nd" rh9-n01 9 10.20.9.11 $b
	chg_fx_host "$nd" rh9-n02 9 10.20.9.12 osrel $b
	chg_fx_host "$nd" s4rh8-n01 8 10.20.8.21 s4 $b
	chg_fx_host "$nd" rh8-dead 8 10.20.8.99 dead $b
	chg_fx_host "$nd" rh8-noasset 8 10.20.8.31 $b
}

# 시나리오 호스트 이름 (assets.txt 에 있는 8대 + 없는 1대)
CHG_HOSTS="rh7-n01 rh7-n02 rh8-n01 rh8-n02 rh9-n01 rh9-n02 s4rh8-n01 rh8-dead rh8-noasset"

# chg_write_conf <conf_dir> <bindpw_alpha> <bindpw_beta> [network_scripts_dir]
#   ldap_config.conf (infra alpha / beta, 사이트 a1·a2, s4 규칙 켬, 퍼미션 600) · ip_change.conf · assets.txt 를 쓴다.
chg_write_conf() {
	local d=$1 pa=$2 pb=$3 nsd=${4:-/etc/sysconfig/network-scripts} i
	mkdir -p "$d"
	{
		printf '# 시험용 ldap_config.conf (test/lib_change.sh) — bindpw 는 DEMO_BINDPW_ 접두 시험 값\n'
		printf 's4.enabled  = true\ns4.prefix   = s4\ns4.services = nslcd,ntp\n\n'
		for i in alpha beta; do
			local pw=$pa
			[[ $i == beta ]] && pw=$pb
			local k=1
			[[ $i == beta ]] && k=2
			printf 'infra.%s.dns    = 10.10.%d.10, 10.10.%d.11\n' "$i" "$k" "$k"
			printf 'infra.%s.ntp    = 10.20.%d.10, 10.20.%d.11\n' "$i" "$k" "$k"
			printf 'infra.%s.uri1   = ldap://10.10.%d.20/\ninfra.%s.uri2   = ldap://10.10.%d.21/\ninfra.%s.uri3   = NONE\n' "$i" "$k" "$i" "$k" "$i"
			printf 'infra.%s.binddn = uid=svcaccount,ou=user,ou=system,dc=example,dc=com\n' "$i"
			printf 'infra.%s.bindpw = %s\n' "$i" "$pw"
			printf 'infra.%s.site.a1.uri_order   = uri1,uri2,uri3\ninfra.%s.site.a1.storage     = %s1\ninfra.%s.site.a1.mountpoint  = /appl1\ninfra.%s.site.a1.wappl_mount = /wappl1\n' \
				"$i" "$i" "$i" "$i" "$i"
			printf 'infra.%s.site.a2.uri_order  = uri2,uri1,uri3\ninfra.%s.site.a2.storage    = %s2\ninfra.%s.site.a2.mountpoint = /appl2\n\n' \
				"$i" "$i" "$i" "$i"
		done
	} > "$d/ldap_config.conf"
	chmod 600 "$d/ldap_config.conf"
	printf '# 시험용 ip_change.conf — rhel9_path 없음(RHEL 9 도 network-scripts 기준)\nnetwork_scripts_dir=%s\n' "$nsd" > "$d/ip_change.conf"
	{
		printf '# hostname<TAB>site\n'
		printf 'rh7-n01\ta1\nrh7-n02\ta1\nrh8-n01\ta1\nrh8-n02\ta2\nrh9-n01\ta2\nrh9-n02\ta1\ns4rh8-n01\ta1\nrh8-dead\ta2\n'
	} > "$d/assets.txt"
}
