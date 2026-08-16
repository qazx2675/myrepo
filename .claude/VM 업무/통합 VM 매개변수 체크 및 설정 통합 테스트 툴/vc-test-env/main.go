// vc-test-env: 실 vCenter 구조를 vcsim으로 복제해서 테스트 환경을 만드는 도구.
//
// 사용법:
//
//	vc-test-env [-vc=<host>] [-refresh]         실 vCenter 복제 + vcsim 기동 (기본 동작)
//	vc-test-env extract -vc=<host>              레시피만 추출/갱신
//	vc-test-env tree -vc=<host>                 대상(실 vCenter/vcsim 무엇이든)을 트리로 출력
//	vc-test-env diff -vc=<host> -sim=<addr>     원본 레시피와 떠 있는 vcsim을 비교
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"vc-test-env/internal/builder"
	"vc-test-env/internal/connect"
	"vc-test-env/internal/history"
	"vc-test-env/internal/inventory"
	"vc-test-env/internal/recipe"
	"vc-test-env/internal/tree"
)

func flagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ExitOnError)
}

func main() {
	ctx := context.Background()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "extract":
			exitOnErr(runExtract(ctx, os.Args[2:]))
			return
		case "tree":
			exitOnErr(runTree(ctx, os.Args[2:]))
			return
		case "diff":
			exitOnErr(runDiff(ctx, os.Args[2:]))
			return
		}
	}
	exitOnErr(runUp(ctx, os.Args[1:]))
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
}

// --- extract ---

func runExtract(ctx context.Context, args []string) error {
	fs := flagSet("extract")
	vc := fs.String("vc", "", "실 vCenter 주소")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := *vc
	if target == "" {
		return fmt.Errorf("-vc=<vCenter 주소>가 필요합니다")
	}

	r, err := extract(ctx, target)
	if err != nil {
		return err
	}

	path, err := history.RecipePath(target)
	if err != nil {
		return err
	}
	if err := recipe.Save(path, r); err != nil {
		return err
	}
	fmt.Println("레시피 저장됨:", path)
	return nil
}

func extract(ctx context.Context, target string) (*recipe.Recipe, error) {
	c, err := connect.Client(ctx, target)
	if err != nil {
		return nil, err
	}
	defer c.Logout(ctx)

	inv, err := inventory.Walk(ctx, c)
	if err != nil {
		return nil, err
	}

	return &recipe.Recipe{
		Source:      target,
		ExtractedAt: time.Now().Format(time.RFC3339),
		Inventory:   inv,
	}, nil
}

// --- tree ---

func runTree(ctx context.Context, args []string) error {
	fs := flagSet("tree")
	vc := fs.String("vc", "", "조회할 대상(실 vCenter 또는 vcsim) 주소")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *vc == "" {
		return fmt.Errorf("-vc=<대상 주소>가 필요합니다")
	}

	c, err := connect.Client(ctx, *vc)
	if err != nil {
		return err
	}
	defer c.Logout(ctx)

	inv, err := inventory.Walk(ctx, c)
	if err != nil {
		return err
	}
	tree.Render(os.Stdout, inv)
	return nil
}

// --- diff ---

func runDiff(ctx context.Context, args []string) error {
	fs := flagSet("diff")
	vc := fs.String("vc", "", "원본 실 vCenter 주소 (캐시된 레시피를 찾는 키)")
	sim := fs.String("sim", "", "비교할 vcsim 주소")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *vc == "" || *sim == "" {
		return fmt.Errorf("-vc=<원본 vCenter>와 -sim=<vcsim 주소>가 모두 필요합니다")
	}

	path, err := history.RecipePath(*vc)
	if err != nil {
		return err
	}
	original, err := recipe.Load(path)
	if err != nil {
		return fmt.Errorf("원본 레시피를 찾을 수 없습니다(먼저 extract 필요): %w", err)
	}

	c, err := connect.Client(ctx, *sim)
	if err != nil {
		return err
	}
	defer c.Logout(ctx)

	rebuilt, err := inventory.Walk(ctx, c)
	if err != nil {
		return err
	}

	diffs := recipe.Diff(original.Inventory, rebuilt)
	if len(diffs) == 0 {
		fmt.Println("차이 없음")
		return nil
	}
	for _, d := range diffs {
		fmt.Println("-", d)
	}
	return nil
}

