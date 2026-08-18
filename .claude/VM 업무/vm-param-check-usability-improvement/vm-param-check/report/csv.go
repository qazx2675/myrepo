package report

import (
	"encoding/csv"
	"os"
	"sort"
	"strconv"
	"unicode"

	"vm-param-check/model"
)

var csvHeader = []string{"VM명", "소스", "항목Key", "기대값", "실제값", "결과", "비고"}
var summaryCSVHeader = []string{"VM명", "전체결과", "OK", "FAIL", "설정없음", "미지원", "정보"}

// sourceRank는 CSV 정렬 시 소스 컬럼의 노출 순서를 고정한다.
// 사람이 스프레드시트로 훑을 때 "공통설정 -> 호스트 -> ev01 -> ev02 -> ev03 -> 네트워크" 순으로
// 읽히는 게 자연스러워서 이 순서로 정렬한다.
var sourceRank = map[string]int{
	"-":       0,
	"host":    1,
	"ev01":    2,
	"ev02":    3,
	"ev03":    4,
	"network": 5,
}

// WriteCSV는 findings 전부(OK 포함, 필터링 없음)를 CSV 한 파일에 기록한다.
// 정렬 순서: VM명 -> 소스(sourceRank) -> 항목Key. VM별로 묶이고 그 안에서 카테고리가
// 뭉쳐 있어야 스프레드시트에서 훑어보기 쉽다.
func WriteCSV(path string, findings []model.Finding) error {
	sorted := make([]model.Finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.VM != b.VM {
			return a.VM < b.VM
		}
		ra, rb := sourceRank[a.Source], sourceRank[b.Source]
		if ra != rb {
			return ra < rb
		}
		return naturalLess(a.Key, b.Key)
	})

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Excel에서 한글이 깨지지 않도록 UTF-8 BOM을 앞에 붙인다.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, fd := range sorted {
		row := []string{fd.VM, fd.Source, fd.Key, fd.Expected, fd.Actual, fd.Result, fd.Note}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// WriteSummaryCSV는 VM별 한 줄 요약(PASS/FAIL, OK/FAIL/설정없음/정보 개수)만 담은
// 별도 CSV를 만든다. 대수가 많을 때 상세 CSV(WriteCSV)를 열지 않고도 이 파일 하나로
// 전체 현황을 훑어볼 수 있게 하려는 용도 — 상세 CSV와 파일을 분리해서 관리한다.
func WriteSummaryCSV(path string, findings []model.Finding) error {
	statuses := Summarize(findings)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(summaryCSVHeader); err != nil {
		return err
	}
	for _, s := range statuses {
		row := []string{
			s.VM, s.Overall,
			strconv.Itoa(s.OK), strconv.Itoa(s.Fail), strconv.Itoa(s.NoValue), strconv.Itoa(s.Unsupported), strconv.Itoa(s.Info),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// naturalLess는 "sched.vcpu2.affinity" < "sched.vcpu10.affinity"가 되도록 문자열을
// 숫자 구간(연속된 digit)과 텍스트 구간으로 쪼개서 비교한다. 순수 문자열 비교(<)로는
// "10"이 "2"보다 문자상 앞에 와서(1<2) vcpu 개수가 두 자리로 넘어가면 순서가 깨진다.
func naturalLess(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	ai, bi := 0, 0
	for ai < len(ar) && bi < len(br) {
		if unicode.IsDigit(ar[ai]) && unicode.IsDigit(br[bi]) {
			aEnd := ai
			for aEnd < len(ar) && unicode.IsDigit(ar[aEnd]) {
				aEnd++
			}
			bEnd := bi
			for bEnd < len(br) && unicode.IsDigit(br[bEnd]) {
				bEnd++
			}
			an := trimLeadingZeros(string(ar[ai:aEnd]))
			bn := trimLeadingZeros(string(br[bi:bEnd]))
			if len(an) != len(bn) {
				return len(an) < len(bn) // 자릿수가 다르면 짧은 쪽(=작은 수)이 앞
			}
			if an != bn {
				return an < bn // 자릿수 같으면 문자열 비교가 곧 숫자 비교와 같은 결과
			}
			ai, bi = aEnd, bEnd
			continue
		}
		if ar[ai] != br[bi] {
			return ar[ai] < br[bi]
		}
		ai++
		bi++
	}
	return len(ar)-ai < len(br)-bi
}

func trimLeadingZeros(s string) string {
	i := 0
	for i < len(s)-1 && s[i] == '0' {
		i++
	}
	return s[i:]
}
