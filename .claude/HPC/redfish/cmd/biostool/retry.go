package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// retry.go 는 `check`/`allcheck` 의 -retry-from (단계 7: 실패 건 재점검 + 결과 병합) 입니다.
//
// 이전 결과 폴더의 retry.txt 대상만 다시 점검해 새 결과 폴더에 쓰고, 이전 결과의 해당 호스트 행을
// 새 행으로 바꾼 병합본(merged_*)을 같은 새 폴더에 함께 만듭니다.
//   - 이전 결과 폴더의 파일은 읽기만 합니다 (열기·쓰기·삭제·이름 변경 없음).
//   - 호스트 키는 결과 파일의 호스트 열(check: result.tsv hostname, allcheck: summary.tsv host)입니다.
//     retry.txt 의 원문(Input)이 아니라 이번 실행에서 같은 규칙으로 해석한 Target.Hostname 을 쓰므로
//     `host`, `host-m`, `IP:포트` 어느 표기든 이전 행과 같은 키가 됩니다 (대소문자 무시).
//   - 이전 결과의 재시도 대상이 아닌 호스트의 행(AUTH_FAIL 포함)은 한 글자도 바꾸지 않고 제자리에 둡니다.
//   - 재시도한 호스트의 이전 행은 새 행 전체로 바뀝니다 (호스트당 행이 겹치거나 사라지지 않음).

// errNoRetryTargets 는 재시도 목록이 없거나 비어 있음을 뜻합니다 (오류가 아니라 정상 종료로 다룹니다).
var errNoRetryTargets = errors.New("재시도할 대상이 없습니다")

// retryPrevDir 는 -retry-from 값(폴더 또는 그 안의 retry.txt)에서 이전 결과 폴더를 구합니다.
func retryPrevDir(pathOrDir string) string {
	if fi, err := os.Stat(pathOrDir); err == nil && !fi.IsDir() {
		return filepath.Dir(pathOrDir)
	}
	return pathOrDir
}

// retryPreflight 는 -retry-from 을 쓸 때의 사전 점검입니다 (접속·결과 폴더 생성 전).
// -user 를 같이 지정했으면 오류, 재시도 목록이 없거나 비어 있으면 안내를 출력하고 proceed=false 입니다 (종료코드 0, 아무것도 쓰지 않음).
func retryPreflight(fs *flag.FlagSet, o *commonOpts, out io.Writer) (proceed bool, err error) {
	if o.retryFrom == "" {
		return true, nil
	}
	userSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "user" {
			userSet = true
		}
	})
	if userSet {
		return false, fmt.Errorf("-retry-from 과 -user 를 함께 쓸 수 없습니다 (대상은 이전 결과의 retry.txt 입니다)")
	}
	if _, err := loadRetryList(o.retryFrom); err != nil {
		if errors.Is(err, errNoRetryTargets) {
			fmt.Fprintln(out, err.Error())
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ---- 이전 결과 읽기 ----

// tsvTable 은 머리글을 검증한 TSV 의 데이터 행입니다 (모든 행의 열 수가 머리글과 같음).
type tsvTable struct {
	Rows [][]string
}

// readTSVTable 은 TSV 를 읽어 머리글이 header 와 정확히 같은지, 모든 행의 열 수가 같은지 확인합니다.
// UTF-8 BOM, CRLF, 빈 줄은 무시합니다. 파일을 열기만 합니다 (읽기 전용).
func readTSVTable(path string, header []string) (*tsvTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	var rows [][]string
	var lineNos []int
	for i, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		rows = append(rows, strings.Split(ln, "\t"))
		lineNos = append(lineNos, i+1)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("비어 있습니다")
	}
	if len(rows[0]) != len(header) {
		return nil, fmt.Errorf("머리글의 열 수가 다릅니다 (기대 %d, 실제 %d)", len(header), len(rows[0]))
	}
	for j, h := range header {
		if rows[0][j] != h {
			return nil, fmt.Errorf("머리글의 %d번째 열이 %q 입니다 (기대 %q)", j+1, rows[0][j], h)
		}
	}
	for i := 1; i < len(rows); i++ {
		if len(rows[i]) != len(header) {
			return nil, fmt.Errorf("%d번째 줄의 열 수가 다릅니다 (기대 %d, 실제 %d)", lineNos[i], len(header), len(rows[i]))
		}
	}
	return &tsvTable{Rows: rows[1:]}, nil
}

// loadPrevTable 은 이전 결과 폴더의 plain 파일을 읽습니다. 이전 폴더가 재시도 결과라서 merged_<plain> 이 있으면
// 그쪽이 전체 결과이므로 그것을 바탕으로 합니다 (재시도를 여러 번 이어 갈 수 있음).
func loadPrevTable(dir, plain string, header []string) (*tsvTable, error) {
	name := plain
	if _, err := os.Stat(filepath.Join(dir, "merged_"+plain)); err == nil {
		name = "merged_" + plain
	}
	p := filepath.Join(dir, name)
	t, err := readTSVTable(p, header)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("이전 결과 폴더에 %s 가 없습니다 (%s) — -retry-from 은 biostool 결과 폴더(또는 그 안의 retry.txt)를 가리켜야 합니다", plain, dir)
		}
		return nil, fmt.Errorf("이전 결과 %s: %w", p, err)
	}
	return t, nil
}

