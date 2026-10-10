package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// account_gate.go 는 계정 쓰기 요청 본문 검사입니다 (redfish.go checkBody 가 호출).
// 허용 형태를 고정해 두어, 계정 모드에서도 권한 상승·다른 속성 변경·Actions 호출이 불가능하게 합니다.

const (
	acctNameMax = 64  // 계정 이름 최대 글자 수 (벤더 최대 39자, 여유)
	acctPassMax = 256 // 비밀번호 최대 글자 수
)

// adminRoleIDs 는 계정 생성에 허용하는 최고 권한 역할 이름입니다 (Redfish 표준 / Supermicro X10 / Cisco CIMC).
var adminRoleIDs = map[string]bool{"Administrator": true, "Admin": true, "admin": true}

// validAcctText 는 계정 이름·비밀번호 문자열 검사입니다: UTF-8, 제어문자 없음, 길이 상한.
// 비밀번호는 특수문자(@ ! # $ % ^ & * 등)를 그대로 허용합니다 (JSON 이스케이프는 encoding/json 이 처리).
func validAcctText(s string, max int, allowEmpty bool) error {
	if s == "" && !allowEmpty {
		return errors.New("빈 문자열")
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return errors.New("길이 또는 인코딩 오류")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return errors.New("제어문자 포함")
		}
	}
	return nil
}

// hpOemPrivs 는 HPE iLO4(Oem.Hp)/iLO5+(Oem.Hpe) 생성 본문에 허용하는 권한 이름입니다.
var hpOemPrivs = map[string]bool{
	"LoginPriv": true, "RemoteConsolePriv": true, "VirtualMediaPriv": true, "VirtualPowerAndResetPriv": true,
	"iLOConfigPriv": true, "UserConfigPriv": true, "HostBIOSConfigPriv": true, "HostNICConfigPriv": true,
	"HostStorageConfigPriv": true, "SystemRecoveryConfigPriv": true,
}

