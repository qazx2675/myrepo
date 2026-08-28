package main

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// vmPerEsxi 는 ESXi 한 대당 만드는 VM 후보 수다. 규칙 고정(<esxi>ev01 ~ ev03).
const vmPerEsxi = 3

// DetectESXi 는 설정값이 없는 호스트들에 `uname` 을 실행해
// 출력에 "VMkernel" 이 있으면 ESXi 로 판정한다.
func DetectESXi(cfg *Config, hosts []string) map[string]bool {
	res := map[string]bool{}
	if len(hosts) == 0 {
		return res
	}
	lines, _ := runGossh(cfg, hosts, "uname", true)
	for _, h := range hosts {
		if strings.Contains(strings.ToLower(strings.Join(lines[h], "\n")), "vmkernel") {
			res[h] = true
		}
	}
	return res
}

// vmName 은 ESXi 호스트네임으로 VM 이름을 만든다: <esxi>ev01, <esxi>ev02, ...
func vmName(esxi string, n int) string {
	return fmt.Sprintf("%sev%02d", esxi, n)
}

// dnsResolvable 은 host 가 DNS(또는 /etc/hosts)로 해석되는지 확인한다.
// gossh 에러 텍스트 파싱에 의존하지 않고, 존재하지 않는 VM 을 조사 전에 걸러내기 위함.
func dnsResolvable(host string) bool {
	addrs, err := net.LookupHost(host)
	return err == nil && len(addrs) > 0
}

// SurveyVMs 는 각 ESXi 의 VM(<esxi>ev01~ev03)에 대해 설정값·인프라망·O/X 를 수집해 행 목록을 만든다.
//   - VM 이름을 만든 뒤 **DNS 로 해석되는 것만** 조사한다. 해석 안 되는 VM 은 어떤 행도 남기지 않는다.
//   - 위치/상태·O·X 기준 위치는 소속 ESXi(표1)의 값을 상속한다.
//   - 어떤 ESXi 의 조사 대상 VM 이 하나도 없으면(전부 미해석) 그 ESXi 이름으로 "접속불가" 1행.
//   - 타임아웃 VM 은 딱 1회 재조사한다(무한 루프 방지).
func SurveyVMs(cfg *Config, esxiHosts []string, assets map[string]AssetRow) [][]string {
	var vmNames []string
	vmsByEsxi := map[string][]string{}
	for _, e := range esxiHosts {
		for i := 1; i <= vmPerEsxi; i++ {
			v := vmName(e, i)
			if !dnsResolvable(v) {
				fmt.Fprintf(os.Stderr, "[info] VM %s DNS 미해석 → 조사 제외\n", v)
				continue
			}
			vmNames = append(vmNames, v)
			vmsByEsxi[e] = append(vmsByEsxi[e], v)
		}
	}

	collected := map[string]CollectResult{}
	infra := map[string]InfraResult{}
	if len(vmNames) > 0 {
		collected = Collect(cfg, vmNames)
		infra = CollectInfra(cfg, vmNames)

		// 타임아웃 VM 재조사 (1회 제한)
		var retry []string
		for _, v := range vmNames {
			if collected[v].Note == "타임아웃" {
				retry = append(retry, v)
			}
		}
		if len(retry) > 0 {
			rc := Collect(cfg, retry)
			ri := CollectInfra(cfg, retry)
			for _, v := range retry {
				collected[v] = rc[v]
				if r, ok := ri[v]; ok {
					infra[v] = r
				}
			}
		}
	}

	var rows [][]string
	for _, e := range esxiHosts {
		loc := assets[e].Location
		st := assets[e].Status

		var esxiRows [][]string
		for _, v := range vmsByEsxi[e] {
			c := collected[v]
			var configCell, mark string
			var notes []string
			switch {
			case !c.Reached:
				notes = append(notes, c.Note) // 접속불가 / 타임아웃 / gossh 실행 실패
			case c.ConfigValue == "":
				configCell, mark = "없음", "없음"
			default:
				configCell = c.ConfigValue
				m, mn := ApplStatus(cfg.Mounts, c.ConfigValue, loc)
				mark = m
				if mn != "" {
					notes = append(notes, mn)
				}
			}
			ir := infra[v]
			if ir.Note != "" {
				notes = append(notes, ir.Note) // 예: "인프라 스크립트 없음" (파일 A 와 동일하게 표기)
			}
			esxiRows = append(esxiRows, []string{
				v, loc, st, configCell, ir.Value, mark, strings.Join(notes, "; "),
			})
		}

		if len(esxiRows) == 0 {
			// 조사할 수 있는 VM 이 하나도 없음
			rows = append(rows, []string{e, loc, st, "", "", "", "접속불가"})
			continue
		}
		rows = append(rows, esxiRows...)
	}
	return rows
}
