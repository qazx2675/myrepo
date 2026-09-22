package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
)

// ★ pdsh/clush 스타일의 "[콤마+범위 목록]" 패턴을 찾는 정규표현식(괄호 안 내용은 그대로 캡처해서
// expandHostLine에서 콤마로 나눠 각각 처리한다)
var hostRangeRegex = regexp.MustCompile(`(.*?)\[([^\]]+)\](.*)`)

// ★ /user/ 로 시작하는 경로(autofs 마운트 경로)가 명령어에 포함되어 있는지 확인
var autofsUserPathRegex = regexp.MustCompile(`(^|[\s"'])/user/`)

// ★ 가독성용 ANSI 색상. -script 모드에서는 다른 도구가 결과를 파싱/파이프하는 용도라
// 이스케이프 코드가 섞이면 안 되므로 colorEnabled를 false로 두고 그대로 원문을 출력한다.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyanB  = "\033[1;36m"
)

// ★ 원격 명령 출력에 섞여 있을 수 있는 ANSI/OSC 이스케이프 시퀀스를 제거한다. 원격 서버가
// 색상이 있는 MOTD/프롬프트/도구 출력(예: 컬러 ls, 커스텀 테마)을 그대로 돌려주면, 그 안에
// 이스케이프 코드가 완전히 닫히지 않았거나(팔레트 재정의 OSC 등) 우리 쪽 colorGreen/colorReset
// 감싸기로도 되돌릴 수 없는 상태를 만들어 터미널 전체 색이 바뀌어 버리는 문제가 있었다.
// 원격 출력은 항상 이 필터를 거친 뒤에만 우리 자체 색으로 감싼다.
var ansiEscRegex = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[a-zA-Z]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][A-Za-z0-9]|[^\[\]()])`)

func stripANSI(s string) string {
	return ansiEscRegex.ReplaceAllString(s, "")
}

// ★ 색상은 해당 출력이 터미널일 때만 쓴다. "> res", "2> err"처럼 파일로 저장하면 색상 코드가
// 들어가지 않는다(colorEnabled=표준에러용, colorOutEnabled=표준출력용).
var colorEnabled, colorOutEnabled bool

func colorize(color, s string) string {
	if !colorEnabled {
		return s
	}
	return color + s + colorReset
}

func colorizeOut(color, s string) string {
	if !colorOutEnabled {
		return s
	}
	return color + s + colorReset
}

// ★ 진행률 줄 고정 표시: 진행 중에는 맨 아래 줄에 진행률을 두고, 결과/메시지 줄이 찍힐 때마다
// 그 줄을 지우고 → 메시지 출력 → 진행률을 다시 그린다(줄바꿈 없이 제자리 갱신).
// 진행률은 stderr가 터미널일 때만 켜지므로 파일로 리다이렉션하면 저장되지 않는다.
var (
	outMu         sync.Mutex
	progressOn    bool
	progressText  string
	progressDrawn string // 마지막으로 실제 화면에 그린 진행률(같으면 다시 그리지 않음 — 깜빡임 방지)
	stdoutTTY     bool
	stderrTTY     bool

	// -m 행리스트 화면(대체 화면 버퍼)을 보고 있는 동안에는 터미널로 가는 결과 출력을 모아뒀다가
	// 원래 화면으로 돌아올 때 한꺼번에 찍는다(대체 화면에 찍으면 돌아올 때 사라지기 때문).
	monitorOn  bool
	listActive bool
	pendingOut []pendingWrite
)

type pendingWrite struct {
	w *os.File
	s string
}

func isTTYFile(w *os.File) bool {
	if w == os.Stdout {
		return stdoutTTY
	}
	if w == os.Stderr {
		return stderrTTY
	}
	return false
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// fitWidth는 한 줄이 터미널 폭을 넘어 다음 줄로 넘어가지 않게 자른다. 줄이 넘어가면 "\r"이
// 마지막 줄 맨 앞으로만 돌아가서 진행률 줄이 계속 새 줄로 쌓여 보이게 된다. 한글 등 전각 문자는
// 2칸으로 계산한다.
func fitWidth(s string, cols int) string {
	if cols <= 1 {
		return s
	}
	limit := cols - 1
	w := 0
	for i, r := range s {
		rw := 1
		if isWideRune(r) {
			rw = 2
		}
		if w+rw > limit {
			return s[:i]
		}
		w += rw
	}
	return s
}

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// padRight는 화면 폭(한글 2칸) 기준으로 오른쪽을 공백으로 채운다.
func padRight(s string, width int) string {
	if d := width - displayWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func isWideRune(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) || (r >= 0x2E80 && r <= 0xA4CF) ||
		(r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE30 && r <= 0xFE4F) || (r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6)
}

func progressLineLocked() string {
	cols, _ := terminalSize(int(os.Stderr.Fd()))
	return fitWidth(progressText, cols)
}

// drawProgressLocked는 진행률 줄을 제자리에서 덮어쓴다. 예전처럼 줄을 먼저 지우고(\033[K) 다시
// 쓰면 지워진 빈 줄이 순간적으로 보여서 깜빡이므로, 글자를 먼저 덮어쓰고 남는 오른쪽만 지운다.
// 내용이 바뀌지 않았으면 아예 다시 그리지 않는다.
func drawProgressLocked(force bool) {
	line := progressLineLocked()
	if !force && line == progressDrawn {
		return
	}
	fmt.Fprint(os.Stderr, "\r"+line+"\033[K")
	progressDrawn = line
}

// emit은 s(끝에 개행 포함)를 w에 출력하되 진행률 줄과 섞이지 않게 한다.
func emit(w *os.File, s string) {
	outMu.Lock()
	defer outMu.Unlock()
	emitLocked(w, s)
}

func emitLocked(w *os.File, s string) {
	if listActive && isTTYFile(w) {
		pendingOut = append(pendingOut, pendingWrite{w, s})
		return
	}
	if progressOn && !listActive && isTTYFile(w) {
		// 진행률 줄 위치에 결과 줄을 덮어쓰고(남는 부분만 지움) 다음 줄에 진행률을 다시 그리는 것을
		// 한 번의 write로 처리한다 — 지웠다가 다시 쓰는 깜빡임이 없다.
		body := strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\033[K\n")
		line := progressLineLocked()
		fmt.Fprint(w, "\r"+body+"\033[K\n"+line+"\033[K")
		progressDrawn = line
		return
	}
	// 파일/파이프로 리다이렉션된 출력은 화면(진행률 줄)과 무관하므로 그대로 쓴다.
	fmt.Fprint(w, s)
}

// ★ -m 행리스트: 실행 시작(동시 실행 슬롯을 잡은 시점)과 종료 시각을 호스트별로 기록한다.
// 끝난 호스트는 종료 시각에서 멈춘 시간을 보여준다.
const hangThreshold = 60 * time.Second

type hostTiming struct {
	start, end time.Time
}

var (
	timingMu  sync.Mutex
	hostTimes = map[string]*hostTiming{}
)

type hangRow struct {
	host    string
	elapsed time.Duration
	done    bool
}

func collectHangRows() (rows []hangRow, running int) {
	now := time.Now()
	timingMu.Lock()
	for h, t := range hostTimes {
		if t.end.IsZero() {
			if d := now.Sub(t.start); d >= hangThreshold {
				rows = append(rows, hangRow{h, d, false})
				running++
			}
		} else if d := t.end.Sub(t.start); d >= hangThreshold {
			rows = append(rows, hangRow{h, d, true})
		}
	}
	timingMu.Unlock()
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].done != rows[j].done {
			return !rows[i].done // 실행 중인 호스트를 위에
		}
		if rows[i].elapsed != rows[j].elapsed {
			return rows[i].elapsed > rows[j].elapsed
		}
		return rows[i].host < rows[j].host
	})
	return rows, running
}

func countHangRunning() int {
	now := time.Now()
	n := 0
	timingMu.Lock()
	for _, t := range hostTimes {
		if t.end.IsZero() && now.Sub(t.start) >= hangThreshold {
			n++
		}
	}
	timingMu.Unlock()
	return n
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	return fmt.Sprintf("%02d:%02d:%02d", h, m, d/time.Second)
}

var monitorStart time.Time

// renderHangListLocked는 대체 화면 버퍼에 행리스트를 그린다. 화면 전체를 지우지 않고 맨 위로
// 이동해서 줄마다 덮어쓰고(남는 부분만 지움) 마지막에 아래쪽 나머지를 지워서 깜빡이지 않는다.
func renderHangListLocked() {
	cols, lines := terminalSize(int(os.Stderr.Fd()))
	if lines <= 0 {
		lines = 24
	}
	rows, running := collectHangRows()

	hostW := displayWidth("호스트명")
	for _, r := range rows {
		if w := displayWidth(r.host); w > hostW {
			hostW = w
		}
	}
	if hostW > 40 {
		hostW = 40
	}

	var b strings.Builder
	b.WriteString("\033[H")
	put := func(s string) { b.WriteString(fitWidth(s, cols) + "\033[K\n") }
	put(fmt.Sprintf("=== 행리스트: %d초 이상 실행 중이거나 실행했던 호스트 ===  경과 %s", int(hangThreshold.Seconds()), fmtDuration(time.Since(monitorStart))))
	put(fmt.Sprintf("%s   실행 중 %d대 / 끝남 %d대", progressText, running, len(rows)-running))
	put("")
	if len(rows) == 0 {
		put(fmt.Sprintf("  (%d초 이상 걸린 호스트가 아직 없습니다)", int(hangThreshold.Seconds())))
	} else {
		put("  " + padRight("호스트명", hostW) + "  " + padRight("상태", 6) + "  진행시간")
		// 제목 3줄 + 헤더 1줄 + 안내 1줄 + "외 N대" 1줄을 빼고 남는 만큼만 보여준다.
		maxRows := lines - 6
		if maxRows < 1 {
			maxRows = 1
		}
		shown := rows
		if len(shown) > maxRows {
			shown = shown[:maxRows]
		}
		for _, r := range shown {
			state := "실행중"
			if r.done {
				state = "끝남"
			}
			put("  " + padRight(r.host, hostW) + "  " + padRight(state, 6) + "  " + fmtDuration(r.elapsed))
		}
		if len(rows) > len(shown) {
			put(fmt.Sprintf("  ... 외 %d대 (화면 높이만큼만 표시)", len(rows)-len(shown)))
		}
	}
	b.WriteString(fitWidth("(Enter: 원래 화면으로 돌아가기)", cols) + "\033[K\033[J")
	fmt.Fprint(os.Stderr, b.String())
}

func enterListLocked() {
	listActive = true
	fmt.Fprint(os.Stderr, "\033[?1049h") // 대체 화면 버퍼 — 원래 화면/스크롤백을 건드리지 않는다
	renderHangListLocked()
}

func leaveListLocked() {
	if !listActive {
		return
	}
	fmt.Fprint(os.Stderr, "\033[?1049l") // 원래 화면으로 복귀(커서 위치도 복원됨)
	listActive = false
	queued := pendingOut
	pendingOut = nil
	for _, p := range queued {
		emitLocked(p.w, p.s)
	}
	if progressOn {
		drawProgressLocked(true)
	}
}

func leaveListIfActive() {
	outMu.Lock()
	leaveListLocked()
	outMu.Unlock()
}

// startMonitorKeys는 Enter 키로 원래 화면 <-> 행리스트 화면을 오간다.
func startMonitorKeys() {
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			if buf[0] != '\n' && buf[0] != '\r' {
				continue
			}
			outMu.Lock()
			if !monitorOn {
				outMu.Unlock()
				return
			}
			if listActive {
				leaveListLocked()
			} else {
				enterListLocked()
			}
			outMu.Unlock()
		}
	}()
}

var (
	cbreakRestore func()
	cursorHidden  bool
)

// restoreTerminal은 행리스트 화면/커서 숨김/입력 모드를 원래대로 돌린다. 정상 종료뿐 아니라
// Ctrl+C 4회 강제 종료, SIGTERM/SIGHUP 종료 때도 호출해서 터미널이 이상한 상태로 남지 않게 한다.
func restoreTerminal() {
	outMu.Lock()
	defer outMu.Unlock()
	monitorOn = false
	if listActive {
		fmt.Fprint(os.Stderr, "\033[?1049l")
		listActive = false
	}
	if cursorHidden {
		fmt.Fprint(os.Stderr, "\033[?25h")
		cursorHidden = false
	}
	if cbreakRestore != nil {
		cbreakRestore()
		cbreakRestore = nil
	}
}

// ★ pdsh 스타일 "플래그+값 붙여쓰기"를 -w에 한해 지원한다.
// "-w^file", "-wfile" 처럼 공백/등호 없이 붙어 있는 경우를 Go flag 패키지가 이해하는
// "-w" "값" 두 토큰으로 분리한다. "-w=value"(Go 관용 표기)와 "-w" 단독, "-w ^file"
// (이미 공백으로 분리되어 있는 경우)은 그대로 둔다.
func preprocessArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-w") && len(a) > 2 && a[2] != '=' {
			out = append(out, "-w", a[2:])
			continue
		}
		out = append(out, a)
	}
	return out
}

// ★ esxi[0001-0020] 및 clush(NodeSet) 스타일 콤마+범위 혼합 표기(예: qwer[2660,2671,2676,2826-2829])를
// 자동으로 확장해 주는 함수. 괄호가 여러 번 나오면(예: hostname[0001-0002]ev[01-03]) 재귀적으로
// 전부 풀어준다.
func expandHostLine(line string) []string {
	matches := hostRangeRegex.FindStringSubmatch(line)
	if matches == nil {
		return []string{line}
	}

	prefix := matches[1]
	content := matches[2]
	suffix := matches[3]

	var results []string
	for _, item := range strings.Split(content, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if idx := strings.Index(item, "-"); idx > 0 {
			startStr := item[:idx]
			endStr := item[idx+1:]
			start, err1 := strconv.Atoi(startStr)
			end, err2 := strconv.Atoi(endStr)
			if err1 != nil || err2 != nil {
				results = append(results, expandHostLine(prefix+item+suffix)...)
				continue
			}
			if start > end {
				start, end = end, start
			}
			padLen := 0
			if len(startStr) > 1 && startStr[0] == '0' {
				padLen = len(startStr)
			}
			for i := start; i <= end; i++ {
				results = append(results, expandHostLine(prefix+formatNum(i, padLen)+suffix)...)
			}
			continue
		}
		// 콤마로 구분된 단일 값(범위가 아님)은 원래 자릿수 그대로 사용
		results = append(results, expandHostLine(prefix+item+suffix)...)
	}
	return results
}

// formatNum은 width>0이면 0-padding을 적용해 숫자를 문자열로 만든다.
func formatNum(n, width int) string {
	if width > 0 {
		return fmt.Sprintf("%0*d", width, n)
	}
	return strconv.Itoa(n)
}

// splitTrailingDigits는 호스트명 끝의 연속된 숫자(있다면)와 그 앞부분을 나눈다.
// 0-padding(예: "0001")을 그대로 보존하기 위해 문자열 그대로 반환한다.
func splitTrailingDigits(host string) (prefix string, numStr string, ok bool) {
	i := len(host)
	for i > 0 && host[i-1] >= '0' && host[i-1] <= '9' {
		i--
	}
	if i == len(host) {
		return "", "", false
	}
	return host[:i], host[i:], true
}

// dedupeSortedInts는 정렬 후 중복을 제거한다.
func dedupeSortedInts(nums []int) []int {
	sort.Ints(nums)
	out := nums[:0]
	for i, n := range nums {
		if i == 0 || n != out[len(out)-1] {
			out = append(out, n)
		}
	}
	return out
}

// foldRangeSet은 정렬/중복제거된 정수 목록을 clush(ClusterShell NodeSet)와 동일한 콤마+범위
// 표기로 접는다: 연속 3개 이상이면 "lo-hi", 아니면 낱개 숫자, 그 외엔 콤마로 나열.
// 예) [2660,2671,2676,2826,2827,2828,2829] -> "2660,2671,2676,2826-2829"
func foldRangeSet(nums []int, width int) string {
	var parts []string
	i := 0
	for i < len(nums) {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, formatNum(nums[i], width)+"-"+formatNum(nums[j], width))
		} else {
			parts = append(parts, formatNum(nums[i], width))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

// foldRangeSetBracketed는 clush와 동일하게, 값이 하나뿐이면 괄호 없이 그대로, 여러 개면
// "[콤마+범위]"로 감싼다.
func foldRangeSetBracketed(nums []int, width int) string {
	nums = dedupeSortedInts(nums)
	if len(nums) == 1 {
		return formatNum(nums[0], width)
	}
	return "[" + foldRangeSet(nums, width) + "]"
}

// compressHosts는 expandHostLine의 역방향이다: 접두어가 같은 호스트들의 끝자리 숫자를
// clush(ClusterShell NodeSet)와 동일한 표기로 압축한다(예: "qwer[2660,2671,2676,2826-2829]").
// 결과 토큰은 expandHostLine이 그대로 다시 풀 수 있는 형태라, 이 함수의 출력을 그대로
// 파일에 저장해서 -w로 다시 넣어도 동작한다.
func compressHosts(hosts []string) []string {
	type group struct {
		nums  []int
		width int // 0-padding이 필요한 멤버가 있으면 그 폭(가장 넓은 것), 없으면 0
	}

	groups := map[string]*group{}
	var order []string
	var singles []string

	for _, h := range hosts {
		prefix, numStr, ok := splitTrailingDigits(h)
		if !ok {
			singles = append(singles, h)
			continue
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			singles = append(singles, h)
			continue
		}
		g, exists := groups[prefix]
		if !exists {
			g = &group{}
			groups[prefix] = g
			order = append(order, prefix)
		}
		g.nums = append(g.nums, n)
		// ★ clush와 동일하게: 0-padding이 필요한 멤버(선행 0)가 하나라도 있으면 그 폭으로
		// 통일해서 패딩하고, 전부 패딩이 없으면(자연수 그대로) 자릿수가 달라도 그냥 합친다
		// (예: a9,a10,a11 -> a[9-11]).
		if len(numStr) > 1 && numStr[0] == '0' && len(numStr) > g.width {
			g.width = len(numStr)
		}
	}

	var out []string
	for _, prefix := range order {
		g := groups[prefix]
		out = append(out, prefix+foldRangeSetBracketed(g.nums, g.width))
	}
	out = mergeRangeTokens(out)
	out = append(out, singles...)
	return out
}

// 접힌 토큰 "A<숫자>B[콤마+범위]"에서 A/숫자/B/괄호 안 내용을 분리하기 위한 패턴.
// 예: "hostname0001ev[01-03]" -> A="hostname", N="0001", B="ev", bracket="01-03"
var foldedTokenRegex = regexp.MustCompile(`^(.*?)(\d+)(\D*)\[([^\]]+)\]$`)

// mergeRangeTokens는 괄호 앞의 접두어 안에 있는 숫자 덩어리(첫 번째 축)가 다른 토큰들을,
// 괄호 안 내용(두 번째 축)이 완전히 같을 때만 다차원으로 합친다.
// "hostname0001ev[01-03]", "hostname0002ev[01-03]" -> "hostname[0001-0002]ev[01-03]"
func mergeRangeTokens(tokens []string) []string {
	type key struct {
		a, b, bracket string
	}
	type member struct {
		n      int
		numStr string
		token  string
	}
	groups := map[key][]member{}
	var order []key
	for _, t := range tokens {
		m := foldedTokenRegex.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		// 숫자 바로 뒤에 범위가 붙거나(node1[01-03]) 구분 기호만 있는 경우(node1-[01-03])는
		// 합치면 헷갈리므로 합치지 않는다. 숫자 사이에 글자가 끼어 있을 때만(ev 등) 다차원으로 접는다.
		if !hasLetter(m[3]) {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		k := key{a: m[1], b: m[3], bracket: m[4]}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], member{n: n, numStr: m[2], token: t})
	}

	replace := map[string]string{} // 원본 토큰 -> 합친 토큰(첫 멤버) 또는 "" (삭제)
	for _, k := range order {
		ms := groups[k]
		if len(ms) < 2 {
			continue // 합칠 상대가 없으면 그대로 둔다
		}
		width := 0
		nums := make([]int, len(ms))
		for i, m := range ms {
			nums[i] = m.n
			if len(m.numStr) > 1 && m.numStr[0] == '0' && len(m.numStr) > width {
				width = len(m.numStr)
			}
		}
		merged := k.a + foldRangeSetBracketed(nums, width) + k.b + "[" + k.bracket + "]"
		for _, m := range ms {
			replace[m.token] = ""
		}
		replace[ms[0].token] = merged
	}

	var out []string
	for _, t := range tokens {
		if r, ok := replace[t]; ok {
			if r != "" {
				out = append(out, r)
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

func writeHostsToFile(filename string, hosts []string) {
	if len(hosts) == 0 {
		return
	}
	file, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "결과 파일 생성 실패 (%s): %v\n", filename, err)
		return
	}
	defer file.Close()
	// ★ 결과 파일은 요약(압축) 표기가 아니라 호스트 이름을 줄바꿈으로 하나씩 그대로 기록한다.
	// -w로 그대로 다시 읽어도 동작한다(압축 표기는 화면 요약/-b 출력에서만 사용).
	for _, h := range hosts {
		file.WriteString(h + "\n")
	}
}

// ★ [변경] 키(패스워드 없는) 인증을 항상 먼저 시도하고, -p로 비밀번호를 지정했어도
// 그건 폴백으로만 쓴다. ssh.ClientConfig.Auth는 나열된 순서대로 시도하다가 먼저 성공하는
// 것에서 멈추므로, 키만으로 접속되는 호스트는 -p를 줬어도 비밀번호가 실제로 쓰이지 않는다.
func getAuthMethods(keyPath string, password string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	var signers []ssh.Signer
	var paths []string

	home, _ := os.UserHomeDir()

	if keyPath != "" {
		if strings.HasPrefix(keyPath, "~/") {
			paths = []string{filepath.Join(home, keyPath[2:])}
		} else {
			paths = []string{keyPath}
		}
	} else {
		paths = []string{
			filepath.Join(home, ".ssh", "id_ed25519"),
			filepath.Join(home, ".ssh", "id_ecdsa"),
			filepath.Join(home, ".ssh", "id_rsa"),
		}
	}

	for _, p := range paths {
		key, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}

	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	if password != "" {
		methods = append(methods, ssh.Password(password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("사용 가능한 SSH 키를 찾을 수 없습니다 (~/.ssh/ 하위 확인)")
	}

	return methods, nil
}

// renderResultLines는 printPdshStyle이 host 접두어 없이 찍을 본문 줄들을 그대로 만들어준다.
// -b(묶어 출력) 모드에서 호스트별 결과를 비교하기 위해 printPdshStyle과 별도로 필요하다.
func renderResultLines(output string, err error) string {
	output = stripANSI(strings.TrimSpace(output))

	if err != nil {
		if output != "" {
			var lines []string
			for _, line := range strings.Split(output, "\n") {
				lines = append(lines, "ERROR: "+strings.TrimSpace(line))
			}
			return strings.Join(lines, "\n")
		}
		return fmt.Sprintf("ERROR: %v", err)
	}

	if output == "" {
		return ""
	}

	var lines []string
	for _, line := range strings.Split(output, "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}
	return strings.Join(lines, "\n")
}

// ★ [변경] 접속 실패/명령어 오류(err != nil)는 표준출력이 아니라 표준에러로 보낸다.
// "gossh -w a cmd > res"처럼 단순 리다이렉션했을 때, 접속불가/명령어 오타 메시지 같은
// 에러성 메시지는 res에 안 남고 실제 명령 실행 결과만 남아야 하기 때문(일반적인 CLI 도구의
// stdout/stderr 관례와 동일하게 맞춘 것).
func printPdshStyle(host string, output string, err error) {
	text := renderResultLines(output, err)
	if text == "" {
		return
	}
	if err != nil {
		for _, line := range strings.Split(text, "\n") {
			emit(os.Stderr, colorize(colorRed, fmt.Sprintf("%s: %s", host, line))+"\n")
		}
		return
	}
	for _, line := range strings.Split(text, "\n") {
		emit(os.Stdout, colorizeOut(colorGreen, fmt.Sprintf("%s: %s", host, line))+"\n")
	}
}

// printBunched는 clush -b와 동일하게, 결과 본문이 완전히 같은 호스트끼리 묶어서
// 한 번만 출력한다. hosts 순서대로 훑으면서 처음 보는 결과 내용마다 그룹을 만든다.
func printBunched(hosts []string, outputs map[string]string) {
	type group struct {
		text  string
		hosts []string
	}
	var groups []*group
	seen := map[string]*group{}

	for _, h := range hosts {
		text, ok := outputs[h]
		if !ok || text == "" {
			continue
		}
		g, exists := seen[text]
		if !exists {
			g = &group{text: text}
			seen[text] = g
			groups = append(groups, g)
		}
		g.hosts = append(g.hosts, h)
	}

	divider := strings.Repeat("-", 20)
	for _, g := range groups {
		fmt.Println(colorizeOut(colorCyanB, divider))
		fmt.Println(colorizeOut(colorCyanB, strings.Join(compressHosts(g.hosts), ",")))
		fmt.Println(colorizeOut(colorCyanB, divider))
		fmt.Println(g.text)
	}
}

// printUnreachableGroup은 -b 모드에서 접속 자체가 안 된 호스트(타임아웃/Refused)를
// 별도 그룹으로 묶어서 보여준다. 이 호스트들은 세션이 아예 생성되지 않아 결과 본문이
// 없으므로(printBunched의 내용 비교 대상이 아님) 접속불가라는 이유 하나로만 묶는다.
// ★ 접속불가 그룹은 에러성 정보라 표준에러로 보낸다(printPdshStyle과 동일한 이유).
func printUnreachableGroup(failedHosts, refusedHosts []string) {
	unreachable := append(append([]string{}, failedHosts...), refusedHosts...)
	if len(unreachable) == 0 {
		return
	}
	divider := strings.Repeat("-", 20)
	fmt.Fprintln(os.Stderr, colorize(colorRed, divider))
	fmt.Fprintln(os.Stderr, colorize(colorRed, strings.Join(compressHosts(unreachable), ",")))
	fmt.Fprintln(os.Stderr, colorize(colorRed, divider))
	fmt.Fprintln(os.Stderr, colorize(colorRed, "접속불가 (Timeout/Refused)"))
}

// printCommandResult는 pdsh와 같은 방식으로 명령 결과를 나눠서 출력한다.
//   - 원격 표준출력: 종료코드와 상관없이 항상 표준출력(> res 로 저장됨). -b면 그룹 묶음 대상.
//   - 원격 표준에러: 표준에러(화면에만, 저장 안 됨). "No such file or directory",
//     "command not found" 같은 메시지가 여기에 해당한다.
//   - 종료코드가 0이 아니면 표준에러에 "ERROR: 종료코드 N" 한 줄을 추가로 알린다.
func printCommandResult(host, stdout, stderr string, runErr error, bunchMode bool, bunchOutputs map[string]string, mu *sync.Mutex) {
	if outText := renderResultLines(stdout, nil); outText != "" {
		if bunchMode {
			mu.Lock()
			bunchOutputs[host] = outText
			mu.Unlock()
		} else {
			for _, line := range strings.Split(outText, "\n") {
				emit(os.Stdout, colorizeOut(colorGreen, fmt.Sprintf("%s: %s", host, line))+"\n")
			}
		}
	}
	if errText := renderResultLines(stderr, nil); errText != "" {
		for _, line := range strings.Split(errText, "\n") {
			emit(os.Stderr, colorize(colorRed, fmt.Sprintf("%s: %s", host, line))+"\n")
		}
	}
	if runErr != nil {
		msg := runErr.Error()
		var exitErr *ssh.ExitError
		if errors.As(runErr, &exitErr) {
			msg = fmt.Sprintf("종료코드 %d", exitErr.ExitStatus())
		}
		emit(os.Stderr, colorize(colorRed, fmt.Sprintf("%s: ERROR: %s", host, msg))+"\n")
	}
}

// ★ -pm 점검(anaconda 확인 + 특정 autofs 계정 경로 확인)은 세션 하나로 합쳐서 본 명령 전에 한 번만
// 실행한다(예전엔 세션을 2개 더 열어서 호스트당 약 +48% 느렸음). 이 점검은 autofs가 멈추면 끝나지
// 않을 수 있어서 점검에만 제한시간을 둔다 — 본 명령에는 제한시간이 없다(별도 세션).
const pmCheckTimeout = 10 * time.Second

// runPmCheck는 점검 결과를 돌려준다. 제한시간 안에 받은 줄까지만 반영하고, 못 받은 항목은
// "해당 없음/미접근"으로 본다(예: autofs 경로 확인이 멈추면 svrautoOK=false).
func runPmCheck(ctx context.Context, client *ssh.Client) (osInstall, svrautoOK bool) {
	sess, err := client.NewSession()
	if err != nil {
		return false, false
	}
	defer sess.Close()
	out, err := sess.StdoutPipe()
	if err != nil {
		return false, false
	}
	// 줄마다 고유한 표시를 붙여서 MOTD 등 다른 출력과 섞여도 정확히 구분한다.
	checkCmd := `sh -c 'if grep -q anaconda ~/.profile 2>/dev/null; then echo GOSSH_PM_OS=1; else echo GOSSH_PM_OS=0; fi; if [ -d /user/svrauto ]; then echo GOSSH_PM_SVR=1; else echo GOSSH_PM_SVR=0; fi'`
	if err := sess.Start(checkCmd); err != nil {
		return false, false
	}

	lines := make(chan string)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			select {
			case lines <- strings.TrimSpace(sc.Text()):
			case <-done:
				return
			}
		}
	}()

	timer := time.NewTimer(pmCheckTimeout)
	defer timer.Stop()
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return
			}
			switch l {
			case "GOSSH_PM_OS=1":
				osInstall = true
			case "GOSSH_PM_SVR=1":
				svrautoOK = true
			}
		case <-timer.C:
			return // autofs 멈춤으로 판단 — 세션은 defer Close로 끊고 받은 결과까지만 사용
		case <-ctx.Done():
			return
		}
	}
}

func runSSHCommand(host string, command string, user string, authMethods []ssh.AuthMethod, port string, timeout time.Duration, pmMode bool, bunchMode bool, bunchOutputs map[string]string, wg *sync.WaitGroup, sem chan struct{}, dnsSem chan struct{}, successCount *int, failedHosts *[]string, refusedHosts *[]string, osInstallHosts *[]string, noSvrAutoHosts *[]string, mu *sync.Mutex, completed *int64) {
	defer wg.Done()
	defer atomic.AddInt64(completed, 1) // ★ 진행률 카운트 — 성공/실패 어떤 경로로 끝나든 항상 1 증가
	select {
	case sem <- struct{}{}:
	case <-abortCh:
		atomic.AddInt64(&notRunCount, 1)
		return
	}
	defer func() { <-sem }()
	if atomic.LoadInt32(&aborted) == 1 {
		atomic.AddInt64(&notRunCount, 1)
		return
	}
	ctl := registerHost(host)
	defer unregisterHost(host, ctl)

	// ★ "host:port" 표기는 그대로 쓰고, 그 외(일반 호스트명/IPv4/IPv6 주소)는 -P 포트를 붙인다.
	// IPv6 주소(예: fe80::1)는 콜론이 들어있어도 포트 표기가 아니므로 [주소]:포트로 만든다.
	target := host
	if net.ParseIP(host) != nil || !strings.Contains(host, ":") {
		target = net.JoinHostPort(host, port)
	}

	// 0. ★ DNS 조회 선행 — 대량 동시 실행 시 리졸버 혼잡으로 간헐적 실패가 나는 걸 짧은 재시도로
	// 흡수한다. 여기서 최종 실패하면 ssh.Dial까지 갈 필요 없이 바로 접속불가로 분류한다.
	if err := lookupHostWithRetry(ctl.ctx, dnsSem, host); err != nil {
		if ctl.wasCanceled() {
			handleCanceled(host)
			return
		}
		printPdshStyle(host, "", fmt.Errorf("DNS 조회 실패: %v", err))
		mu.Lock()
		*failedHosts = append(*failedHosts, host)
		mu.Unlock()
		return
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	// 1. SSH 접속 시도
	// ssh.Dial과 동일한 동작(TCP 접속 후 SSH 핸드셰이크)이지만, Ctrl+C로 걸린 접속을 끊을 수
	// 있도록 컨텍스트/연결을 직접 다룬다.
	var client *ssh.Client
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctl.ctx, "tcp", target)
	if err == nil {
		ctl.setConn(conn)
		// ★ -t는 로그인(핸드셰이크+인증) 완료까지만 적용한다. TCP만 받고 SSH 응답이 없는 서버(멈춘
		// sshd, 인증 지연 등)에서 무한 대기하던 문제 수정. 로그인에 성공하면 제한을 바로 해제하므로
		// 그 뒤 실행하는 명령(드라이버 설치처럼 10분 이상 걸리는 작업 포함)에는 영향이 없다.
		conn.SetDeadline(time.Now().Add(timeout))
		c, chans, reqs, herr := ssh.NewClientConn(conn, target, config)
		if herr != nil {
			conn.Close()
			err = herr
		} else {
			conn.SetDeadline(time.Time{})
			client = ssh.NewClient(c, chans, reqs)
		}
	}
	if err != nil {
		if ctl.wasCanceled() {
			handleCanceled(host)
			return
		}
		printPdshStyle(host, "", fmt.Errorf("SSH 접속 실패: %v", err))
		mu.Lock()
		if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
			*refusedHosts = append(*refusedHosts, host)
		} else {
			*failedHosts = append(*failedHosts, host)
		}
		mu.Unlock()
		return
	}
	defer client.Close()

	// 2. ★ -pm: OS 설치 중 여부 + 특정 autofs 계정 경로 접근 여부를 세션 하나로 한 번에 점검
	pmNoSvrAuto := false
	if pmMode {
		osInstall, svrautoOK := runPmCheck(ctl.ctx, client)
		if ctl.wasCanceled() {
			handleCanceled(host)
			return
		}
		if osInstall {
			emit(os.Stderr, colorize(colorYellow, fmt.Sprintf("%s: OS 설치중 (~/.profile anaconda 감지)", host))+"\n")
			mu.Lock()
			*osInstallHosts = append(*osInstallHosts, host)
			mu.Unlock()
			return // 설치 중이면 명령 실행하지 않고 종료
		}
		pmNoSvrAuto = !svrautoOK
	}

	// 3. 메인 명령어 실행용 세션 생성
	session, err := client.NewSession()
	if err != nil {
		if ctl.wasCanceled() {
			handleCanceled(host)
			return
		}
		printPdshStyle(host, "", fmt.Errorf("세션 생성 실패: %v", err))
		mu.Lock()
		*failedHosts = append(*failedHosts, host)
		mu.Unlock()
		return
	}
	defer session.Close()

	// ★ 원격 표준출력/표준에러를 따로 받는다(pdsh 방식). 종료코드가 0이 아니어도 표준출력은
	// 결과로 저장되고, 표준에러(No such file or directory, command not found 등)는 화면에만 나간다.
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf
	err = session.Run(command)
	if err != nil && ctl.wasCanceled() {
		handleCanceled(host)
		return
	}
	printCommandResult(host, stdoutBuf.String(), stderrBuf.String(), err, bunchMode, bunchOutputs, mu)

	mu.Lock()
	*successCount++
	if pmNoSvrAuto {
		*noSvrAutoHosts = append(*noSvrAutoHosts, host)
	}
	mu.Unlock()
}

// ★ autofs 안전장치: 명령어에 "/user/..." 로 시작하는 경로가 포함되어 있으면 true.
// "echo 'user'" 처럼 단어 중간에 user가 들어간 경우는 대상이 아니고,
// 반드시 "/user/" 형태의 경로여야만 감지된다.
const autofsSafeConcurrency = 450

// ★ DNS 조회 전용 동시성 제한. SSH 접속 동시성(-c, 최대 450)과 별개로 훨씬 낮게 잡아서,
// 대량 호스트를 한꺼번에 처리할 때 리졸버/DNS 서버에 걸리는 순간 부하를 줄인다.
const dnsLookupConcurrency = 50

// 재검증 단계의 접속(로그인) 제한시간 상한.
const retryConnectTimeout = 8 * time.Second

// lookupHostWithRetry는 SSH 접속(ssh.Dial) 전에 이름 해석만 먼저 수행한다. 대량 동시 실행 시
// 간헐적으로 DNS 조회가 실패/타임아웃되는 경우가 있어(리졸버 혼잡), 실제 접속 타임아웃(-t, 기본
// 15초)만큼 기다리는 대신 250ms 간격으로 최대 2회만 짧게 재시도한다 — 진짜 접속불가 호스트에는
// 영향이 없고(어차피 이후 ssh.Dial에서 -t 타임아웃으로 판정), DNS 혼잡으로 인한 오분류만 줄인다.
func lookupHostWithRetry(ctx context.Context, dnsSem chan struct{}, host string) error {
	if net.ParseIP(host) != nil {
		return nil // IPv4/IPv6 주소는 이름 해석이 필요 없음
	}
	hostOnly := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	}
	if net.ParseIP(hostOnly) != nil {
		return nil // IP는 이름 해석이 필요 없음
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case dnsSem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		lctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, lastErr = net.DefaultResolver.LookupHost(lctx, hostOnly)
		cancel()
		<-dnsSem
		if lastErr == nil {
			return nil
		}
		if attempt < 2 {
			select {
			case <-time.After(250 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

// ★ Ctrl+C 처리(pdsh 방식): 1회=진행 중 호스트 목록 표시, 1초 내 2회=현재 걸린 호스트 취소 후
// 나머지 계속, 1초 내 3회=전체 중단(지금까지 결과로 요약/결과파일 저장). 호스트별로 취소 가능한
// 컨텍스트와 연결(conn)을 들고 있어야 걸려있는 접속/명령 실행을 끊을 수 있다.
type hostCtl struct {
	ctx      context.Context
	cancelFn context.CancelFunc
	mu       sync.Mutex
	conn     net.Conn
	canceled bool
}

func (c *hostCtl) cancel() {
	c.mu.Lock()
	c.canceled = true
	conn := c.conn
	c.mu.Unlock()
	c.cancelFn()
	if conn != nil {
		conn.Close()
	}
}

func (c *hostCtl) wasCanceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canceled
}

func (c *hostCtl) setConn(conn net.Conn) {
	c.mu.Lock()
	c.conn = conn
	canceled := c.canceled
	c.mu.Unlock()
	if canceled {
		conn.Close()
	}
}

var (
	activeMu      sync.Mutex
	activeHosts   = map[string]*hostCtl{}
	abortCh       = make(chan struct{})
	abortOnce     sync.Once
	aborted       int32
	notRunCount   int64
	canceledMu    sync.Mutex
	canceledHosts []string
)

func registerHost(host string) *hostCtl {
	ctx, cancel := context.WithCancel(context.Background())
	c := &hostCtl{ctx: ctx, cancelFn: cancel}
	activeMu.Lock()
	activeHosts[host] = c
	activeMu.Unlock()
	timingMu.Lock()
	hostTimes[host] = &hostTiming{start: time.Now()}
	timingMu.Unlock()
	return c
}

func unregisterHost(host string, c *hostCtl) {
	timingMu.Lock()
	if t := hostTimes[host]; t != nil {
		t.end = time.Now()
	}
	timingMu.Unlock()
	activeMu.Lock()
	delete(activeHosts, host)
	activeMu.Unlock()
	c.cancelFn()
}

func cancelActiveHosts() int {
	activeMu.Lock()
	defer activeMu.Unlock()
	for _, c := range activeHosts {
		c.cancel()
	}
	return len(activeHosts)
}

func abortAll() {
	abortOnce.Do(func() {
		atomic.StoreInt32(&aborted, 1)
		close(abortCh)
	})
	cancelActiveHosts()
}

func handleCanceled(host string) {
	emit(os.Stderr, colorize(colorYellow, fmt.Sprintf("%s: 사용자 취소", host))+"\n")
	canceledMu.Lock()
	canceledHosts = append(canceledHosts, host)
	canceledMu.Unlock()
}

func startInterruptHandler() {
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		count := 0
		var last time.Time
		for range sigCh {
			now := time.Now()
			if count > 0 && now.Sub(last) > time.Second {
				count = 0
			}
			count++
			last = now
			// 행리스트 화면을 보고 있었다면 원래 화면으로 돌아온 뒤 안내를 찍는다(안 그러면 안내가 안 보임).
			leaveListIfActive()
			switch {
			case count == 1:
				activeMu.Lock()
				var names []string
				for h := range activeHosts {
					names = append(names, h)
				}
				activeMu.Unlock()
				sort.Strings(names)
				emit(os.Stderr, fmt.Sprintf("\n[Ctrl+C] 진행 중 %d대: %s\n         1초 내 한 번 더: 현재 걸린 호스트 취소 후 계속 / 그 뒤 1초 내 한 번 더: 전체 중단\n", len(names), strings.Join(compressHosts(names), ",")))
			case count == 2:
				n := cancelActiveHosts()
				emit(os.Stderr, fmt.Sprintf("\n[Ctrl+C x2] 현재 진행 중인 %d대를 취소하고 다음 호스트를 계속 진행합니다.\n", n))
			case count == 3:
				emit(os.Stderr, "\n[Ctrl+C x3] 전체 작업을 중단합니다. 지금까지의 결과로 마무리합니다.\n")
				abortAll()
			default:
				restoreTerminal()
				os.Exit(130)
			}
		}
	}()
}

// startTerminalGuard는 진행률(커서 숨김)이나 -m(입력 모드 변경)을 쓰는 동안 SIGTERM/SIGHUP으로
// 종료돼도 터미널을 원래 상태로 되돌리고 종료한다.
func startTerminalGuard() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-ch
		restoreTerminal()
		if s == syscall.SIGHUP {
			os.Exit(129)
		}
		os.Exit(143)
	}()
}

// ★ 옵션 도움말(그냥 실행하거나 -h). 맨 위에 옵션별 한 줄 요약, 그 아래에 한글 상세 설명.
type optionDoc struct {
	flag, arg, summary string
	detail             []string
}

var optionDocs = []optionDoc{
	{"w", "<파일>", "호스트 목록 파일 (필수)", []string{
		"한 줄에 호스트 하나, 또는 쉼표/공백으로 여러 개를 나열할 수 있습니다. # 으로 시작하는 줄은 주석입니다.",
		"범위 표기를 지원합니다: esxi[0001-0020], qwer[2660,2671,2826-2829], hostname[0001-0002]ev[01-03]",
		"중복 호스트는 자동으로 제거됩니다. -w 파일, -w=파일, -w^파일, -w파일 형태 모두 가능합니다.",
	}},
	{"u", "<계정>", "SSH 접속 계정 (기본 root)", nil},
	{"p", "<비밀번호>", "SSH 비밀번호 (키 인증 실패 시에만 사용)", []string{
		"SSH 키 인증을 항상 먼저 시도하고, 키로 접속되지 않는 호스트에만 이 비밀번호를 사용합니다.",
	}},
	{"i", "<키파일>", "SSH 개인키 경로 (미지정 시 자동 탐색)", []string{
		"지정하지 않으면 ~/.ssh/id_ed25519 → id_ecdsa → id_rsa 순서로 찾습니다.",
	}},
	{"P", "<포트>", "SSH 포트 (기본 22)", []string{
		"호스트 파일에 host:포트 형태로 적은 호스트는 그 포트를 우선 사용합니다.",
	}},
	{"c", "<N>", "동시 접속 수 (기본 1000)", []string{
		fmt.Sprintf("명령어에 /user/ 경로가 들어 있으면 autofs 보호를 위해 자동으로 %d으로 제한됩니다.", autofsSafeConcurrency),
	}},
	{"cf", "<N>", "동시 접속 수 강제 지정 (/user/ 자동 제한 무시)", []string{
		"/user/ 경로 감지로 인한 자동 제한을 무시하고 이 값으로 병렬 실행합니다.",
	}},
	{"t", "<초>", "접속 제한시간 (기본 15초, 명령 실행 시간엔 미적용)", []string{
		"TCP 연결 + SSH 핸드셰이크 + 인증(로그인)까지만 적용됩니다. 로그인한 뒤 명령 실행 시간에는 제한이 없어서",
		"10분 이상 걸리는 드라이버 설치 같은 작업도 끊기지 않습니다.",
		"1차 실행 후 접속불가로 분류된 호스트는 낮은 동시성과 최대 8초 제한으로 한 번 더 재검증합니다.",
	}},
	{"b", "", "결과가 같은 호스트끼리 묶어서 출력 (clush -b 방식)", []string{
		"결과 본문이 같은 호스트를 esxi[0001-0010] 같은 요약 표기로 묶어 한 번만 출력합니다.",
		"접속불가 호스트는 별도 그룹으로 표시됩니다. -script와 같이 쓰면 무시되고 호스트별로 출력합니다.",
	}},
	{"m", "", "행리스트보기 (오래 걸리는 호스트 확인)", []string{
		fmt.Sprintf("실행 중 Enter를 누르면 %d초 이상 실행 중이거나 실행했던 호스트와 진행시간을 보여줍니다.", int(hangThreshold.Seconds())),
		"도중에 끝난 호스트는 끝난 시각에서 시간이 멈춥니다. 다시 Enter를 누르면 원래 화면으로 돌아갑니다.",
		"터미널에서 실행한 경우에만 동작합니다.",
	}},
	{"pm", "", "OS 설치중 감지 + 특정 autofs 계정 경로 접근 점검", []string{
		"~/.profile에 anaconda가 있으면 OS 설치중으로 보고 명령을 실행하지 않습니다(<호스트파일>_os_install).",
		"특정 autofs 계정 경로에 접근할 수 없는 호스트는 <호스트파일>_nosvrauto에 기록합니다.",
		fmt.Sprintf("점검은 본 명령 전에 한 번 실행하며 점검에만 %d초 제한이 있습니다(본 명령은 제한 없음).", int(pmCheckTimeout.Seconds())),
	}},
	{"script", "", "요약·색상 없이 호스트별 결과만 출력", []string{
		"작업 요약 블록, 색상, 재검증 안내를 출력하지 않습니다. 다른 스크립트에서 결과를 읽을 때 사용합니다.",
		"실행 파일 이름이 pdsh이면 기본으로 켜집니다(-script=false로 끌 수 있음).",
	}},
	{"dnlgjawkrdjqghkrdls", "", "위험 명령 실행 허용 (재부팅/종료 등)", []string{
		"reboot, poweroff, shutdown, halt, init 0, init 6, ddc 가 들어간 명령은 기본적으로 실행을 거부합니다.",
		"이 옵션을 주면 대상 호스트 목록을 보여주고 y/N 확인을 받은 뒤 실행합니다.",
	}},
}

func printUsage() {
	name := filepath.Base(os.Args[0])
	var b strings.Builder
	fmt.Fprintf(&b, "%s - 여러 서버에 동시에 SSH 명령을 실행합니다 (pdsh 방식 출력: 호스트명: 결과)\n\n", name)
	fmt.Fprintf(&b, "사용법: %s -w <호스트파일> [옵션] \"명령어\"\n", name)
	fmt.Fprintf(&b, "  예) %s -w hosts.txt \"cat /etc/os-release\"\n\n", name)

	label := func(d optionDoc) string {
		if d.arg == "" {
			return "-" + d.flag
		}
		return "-" + d.flag + " " + d.arg
	}
	b.WriteString("[옵션 요약]\n")
	width := 0
	for _, d := range optionDocs {
		if w := displayWidth(label(d)); w > width {
			width = w
		}
	}
	for _, d := range optionDocs {
		fmt.Fprintf(&b, "  %s  %s\n", padRight(label(d), width), d.summary)
	}

	b.WriteString("\n[옵션 상세]\n")
	for _, d := range optionDocs {
		fmt.Fprintf(&b, "  %s\n", label(d))
		fmt.Fprintf(&b, "      %s\n", d.summary)
		for _, line := range d.detail {
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}

	// 설명표에 빠진 옵션이 새로 생겨도 도움말에서 누락되지 않게 한다.
	documented := map[string]bool{}
	for _, d := range optionDocs {
		documented[d.flag] = true
	}
	flag.VisitAll(func(f *flag.Flag) {
		if !documented[f.Name] {
			fmt.Fprintf(&b, "  -%s\n      %s\n", f.Name, f.Usage)
		}
	})
	fmt.Fprint(os.Stderr, b.String())
}

func isAutofsUserPath(command string) bool {
	return autofsUserPathRegex.MatchString(command)
}

func main() {
	hostFile := flag.String("w", "", "호스트 목록 파일 경로")
	user := flag.String("u", "root", "SSH 접속 계정")
	password := flag.String("p", "", "SSH 접속 비밀번호")
	keyPath := flag.String("i", "", "SSH 키 파일 경로")
	port := flag.String("P", "22", "SSH 포트")
	concurrency := flag.Int("c", 1000, "동시 접속 수")
	forceConcurrency := flag.Int("cf", 0, "동시 접속 수 강제 지정 (autofs /user/ 경로 감지로 인한 자동 제한을 무시)")
	timeoutSec := flag.Int("t", 15, "접속 타임아웃 초 (기본 15초)")
	dangerConfirm := flag.Bool("dnlgjawkrdjqghkrdls", false, "위험 작업 강제 실행 확인 옵션")
	// ★ 실행 파일 이름이 pdsh면 -script 기본값을 true로 (필요하면 -script=false로 명시적 해제 가능)
	isPdshName := filepath.Base(os.Args[0]) == "pdsh"
	scriptMode := flag.Bool("script", isPdshName, "작업 요약 출력 숨김 (순수 결과만 출력). 실행 파일 이름이 pdsh면 기본값 true")
	pmMode := flag.Bool("pm", false, "OS 설치중 감지 + 특정 autofs 계정 경로 접근 점검")
	bMode := flag.Bool("b", false, "clush 스타일: 결과가 동일한 호스트끼리 묶어서 출력 (-script와 함께 쓰면 무시되고 호스트별로 출력)")
	mMode := flag.Bool("m", false, "행리스트보기: 실행 중 Enter를 누르면 60초 이상 실행 중인(또는 실행했던) 호스트와 진행시간을 보여줌, 다시 Enter로 복귀")

	// ★ pdsh 스타일 "-w^file"/"-wfile" 붙여쓰기 지원을 위해 flag.Parse() 대신 전처리한 인자로 파싱
	flag.Usage = printUsage
	flag.CommandLine.Parse(preprocessArgs(os.Args[1:]))

	stdoutTTY = isCharDevice(os.Stdout)
	stderrTTY = isCharDevice(os.Stderr)
	colorEnabled = !*scriptMode && stderrTTY
	colorOutEnabled = !*scriptMode && stdoutTTY

	args := flag.Args()
	if *hostFile == "" || len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	authMethods, err := getAuthMethods(*keyPath, *password)
	if err != nil {
		log.Fatalf("SSH 인증 설정 오류: %v\n", err)
	}

	cleanHostFile := strings.TrimPrefix(*hostFile, "^")

	// ★ 같은 위치에서 다시 실행하면, 이전 실행이 남긴 결과 파일이 이번 실행 결과와 안 맞게
	// 그대로 남아있을 수 있다(예: 이전엔 실패 호스트가 있었지만 이번엔 하나도 없는 경우 —
	// writeHostsToFile은 대상이 없으면 파일을 만들지 않으므로 옛 파일이 그대로 남는다).
	// 실행 시작 시점에 이번 실행이 만들 수 있는 5개 결과 파일명과 정확히 일치하는 파일만
	// (와일드카드 없이 완전일치로) 미리 지운다.
	offFilename := cleanHostFile + "_res_off"
	refusedFilename := cleanHostFile + "_res_refsed"
	osInstallFilename := cleanHostFile + "_os_install"
	noSvrAutoFilename := cleanHostFile + "_nosvrauto"
	cancelFilename := cleanHostFile + "_res_cancel"
	for _, f := range []string{offFilename, refusedFilename, osInstallFilename, noSvrAutoFilename, cancelFilename} {
		os.Remove(f)
	}

	command := strings.Join(args, " ")
	// ★ [버그 수정] 예전에는 여기서 strings.Trim(command, "\"'")로 앞뒤 따옴표를 무조건
	// 제거했는데, 이러면 명령어 끝이 실제로 따옴표로 끝나는 경우(예: grep 'asdf')까지
	// 그 따옴표를 잘라내버려서 명령어가 깨졌다(실제로 재현: `cat test |grep 'asdf'`가
	// `cat test |grep 'asdf`로 깨짐). 쉘이 넘겨준 인자에는 이미 실제 따옴표가 없으므로
	// 이 처리 자체가 불필요해서 제거했다.

	// ★ autofs 안전장치: /user/ 경로가 명령어에 포함되어 있으면 병렬 수를 450으로 강제.
	// -cf 로 명시적으로 병렬 수를 지정한 경우에만 이 제한을 무시하고 지정값을 그대로 쓴다.
	effectiveConcurrency := *concurrency
	if *forceConcurrency > 0 {
		effectiveConcurrency = *forceConcurrency
	} else if isAutofsUserPath(command) {
		effectiveConcurrency = autofsSafeConcurrency
		// ★ 실행 파일 이름이 pdsh면 이 안내 메시지를 찍지 않는다(위험 작업 경고 메시지만 예외).
		// pdsh 대체용으로 쓸 때는 pdsh에 없는 gossh 전용 메시지가 섞이면 안 되기 때문.
		if !isPdshName {
			fmt.Fprintln(os.Stderr, colorize(colorYellow, fmt.Sprintf("[안전장치] 명령어에 \"/user/\" 경로가 감지되어 병렬 실행 수를 %d대로 자동 제한합니다. (원래 지정값 무시: -c %d)", autofsSafeConcurrency, *concurrency)))
			fmt.Fprintln(os.Stderr, colorize(colorYellow, "           이 경로가 autofs 마운트가 아니거나 더 높은 병렬 수가 필요하면 -cf <숫자> 옵션으로 강제 지정하세요."))
		}
	}

	lowerCmd := strings.ToLower(command)
	isDangerous := strings.Contains(lowerCmd, "reboot") ||
		strings.Contains(lowerCmd, "poweroff") ||
		strings.Contains(lowerCmd, "shutdown") ||
		strings.Contains(lowerCmd, "halt") ||
		strings.Contains(lowerCmd, "init 0") ||
		strings.Contains(lowerCmd, "init 6") ||
		strings.Contains(lowerCmd, "ddc")

	if isDangerous && !*dangerConfirm {
		fmt.Fprintln(os.Stderr, colorize(colorRed, "================================================================"))
		fmt.Fprintln(os.Stderr, colorize(colorRed, " [경고] 위험 작업(시스템 종료/재부팅)이 감지되었습니다!"))
		fmt.Fprintln(os.Stderr, colorize(colorRed, "================================================================"))
		fmt.Fprintf(os.Stderr, " 감지된 명령어 : %s\n", command)
		fmt.Fprintln(os.Stderr, " 실행을 원하신다면 명령어에 '-dnlgjawkrdjqghkrdls' 옵션을 추가하세요.")
		fmt.Fprintln(os.Stderr, " 예시) ./gossh -dnlgjawkrdjqghkrdls -w kdh.txt reboot")
		os.Exit(1)
	}

	file, err := os.Open(cleanHostFile)
	if err != nil {
		log.Fatalf("호스트 파일을 열 수 없습니다: %v\n", err)
	}
	defer file.Close()

	var hosts []string
	scanner := bufio.NewScanner(file)
	// ★ 기본 한 줄 최대 길이(64KB)를 넘는 줄(호스트를 한 줄에 수천 대 가로로 나열한 경우)이 있으면
	// 스캐너가 조용히 멈춰서 호스트가 통째로 누락됐다. 최대 64MB까지 허용하고 오류는 알린다.
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			// ★ 한 줄에 "hostname1 hostname2 hostname3"처럼 공백(가로)으로 나열된 경우도
			// 쉼표와 동일하게 각각 별도 호스트로 인식한다(쉼표+공백 혼용도 가능).
			for _, token := range strings.FieldsFunc(line, func(r rune) bool {
				return r == ',' || unicode.IsSpace(r)
			}) {
				if token != "" {
					expanded := expandHostLine(token)
					hosts = append(hosts, expanded...)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("호스트 파일을 읽는 중 오류가 발생했습니다: %v\n", err)
	}

	if len(hosts) == 0 {
		fmt.Fprintf(os.Stderr, "경고: %s 파일에 등록된 호스트가 없습니다.\n", cleanHostFile)
		os.Exit(0)
	}

	// 고유 호스트 중복 제거
	hostMap := make(map[string]bool)
	var uniqueHosts []string
	for _, h := range hosts {
		if !hostMap[h] {
			hostMap[h] = true
			uniqueHosts = append(uniqueHosts, h)
		}
	}
	hosts = uniqueHosts

	if *dangerConfirm {
		fmt.Fprintln(os.Stderr, "\n================================================================")
		fmt.Fprintf(os.Stderr, " [주의] 위험 작업 옵션이 활성화되었습니다. 실행 명령어: %s\n", command)
		fmt.Fprintf(os.Stderr, " 대상 호스트 (총 %d대):\n", len(hosts))
		fmt.Fprintln(os.Stderr, "================================================================")
		for _, h := range hosts {
			fmt.Fprintf(os.Stderr, " - %s\n", h)
		}
		fmt.Fprintln(os.Stderr, "================================================================")
		fmt.Fprintln(os.Stderr, " 작업대상이 맞는지 다시한번더 확인하세요. 실수를 하게되면 회사 전체직원의 100만원이 증발됩니다.")
		fmt.Fprint(os.Stderr, "정말로 위 서버들에 명령을 실행하시겠습니까? (y/N): ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Fprintln(os.Stderr, "작업이 취소되었습니다.")
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "\n승인되었습니다. 작업을 시작합니다...")
	}

	// ★ glibc 리졸버는 대량 동시 조회 시 내부 스레드풀 제약으로 간헐적 실패/지연이 날 수 있어,
	// Go 자체 순수 구현 리졸버를 쓰도록 강제한다(시스템 리졸버 스레드풀 병목 회피).
	net.DefaultResolver = &net.Resolver{PreferGo: true}

	var wg sync.WaitGroup
	sem := make(chan struct{}, effectiveConcurrency)
	dnsSem := make(chan struct{}, dnsLookupConcurrency)
	timeout := time.Duration(*timeoutSec) * time.Second

	var mu sync.Mutex
	var successCount int
	var failedHosts []string
	var refusedHosts []string
	var osInstallHosts []string
	var noSvrAutoHosts []string // ★ /user/svrauto 미접근 호스트 기록

	// ★ -b는 -script와 같이 오면 무시한다(순수 결과 모드에서는 묶어 보여주는 요약형 출력이
	// 목적과 안 맞음). bunchMode가 false면 기존과 동일하게 즉시 호스트별로 출력한다.
	bunchMode := *bMode && !*scriptMode
	bunchOutputs := map[string]string{}

	startTime := time.Now()

	// ★ 진행률 표시(카운트, 1초 갱신). 표준에러로 출력하고(데이터 아님, 리다이렉션에 안 섞이게),
	// pdsh 이름일 때는 아예 찍지 않는다(pdsh에 없는 gossh 전용 출력이 섞이면 안 되기 때문).
	var completed int64
	var progressWG sync.WaitGroup
	progressStop := make(chan struct{})
	total := int64(len(hosts))

	// ★ -m(행리스트보기)는 키 입력(Enter)을 받아야 하므로 입력/표준에러가 모두 터미널일 때만 켠다.
	// -m은 사용자가 명시적으로 준 옵션이라 pdsh 이름이어도 동작한다.
	if *mMode {
		if !stderrTTY || !isCharDevice(os.Stdin) {
			fmt.Fprintln(os.Stderr, "[-m] 터미널에서 실행한 경우에만 행리스트보기를 쓸 수 있어 무시합니다.")
		} else if restore, err := enableCbreak(int(os.Stdin.Fd())); err != nil {
			fmt.Fprintf(os.Stderr, "[-m] 키 입력 모드를 설정할 수 없어 무시합니다: %v\n", err)
		} else {
			cbreakRestore = restore
			monitorOn = true
		}
	}

	// ★ 진행률 표시(카운트, 1초 갱신). 표준에러로 출력하고(데이터 아님, 리다이렉션에 안 섞이게),
	// pdsh 이름일 때는 아예 찍지 않는다(pdsh에 없는 gossh 전용 출력이 섞이면 안 되기 때문). 단 -m을
	// 명시한 경우엔 행리스트 안내가 진행률 줄에 붙으므로 켠다.
	progressEnabled := stderrTTY && (!isPdshName || monitorOn)
	if progressEnabled {
		monitorStart = startTime
		startTerminalGuard()
		printProgress := func() {
			c := atomic.LoadInt64(&completed)
			pct := int64(0)
			if total > 0 {
				pct = c * 100 / total
			}
			text := fmt.Sprintf("진행: %d/%d (%d%%)", c, total, pct)
			if monitorOn {
				text += fmt.Sprintf("  [Enter: 행리스트보기 - %d초 이상 실행 중 %d대]", int(hangThreshold.Seconds()), countHangRunning())
			}
			outMu.Lock()
			progressText = text
			if listActive {
				renderHangListLocked()
			} else {
				drawProgressLocked(false)
			}
			outMu.Unlock()
		}
		outMu.Lock()
		progressOn = true
		cursorHidden = true
		fmt.Fprint(os.Stderr, "\033[?25l") // 진행 중 커서 숨김(줄 맨 앞에서 커서가 깜빡이는 것 방지)
		outMu.Unlock()
		printProgress()
		if monitorOn {
			startMonitorKeys()
		}
		progressWG.Add(1)
		go func() {
			defer progressWG.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					printProgress()
				case <-progressStop:
					leaveListIfActive()
					printProgress()
					outMu.Lock()
					progressOn = false
					fmt.Fprintln(os.Stderr)
					outMu.Unlock()
					restoreTerminal()
					return
				}
			}
		}()
	}

	startInterruptHandler()

	for _, host := range hosts {
		wg.Add(1)
		go runSSHCommand(host, command, *user, authMethods, *port, timeout, *pmMode, bunchMode, bunchOutputs, &wg, sem, dnsSem, &successCount, &failedHosts, &refusedHosts, &osInstallHosts, &noSvrAutoHosts, &mu, &completed)
	}

	wg.Wait()

	if progressEnabled {
		close(progressStop)
		progressWG.Wait()
	}

	// ★ 재검증: 대량 동시 실행 시 DNS/접속 혼잡으로 실제로는 접속 가능한데 "접속불가"로
	// 오분류되는 경우가 있어, 1차 실행이 끝난 뒤 접속불가(failedHosts+refusedHosts)로 분류된
	// 호스트만 훨씬 낮은 동시성으로 한 번 더 시도한다(명령어까지 실제로 재실행). 이미 성공한
	// 호스트는 건드리지 않으므로, 실패 대상이 적은 일반적인 경우엔 전체 속도에 미치는 영향이
	// 거의 없다(실패 대상이 원래부터 많다면 그만큼 재검증에도 시간이 걸릴 수 있음 — 애초에
	// 실제로 다운된 호스트가 많다는 뜻이라 이 경우는 불가피함).
	if atomic.LoadInt32(&aborted) != 1 {
		retryHosts := append(append([]string{}, failedHosts...), refusedHosts...)
		if len(retryHosts) > 0 {
			if !*scriptMode {
				fmt.Fprintf(os.Stderr, "재검증 중(%d대, 낮은 동시성으로 재시도)...\n", len(retryHosts))
			}
			failedHosts = nil
			refusedHosts = nil
			successBefore := successCount
			retryConcurrency := effectiveConcurrency
			if retryConcurrency > 50 {
				retryConcurrency = 50
			}
			retrySem := make(chan struct{}, retryConcurrency)
			// ★ 재검증은 1차 실행의 혼잡이 풀린 뒤라 정상 호스트는 금방 접속된다. 접속(로그인) 제한시간을
			// 최대 8초로 줄여서 응답 없는 호스트 때문에 늘어나는 시간을 줄인다(-t가 더 짧으면 -t 사용).
			// 본 명령 실행 시간에는 제한이 없다.
			retryTimeout := timeout
			if retryTimeout > retryConnectTimeout {
				retryTimeout = retryConnectTimeout
			}
			var retryWG sync.WaitGroup
			for _, h := range retryHosts {
				retryWG.Add(1)
				go runSSHCommand(h, command, *user, authMethods, *port, retryTimeout, *pmMode, bunchMode, bunchOutputs, &retryWG, retrySem, dnsSem, &successCount, &failedHosts, &refusedHosts, &osInstallHosts, &noSvrAutoHosts, &mu, &completed)
			}
			retryWG.Wait()
			if recovered := successCount - successBefore; recovered > 0 && !*scriptMode {
				fmt.Fprintf(os.Stderr, "재검증 결과: %d대는 실제로 접속/실행 가능(최초엔 일시적 혼잡 등으로 오분류됨).\n", recovered)
			}
		}
	}

	if bunchMode {
		printBunched(hosts, bunchOutputs)
		printUnreachableGroup(failedHosts, refusedHosts)
	}

	// 결과 파일 생성 및 저장 (파일명은 실행 시작 시점에 이미 계산해 둠)
	writeHostsToFile(offFilename, failedHosts)
	writeHostsToFile(refusedFilename, refusedHosts)
	writeHostsToFile(osInstallFilename, osInstallHosts)
	writeHostsToFile(noSvrAutoFilename, noSvrAutoHosts)
	writeHostsToFile(cancelFilename, canceledHosts)

	// -script 옵션이 없을 때만 요약 출력. 표준에러로 보낸다 — 이 블록은 데이터가 아니라
	// 상태 요약이라, "gossh -w a cmd > res" 같은 단순 리다이렉션에서 res에 섞이면 안 된다.
	if !*scriptMode {
		fmt.Fprintln(os.Stderr, colorize(colorCyanB, "\n================= 작업 요약 ================="))
		fmt.Fprintf(os.Stderr, "총 대상 서버 : %d 대 (소요시간: %v)\n", len(hosts), time.Since(startTime))
		fmt.Fprintf(os.Stderr, " 동시 접속 수 : %d\n", effectiveConcurrency)
		fmt.Fprintln(os.Stderr, colorize(colorGreen, fmt.Sprintf(" 정상 접속 가능 : %d 대", successCount)))

		// OS 설치 중 출력 (-pm 옵션을 준 경우에만 기록되므로 바로 출력)
		if len(osInstallHosts) > 0 {
			fmt.Fprintln(os.Stderr, colorize(colorYellow, fmt.Sprintf(" OS 설치중 : %d 대", len(osInstallHosts))))
			for _, h := range osInstallHosts {
				fmt.Fprintf(os.Stderr, "   - %s\n", h)
			}
			fmt.Fprintf(os.Stderr, "  -> %s 에 목록 저장됨\n", osInstallFilename)
		}

		// ★ pm 옵션을 사용했고, 미접근 서버가 존재하는 경우 출력
		if *pmMode && len(noSvrAutoHosts) > 0 {
			fmt.Fprintln(os.Stderr, colorize(colorYellow, fmt.Sprintf(" svrauto미접근 : 접속가능 %d대 중 %d대", successCount, len(noSvrAutoHosts))))
			fmt.Fprintf(os.Stderr, "  -> %s 에 목록 저장됨\n", noSvrAutoFilename)
		}

		failLine := fmt.Sprintf(" 접속 불가(Timeout 등) : %d 대", len(failedHosts))
		if len(failedHosts) > 0 {
			fmt.Fprintln(os.Stderr, colorize(colorRed, failLine))
			fmt.Fprintf(os.Stderr, "  -> %s 에 목록 저장됨\n", offFilename)
		} else {
			fmt.Fprintln(os.Stderr, failLine)
		}

		refusedLine := fmt.Sprintf(" Refused(포트 닫힘) : %d 대", len(refusedHosts))
		if len(refusedHosts) > 0 {
			fmt.Fprintln(os.Stderr, colorize(colorRed, refusedLine))
			fmt.Fprintf(os.Stderr, "  -> %s 에 목록 저장됨\n", refusedFilename)
		} else {
			fmt.Fprintln(os.Stderr, refusedLine)
		}
		if len(canceledHosts) > 0 {
			fmt.Fprintln(os.Stderr, colorize(colorYellow, fmt.Sprintf(" 사용자 취소(Ctrl+C) : %d 대", len(canceledHosts))))
			fmt.Fprintf(os.Stderr, "  -> %s 에 목록 저장됨\n", cancelFilename)
		}
		if atomic.LoadInt32(&aborted) == 1 {
			fmt.Fprintln(os.Stderr, colorize(colorYellow, fmt.Sprintf(" 전체 중단됨 : 미실행 %d 대", atomic.LoadInt64(&notRunCount))))
		}
		fmt.Fprintln(os.Stderr, colorize(colorCyanB, "============================================="))
	}
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