// loadPrevCheck 는 check 의 이전 result.tsv 를 읽고 프로파일이 같은지 확인합니다.
func loadPrevCheck(dir, profile string) (*tsvTable, error) {
	t, err := loadPrevTable(dir, "result.tsv", resultHeader)
	if err != nil {
		return nil, err
	}
	for _, r := range t.Rows {
		if r[2] != tsvField(profile) {
			return nil, fmt.Errorf("이전 결과의 프로파일(%s)이 이번 -profile(%s)과 다릅니다. 같은 프로파일로 실행하십시오", r[2], profile)
		}
	}
	return t, nil
}

// loadPrevAll 은 allcheck 의 이전 summary.tsv 와 all_diff.tsv 를 읽습니다.
func loadPrevAll(dir string) (sum, diff *tsvTable, err error) {
	if sum, err = loadPrevTable(dir, "summary.tsv", summaryHeader); err != nil {
		return nil, nil, err
	}
	if diff, err = loadPrevTable(dir, "all_diff.tsv", allDiffHeader); err != nil {
		return nil, nil, err
	}
	return sum, diff, nil
}

// ---- 병합 ----

// rowHostKey 는 호스트 열 값을 비교용 키로 만듭니다 (대소문자 무시).
func rowHostKey(s string) string { return strings.ToLower(s) }

// mergeRowsByHost 는 prev 행 중 retried 호스트(col 열)의 행을 cur 의 같은 호스트 행으로 제자리에서 바꿉니다.
// 이전에 행이 있던 호스트는 그 첫 행 위치에 새 행 전부가 들어가고(나머지 이전 행은 제거), 이전에 행이 없던 호스트의
// 새 행은 끝에 붙습니다. retried 가 아닌 호스트의 행은 순서·내용 그대로입니다. cur 에 나온 호스트는 모두 retried 로 봅니다.
func mergeRowsByHost(prev, cur [][]string, col int, retried map[string]bool) [][]string {
	set := make(map[string]bool, len(retried))
	for k := range retried {
		set[k] = true
	}
	byHost := map[string][][]string{}
	var order []string
	for _, r := range cur {
		k := rowHostKey(r[col])
		if _, ok := byHost[k]; !ok {
			order = append(order, k)
		}
		byHost[k] = append(byHost[k], r)
		set[k] = true
	}
	placed := map[string]bool{}
	out := make([][]string, 0, len(prev)+len(cur))
	for _, r := range prev {
		k := rowHostKey(r[col])
		if !set[k] {
			out = append(out, r)
			continue
		}
		if !placed[k] {
			placed[k] = true
			out = append(out, byHost[k]...)
		}
	}
	for _, k := range order {
		if !placed[k] {
			out = append(out, byHost[k]...)
		}
	}
	return out
}

// hostState 는 병합 결과의 호스트 1대의 대표 상태입니다.
type hostState struct {
	Key, Name, State string
}

// hostStates 는 행을 호스트별(처음 나온 순서)로 묶어 대표 상태를 구합니다. resCol 은 결과(상태) 열입니다.
func hostStates(rows [][]string, hostCol, resCol int, reduce func([]string) string) []hostState {
	idx := map[string]int{}
	var out []hostState
	var res [][]string
	for _, r := range rows {
		k := rowHostKey(r[hostCol])
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, hostState{Key: k, Name: r[hostCol]})
			res = append(res, nil)
		}
		res[i] = append(res[i], r[resCol])
	}
	for i := range out {
		out[i].State = reduce(res[i])
	}
	return out
}

// itemStatuses 는 result.tsv 의 항목 단위 결과 값입니다 (그 밖의 값은 호스트 단위 오류 상태).
var itemStatuses = map[string]bool{
	StatusOK: true, StatusPendingOK: true, StatusFail: true, StatusUnverified: true,
	StatusPendingExists: true, StatusMappingMissing: true, StatusPendingNoJob: true,
}