func checkAcctOem(raw json.RawMessage) (map[string]interface{}, error) {
	var oem map[string]json.RawMessage
	if json.Unmarshal(raw, &oem) != nil || len(oem) != 1 {
		return nil, errors.New("Oem 은 Hp 또는 Hpe 하나만 허용")
	}
	var key string
	var inner json.RawMessage
	for k, v := range oem {
		if k != "Hp" && k != "Hpe" {
			return nil, fmt.Errorf("Oem 키 %q 는 허용되지 않음", k)
		}
		key, inner = k, v
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(inner, &o) != nil || o == nil {
		return nil, errors.New("Oem 본문은 JSON 객체여야 함")
	}
	out := map[string]interface{}{}
	for k, v := range o {
		switch k {
		case "LoginName":
			var s string
			if json.Unmarshal(v, &s) != nil || validAcctText(s, acctNameMax, false) != nil {
				return nil, errors.New("LoginName 이 올바르지 않음")
			}
			out[k] = s
		case "Privileges":
			var p map[string]bool
			if json.Unmarshal(v, &p) != nil || len(p) == 0 {
				return nil, errors.New("Privileges 는 불리언 객체여야 함")
			}
			for pk, pv := range p {
				if !hpOemPrivs[pk] || !pv {
					return nil, fmt.Errorf("권한 %q 는 허용되지 않음 (true 만)", pk)
				}
			}
			out[k] = p
		default:
			return nil, fmt.Errorf("Oem 속성 %q 는 허용되지 않음", k)
		}
	}
	return map[string]interface{}{key: out}, nil
}

// checkAccountCreateBody 는 계정 컬렉션 POST 본문을 검사합니다.
// 허용 키: UserName, Password, RoleId(관리자 역할 이름만), Enabled(true), PasswordChangeRequired(false), Oem(HPE 권한).
// UserName·Password 는 필수이고 RoleId 또는 Oem 으로 권한이 지정되어야 합니다.
func checkAccountCreateBody(body []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil || m == nil {
		return nil, errors.New("계정 생성 본문은 JSON 객체여야 함")
	}
	out := map[string]interface{}{}
	for k, v := range m {
		switch k {
		case "UserName", "Password":
			var s string
			max := acctNameMax
			if k == "Password" {
				max = acctPassMax
			}
			if json.Unmarshal(v, &s) != nil {
				return nil, fmt.Errorf("%s 는 문자열이어야 함", k)
			}
			if err := validAcctText(s, max, false); err != nil {
				return nil, fmt.Errorf("%s: %v", k, err)
			}
			out[k] = s
		case "RoleId":
			var s string
			if json.Unmarshal(v, &s) != nil || !adminRoleIDs[s] {
				return nil, errors.New("RoleId 는 Administrator/Admin/admin 만 허용")
			}
			out[k] = s
		case "Enabled":
			var b bool
			if json.Unmarshal(v, &b) != nil || !b {
				return nil, errors.New("Enabled 는 true 만 허용")
			}
			out[k] = true
		case "PasswordChangeRequired":
			var b bool
			if json.Unmarshal(v, &b) != nil || b {
				return nil, errors.New("PasswordChangeRequired 는 false 만 허용")
			}
			out[k] = false
		case "Oem":
			o, err := checkAcctOem(v)
			if err != nil {
				return nil, err
			}
			out[k] = o
		default:
			return nil, fmt.Errorf("계정 생성 본문에 허용되지 않은 키 %q", k)
		}
	}
	if out["UserName"] == nil || out["Password"] == nil {
		return nil, errors.New("UserName·Password 가 필요함")
	}
	if out["RoleId"] == nil && out["Oem"] == nil {
		return nil, errors.New("RoleId 또는 Oem 권한이 필요함")
	}
	return json.Marshal(out)
}

// checkAccountPatchBody 는 계정 멤버 PATCH 본문을 정확히 아래 4가지 중 하나로만 허용합니다.
//
//	{"Password": P}                                              비밀번호 변경
//	{"UserName": N, "Password": P, "RoleId": 관리자, "Enabled": true}  빈 슬롯에 계정 생성 (Dell iDRAC8/9, Lenovo Purley)
//	{"Enabled": false, "RoleId": "None"}                         슬롯 롤백 1단계 (비활성)
//	{"UserName": ""}                                             슬롯 롤백 2단계 (이름 비움)
func checkAccountPatchBody(body []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil || m == nil {
		return nil, errors.New("계정 PATCH 본문은 JSON 객체여야 함")
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	has := func(ks ...string) bool {
		if len(keys) != len(ks) {
			return false
		}
		for _, k := range ks {
			if _, ok := m[k]; !ok {
				return false
			}
		}
		return true
	}
	str := func(k string, max int, allowEmpty bool) (string, error) {
		var s string
		if json.Unmarshal(m[k], &s) != nil {
			return "", fmt.Errorf("%s 는 문자열이어야 함", k)
		}
		if err := validAcctText(s, max, allowEmpty); err != nil {
			return "", fmt.Errorf("%s: %v", k, err)
		}
		return s, nil
	}
	isBool := func(k string, want bool) bool {
		var b bool
		return json.Unmarshal(m[k], &b) == nil && b == want
	}
	switch {
	case has("Password"):
		p, err := str("Password", acctPassMax, false)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]interface{}{"Password": p})
	case has("UserName", "Password", "RoleId", "Enabled"):
		n, err := str("UserName", acctNameMax, false)
		if err != nil {
			return nil, err
		}
		p, err := str("Password", acctPassMax, false)
		if err != nil {
			return nil, err
		}
		r, err := str("RoleId", 32, false)
		if err != nil || !adminRoleIDs[r] {
			return nil, errors.New("RoleId 는 Administrator/Admin/admin 만 허용")
		}
		if !isBool("Enabled", true) {
			return nil, errors.New("Enabled 는 true 만 허용")
		}
		return json.Marshal(map[string]interface{}{"UserName": n, "Password": p, "RoleId": r, "Enabled": true})
	case has("Enabled", "RoleId"):
		r, err := str("RoleId", 32, false)
		if err != nil || r != "None" || !isBool("Enabled", false) {
			return nil, errors.New(`롤백 비활성은 {"Enabled":false,"RoleId":"None"} 만 허용`)
		}
		return json.Marshal(map[string]interface{}{"Enabled": false, "RoleId": "None"})
	case has("UserName"):
		var s string
		if json.Unmarshal(m["UserName"], &s) != nil || s != "" {
			return nil, errors.New(`이름 비우기는 {"UserName":""} 만 허용`)
		}
		return json.Marshal(map[string]string{"UserName": ""})
	}
	return nil, fmt.Errorf("계정 PATCH 본문 형태가 허용 목록에 없음 (키 %s)", strings.Join(sortedStrings(keys), ","))
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// jsonNoHTMLEscape 는 HTML 이스케이프 없이 JSON 으로 만듭니다 (표시·테스트 비교용; 전송은 json.Marshal).
func jsonNoHTMLEscape(v interface{}) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}
