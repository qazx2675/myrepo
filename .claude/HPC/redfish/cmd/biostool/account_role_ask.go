package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// account_role_ask.go 는 bios.conf 에 user2 는 있는데 그 호스트의 벤더에 쓸 권한(user2_role_<vendor>, user2_role)이
// 없을 때 계정 생성 직전에 권한을 묻는 부분입니다.
//
//   - 설정이 있으면 이 파일은 아무것도 하지 않습니다 (roleFor 가 설정 값을 그대로 씀. 해석 순서 불변).
//   - 벤더(fallbackVendorKey)마다 한 번만 묻고 같은 실행의 같은 벤더 호스트에는 답을 다시 씁니다 (acctEnv.asked).
//   - 답은 파일에 쓰지 않습니다. 고른 값은 설정 값과 같은 칸(acctHost.Role)에 들어가 이후 경로(RoleId 후보, Dell 슬롯,
//     HPE Oem 권한, Lenovo·Supermicro, Cisco CIMC·UCSM 역할)를 그대로 지납니다. 둘째 경로는 없습니다.
//   - 직접 입력도 설정과 같은 검증(acctRoleName: 대소문자 무시 Administrator / Operator / ReadOnly)을 합니다.
//     계정 쓰기 관문(allowedRoleIDs)이 그 밖의 권한은 BMC 로 보내지 않으므로, 받지 못하는 값은 묻는 자리에서 다시 묻습니다.
//   - 터미널이 아니거나 -no-prompt 면 묻지 못하므로 오류로 알리고 만들지 않습니다 (설정 안내 포함).

// maxRoleTry 는 한 벤더의 권한 질문을 다시 묻는 최대 횟수입니다.
const maxRoleTry = 5

// noRoleDetail 은 권한이 정해지지 않은 채 쓰기 직전까지 온 계정 칸의 사유입니다 (보내지 않음).
func noRoleDetail(user string) string {
	return "계정 " + sanitizeDetail(user) + " 의 권한이 정해지지 않아 만들지 않음 — bios.conf 에 user2_role_<vendor> 또는 user2_role 을 적으십시오"
}

// roleIsAsked 는 sp 의 권한 role 이 설정이 아니라 운영자 답에서 왔는지 봅니다 (두 번째 계정, 벤더 key).
func (e *acctEnv) roleIsAsked(sp acctSpec, key, role string) bool {
	return sp.Second && role != "" && sp.Role == "" && e.conf != nil && e.conf.User2Roles[key] == "" && e.asked[key] == role
}

// roleAsk 는 권한을 묻는 벤더 하나와 그 호스트의 user2 칸입니다.
type roleAsk struct {
	key   string
	cells []*acctHost
}

// unresolvedUser2 는 만들 user2 칸 중 권한이 아직 없는 것을 벤더 키별로 모읍니다 (처음 나온 순서).
func unresolvedUser2(groups [][]*acctHost) []*roleAsk {
	var out []*roleAsk
	idx := map[string]*roleAsk{}
	for _, h := range flattenAcct(groups) {
		if h == nil || h.spec == nil || !h.spec.Second || h.Status != acctNeedCreate || h.acctRole() != "" {
			continue
		}
		a := idx[h.vkey]
		if a == nil {
			a = &roleAsk{key: h.vkey}
			idx[h.vkey] = a
			out = append(out, a)
		}
		a.cells = append(a.cells, h)
	}
	return out
}

// roleVendorText 는 질문·오류에 보일 벤더 이름입니다 (모르는 벤더는 "벤더 미확인").
func roleVendorText(key string) string {
	if key == "" {
		return "벤더 미확인"
	}
	return vendorLabel(key)
}

// roleConfHint 는 이 질문이 나오지 않게 하는 bios.conf 줄입니다.
func roleConfHint(key string) string {
	if key == "" {
		return "user2_role"
	}
	return "user2_role_" + key + " 또는 user2_role"
}

// parseRoleAnswer 는 직접 입력한 권한 이름을 표준 이름으로 바꿉니다: 설정과 같은 acctRoleName (대소문자 무시)
// 또는 BMC RoleId 별칭(acctRoleOf: admin / user / read-only 등). 모르는 값이면 "".
func parseRoleAnswer(text string) string {
	text = strings.TrimSpace(text)
	if r := acctRoleName(text); r != "" {
		return r
	}
	for _, r := range []string{roleAdministrator, roleOperator, roleReadOnly} {
		for _, id := range acctRoleIDs[r] {
			if strings.EqualFold(text, id) {
				return r
			}
		}
	}
	return ""
}

// canAskRole 은 권한을 물어볼 수 있는지 봅니다 (confirmCreate 와 같은 기준: -no-prompt 아님, 터미널이거나 -stdin-ok).
func canAskRole(in *os.File, af *accountFlags) bool {
	if af == nil {
		return stdinIsTerminal(in)
	}
	return !af.noPrompt && (af.stdinOK || stdinIsTerminal(in))
}