// checkRowsState 는 result.tsv 의 한 호스트 행들의 결과 열에서 대표 상태를 고릅니다 (hostCheck.state 와 같은 규칙).
func checkRowsState(results []string) string {
	for _, r := range results {
		if !itemStatuses[r] {
			return r // 호스트 단위 오류
		}
	}
	for _, st := range statePrecedence {
		for _, r := range results {
			if r == st {
				return st
			}
		}
	}
	return StatusOK
}

func firstResult(results []string) string { return results[0] }

// isAllRetryStatus 는 allcheck 의 retry.txt 대상 상태입니다 (allcheck.go 의 retry.txt 규칙과 같음).
func isAllRetryStatus(s string) bool { return isRetryStatus(s) || s == StatusRefUnreachable }

// retryMerge 는 재시도 병합 1회의 통계입니다.
type retryMerge struct {
	PrevDir   string
	Retried   int            // 재시도한 호스트 수
	Recovered int            // 호스트 단위 오류 없이 점검이 끝난 호스트 (결과가 FAIL 이어도 복구로 센다)
	Still     int            // 여전히 일시 오류 (UNREACHABLE/TIMEOUT/BMC_ERROR/SKIPPED_AUTH_STOP, allcheck 는 REF_UNREACHABLE 포함)
	Other     int            // 다른 오류 (AUTH_FAIL, UNSUPPORTED, NO_HOSTS_ENTRY, HTTP_ERROR ...)
	Total     int            // 병합본의 호스트 수
	ByState   map[string]int // 병합본의 호스트 대표 상태별 수
	Files     []string       // 새 폴더에 쓴 병합 파일 이름
}

// tally 는 병합본 호스트 상태의 집계를 채우고, 재시도 대상 호스트의 입력 목록을 돌려줍니다.
// 이번에 재시도한 호스트는 retry.txt 와 같은 원문(inputOf), 이전 결과에만 있던 호스트는 호스트 열 값을 씁니다.
func (m *retryMerge) tally(states []hostState, inputOf map[string]string, isRetry func(string) bool) (retry []string) {
	m.Total = len(states)
	m.ByState = map[string]int{}
	for _, s := range states {
		m.ByState[s.State]++
		if isRetry(s.State) {
			in, ok := inputOf[s.Key]
			if !ok {
				in = s.Name
			}
			retry = append(retry, in)
		}
	}
	return retry
}

// infoText 는 merged_run_info.txt 본문입니다 (비밀번호·토큰·계정 없음).
func (m *retryMerge) infoText(cmd string, now time.Time, extra ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "도구: biostool %s (%s, 재시도 병합)\n", toolVersion, cmd)
	for _, l := range extra {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "시각: %s\n", now.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "이전 결과 폴더: %s (읽기만 함)\n", m.PrevDir)
	fmt.Fprintf(&b, "재시도한 호스트: %d\n", m.Retried)
	fmt.Fprintf(&b, "  복구(호스트 오류 없이 점검 끝남): %d\n", m.Recovered)
	fmt.Fprintf(&b, "  여전히 일시오류: %d (→ merged_retry.txt)\n", m.Still)
	fmt.Fprintf(&b, "  다른 오류: %d\n", m.Other)
	fmt.Fprintf(&b, "병합본 호스트: %d\n", m.Total)
	b.WriteString("병합본 호스트 기준 집계 (대표 상태):\n")
	for _, k := range sortedKeys(m.ByState) {
		fmt.Fprintf(&b, "  %s: %d\n", k, m.ByState[k])
	}
	return b.String()
}

// writeRetryMergeSection 은 터미널 리포트 끝의 [재시도 병합] 블록입니다.
func writeRetryMergeSection(w io.Writer, m *retryMerge, dir string, p painter) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s [재시도 병합] 이전 결과: %s\n", p.cyan("*"), filepath.ToSlash(m.PrevDir))
	fmt.Fprintf(w, "  재시도 %d대 → 복구 %d대 / 여전히 일시오류 %d대 (→ merged_retry.txt) / 다른 오류 %d대\n",
		m.Retried, m.Recovered, m.Still, m.Other)
	parts := make([]string, 0, len(m.ByState))
	for _, k := range sortedKeys(m.ByState) {
		parts = append(parts, fmt.Sprintf("%s %d", k, m.ByState[k]))
	}
	fmt.Fprintf(w, "  병합본 전체 %d대: %s\n", m.Total, strings.Join(parts, ", "))
	fmt.Fprintf(w, "  병합 파일: %s/ (%s)\n", filepath.ToSlash(dir), strings.Join(m.Files, " · "))
}

