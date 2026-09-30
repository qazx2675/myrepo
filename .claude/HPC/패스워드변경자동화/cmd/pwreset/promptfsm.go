package main

import "regexp"

// promptfsm 은 비밀번호 변경 프롬프트 흐름을 처리하는 상태머신입니다.
//
// SSH 인증 단계(keyboard-interactive 콜백)와 로그인 후 pty 셸 단계가 동일한
// 상태머신을 공유합니다. 두 경로 모두 서버가 던지는 프롬프트 문구는 같기
// 때문입니다("Current password", "New password", "Retype new password").

type fsmState int

const (
	stateWaitCurrentOrNew fsmState = iota // 현재/새 비밀번호 중 먼저 오는 것을 기다림
	stateWaitNew                          // 새 비밀번호를 기다림
	stateWaitRetype                       // 새 비밀번호 재입력을 기다림
	stateDoneOrClosed                     // 재입력까지 마침 (성공 판정 단계)
)

var (
	reCurrent  = regexp.MustCompile(`(?i)current.*password`)
	reNew      = regexp.MustCompile(`(?i)new.*password`)
	reRetype   = regexp.MustCompile(`(?i)(retype|re-?enter|again)`)
	rePassword = regexp.MustCompile(`(?i)password`)
	reSuccess  = regexp.MustCompile(`(?i)all authentication tokens updated`)
)

// shellPrompts 는 로그인 후 셸 프롬프트로 볼 정규식 목록입니다.
// csh/tcsh(%), bash/sh($), root(#) 등을 확장할 수 있도록 목록으로 관리합니다.
var shellPrompts = []*regexp.Regexp{
	regexp.MustCompile(`[#$%>]\s*$`),
}

// promptFSM 은 한 번의 접속 시도(한 후보 비밀번호) 동안의 프롬프트 상태입니다.
type promptFSM struct {
	state       fsmState
	currentPass string // 현재 시도 중인(=재입력할) 비밀번호
	newPass     string // 적용할 새 비밀번호
	success     bool   // 성공 문구/셸 프롬프트를 감지했는지
}

func newPromptFSM(currentPass, newPass string) *promptFSM {
	return &promptFSM{state: stateWaitCurrentOrNew, currentPass: currentPass, newPass: newPass}
}

// feed 는 서버 출력 조각(또는 keyboard-interactive 질문 한 개)을 받아,
// stdin(또는 응답 배열)에 써야 할 응답 목록을 순서대로 돌려줍니다.
//
// 한 조각에 여러 프롬프트가 이어져 오는 경우까지 대비해 상태를 연속 전이합니다.
func (f *promptFSM) feed(text string) []string {
	var out []string
	for {
		switch f.state {
		case stateWaitCurrentOrNew:
			if reCurrent.MatchString(text) {
				out = append(out, f.currentPass)
				f.state = stateWaitNew
				continue
			}
			if reNew.MatchString(text) {
				out = append(out, f.newPass)
				f.state = stateWaitRetype
				continue
			}
		case stateWaitNew:
			if reNew.MatchString(text) {
				out = append(out, f.newPass)
				f.state = stateWaitRetype
				continue
			}
		case stateWaitRetype:
			if reRetype.MatchString(text) {
				out = append(out, f.newPass)
				f.state = stateDoneOrClosed
				continue
			}
		case stateDoneOrClosed:
			if reSuccess.MatchString(text) || matchShellPrompt(text) {
				f.success = true
			}
		}
		break
	}
	return out
}

// answer 는 keyboard-interactive 질문 한 개에 대한 응답을 돌려줍니다.
// 상태머신이 처리하지 못하는 로그인용 "Password:" 프롬프트는 현재 후보
// 비밀번호로 응답합니다.
func (f *promptFSM) answer(question string) string {
	if r := f.feed(question); len(r) > 0 {
		return r[len(r)-1]
	}
	if rePassword.MatchString(question) {
		return f.currentPass
	}
	return ""
}

// completed 는 새 비밀번호 재입력까지 마쳤는지(=변경이 적용되었는지) 여부입니다.
func (f *promptFSM) completed() bool {
	return f.state == stateDoneOrClosed
}

func matchShellPrompt(text string) bool {
	for _, re := range shellPrompts {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}
