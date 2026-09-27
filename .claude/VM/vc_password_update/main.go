// vc_password_update: 여러 vCenter를 순회하며 admin 계정(Administrator@vsphere.local) 권한으로
// 대상 계정(기본 lscsystems@vsphere.local)의 비밀번호를 "기존과 같은 값"으로 강제 재설정한다.
// vCenter 비밀번호는 만료일 자체를 늘릴 수 없어, 같은 값으로 admin 재설정(ssoadmin.ResetPersonPassword)해서
// 비밀번호 이력 정책을 우회하고 만료 타이머만 리셋하는 방식이다(랩 검증 완료 — WORKFLOW.md 참고).
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/ssoadmin"
	"github.com/vmware/govmomi/sts"
	"github.com/vmware/govmomi/vim25/soap"
)

const defaultTargetID = "lscsystems@vsphere.local"
const defaultAdminID = "Administrator@vsphere.local"

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, s.Err()
}

// secretFile: secret_lib.sh 의 secret_file() 규칙과 동일 — <dir>/vcenter_<계정, '/'는 '_'>.enc
func secretFile(dir, id string) string {
	return filepath.Join(dir, "vcenter_"+strings.ReplaceAll(id, "/", "_")+".enc")
}

// decryptSecret: secret_lib.sh 의 secret_get() 과 완전히 같은 openssl 파라미터로 복호화한다
// (같은 key 파일 + 같은 -aes-256-cbc -md sha256 -a 옵션이어야 passwd_update.sh 로 저장한 파일을 풀 수 있음).
func decryptSecret(dir, id string) (string, error) {
	keyPath := filepath.Join(dir, "key")
	encPath := secretFile(dir, id)
	if _, err := os.Stat(keyPath); err != nil {
		return "", fmt.Errorf("키 파일이 없습니다: %s", keyPath)
	}
	if _, err := os.Stat(encPath); err != nil {
		return "", fmt.Errorf("암호 파일이 없습니다: %s (passwd_update.sh 로 먼저 저장하세요)", encPath)
	}
	cmd := exec.Command("openssl", "enc", "-d", "-aes-256-cbc", "-md", "sha256", "-a",
		"-pass", "file:"+keyPath, "-in", encPath)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("복호화 실패: %v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	pw := out.String()
	if pw == "" {
		return "", fmt.Errorf("복호화 결과가 비어 있습니다 (키가 다른 서버의 것일 수 있음)")
	}
	return pw, nil
}

// ssoLogin: govmomi vim25 세션과 별개로, SSO Admin API 용 세션을 STS 토큰으로 발급받아 로그인한다.
// (govmomi 세션 쿠키는 SSO Admin 서버 인증에 쓸 수 없어 별도 로그인이 필요 — govmomi cli/sso 참고)
func ssoLogin(ctx context.Context, vc *govmomi.Client, login *url.Userinfo) (*ssoadmin.Client, error) {
	c, err := ssoadmin.NewClient(ctx, vc.Client)
	if err != nil {
		return nil, fmt.Errorf("ssoadmin 클라이언트 생성 실패: %w", err)
	}
	tokens, err := sts.NewClient(ctx, vc.Client)
	if err != nil {
		return nil, fmt.Errorf("STS 클라이언트 생성 실패: %w", err)
	}
	security, err := tokens.Issue(ctx, sts.TokenRequest{
		Certificate: vc.Client.Certificate(),
		Userinfo:    login,
	})
	if err != nil {
		return nil, fmt.Errorf("STS 토큰 발급 실패: %w (vCenter와 이 서버의 시계가 어긋나면 발생할 수 있음)", err)
	}
	if err := c.Login(c.WithHeader(ctx, soap.Header{Security: security})); err != nil {
		return nil, fmt.Errorf("SSO Admin 로그인 실패: %w", err)
	}
	return c, nil
}

// resetOne: vCenter 한 대에 admin 으로 접속해 대상 계정 비밀번호를 강제 재설정한다.
func resetOne(ctx context.Context, host, adminID, adminPW, targetName, newPW string) error {
	u := &url.URL{Scheme: "https", Host: host, Path: "/sdk"}
	u.User = url.UserPassword(adminID, adminPW)

	vc, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return fmt.Errorf("vCenter 로그인 실패: %w", err)
	}
	defer vc.Logout(ctx)

	sc, err := ssoLogin(ctx, vc, u.User)
	if err != nil {
		return err
	}
	defer sc.Logout(ctx)

	if _, err := sc.FindPersonUser(ctx, targetName); err != nil {
		return fmt.Errorf("대상 계정 '%s' 조회 실패: %w (계정명 확인 필요)", targetName, err)
	}

	if err := sc.ResetPersonPassword(ctx, targetName, newPW); err != nil {
		return fmt.Errorf("비밀번호 재설정 실패: %w", err)
	}
	return nil
}

func main() {
	dir := flag.String("dir", "", "vcenter_<계정>.enc 와 key 가 있는 secret 폴더 경로 (필수, passwd_update.sh 로 생성)")
	vcFile := flag.String("vc", "", "vCenter 목록 파일 경로 (필수, 한 줄에 IP 하나, '#' 주석)")
	id := flag.String("id", defaultTargetID, "비밀번호를 갱신할 대상 계정")
	adminID := flag.String("adminId", defaultAdminID, "로그인에 사용할 admin 계정")
	timeoutSec := flag.Int("timeout", 30, "vCenter 한 대당 접속 제한시간(초) — 응답 없는 vCenter가 전체를 막지 않게 함")
	flag.Parse()

	if *dir == "" || *vcFile == "" {
		fmt.Fprintln(os.Stderr, "사용법: vc_password_update -dir <secret 폴더> -vc <vcenter.txt> [-id <대상 계정>] [-adminId <admin 계정>]")
		os.Exit(2)
	}
	adminPW := os.Getenv("ADMIN_PASSWORD")
	if adminPW == "" {
		log.Fatal("[오류] 환경변수 'ADMIN_PASSWORD' 가 설정되지 않았습니다 (admin 계정 로그인 비밀번호)")
	}

	targetPW, err := decryptSecret(*dir, *id)
	if err != nil {
		log.Fatalf("[오류] 대상 계정 비밀번호 복호화 실패: %v", err)
	}
	// SSO Admin API 는 계정의 로컬 이름만 받는다 (도메인 접미사 제외)
	targetName := strings.SplitN(*id, "@", 2)[0]

	hosts, err := readLines(*vcFile)
	if err != nil {
		log.Fatalf("[오류] vCenter 목록 로드 실패 (%s): %v", *vcFile, err)
	}
	if len(hosts) == 0 {
		log.Fatalf("[오류] %s 에 vCenter 가 없습니다.", *vcFile)
	}

	fmt.Printf("[INFO] 비밀번호 갱신 시작: 대상 계정 '%s', vCenter %d대 (admin: %s)\n", *id, len(hosts), *adminID)

	timeout := time.Duration(*timeoutSec) * time.Second
	var okCount, failCount int
	for _, host := range hosts {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err := resetOne(ctx, host, *adminID, adminPW, targetName, targetPW)
		cancel()
		if err != nil {
			fmt.Printf("  -> [%s] 실패: %v\n", host, err)
			failCount++
			continue
		}
		fmt.Printf("  -> [%s] 성공\n", host)
		okCount++
	}

	fmt.Printf("\n[INFO] 완료 — 성공 %d / 실패 %d (전체 %d)\n", okCount, failCount, len(hosts))
	if failCount > 0 {
		os.Exit(1)
	}
}
