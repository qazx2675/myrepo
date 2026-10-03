// paths.go - os_check·awx 스크립트 경로 해석 (autofs 로 os8_mgmt·os6_mgmt 가 같은 경로를 쓰는 경우 포함)
package main

import "path"

// os6OSCheckPath: os6_mgmt 위 os_check 경로. os6_os_check_sh 명시값 우선,
// 비어 있으면 os6_mgmt 가 설정된 경우 os_check_sh(autofs 동일 경로), 아니면 "".
func os6OSCheckPath() string {
	if os6_os_check_sh == "-" { // "-" = 2차(이중) 체크 끄기
		return ""
	}
	if os6_os_check_sh != "" {
		return os6_os_check_sh
	}
	if os6_mgmt != "" {
		return os_check_sh
	}
	return ""
}

// dhcpCandidates: dhcp.sh 원본 후보 (앞이 우선). awx_dir 이 비면 앞 둘은 생략.
func dhcpCandidates(osCheck string) []string {
	var c []string
	if awx_dir != "" {
		c = append(c, path.Join(awx_dir, "awxkit", "dhcp.sh"), path.Join(awx_dir, "dhcp.sh"))
	}
	if osCheck != "" {
		c = append(c, path.Join(path.Dir(osCheck), "dhcp.sh"))
	}
	return c
}

// firstExisting: 후보 중 처음으로 존재하는 파일 (없으면 "")
func firstExisting(cands []string) string {
	for _, p := range cands {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// dhcpLinkSh: 원격 sh 문. 존재하는 첫 후보를 dhcp.sh 로 링크, 없으면 생략 (항상 성공 종료)
func dhcpLinkSh(cands []string) string {
	s := "{ "
	for i, p := range cands {
		kw := "if"
		if i > 0 {
			kw = "elif"
		}
		s += kw + " [ -f " + shq(p) + " ]; then ln -s " + shq(p) + " dhcp.sh; "
	}
	if len(cands) > 0 {
		s += "fi; "
	}
	return s + "true; }"
}