// --- up (기본 동작) ---

func runUp(ctx context.Context, args []string) error {
	fs := flagSet("up")
	vcFlag := fs.String("vc", "", "복제할 실 vCenter 주소")
	refresh := fs.Bool("refresh", false, "캐시된 레시피를 쓰지 않고 다시 추출")
	if err := fs.Parse(args); err != nil {
		return err
	}

	target, err := resolveTarget(*vcFlag)
	if err != nil {
		return err
	}

	h, err := history.Load()
	if err != nil {
		return err
	}

	path, err := history.RecipePath(target)
	if err != nil {
		return err
	}

	var r *recipe.Recipe
	if !*refresh {
		if cached, err := recipe.Load(path); err == nil {
			r = cached
			fmt.Println("캐시된 레시피 사용:", path, "(최신화하려면 -refresh)")
		}
	}
	if r == nil {
		fmt.Println(target, "에서 추출 중...")
		r, err = extract(ctx, target)
		if err != nil {
			return err
		}
		if err := recipe.Save(path, r); err != nil {
			return err
		}
		fmt.Println("레시피 저장됨:", path)
	}
	if err := h.Add(target); err != nil {
		return err
	}

	fmt.Println("vcsim 기동 중...")
	res, err := builder.Start(ctx, r)
	if err != nil {
		return err
	}
	defer res.Close()

	fmt.Println()
	fmt.Println("=== 재생성된 구조 (vcsim을 직접 조회) ===")
	rebuiltInv, err := inventory.Walk(ctx, res.Client)
	if err != nil {
		return fmt.Errorf("재생성된 vcsim 조회 실패: %w", err)
	}
	tree.Render(os.Stdout, rebuiltInv)

	fmt.Println()
	fmt.Println("=== 접속 정보 ===")
	fmt.Println("vcsim 주소:", res.URL)
	fmt.Printf("Go 도구 예시: ./vm-param-check -vcTargetIP=%s -id=administrator@vsphere.local ...\n", res.URL)
	fmt.Printf("PowerCLI 예시: Connect-VIServer -Server %s -User administrator@vsphere.local -Password <아무값> -Force\n", res.URL)
	fmt.Println()
	fmt.Println("Ctrl+C를 누르면 vcsim이 종료됩니다.")

	waitForInterrupt()
	fmt.Println("종료 중...")
	return nil
}

func resolveTarget(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}

	h, err := history.Load()
	if err != nil {
		return "", err
	}

	switch len(h.Targets) {
	case 0:
		return promptLine("접속할 vCenter 주소를 입력하세요: ")
	case 1:
		return h.Targets[0], nil
	default:
		fmt.Println("이전에 접속했던 vCenter 목록:")
		for i, t := range h.Targets {
			fmt.Printf("  %d) %s\n", i+1, t)
		}
		fmt.Printf("  %d) 새로 추가\n", len(h.Targets)+1)
		choice, err := promptLine("선택: ")
		if err != nil {
			return "", err
		}
		n, err := strconv.Atoi(choice)
		if err != nil || n < 1 || n > len(h.Targets)+1 {
			return "", fmt.Errorf("잘못된 선택입니다: %s", choice)
		}
		if n == len(h.Targets)+1 {
			return promptLine("접속할 vCenter 주소를 입력하세요: ")
		}
		return h.Targets[n-1], nil
	}
}

func promptLine(prompt string) (string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	if line == "" {
		return "", fmt.Errorf("빈 값은 입력할 수 없습니다")
	}
	return line, nil
}

func waitForInterrupt() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
