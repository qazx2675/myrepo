// sessionuser.go - v0.8.0 세션 user: -user 없이 실행했을 때, user 가 처음 필요한 기능(신규 설치 양식 w→2, gossh 작업 s)이
// 한 번만 묻고 TUI 프로세스가 끝날 때까지 기억한다. x/w(AWX 실행)/m 의 기존 user 질문은 그대로다.
package main

import (
	"fmt"
	"io"
	"strings"
)

// sessionUser: TUI 에서 한 번 고른 세션 user (비면 아직 안 고름). forceUser 가 있으면 쓰이지 않는다.
// TUI 가 시작될 때 비워지고, 정해지면 헤더 `[user: …]` 에 forceUser 와 같은 자리로 표시된다 (job 필터링은 하지 않음).
var sessionUser string

// fixedUser: -user 로 지정한 user (디렉터리 이름으로 쓸 수 있는 값일 때만). 없으면 "".
func fixedUser() string {
	if forceUser != "" && validChangeUser(forceUser) {
		return forceUser
	}
	return ""
}

// headerUser: 헤더 `[user: …]` 에 보일 user — forceUser 우선, 없으면 세션 user
func headerUser() string {
	if forceUser != "" {
		return forceUser
	}
	return sessionUser
}

// needSessionUser: 기능이 user 를 필요로 할 때 부르는 공통 진입점. user 가 정해지면 next(user) 로 이어 준다.
//   - -user 로 실행했으면(forceUser) 묻지 않고 바로 next(forceUser)
//   - 세션 user 를 이미 골랐으면 바로 next(sessionUser)
//   - 아니면 user 선택 화면(x 와 같은 info_mn.sh 목록 + 직접 입력)을 열고, 고르면 sessionUser 에 기억한 뒤 next(user).
//     Esc/q 로 취소하면 next 는 호출되지 않는다.
//
// 선택 화면은 st.oo 를 쓰므로 호출하는 쪽은 st.oo 가 비어 있을 때(메뉴 키 처리 시점) 불러야 한다.
// next 가 새 화면(st.oo 등)을 열어도 된다 — 선택 화면은 next 호출 전에 닫힌다.
func (st *tuiState) needSessionUser(next func(user string)) {
	if u := fixedUser(); u != "" {
		next(u)
		return
	}
	if sessionUser != "" {
		next(sessionUser)
		return
	}
	o := st.newUserStep(false, nil) // forceUser 가 없으니 항상 user 선택 단계 (-user 값이 이름으로 부적합한 경우도 포함)
	o.onUser = next
	st.oo = o
}

// askUserCLI: CLI 진입(auto_setup s, auto_setup newhost)용 비대화형 user 질문.
// forceUser(-user) 가 있으면 묻지 않고, 세션 user 가 이미 있으면 그대로 쓰고, 아니면 user 메뉴(없으면 직접 입력)를 보여
// 한 줄을 읽는다 (잘못된 입력은 3번까지 다시 묻는다). in 은 한 글자씩 읽어 줄 끝에서 멈추므로 뒤따르는 입력은 소비하지 않는다.
func askUserCLI(in io.Reader, out io.Writer) (string, error) {
	if u := fixedUser(); u != "" {
		return u, nil
	}
	if sessionUser != "" {
		return sessionUser, nil
	}
	menu, route := userMenu()
	if menu != nil {
		fmt.Fprintln(out, "이 세션에서 사용할 user 를 선택하세요")
		for _, m := range menu {
			fmt.Fprintln(out, " "+m)
		}
	} else {
		fmt.Fprintln(out, userMenuMissingMsg)
	}
	prompt := "user 이름 입력 > "
	if menu != nil {
		prompt = "번호 입력 > "
	}
	for try := 0; try < 3; try++ {
		fmt.Fprint(out, prompt)
		line, err := readLineRaw(in)
		if err != nil && strings.TrimSpace(line) == "" {
			fmt.Fprintln(out)
			return "", fmt.Errorf("user 입력을 받지 못했습니다 (-user <이름> 으로 지정할 수도 있습니다)")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		user, errMsg := pickUser(menu, route, line)
		if errMsg == "" && !validChangeUser(user) {
			errMsg = "[X] user 이름이 올바르지 않습니다 (. 와 .. 는 쓸 수 없음) — 다시 입력하세요"
		}
		if errMsg != "" {
			fmt.Fprintln(out, errMsg)
			continue
		}
		sessionUser = user
		return user, nil
	}
	return "", fmt.Errorf("user 를 정하지 못했습니다 (-user <이름> 으로 지정할 수도 있습니다)")
}

// readLineRaw: in 에서 줄바꿈까지 한 글자(바이트)씩 읽는다 (\r 은 무시). 버퍼링하지 않아 줄 뒤의 입력을 소비하지 않는다.
// 줄바꿈 없이 끝나면 읽은 만큼과 오류(io.EOF 포함)를 돌려준다.
func readLineRaw(in io.Reader) (string, error) {
	var b []byte
	one := make([]byte, 1)
	for {
		n, err := in.Read(one)
		if n == 1 {
			switch one[0] {
			case '\n':
				return string(b), nil
			case '\r':
			default:
				b = append(b, one[0])
			}
		}
		if err != nil {
			return string(b), err
		}
	}
}
