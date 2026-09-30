// pwreset 은 비밀번호 롤테이션 스크립트가 누락되어 SSH 접속 시 비밀번호
// 변경 프롬프트가 뜨는 Rocky Linux 서버를, 알고 있는 과거 비밀번호 후보로
// 자동 로그인한 뒤 지정된 새 비밀번호로 맞춰주는 소규모 복구 자동화 도구입니다.
//
// 사용법:
//
//	pwreset -list list.txt -oldpw old_password.txt -newpw newpass.enc -key key.bin -out result.csv
//	pwreset -encrypt -in plain.txt -out newpass.enc -key key.bin
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
)

func main() {
	var (
		doEncrypt = flag.Bool("encrypt", false, "새 비밀번호 평문을 암호화해 -out 에 저장하는 서브커맨드")
		in        = flag.String("in", "", "-encrypt: 평문 비밀번호 파일 경로")

		list  = flag.String("list", "list.txt", "대상 IP 목록 파일 (줄바꿈으로 IP 나열, 계정 root 고정)")
		oldpw = flag.String("oldpw", "old_password.txt", "과거 비밀번호 후보 목록 (최신이 맨 위)")
		newpw = flag.String("newpw", "newpass.enc", "암호화된 새 비밀번호 파일")
		key   = flag.String("key", "key.bin", "AES-256 키 파일 (없으면 -encrypt 시 생성)")
		out   = flag.String("out", "result.csv", "결과 CSV 경로 (-encrypt 시에는 암호문 출력 경로)")
	)
	flag.Parse()

	var err error
	if *doEncrypt {
		err = runEncrypt(*in, *out, *key)
	} else {
		err = runReset(*list, *oldpw, *newpw, *key, *out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류: "+err.Error())
		os.Exit(1)
	}
}

// runEncrypt 는 평문 새 비밀번호를 암호화해 파일로 저장합니다.
func runEncrypt(in, out, keyPath string) error {
	if in == "" {
		return fmt.Errorf("-encrypt 에는 -in <평문파일> 이 필요합니다")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("평문 파일: %w", err)
	}
	plain := strings.TrimRight(string(raw), "\r\n")

	k, err := loadOrCreateKey(keyPath)
	if err != nil {
		return fmt.Errorf("키 파일: %w", err)
	}
	ct, err := encrypt([]byte(plain), k)
	if err != nil {
		return fmt.Errorf("암호화: %w", err)
	}
	if err := os.WriteFile(out, ct, 0o600); err != nil {
		return fmt.Errorf("암호문 저장: %w", err)
	}
	fmt.Printf("암호화 완료: %s (키: %s)\n", out, keyPath)
	return nil
}

// runReset 은 대상 서버들의 비밀번호 변경 프롬프트를 후보 비밀번호로 처리합니다.
func runReset(list, oldpw, newpw, keyPath, out string) error {
	k, err := loadKey(keyPath)
	if err != nil {
		return fmt.Errorf("키 파일: %w", err)
	}
	encNew, err := os.ReadFile(newpw)
	if err != nil {
		return fmt.Errorf("새 비밀번호 파일: %w", err)
	}
	newPassBytes, err := decrypt(encNew, k)
	if err != nil {
		return fmt.Errorf("새 비밀번호 복호화: %w", err)
	}
	newPass := string(newPassBytes)

	targets, err := loadTargets(list)
	if err != nil {
		return fmt.Errorf("대상 목록: %w", err)
	}
	candidates, err := loadPasswords(oldpw)
	if err != nil {
		return fmt.Errorf("후보 비밀번호: %w", err)
	}

	fmt.Printf("대상=%d대  후보=%d개\n", len(targets), len(candidates))

	// 서버별 goroutine 병렬 처리 (계정 잠금 정책 없음 전제, 시도 간 딜레이 불필요).
	results := make([]Result, len(targets))
	var wg sync.WaitGroup
	for i, ip := range targets {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			results[i] = processHost(ip, candidates, newPass)
		}(i, ip)
	}
	wg.Wait()

	if err := writeCSV(out, results); err != nil {
		return fmt.Errorf("결과 저장: %w", err)
	}
	fmt.Printf("결과 저장: %s\n", out)
	return nil
}

// processHost 는 한 대상에 대해 후보 비밀번호를 최신부터 순서대로 시도합니다.
//
// Permission denied(접속 실패) 시 다음 후보로 재시도하고, 모두 실패하면
// "접속불가" 로 기록합니다.
func processHost(ip string, candidates []string, newPass string) Result {
	for idx, cand := range candidates {
		applied, authFailed := tryCandidate(ip, cand, newPass)
		if authFailed {
			continue
		}
		return Result{IP: ip, FoundIndex: idx, Status: "성공", Applied: applied}
	}
	return Result{IP: ip, FoundIndex: -1, Status: "접속불가", Applied: false}
}
