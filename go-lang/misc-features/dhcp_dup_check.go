// dhcp_dup_check.go - /user/dhcp 대역 파일들과 입력 파일(a.txt)을 대조하여 host/IP/MAC 중복 검사, 중복 항목 _delete.txt 로 저장
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	var aFilePath string
	flag.StringVar(&aFilePath, "w", "a.txt", "비교할 입력 파일 지정 (기본값: a.txt)")
	flag.Parse()

	targetDir := "/user/dhcp"
	aHosts := make(map[string][]string)
	aIPs := make(map[string][]string)
	aMACs := make(map[string][]string)

	aFile, err := os.Open(aFilePath)
	if err != nil {
		fmt.Printf("%s 파일 열기 실패: %v\n", aFilePath, err)
		return
	}
	defer aFile.Close()

	loadCount := 0
	scanner := bufio.NewScanner(aFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 6 {
			host, ip, mac := fields[3], fields[4], strings.ToLower(fields[5])
			aHosts[host] = fields
			aIPs[ip] = fields
			aMACs[mac] = fields
			loadCount++
		}
	}

	filePattern := regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)
	files, err := os.ReadDir(targetDir)
	if err != nil {
		fmt.Printf("디렉토리 읽기 실패: %v\n", err)
		return
	}

	hostRegex := regexp.MustCompile(`host\s+([^\s{]+)`)
	macRegex := regexp.MustCompile(`ethernet\s+([0-9a-fA-F:]+);?`)
	ipRegex := regexp.MustCompile(`fixed-address\s+([0-9\.]+);?`)

	fmt.Printf("=== %s 대조 DHCP 중복 검사 시작 ===\n", aFilePath)
	foundCount, checkCount := 0, 0
	var deleteLines []string

	for _, file := range files {
		if file.IsDir() || !filePattern.MatchString(file.Name()) {
			continue
		}
		filePath := filepath.Join(targetDir, file.Name())
		f, err := os.Open(filePath)
		if err != nil {
			continue
		}
		fScanner := bufio.NewScanner(f)
		var dHost, dIP, dMAC string
		var matchedLine []string
		var isDup, alreadyPrinted bool

		flushBlock := func() {
			if isDup {
				outFields := make([]string, len(matchedLine))
				copy(outFields, matchedLine)
				if len(outFields) >= 6 {
					if dHost != "" {
						outFields[3] = dHost
					}
					if dIP != "" {
						outFields[4] = dIP
					}
					if dMAC != "" {
						outFields[5] = dMAC
					}
				}
				deleteLines = append(deleteLines, strings.Join(outFields, " "))
			}
			dHost, dIP, dMAC = "", "", ""
			matchedLine = nil
			isDup, alreadyPrinted = false, false
		}

		for fScanner.Scan() {
			line := strings.TrimSpace(fScanner.Text())
			if line == "" {
				continue
			}
			if match := hostRegex.FindStringSubmatch(line); len(match) > 1 {
				flushBlock()
				dHost = match[1]
				checkCount++
				if aFields, ok := aHosts[dHost]; ok {
					if !alreadyPrinted {
						fmt.Printf("[중복 발견] 입력 호스트: (%s) <-> 파일: %s (호스트: %s)\n", aFields[3], file.Name(), dHost)
						foundCount++
						alreadyPrinted = true
					}
					isDup = true
					if matchedLine == nil {
						matchedLine = aFields
					}
				}
			}
			if match := macRegex.FindStringSubmatch(line); len(match) > 1 {
				dMAC = strings.ToLower(match[1])
				if aFields, ok := aMACs[dMAC]; ok {
					if !alreadyPrinted {
						fmt.Printf("[중복 발견] MAC: %s (파일: %s)\n", dMAC, file.Name())
						foundCount++
						alreadyPrinted = true
					}
					isDup = true
					if matchedLine == nil {
						matchedLine = aFields
					}
				}
			}
			if match := ipRegex.FindStringSubmatch(line); len(match) > 1 {
				dIP = match[1]
				if aFields, ok := aIPs[dIP]; ok {
					if !alreadyPrinted {
						fmt.Printf("[중복 발견] IP: %s (파일: %s)\n", dIP, file.Name())
						foundCount++
						alreadyPrinted = true
					}
					isDup = true
					if matchedLine == nil {
						matchedLine = aFields
					}
				}
			}
		}
		flushBlock()
		f.Close()
	}

	fmt.Println("------------------------------------------------")
	fmt.Printf("디버그: %s에서 로드된 데이터: %d건 / 검사한 호스트 수: %d건\n", aFilePath, loadCount, checkCount)

	if foundCount == 0 {
		fmt.Println("중복된 항목이 발견되지 않았습니다.")
		return
	}
	ext := filepath.Ext(aFilePath)
	outFileName := strings.TrimSuffix(aFilePath, ext) + "_delete.txt"
	outFile, err := os.Create(outFileName)
	if err != nil {
		fmt.Printf("삭제 기록 파일 생성 실패: %v\n", err)
		return
	}
	defer outFile.Close()
	for _, line := range deleteLines {
		outFile.WriteString(line + "\n")
	}
	fmt.Printf("중복 항목 기록 파일 생성 완료: %s\n", outFileName)
}