// askUser2Roles 는 groups 에서 만들 user2 칸 가운데 권한이 설정에 없는 벤더를 찾아 벤더마다 한 번 묻고,
// 답을 그 벤더의 모든 칸(Role)과 e.asked 에 넣습니다. 이미 답한 벤더는 다시 묻지 않습니다.
// 권한이 정해지지 않은 칸이 없으면 아무것도 하지 않습니다. 묻지 못하거나 답을 받지 못하면 오류이며 이때 Role 은 비어 있습니다.
func (e *acctEnv) askUser2Roles(in *os.File, w io.Writer, groups [][]*acctHost, af *accountFlags) error {
	asks := unresolvedUser2(groups)
	if len(asks) == 0 {
		return nil
	}
	user := ""
	for _, s := range e.accounts() {
		if s.Second {
			user = s.User
		}
	}
	// 이번 실행에서 이미 답한 벤더는 다시 묻지 않고 그 답을 쓴다.
	var todo []*roleAsk
	for _, a := range asks {
		if r := e.asked[a.key]; r != "" {
			setAskedRole(a.cells, r)
			continue
		}
		todo = append(todo, a)
	}
	if len(todo) == 0 {
		return nil
	}
	if !canAskRole(in, af) {
		var parts []string
		for _, a := range todo {
			parts = append(parts, fmt.Sprintf("%s %d대", roleVendorText(a.key), len(a.cells)))
		}
		return fmt.Errorf("두 번째 계정 %s 의 권한이 설정에 없고 물을 수도 없습니다 (%s) — bios.conf 에 user2_role=Operator 또는 user2_role_<벤더>=Operator 를 적으십시오 (Administrator / Operator / ReadOnly). 터미널에서 실행하면 묻습니다",
			sanitizeDetail(user), strings.Join(parts, ", "))
	}
	if stdinIsTerminal(in) {
		ttyFlushInput(int(in.Fd())) // 점검 중 눌러 둔 Enter 가 답으로 읽히지 않게 비운다
	}
	if e.asked == nil {
		e.asked = map[string]string{}
	}
	for _, a := range todo {
		r, err := askRoleOnce(in, w, sanitizeDetail(user), a)
		if err != nil {
			return err
		}
		e.asked[a.key] = r
		setAskedRole(a.cells, r)
		fmt.Fprintf(w, "→ %s %d대의 %s 권한: %s (이번 실행에만 적용, 설정 파일은 바꾸지 않음)\n", roleVendorText(a.key), len(a.cells), sanitizeDetail(user), r)
	}
	return nil
}

// setAskedRole 은 운영자가 고른 권한을 칸에 넣습니다 (설정 값과 같은 Role 칸).
func setAskedRole(cells []*acctHost, role string) {
	for _, h := range cells {
		h.Role, h.roleAsked = role, true
	}
}

// askRoleOnce 는 한 벤더의 권한을 묻습니다: 1 Administrator, 2 Operator, 3 직접 입력.
func askRoleOnce(in io.Reader, w io.Writer, user string, a *roleAsk) (string, error) {
	fmt.Fprintf(w, "\n두 번째 계정 %s 의 권한이 설정에 없습니다. 벤더: %s, 호스트 %d대\n", user, roleVendorText(a.key), len(a.cells))
	fmt.Fprintln(w, "  1 Administrator")
	fmt.Fprintln(w, "  2 Operator")
	fmt.Fprintln(w, "  3 직접 입력 (ReadOnly 등)")
	fmt.Fprintf(w, "(bios.conf 에 %s 을 적으면 이 질문이 나오지 않습니다. 답은 이번 실행에만 쓰고 파일에는 쓰지 않습니다.)\n", roleConfHint(a.key))
	for try := 0; try < maxRoleTry; try++ {
		text, err := readRoleLine(in, w, "선택 [1-3]: ")
		if err != nil {
			return "", err
		}
		switch text {
		case "1":
			return roleAdministrator, nil
		case "2":
			return roleOperator, nil
		case "3":
			if r, err := askRoleText(in, w); err != nil || r != "" {
				return r, err
			}
			continue
		}
		if r := parseRoleAnswer(text); r != "" && text != "" { // 번호 대신 권한 이름을 바로 써도 된다
			return r, nil
		}
		fmt.Fprintln(w, "1, 2, 3 중에서 고르십시오.")
	}
	return "", errors.New("권한 선택이 올바르지 않아 중단합니다")
}

// askRoleText 는 3 직접 입력입니다. 빈 값은 다시 묻고, 설정과 같은 검증을 통과하지 못하면 안내 후 "" (번호 선택으로 돌아감).
func askRoleText(in io.Reader, w io.Writer) (string, error) {
	for try := 0; try < maxRoleTry; try++ {
		text, err := readRoleLine(in, w, "권한 이름 (Administrator / Operator / ReadOnly): ")
		if err != nil {
			return "", err
		}
		if text == "" {
			fmt.Fprintln(w, "비어 있습니다. 권한 이름을 입력하십시오.")
			continue
		}
		if r := parseRoleAnswer(text); r != "" {
			if r != text {
				fmt.Fprintf(w, "%s 는 %s 로 처리합니다.\n", sanitizeDetail(text), r)
			}
			return r, nil
		}
		fmt.Fprintf(w, "알 수 없는 권한입니다: %s — Administrator, Operator, ReadOnly 중 하나여야 합니다 (BMC 로 보낼 수 있는 권한만 허용).\n", sanitizeDetail(text))
		return "", nil
	}
	return "", errors.New("권한 이름 입력이 올바르지 않아 중단합니다")
}

// readRoleLine 은 한 줄을 읽어 앞뒤 공백을 뺍니다. 입력이 끝났고(EOF) 읽은 내용이 없으면 오류입니다.
func readRoleLine(in io.Reader, w io.Writer, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	line := readRawLine(in)
	text := strings.TrimSpace(line)
	if !strings.HasSuffix(line, "\n") {
		fmt.Fprintln(w)
		if text == "" {
			return "", errors.New("입력이 끝나 두 번째 계정의 권한을 정하지 못했습니다")
		}
	}
	return text, nil
}
