// conf.go - 빈 변수를 설정 파일(conf/auto_setup.conf)로 채움 (빌드 없이 환경별 값 지정)
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const confFileName = "auto_setup.conf"

// confVars: 설정 파일 키 → main.go 의 빈 변수 (키 이름 = 변수 이름)
func confVars() map[string]*string {
	return map[string]*string{
		"os6_mgmt":        &os6_mgmt,
		"os6_gossh":       &os6_gossh,
		"os6_autosetup":   &os6_autosetup,
		"os_check_sh":     &os_check_sh,
		"awx_dir":         &awx_dir,
		"os8_mgmt":        &os8_mgmt,
		"os8_autosetup":   &os8_autosetup,
		"os6_os_check_sh": &os6_os_check_sh,
		"ldap_share_dir":  &ldap_share_dir,
	}
}

// confPaths: 설정 파일 후보 (앞이 우선). AUTO_SETUP_CONF 는 디렉터리.
func confPaths() []string {
	var c []string
	if d := os.Getenv("AUTO_SETUP_CONF"); d != "" {
		c = append(c, filepath.Join(d, confFileName))
	}
	if exe, err := os.Executable(); err == nil {
		c = append(c, filepath.Join(filepath.Dir(exe), "conf", confFileName))
	}
	return append(c, filepath.Join("/etc/auto_setup", confFileName))
}

// loadConf: 첫 번째로 존재하는 설정 파일의 key=value 를 빈 변수에만 채운다
// (빌드 때 -X 로 넣은 값이 우선). 알 수 없는 키는 무시. 읽은 파일 경로를 반환(없으면 "").
func loadConf(paths []string) string {
	vars := confVars()
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			i := strings.IndexByte(line, '=')
			if i < 0 {
				continue
			}
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])
			if n := len(val); n >= 2 && (val[0] == '"' || val[0] == '\'') && val[n-1] == val[0] {
				val = val[1 : n-1]
			}
			if v, ok := vars[key]; ok && *v == "" {
				*v = val
			}
		}
		return p
	}
	return ""
}
