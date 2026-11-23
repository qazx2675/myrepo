// confirm.go - 시작 경고, 현황·방식 표시, 재설치 확인
package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

const (
	red   = "\033[31m"
	reset = "\033[0m"
)

// 설치 방식
const (
	methodMount = "mount"
	methodScp   = "scp"
)

// Target 은 호스트 하나의 작업 상태다. Skip 또는 Fail 이 비어 있지 않으면 설치하지 않는다.
type Target struct {
	Entry
	Status *HostStatus
	Method string // methodMount / methodScp
	Skip   string // 건너뛴 사유
	Fail   string // fail 사유 (fail_list 에 쓴다)

	NewDriver string // 설치 후 nvidia-smi 드라이버 버전
	NewCUDA   string // 설치 후 nvidia-smi CUDA 버전

	DcgmOnly bool       // 같은 버전이라 드라이버는 건너뛰고 DCGM 만 맞춘다
	Dcgm     *DcgmState // DCGM 설치 뒤 확인한 상태 (DCGM 단계를 안 했으면 nil)
	DcgmNote string     // DCGM 을 건너뛴 사유 (예: skip(CUDA 11.4))
}

// 같은 버전이라 건너뛸 때의 Skip 사유
const skipSameVersion = "같은 버전 설치됨"

// ConfirmStart 는 대상 목록과 요청 버전을 빨간색 경고로 보여 주고 y 를 입력해야 true 를 돌려준다.
func ConfirmStart(in *bufio.Reader, out io.Writer, es []Entry) bool {
	fmt.Fprint(out, red)
	fmt.Fprintf(out, "===== GPU 드라이버 설치 대상 %d대 =====\n", len(es))
	for _, e := range es {
		fmt.Fprintf(out, "  %-20s %s\n", e.Host, e.Version)
	}
	fmt.Fprintln(out, "작업 전, 실제로 작업 가능한 장비(서비스 중지·드레인 완료)가 맞는지 확인하세요.")
	fmt.Fprint(out, "진행하려면 y 를 입력하세요: "+reset)
	s, _ := in.ReadString('\n')
	return strings.TrimSpace(s) == "y" || strings.TrimSpace(s) == "Y"
}

// BuildTargets 는 현황으로 Target 을 만든다. 현황을 못 읽은 호스트는 Fail,
// /user/<MOUNT_USER> 접근과 .run 읽기가 모두 되면 mount, 아니면 scp 로 분류한다.
func BuildTargets(es []Entry, sts map[string]*HostStatus) []*Target {
	var ts []*Target
	for _, e := range es {
		st := sts[e.Host]
		if st == nil {
			st = &HostStatus{Err: "응답 없음"}
		}
		t := &Target{Entry: e, Status: st, Method: methodScp}
		if st.Err != "" {
			t.Fail = st.Err
		} else if st.UserMount && st.RunReadable[e.Version] {
			t.Method = methodMount
		}
		ts = append(ts, t)
	}
	return ts
}

// notes 는 현황에서 눈여겨볼 항목을 짧게 모은다(표시용).
func notes(st *HostStatus) string {
	var n []string
	if st.GPUs == 0 {
		n = append(n, "GPU 없음")
	}
	if !st.KernelDevel {
		n = append(n, "kernel-devel 없음")
	}
	if !st.GCC {
		n = append(n, "gcc 없음")
	}
	if st.Nouveau {
		n = append(n, "nouveau 로드됨")
	}
	if st.NVSwitch || st.FabricManager != "" {
		n = append(n, "fabricmanager 버전을 드라이버와 맞춰야 함")
	}
	return strings.Join(n, ", ")
}

// PrintStatus 는 호스트별 현황과 mount/scp 판정을 표로 출력한다.
func PrintStatus(out io.Writer, ts []*Target) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HOST\thostname\t현재드라이버\tCUDA\t요청버전\t방식\t비고")
	for _, t := range ts {
		st := t.Status
		if t.Fail != "" {
			fmt.Fprintf(w, "%s\t-\t-\t-\t%s\t-\tfail: %s\n", t.Host, t.Version, t.Fail)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", t.Host, st.Hostname, dash(st.Driver), dash(st.CUDA),
			t.Version, t.Method, notes(st))
	}
	w.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// AskReinstall 은 이미 드라이버가 깔린 호스트를 "hostname 현재 → 요청" 으로 보여 주고
// a(전체)/s(골라서)/n(건너뛰기) 를 묻는다. 현재와 요청 버전이 같은 호스트는 a 에서도 건너뛰고,
// s 에서 번호로 고른 경우에만 다시 설치한다. 선택되지 않은 호스트는 Skip 을 채운다.
func AskReinstall(in *bufio.Reader, out io.Writer, ts []*Target) error {
	var cand []*Target
	for _, t := range ts {
		if t.Fail == "" && t.Skip == "" && t.Status.Driver != "" {
			cand = append(cand, t)
		}
	}
	if len(cand) == 0 {
		return nil
	}
	fmt.Fprintln(out, "이미 드라이버가 설치된 서버:")
	for i, t := range cand {
		same := ""
		if t.Status.Driver == t.Version {
			same = "  (같은 버전, 기본 건너뜀)"
		}
		fmt.Fprintf(out, "  %d) %s %s → %s%s\n", i+1, t.Host, t.Status.Driver, t.Version, same)
	}
	for {
		fmt.Fprint(out, "재설치 a(전체)/s(골라서)/n(건너뛰기): ")
		s, err := in.ReadString('\n')
		pick := map[int]bool{}
		switch strings.TrimSpace(s) {
		case "a", "A":
			for i, t := range cand {
				pick[i] = t.Status.Driver != t.Version
			}
		case "n", "N":
		case "s", "S":
			p, err := askNumbers(in, out, len(cand))
			if err != nil {
				return err
			}
			pick = p
		default:
			if err != nil {
				return fmt.Errorf("재설치 여부를 받지 못했습니다: %w", err)
			}
			fmt.Fprintln(out, "잘못된 입력입니다.")
			continue
		}
		for i, t := range cand {
			if !pick[i] {
				if t.Status.Driver == t.Version {
					t.Skip = skipSameVersion
				} else {
					t.Skip = "재설치 안 함"
				}
			}
		}
		return nil
	}
}

// askNumbers 는 "1 3" / "1,3" 형식의 번호(1~n)를 받는다. 잘못되면 다시 묻는다.
func askNumbers(in *bufio.Reader, out io.Writer, n int) (map[int]bool, error) {
	for {
		fmt.Fprintf(out, "재설치할 번호 (1-%d, 공백·쉼표 구분): ", n)
		s, err := in.ReadString('\n')
		f := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r' })
		pick := map[int]bool{}
		okAll := len(f) > 0
		for _, x := range f {
			k, e := strconv.Atoi(x)
			if e != nil || k < 1 || k > n {
				okAll = false
				break
			}
			pick[k-1] = true
		}
		if okAll {
			return pick, nil
		}
		if err != nil {
			return nil, fmt.Errorf("재설치 번호를 받지 못했습니다: %w", err)
		}
		fmt.Fprintln(out, "잘못된 입력입니다.")
	}
}
