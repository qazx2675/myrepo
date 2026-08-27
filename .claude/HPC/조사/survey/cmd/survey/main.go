package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// confRelPath 는 실행 파일이 있는 디렉터리 기준 설정 파일 경로다.
const confRelPath = "conf/conf.toml"

func confPath() string {
	exe, err := os.Executable()
	if err != nil {
		return confRelPath
	}
	return filepath.Join(filepath.Dir(exe), confRelPath)
}

func main() {
	cfg, err := LoadConfig(confPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "[error] conf 로드 실패:", err)
		os.Exit(1)
	}

	hostnames, assets, err := LoadAsset(cfg.AssetFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[error] 자산양식 로드 실패:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[info] 조사 대상 %d대\n", len(hostnames))

	collected := Collect(cfg, hostnames)

	rows := make([][]string, 0, len(hostnames))
	okCnt, failCnt := 0, 0
	for _, h := range hostnames {
		a := assets[h]
		c := collected[h]

		var notes []string
		if c.Note != "" {
			notes = append(notes, c.Note)
		}

		infra, ierr := InfraNet(cfg.InfraNet, h)
		if ierr != nil {
			notes = append(notes, "인프라망 조사 실패")
		}

		mark, mnote := ApplStatus(cfg.Mounts, c.ConfigValue, a.Location)
		if mnote != "" {
			notes = append(notes, mnote)
		}

		if c.Note == "" {
			okCnt++
		} else {
			failCnt++
		}

		rows = append(rows, []string{
			h, a.Location, a.Status, c.ConfigValue, infra, mark, strings.Join(notes, "; "),
		})
	}

	out := fmt.Sprintf("result_%s.tsv", time.Now().Format("20060102_1504"))
	if err := WriteTSV(out, rows); err != nil {
		fmt.Fprintln(os.Stderr, "[error] 결과 파일 저장 실패:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[info] 완료: 성공 %d / 실패 %d -> %s\n", okCnt, failCnt, out)
}
