// Package config 는 ip_change.conf (평문 key=value 형식) 를 읽어들입니다.
//
// 대상 노드에 jq 가 없기 때문에 JSON 대신 평문 key=value 를 씁니다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// DefaultNetworkScriptsDir 은 conf 를 지정하지 않았을 때 쓰는 기본 경로입니다.
const DefaultNetworkScriptsDir = "/etc/sysconfig/network-scripts"

// Config 는 ip_change.conf 전체입니다.
type Config struct {
	// NetworkScriptsDir 는 RHEL 8 이하(및 현재의 RHEL 9)에서 ifcfg-* 를 찾을 경로입니다.
	NetworkScriptsDir string
	// RHEL9Path 는 추후 RHEL 9 이상에서 기본 네트워크 설정 경로 방식이 바뀔 경우를
	// 대비한 자리입니다. 비어 있으면 무시하고 NetworkScriptsDir 를 그대로 씁니다.
	// 설정돼 있으면, 노드의 OS 메이저 버전이 9 이상일 때만 이 경로를 대신 씁니다.
	// 이 경로 아래에서도 ifcfg(KEY=VALUE) 형식 파일을 찾는다고 가정합니다 —
	// NetworkManager keyfile(.nmconnection) 형식 파싱은 지원하지 않습니다.
	RHEL9Path string
}

// Load 는 지정한 경로의 설정 파일을 읽어 Config 로 만듭니다.
// 파일이 없으면 기본값으로 채운 Config 를 돌려줍니다(있으면 좋고 없어도 되는 설정).
func Load(path string) (*Config, error) {
	cfg := &Config{NetworkScriptsDir: DefaultNetworkScriptsDir}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: key=value 형식이 아닙니다: %q", path, lineNo, line)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "network_scripts_dir":
			if val != "" {
				cfg.NetworkScriptsDir = val
			}
		case "rhel9_path":
			cfg.RHEL9Path = val
		default:
			return nil, fmt.Errorf("%s:%d: 알 수 없는 키입니다: %q", path, lineNo, key)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}
