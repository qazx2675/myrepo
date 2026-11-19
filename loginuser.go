package main

// -lu : 서버별로 "어떤 user 가 SSH 로 로그인했는지"를 /var/log/secure* 에서 시간 범위로 조회하는 옵션.
// 시작·끝 시간(YYYYMMDDHH)을 묻고, 서버마다 SSH 연결 하나로 조회 스크립트를 원격 bash 의 표준입력("bash -s")으로
// 보내 실행한다(survey.go 의 접속·동시성·재검증을 그대로 쓴다). 결과는 (호스트, user) 쌍마다 한 줄:
//   호스트|MMDD_HHMM|user   (시간은 그 user 의 범위 안 마지막 로그인 시각, 한 호스트의 user 는 이름순으로 연속)
// 로그 줄에는 연도가 없어서 월·일·시만 비교한다. 시작·끝 연도가 다르면 연말을 넘는 범위로 본다.

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const sufLoginUser = "_loginuser.txt"

// luNoReadMark는 /var/log/secure* 를 하나도 읽지 못했을 때 원격 스크립트가 내는 표시다.
const luNoReadMark = "#NOREAD"

// ---------------------------------------------------------------- 시간 범위

// luRange는 조회 범위다. start·end 는 YYYYMMDDHH 정수이고, 끝 시각은 그 시(時)의 59분 59초까지 포함한다.
type luRange struct{ start, end int }

// wrap은 시작·끝 연도가 달라 연말을 넘는 범위인지 알려준다.
func (r luRange) wrap() bool { return r.start/1000000 != r.end/1000000 }

// startMDH·endMDH 는 연도를 뺀 MMDDHH 값이다.
func (r luRange) startMDH() int { return r.start % 1000000 }
func (r luRange) endMDH() int   { return r.end % 1000000 }

var luTimeRegex = regexp.MustCompile(`^[0-9]{10}$`)

// parseLUTime은 YYYYMMDDHH 를 검사해 정수로 돌려준다. 정확히 숫자 10자리이고 월·일·시가 실제로 있어야 한다.
func parseLUTime(s string) (v int, errMsg string) {
	if !luTimeRegex.MatchString(s) {
		return 0, "YYYYMMDDHH 형식의 숫자 10자리로 입력하세요 (예: 2026111900)."
	}
	y, _ := strconv.Atoi(s[0:4])
	mo, _ := strconv.Atoi(s[4:6])
	d, _ := strconv.Atoi(s[6:8])
	h, _ := strconv.Atoi(s[8:10])
	if y < 1 || mo < 1 || mo > 12 || h > 23 {
		return 0, "없는 월 또는 시간입니다 (월 01~12, 시 00~23)."
	}
	if t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC); d < 1 || t.Month() != time.Month(mo) || t.Day() != d {
		return 0, "그 달에 없는 날짜입니다."
	}
	v, _ = strconv.Atoi(s)
	return v, ""
}

// luHuman은 YYYYMMDDHH 를 "2026-11-19 09시" 로 보여준다.
func luHuman(v int) string {
	s := fmt.Sprintf("%010d", v)
	return fmt.Sprintf("%s-%s-%s %s시", s[0:4], s[4:6], s[6:8], s[8:10])
}

// ---------------------------------------------------------------- 입력

// luPrompt는 질문을 표준에러로 찍고 한 줄을 읽는다. 입력이 끝났으면 안내 후 종료한다.
func luPrompt(msg string) string {
	fmt.Fprint(os.Stderr, msg)
	line, ok := readSurveyLine()
	if !ok {
		fmt.Fprintln(os.Stderr, "\n입력이 끝나 접속 user 조회를 중단합니다.")
		os.Exit(1)
	}
	return strings.TrimSpace(line)
}

// askLUTime은 올바른 YYYYMMDDHH 가 입력될 때까지 묻는다. min 이상이어야 하고(0이면 제한 없음) 아니면 다시 묻는다.
func askLUTime(prompt string, min int) int {
	for {
		v, msg := parseLUTime(luPrompt(prompt))
		if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
			continue
		}
		if v < min {
			fmt.Fprintf(os.Stderr, "끝 시간은 시작 시간(%d)보다 빠를 수 없습니다. 다시 입력하세요.\n", min)
			continue
		}
		return v
	}
}

