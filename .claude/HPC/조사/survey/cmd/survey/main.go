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
	infra := CollectInfra(cfg, hostnames)

	// 설정값이 "없음"(응답 O) 인 호스트 → uname 으로 ESXi(VMkernel) 판별
	var noneHosts []string
	for _, h := range hostnames {
		if c := collected[h]; c.Reached && c.ConfigValue == "" {
			noneHosts = append(noneHosts, h)
		}
	}
	esxiSet := DetectESXi(cfg, noneHosts)

	ts := time.Now().Format("20060102_1504")

	rows := make([][]string, 0, len(hostnames))
	okCnt, failCnt := 0, 0
	for _, h := range hostnames {
		a := assets[h]
		c := collected[h]

		var notes []string
		if c.Note != "" {
			notes = append(notes, c.Note)
		}

		var configCell, mark, mnote string
		switch {
		case !c.Reached:
			// 접속불가 등 — 사유는 특이사항에 있음. 설정값/판정은 공란.
		case c.ConfigValue == "":
			configCell = "없음"
			mark = "없음"
		default:
			configCell = c.ConfigValue
			mark, mnote = ApplStatus(cfg.Mounts, c.ConfigValue, a.Location)
		}
		if mnote != "" {
			notes = append(notes, mnote)
		}
		if esxiSet[h] {
			notes = append(notes, "esxi")
		}

		ir := infra[h]
		if ir.Note != "" {
			notes = append(notes, ir.Note)
		}

		if c.Note == "" {
			okCnt++
		} else {
			failCnt++
		}

		rows = append(rows, []string{
			h, a.Location, a.Status, configCell, ir.Value, mark, strings.Join(notes, "; "),
		})
	}

	out := fmt.Sprintf("result_%s.tsv", ts)
	if err := WriteTSV(out, rows); err != nil {
		fmt.Fprintln(os.Stderr, "[error] 결과 파일 저장 실패:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[info] 완료: 성공 %d / 실패 %d -> %s\n", okCnt, failCnt, out)

	// ESXi 가 있으면 그 VM(<esxi>ev01~ev03)을 별도 파일로 조사
	if len(esxiSet) > 0 {
		var esxiHosts []string
		for _, h := range hostnames {
			if esxiSet[h] {
				esxiHosts = append(esxiHosts, h)
			}
		}
		vmRows := SurveyVMs(cfg, esxiHosts, assets)
		vmOut := fmt.Sprintf("result_vm_%s.tsv", ts)
		if err := WriteTSV(vmOut, vmRows); err != nil {
			fmt.Fprintln(os.Stderr, "[error] VM 결과 파일 저장 실패:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "[info] VM 조사: ESXi %d대 -> %s (%d행)\n", len(esxiHosts), vmOut, len(vmRows))
	}
}
