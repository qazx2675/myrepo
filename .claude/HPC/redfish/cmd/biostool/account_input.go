package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// account_input.go 는 새 비밀번호를 터미널에서 직접 입력받는 부분입니다 (biostool account change).
//
//   - 화면에는 글자마다 * 만 표시합니다 (입력값은 에코하지 않음).
//   - 복사·붙여넣기를 거부합니다. 터미널은 키 입력과 붙여넣기를 완전히 구분해 주지 않으므로
//     (1) 한 번의 read 로 여러 글자가 한꺼번에 도착, (2) 글자 간 도착 간격이 너무 짧음,
//     (3) bracketed paste 시작 시퀀스(ESC [ 200 ~) 를 받으면 붙여넣기로 보고 입력을 버립니다.
//     감지하지 못하는 붙여넣기(터미널 설정에 따라 가능)는 막지 못하므로 안내 문구로 직접 입력을 요청합니다.
//   - 표준입력이 터미널이 아니면(파이프·파일) 입력을 받지 않고 오류를 돌려줍니다.
//   - 비밀번호는 인자·환경변수·파일로 받지 않습니다 (ps·히스토리 노출 방지).

const (
	// pasteMinGap 은 사람이 키를 누르는 최소 간격으로 보는 값입니다. 이보다 빠르게 연달아 도착하면 붙여넣기로 봅니다.
	pasteMinGap  = 8 * time.Millisecond
	maxSecretTry = 3
)

var (
	errSecretInterrupt = errors.New("입력이 취소되었습니다")
	errSecretEOF       = errors.New("입력이 끝났습니다 (EOF)")
	errNotTerminal     = errors.New("표준입력이 터미널이 아닙니다 (새 비밀번호는 터미널에서 직접 입력해야 합니다)")
)

// chunkSource 는 입력 덩어리(read 1회 분량)와 도착 시각을 돌려줍니다.
type chunkSource func() (chunk []byte, at time.Time, err error)

// scanSecret 은 한 번의 비밀번호 입력을 읽습니다. pasted 가 true 면 붙여넣기로 보고 입력을 버린 것입니다.
// 완성된 입력(Enter)이나 pasted/err 로 끝나며, 화면에는 * 만 씁니다.
func scanSecret(next chunkSource, out io.Writer, minGap time.Duration) (secret string, pasted bool, err error) {
	var buf []rune
	var prev time.Time
	finish := func() string {
		s := string(buf)
		for i := range buf {
			buf[i] = 0
		}
		return s
	}
	for {
		chunk, at, rerr := next()
		if rerr != nil {
			if rerr == io.EOF {
				rerr = errSecretEOF
			}
			finish()
			return "", false, rerr
		}
		if len(chunk) == 0 {
			continue
		}
		// bracketed paste: ESC [ 2 0 0 ~ 로 시작하면 붙여넣기
		if strings.HasPrefix(string(chunk), "\x1b[200~") || strings.Contains(string(chunk), "\x1b[200~") {
			finish()
			return "", true, nil
		}
		// 화살표 등 다른 이스케이프 시퀀스는 무시한다 (글자로 넣지 않음)
		if chunk[0] == 0x1b {
			continue
		}
		// 한 번에 여러 글자가 도착: 한 글자(여러 바이트 UTF-8 포함)가 아니면 붙여넣기
		if len(chunk) > 1 {
			r, n := utf8.DecodeRune(chunk)
			if !(r != utf8.RuneError && n == len(chunk)) {
				finish()
				return "", true, nil
			}
		}
		// 직전 글자와 도착 간격이 너무 짧음
		if len(buf) > 0 && !prev.IsZero() && at.Sub(prev) < minGap {
			finish()
			return "", true, nil
		}
		prev = at
		for _, r := range string(chunk) {
			switch {
			case r == '\r' || r == '\n':
				fmt.Fprintln(out)
				return finish(), false, nil
			case r == 0x7f || r == 0x08: // Backspace
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
					fmt.Fprint(out, "\b \b")
				}
			case r == 0x03: // Ctrl-C (raw 모드에서도 ISIG 가 켜져 있어 보통 신호로 처리됨)
				finish()
				fmt.Fprintln(out)
				return "", false, errSecretInterrupt
			case r == 0x04: // Ctrl-D
				if len(buf) == 0 {
					fmt.Fprintln(out)
					return "", false, errSecretEOF
				}
			case r < 0x20:
				// 그 밖의 제어문자는 무시
			default:
				buf = append(buf, r)
				fmt.Fprint(out, "*")
			}
		}
	}
}

// pasteNotice 는 붙여넣기 거부 안내문입니다.
const pasteNotice = "※ 복사·붙여넣기는 사용할 수 없습니다. 붙여넣지 말고 키보드로 직접 입력하십시오. (붙여넣은 입력은 거부됩니다)"