// writeMergedResults 는 check 의 병합 파일(merged_result.tsv, merged_ok.txt, merged_retry.txt, merged_run_info.txt)을
// 새 결과 폴더에 씁니다. cur 은 이번 실행의 result.tsv 행입니다.
func (r *checkRun) writeMergedResults(cur [][]string) error {
	m := &retryMerge{PrevDir: r.RetryFromDir, Retried: len(r.Hosts)}
	retried := map[string]bool{}
	inputOf := map[string]string{}
	for _, h := range r.Hosts {
		k := rowHostKey(tsvField(h.Target.Hostname))
		retried[k] = true
		if _, ok := inputOf[k]; !ok {
			inputOf[k] = h.Target.Input
		}
		switch {
		case h.Status == "":
			m.Recovered++
		case isRetryStatus(h.Status):
			m.Still++
		default:
			m.Other++
		}
	}
	merged := mergeRowsByHost(r.prevResult.Rows, cur, 0, retried)
	states := hostStates(merged, 0, len(resultHeader)-1, checkRowsState)
	retry := m.tally(states, inputOf, isRetryStatus)
	var ok []string
	for _, s := range states {
		if s.State == StatusOK || s.State == StatusPendingOK {
			ok = append(ok, s.Name)
		}
	}

	m.Files = []string{"merged_result.tsv", "merged_ok.txt", "merged_retry.txt", "merged_run_info.txt"}
	if err := writeTSV(filepath.Join(r.Dir, "merged_result.tsv"), resultHeader, merged); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(r.Dir, "merged_ok.txt"), ok); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(r.Dir, "merged_retry.txt"), retry); err != nil {
		return err
	}
	info := m.infoText("check", r.Now, "프로파일: "+r.Profile)
	if err := os.WriteFile(filepath.Join(r.Dir, "merged_run_info.txt"), []byte(info), 0o600); err != nil {
		return err
	}
	r.Retry = m
	return nil
}

// writeMergedResults 는 allcheck 의 병합 파일(merged_summary.tsv, merged_all_diff.tsv, merged_retry.txt,
// merged_run_info.txt)을 새 결과 폴더에 씁니다. 이번 실행의 행은 방금 쓴 summary.tsv·all_diff.tsv 에서 읽습니다.
func (r *allRun) writeMergedResults() error {
	curSum, err := readTSVTable(filepath.Join(r.Dir, "summary.tsv"), summaryHeader)
	if err != nil {
		return fmt.Errorf("이번 summary.tsv: %w", err)
	}
	curDiff, err := readTSVTable(filepath.Join(r.Dir, "all_diff.tsv"), allDiffHeader)
	if err != nil {
		return fmt.Errorf("이번 all_diff.tsv: %w", err)
	}
	m := &retryMerge{PrevDir: r.RetryFromDir, Retried: len(r.Hosts)}
	retried := map[string]bool{}
	inputOf := map[string]string{}
	for _, h := range r.Hosts {
		k := rowHostKey(tsvField(h.Target.Hostname))
		retried[k] = true
		if _, ok := inputOf[k]; !ok {
			inputOf[k] = h.Target.Input
		}
		switch {
		case isAllRetryStatus(h.Status):
			m.Still++
		case h.Status == StatusSame || h.Status == StatusDiff || h.Status == StatusIsReference || h.Status == StatusNoReference:
			m.Recovered++
		default:
			m.Other++
		}
	}
	sum := mergeRowsByHost(r.prevSummary.Rows, curSum.Rows, 0, retried)
	diff := mergeRowsByHost(r.prevAllDiff.Rows, curDiff.Rows, 2, retried)
	states := hostStates(sum, 0, len(summaryHeader)-2, firstResult) // status 는 끝에서 둘째 열 (notes 앞)
	retry := m.tally(states, inputOf, isAllRetryStatus)

	m.Files = []string{"merged_summary.tsv", "merged_all_diff.tsv", "merged_retry.txt", "merged_run_info.txt"}
	if err := writeTSV(filepath.Join(r.Dir, "merged_summary.tsv"), summaryHeader, sum); err != nil {
		return err
	}
	if err := writeTSV(filepath.Join(r.Dir, "merged_all_diff.tsv"), allDiffHeader, diff); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(r.Dir, "merged_retry.txt"), retry); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "merged_run_info.txt"), []byte(m.infoText("allcheck", r.Now)), 0o600); err != nil {
		return err
	}
	r.Retry = m
	return nil
}