// askLURange는 시작 시간과 끝 시간을 차례로 묻는다.
func askLURange() luRange {
	start := askLUTime("1. 시작 시간 (YYYYMMDDHH, 예: 2026111900) : ", 0)
	end := askLUTime("2. 끝 시간 (YYYYMMDDHH, 예: 2026111923) : ", start)
	return luRange{start: start, end: end}
}

// ---------------------------------------------------------------- 원격 스크립트

// luScriptBody는 원격에서 도는 조회 스크립트 본문이다(머리의 LU_START/LU_END/LU_WRAP 는 luScript 가 붙인다).
// /var/log/secure* 를 모두 읽어(.gz 는 zcat, 못 읽는 파일은 건너뜀) 성공한 SSH 로그인("sshd ... Accepted ... for USER")만
// 세고, root 는 뺀다. 출력은 user 이름순 "user MMDD_HHMM"(범위 안 마지막 로그인). 하나도 못 읽으면 #NOREAD 한 줄.
const luScriptBody = `LU_DIR="${GOSSH_LU_LOGDIR:-/var/log}"
LU_OK=0
for f in "$LU_DIR"/secure*; do
  if [ -f "$f" ] && [ -r "$f" ]; then LU_OK=1; fi
done
if [ "$LU_OK" = 0 ]; then
  echo "#NOREAD"
  exit 0
fi
for f in "$LU_DIR"/secure*; do
  [ -f "$f" ] && [ -r "$f" ] || continue
  case "$f" in
    *.gz) zcat "$f" 2>/dev/null ;;
    *) cat "$f" 2>/dev/null ;;
  esac
done | awk -v s="$LU_START" -v e="$LU_END" -v w="$LU_WRAP" '
BEGIN { mon = "JanFebMarAprMayJunJulAugSepOctNovDec"; s += 0; e += 0; w += 0 }
/sshd/ && /Accepted / {
  if (length($1) != 3) next
  p = index(mon, $1)
  if (p == 0 || (p - 1) % 3 != 0) next
  m = (p - 1) / 3 + 1
  d = $2 + 0
  h = substr($3, 1, 2) + 0
  mi = substr($3, 4, 2) + 0
  md = m * 10000 + d * 100 + h
  if (w) { if (md < s && md > e) next } else { if (md < s || md > e) next }
  u = ""
  for (i = 4; i < NF; i++) {
    if ($i == "Accepted") {
      for (j = i + 1; j < NF; j++) {
        if ($j == "for") { u = $(j + 1); break }
      }
      break
    }
  }
  if (u == "" || u == "root" || u == "invalid") next
  k = md * 100 + mi
  if (w && md <= e) k += 100000000
  if (!(u in last) || k > last[u]) { last[u] = k; disp[u] = sprintf("%02d%02d_%02d%02d", m, d, h, mi) }
}
END { for (u in last) print u, disp[u] }
' | LC_ALL=C sort
`

// luShellQuote는 글자를 셸 작은따옴표로 감싼다.
func luShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// luScript는 범위값을 머리에 붙인 전체 스크립트를 만든다. logDir 이 비어 있지 않으면 시험용으로 로그 폴더를 바꾼다
// (원격에서 GOSSH_LU_LOGDIR 로 직접 실행할 때와 같은 변수).
func luScript(r luRange, logDir string) string {
	w := 0
	if r.wrap() {
		w = 1
	}
	head := fmt.Sprintf("LU_START=%06d\nLU_END=%06d\nLU_WRAP=%d\n", r.startMDH(), r.endMDH(), w)
	if logDir != "" {
		head += "GOSSH_LU_LOGDIR=" + luShellQuote(logDir) + "\n"
	}
	return head + luScriptBody
}

// ---------------------------------------------------------------- 결과 표

// luUser는 한 user 의 마지막 로그인 시각("MMDD_HHMM")이다.
type luUser struct{ name, last string }

var luLastRegex = regexp.MustCompile(`^[0-9]{4}_[0-9]{4}$`)