// readNewPassword 는 새 비밀번호를 2번 입력받아 같을 때 돌려줍니다 (최대 maxSecretTry 번 다시 시도).
// 터미널이 아니거나 raw 입력을 지원하지 않으면 오류입니다. 비밀번호는 어디에도 기록하지 않습니다.
func readNewPassword(in *os.File, out io.Writer) (string, error) {
	ask, done, err := openSecretInput(in, out)
	if err != nil {
		return "", err
	}
	defer done()
	fmt.Fprintln(out, pasteNotice)
	for try := 0; try < maxSecretTry; try++ {
		a, err := ask("새 비밀번호: ")
		if err != nil {
			return "", err
		}
		if a == "" {
			fmt.Fprintln(out, "비어 있는 비밀번호는 사용할 수 없습니다.")
			continue
		}
		b, err := ask("새 비밀번호 확인: ")
		if err != nil {
			return "", err
		}
		if a == b {
			return a, nil
		}
		fmt.Fprintln(out, "두 입력이 다릅니다. 다시 입력하십시오.")
	}
	return "", errors.New("새 비밀번호 입력이 일치하지 않아 중단합니다")
}

// readAdminPassword 는 관리자 계정 비밀번호를 1번 입력받습니다 (점검 끝 계정 생성의 직접 입력, readNewPassword 와 같은
// * 표시·붙여넣기 거부). 비밀번호는 어디에도 기록하지 않습니다. 테스트가 바꿔 쓸 수 있도록 변수입니다.
var readAdminPassword = func(in *os.File, out io.Writer, label string) (string, error) {
	ask, done, err := openSecretInput(in, out)
	if err != nil {
		return "", err
	}
	defer done()
	fmt.Fprintln(out, pasteNotice)
	return ask(label)
}

// openSecretInput 은 터미널을 raw 모드로 바꾸고 비밀 입력 1회를 읽는 ask 와 터미널을 되돌리는 done 을 돌려줍니다
// (붙여넣기는 버리고 maxSecretTry 번까지 다시 묻음). 터미널이 아니거나 raw 입력을 지원하지 않으면 오류입니다.
func openSecretInput(in *os.File, out io.Writer) (ask func(label string) (string, error), done func(), err error) {
	fd := int(in.Fd())
	if !ttyIsTerminal(fd) {
		return nil, nil, errNotTerminal
	}
	restore, err := ttyMakeRaw(fd)
	if err != nil {
		return nil, nil, fmt.Errorf("터미널을 raw 모드로 바꿀 수 없습니다: %v", err)
	}
	// 어떤 경로로 끝나도 터미널을 되돌린다 (신호 포함, 호출자가 done 을 defer).
	quit := make(chan struct{})
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigc:
			fmt.Fprint(out, "\x1b[?2004l")
			restore()
			fmt.Fprintln(out)
			os.Exit(130)
		case <-quit:
		}
	}()
	done = func() {
		close(quit)
		signal.Stop(sigc)
		fmt.Fprint(out, "\x1b[?2004l") // bracketed paste 끄기
		restore()
	}
	fmt.Fprint(out, "\x1b[?2004h") // bracketed paste 켜기 (지원 터미널은 붙여넣기를 ESC[200~ 로 감싸 보냄)

	rbuf := make([]byte, 256)
	next := func() ([]byte, time.Time, error) {
		n, err := in.Read(rbuf)
		at := time.Now()
		if n > 0 {
			return append([]byte(nil), rbuf[:n]...), at, nil
		}
		if err == nil {
			err = io.EOF
		}
		return nil, at, err
	}
	ask = func(label string) (string, error) {
		for pasteTry := 0; ; pasteTry++ {
			fmt.Fprint(out, label)
			s, pasted, err := scanSecret(next, out, pasteMinGap)
			if err != nil {
				return "", err
			}
			if !pasted {
				return s, nil
			}
			// 남은 붙여넣기 잔여 입력을 비운다
			drainPending(in)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "거부됨: 붙여넣기로 보이는 입력입니다. 키보드로 직접 입력하십시오.")
			if pasteTry+1 >= maxSecretTry {
				return "", errors.New("붙여넣기로 보이는 입력이 반복되어 중단합니다. 직접 입력하십시오")
			}
		}
	}
	return ask, done, nil
}

// cshQuote 는 csh/tcsh 에서 그대로 복사해 실행할 수 있도록 문자열을 작은따옴표로 감쌉니다.
// csh 는 작은따옴표 안에서도 !(히스토리 확장)을 해석하므로 !는 따옴표 밖에서 \! 로 씁니다.
// 작은따옴표 자체는 '\” 로 씁니다.
func cshQuote(s string) string {
	if s == "" {
		return "''"
	}
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString(`'\''`)
		case '!':
			b.WriteString(`'\!'`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
