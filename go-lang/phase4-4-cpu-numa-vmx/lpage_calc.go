// lpage_calc.go - ESXi Large Page 정합 VM 메모리 산출 (Go 버전, 최종본)
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const (
	align   = 4.0
	step    = 8.0
	reserve = 0.96
)

func usage() {
	fmt.Println(`Usage: lpage [-w host] [-v1 GB] [-v2 GB] [-v3 GB] [-vcnt N]
  -w 대상 호스트 (SSH 전체 물리메모리 조회, 미지정시 로컬)
  -vN N번 VM 희망 메모리(GB), 해당값 근처에서 정합값 산출
  -vcnt VM 수량 (기본 2)`)
	os.Exit(1)
}

func runCmd(host, cmd string) (string, error) {
	var c *exec.Cmd
	if host != "" {
		c = exec.Command("ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", host, cmd)
	} else {
		c = exec.Command("sh", "-c", cmd)
	}
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

func main() {
	args := os.Args[1:]
	host := ""
	vcnt := 2
	vmem := map[int]float64{}
	vFlagRe := regexp.MustCompile(`^-v(\d+)$`)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-w":
			host = args[i+1]
			i++
		case args[i] == "-vcnt":
			n, _ := strconv.Atoi(args[i+1])
			vcnt = n
			i++
		case vFlagRe.MatchString(args[i]):
			m := vFlagRe.FindStringSubmatch(args[i])
			idx, _ := strconv.Atoi(m[1])
			val, _ := strconv.ParseFloat(args[i+1], 64)
			vmem[idx] = val
			i++
		case args[i] == "-h" || args[i] == "--help":
			usage()
		default:
			usage()
		}
	}

	osName, err := runCmd(host, "uname -s")
	if err != nil || osName == "" {
		fmt.Printf("ERROR: %s 접속 실패\n", displayHost(host))
		os.Exit(1)
	}

	var bytesVal float64
	if osName == "VMkernel" {
		out, err := runCmd(host, "esxcli hardware memory get")
		if err != nil {
			os.Exit(1)
		}
		bytesVal = parseEsxiMemory(out)
	} else {
		out, err := runCmd(host, "free -b")
		if err != nil {
			os.Exit(1)
		}
		bytesVal = parseLinuxMemory(out)
	}
	if bytesVal <= 0 {
		os.Exit(1)
	}

	total := bytesVal / 1073741824.0
	usable := total * reserve

	sumReq := 0.0
	unspec := 0
	for i := 1; i <= vcnt; i++ {
		if v, ok := vmem[i]; ok {
			sumReq += v
		} else {
			unspec++
		}
	}
	share := 0.0
	if unspec > 0 {
		s := (usable - sumReq) / float64(unspec)
		if s < 0 {
			s = 0
		}
		share = s
	}
	totReq := sumReq + share*float64(unspec)
	scale := 1.0
	if totReq > usable && totReq > 0 {
		scale = usable / totReq
	}

	fmt.Printf("== HOST: %s (%s) / TOTAL: %.2fGB / USABLE: %.2fGB / VM: %d ==\n\n", displayHost(host), osName, total, usable, vcnt)
	for i := 1; i <= vcnt; i++ {
		t, ok := vmem[i]
		if !ok {
			t = share
		}
		base := math.Floor((t*scale)/align) * align
		fmt.Printf("V%d추천 용량\n", i)
		for n := 0; n < 3; n++ {
			v := base - step*float64(n)
			if v <= 0 {
				break
			}
			fmt.Printf("%d. %.0fGB\n", n+1, v)
		}
		fmt.Println()
	}
}

func displayHost(host string) string {
	if host == "" {
		return "localhost"
	}
	return host
}

func parseEsxiMemory(out string) float64 {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Physical Memory") {
			for _, f := range strings.Fields(line) {
				if n, err := strconv.ParseFloat(f, 64); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

func parseLinuxMemory(out string) float64 {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Mem:") {
			if fields := strings.Fields(line); len(fields) >= 2 {
				n, _ := strconv.ParseFloat(fields[1], 64)
				return n
			}
		}
	}
	return 0
}
