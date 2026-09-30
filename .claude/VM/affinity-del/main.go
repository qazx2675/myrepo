// affinity_del: list.txt 의 VM 에서 sched.vcpuN.affinity 설정(ExtraConfig)을 삭제하는 임시 도구. 값을 빈 문자열로 재구성하면 vSphere 가 키를 제거한다.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

var affinityKey = regexp.MustCompile(`^sched\.vcpu\d+\.affinity$`)

func readList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	return names, sc.Err()
}

func main() {
	vc := flag.String("vc", "", "vCenter 주소/IP (필수)")
	id := flag.String("id", "administrator@vsphere.local", "vCenter 계정 ID")
	pw := flag.String("pw", os.Getenv("VC_PW"), "vCenter 비밀번호 (미지정 시 환경변수 VC_PW)")
	list := flag.String("list", "list.txt", "삭제 대상 VM 이름 목록 파일")
	dry := flag.Bool("dry-run", false, "삭제하지 않고 대상 키만 출력")
	flag.Parse()

	if *vc == "" || *pw == "" {
		fmt.Fprintln(os.Stderr, "사용법: affinity_del -vc <vCenter> [-id ID] [-pw PW | VC_PW 환경변수] [-list list.txt] [-dry-run]")
		os.Exit(2)
	}
	names, err := readList(*list)
	if err != nil {
		fmt.Fprintln(os.Stderr, "목록 파일 오류:", err)
		os.Exit(1)
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "목록 파일에 VM 이름이 없습니다")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	u := &url.URL{Scheme: "https", Host: *vc, Path: "/sdk", User: url.UserPassword(*id, *pw)}
	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vCenter 접속 실패:", err)
		os.Exit(1)
	}
	defer c.Logout(ctx)

	finder := find.NewFinder(c.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "데이터센터 조회 실패:", err)
		os.Exit(1)
	}
	finder.SetDatacenter(dc)

	var ok, skip, fail int
	for _, name := range names {
		vm, err := finder.VirtualMachine(ctx, name)
		if err != nil {
			fmt.Printf("[FAIL] %s: VM 을 찾을 수 없음 (%v)\n", name, err)
			fail++
			continue
		}
		if err := process(ctx, vm, name, *dry, &ok, &skip); err != nil {
			fmt.Printf("[FAIL] %s: %v\n", name, err)
			fail++
		}
	}
	fmt.Printf("\n완료: 삭제 %d / 대상없음 %d / 실패 %d\n", ok, skip, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

func process(ctx context.Context, vm *object.VirtualMachine, name string, dry bool, ok, skip *int) error {
	var mvm mo.VirtualMachine
	if err := vm.Properties(ctx, vm.Reference(), []string{"config.extraConfig"}, &mvm); err != nil {
		return err
	}
	var edits []types.BaseOptionValue
	var keys []string
	if mvm.Config != nil {
		for _, o := range mvm.Config.ExtraConfig {
			ov := o.GetOptionValue()
			if affinityKey.MatchString(ov.Key) {
				keys = append(keys, fmt.Sprintf("%s=%v", ov.Key, ov.Value))
				edits = append(edits, &types.OptionValue{Key: ov.Key, Value: ""})
			}
		}
	}
	if len(edits) == 0 {
		fmt.Printf("[SKIP] %s: sched.vcpuN.affinity 설정 없음\n", name)
		*skip++
		return nil
	}
	if dry {
		fmt.Printf("[DRY ] %s: 삭제 예정 %s\n", name, strings.Join(keys, ", "))
		*ok++
		return nil
	}
	task, err := vm.Reconfigure(ctx, types.VirtualMachineConfigSpec{ExtraConfig: edits})
	if err != nil {
		return err
	}
	if err := task.Wait(ctx); err != nil {
		return err
	}
	fmt.Printf("[ OK ] %s: 삭제 %d건 (%s)\n", name, len(edits), strings.Join(keys, ", "))
	*ok++
	return nil
}