// parseLUOutput은 원격 스크립트 출력을 읽는다. noRead 는 로그를 하나도 읽지 못했다는 뜻이다.
func parseLUOutput(out string) (users []luUser, noRead bool) {
	for _, line := range strings.Split(normalizeSurveyOut(out), "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 1 && f[0] == luNoReadMark:
			return nil, true
		case len(f) == 2 && luLastRegex.MatchString(f[1]):
			users = append(users, luUser{f[0], f[1]})
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].name < users[j].name })
	return users, false
}

// buildLoginUserTable은 호스트 파일 순서대로 "호스트|시각|user" 줄을 만든다(머리줄 없음, user 마다 한 줄).
// 접속·실행 실패는 "호스트|-|-", 로그를 못 읽으면 "호스트|!|!", 범위 안에 로그인이 없으면 "호스트||" 이다.
// down 은 "-" 가 된 호스트, unread 는 "!" 가 된 호스트다.
func buildLoginUserTable(hosts []string, res map[string]surveyHost) (table string, down, unread []string) {
	var b strings.Builder
	for _, h := range hosts {
		r, ok := res[h]
		if !ok || !r.reachable || len(r.cells) == 0 || !r.cells[0].ok {
			down = append(down, h)
			b.WriteString(h + "|-|-\n")
			continue
		}
		users, noRead := parseLUOutput(r.cells[0].out)
		if noRead {
			unread = append(unread, h)
			b.WriteString(h + "|!|!\n")
			continue
		}
		if len(users) == 0 {
			b.WriteString(h + "||\n")
			continue
		}
		for _, u := range users {
			b.WriteString(h + "|" + u.last + "|" + u.name + "\n")
		}
	}
	return b.String(), down, unread
}

// ---------------------------------------------------------------- 진행 흐름

// loginUserFlow는 범위를 묻고 모든 호스트에 조회 스크립트를 실행한다(접속불가는 재검증).
func loginUserFlow(hosts []string, run surveyRunFunc, logDir string) map[string]surveyHost {
	r := askLURange()
	fmt.Fprintf(os.Stderr, "\n조회 범위: %s ~ %s (끝 시각은 59분 59초까지)\n", luHuman(r.start), luHuman(r.end))
	if r.wrap() {
		fmt.Fprintln(os.Stderr, "시작·끝 연도가 달라 연말을 넘는 범위로 봅니다 (로그에는 연도가 없어 월·일·시만 비교).")
	}
	fmt.Fprintf(os.Stderr, "전체 실행: %d대 (동시 실행)\n", len(hosts))
	return run(hosts, []string{luScript(r, logDir)}, true)
}

// printLoginUserDone은 표를 표준출력에 찍고 파일로 저장한 뒤 경고와 경로를 표준에러로 알린다.
func printLoginUserDone(hosts []string, res map[string]surveyHost, hostFile string) {
	table, down, unread := buildLoginUserTable(hosts, res)
	fmt.Fprint(os.Stdout, table)

	path := hostFile + sufLoginUser
	if err := os.WriteFile(path, []byte(table), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "결과 파일 저장 실패 (%s): %v\n", path, err)
	}
	if len(down) > 0 {
		fmt.Fprintf(os.Stderr, "\n접속불가·실행 실패 %d대 (-): %s\n", len(down), surveyHostSample(down, 10))
	}
	if len(unread) > 0 {
		fmt.Fprintln(os.Stderr, "\n"+surveyWarn(fmt.Sprintf("[경고] /var/log/secure* 를 읽을 수 없는 서버 %d대 (!): %s", len(unread), surveyHostSample(unread, 10))))
	}
	fmt.Fprintf(os.Stderr, "\n결과 파일: %s\n", path)
}

// runLoginUser는 -lu 전체를 진행한다(main 에서 호스트 목록을 읽은 뒤 부른다). 종료코드를 돌려준다.
func runLoginUser(hosts []string, hostFile string, o surveyOpts) int {
	os.Remove(hostFile + sufLoginUser)
	start := time.Now()
	res := loginUserFlow(hosts, o.run, os.Getenv("GOSSH_LU_LOGDIR"))
	printLoginUserDone(hosts, res, hostFile)
	fmt.Fprintf(os.Stderr, "소요 시간: %s\n", fmtDuration(time.Since(start)))
	return 0
}
