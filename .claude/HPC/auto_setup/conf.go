// conf.go - 빈 변수를 설정 파일(conf/auto_setup.conf)로 채움 (빌드 없이 환경별 값 지정)
package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const confFileName = "auto_setup.conf"

// 변경 엔진 실행 파일 이름 (engineBin 의 인자)
const (
	ipEngineName   = "ip-change-engine"
	ldapEngineName = "ldap-config-engine"
)

// confLoadedFrom: loadConf 가 마지막으로 읽은 설정 파일 경로 (없으면 ""). changeConfDir 의 기준.
var confLoadedFrom string

// engineExeDir: auto_setup 실행 파일이 있는 디렉터리 (찾지 못하면 ""). 테스트에서 바꾼다.
var engineExeDir = func() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return ""
}

// confVars: 설정 파일 키 → main.go 의 빈 변수 (키 이름 = 변수 이름)
func confVars() map[string]*string {
	return map[string]*string{
		"os6_mgmt":        &os6_mgmt,
		"os6_gossh":       &os6_gossh,
		"os6_autosetup":   &os6_autosetup,
		"os_check_sh":     &os_check_sh,
		"awx_dir":         &awx_dir,
		"awx_profile_1":   &awx_profile_1,
		"awx_profile_2":   &awx_profile_2,
		"awx_profile_3":   &awx_profile_3,
		"awx_profile_4":   &awx_profile_4,
		"awx_profile_5":   &awx_profile_5,
		"awx_profile_6":   &awx_profile_6,
		"awx_profile_7":   &awx_profile_7,
		"awx_profile_8":   &awx_profile_8,
		"awx_profile_9":   &awx_profile_9,
		"os8_mgmt":        &os8_mgmt,
		"os8_autosetup":   &os8_autosetup,
		"os6_os_check_sh": &os6_os_check_sh,
		"ldap_share_dir":  &ldap_share_dir,

		// v0.7.0 변경 메뉴
		"enable_ip_change":   &enable_ip_change,
		"enable_ldap_change": &enable_ldap_change,
		"ip_engine":          &ip_engine,
		"ldap_engine":        &ldap_engine,

		// v0.8.0 신규 설치 양식 템플릿·gossh 작업 실행 위치
		"newhost_template_1": &newhost_template_1,
		"newhost_template_2": &newhost_template_2,
		"newhost_template_3": &newhost_template_3,
		"newhost_template_4": &newhost_template_4,
		"newhost_template_5": &newhost_template_5,
		"newhost_template_6": &newhost_template_6,
		"newhost_template_7": &newhost_template_7,
		"newhost_template_8": &newhost_template_8,
		"newhost_template_9": &newhost_template_9,
		"work_os6_over":      &work_os6_over,

		// 종료 job 표시 기간(일)
		"closed_keep_days": &closed_keep_days,
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
	confLoadedFrom = ""
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		confLoadedFrom = p
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

// confTrue: 켜짐 값 판정 — y/yes/1/true (대소문자 무시, 앞뒤 공백 무시). 그 외(빈 값 포함)는 꺼짐.
func confTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "y", "yes", "1", "true":
		return true
	}
	return false
}

// closedKeepDur: 종료된 job(jobs/done)을 보여 주는 기간 — conf closed_keep_days(일, 1 이상의 정수).
// 비우거나 1 미만·숫자가 아니면 closedKeep(24시간). 화면용 스냅샷만 걸러내며 jobs/done 파일은 지우지 않는다.
func closedKeepDur() time.Duration {
	if v, err := strconv.Atoi(strings.TrimSpace(closed_keep_days)); err == nil && v >= 1 {
		return time.Duration(v) * 24 * time.Hour
	}
	return closedKeep
}

// ipChangeEnabled: IP 변경 메뉴 가시성 = --admin 이거나 conf enable_ip_change 가 켜짐
func ipChangeEnabled() bool { return adminMode || confTrue(enable_ip_change) }

// ldapChangeEnabled: LDAP 변경 메뉴 가시성 = --admin 이거나 conf enable_ldap_change 가 켜짐
func ldapChangeEnabled() bool { return adminMode || confTrue(enable_ldap_change) }

// engineBin: 변경 엔진 실행 파일 경로. name 은 ipEngineName 또는 ldapEngineName.
// conf 의 ip_engine / ldap_engine 이 있으면 존재 확인 없이 그대로 돌려주고(실행 때 오류가 보이게),
// 비면 <auto_setup 실행 파일 디렉터리>/<name> → PATH 순으로 찾는다. 못 찾으면 "".
func engineBin(name string) string {
	switch name {
	case ipEngineName:
		if ip_engine != "" {
			return ip_engine
		}
	case ldapEngineName:
		if ldap_engine != "" {
			return ldap_engine
		}
	}
	if d := engineExeDir(); d != "" {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// changeConfDir: 변경용 conf(ip_change.conf·ldap_config.conf·assets.txt) 디렉터리 —
// auto_setup.conf 를 읽은 디렉터리, 읽은 파일이 없으면 /etc/auto_setup
func changeConfDir() string {
	if confLoadedFrom != "" {
		return filepath.Dir(confLoadedFrom)
	}
	return "/etc/auto_setup"
}

// changeConfPath: changeConfDir()/<name> (name = ip_change.conf | ldap_config.conf | assets.txt)
func changeConfPath(name string) string { return filepath.Join(changeConfDir(), name) }
