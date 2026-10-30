package mockbmc

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// accounts.go 는 mock BMC 의 계정 서비스입니다 (biostool account create/change 시험용).
// Options.Accounts 가 nil 이 아니면 켜지며, /redfish/v1/AccountService (+ Accounts, Roles) 를 동적으로 응답합니다.
// 로그인은 Options.User/Pass(관리자) 외에 이 계정 표(Enabled, UserName, Password 일치)도 받습니다.

// Account 는 mock 계정 슬롯 1개입니다. UserName 이 비어 있으면 빈 슬롯입니다.
type Account struct {
	UserName string
	Password string
	RoleID   string
	Enabled  bool

	seeded string // 처음 넣은 비밀번호 (DenyNewLogin 판정용)
}

// AccountOptions 는 계정 서비스 동작 옵션입니다.
type AccountOptions struct {
	// Slots 는 처음 슬롯 목록입니다 (Id 는 1 부터). 비어 있어도 계정 서비스는 켜집니다.
	Slots []Account
	// SlotStyle 이면 컬렉션 POST·멤버 DELETE 는 405 이고 빈 슬롯 PATCH 로만 만들 수 있습니다 (Dell iDRAC8/9, Lenovo Purley).
	SlotStyle bool
	// DenyNewLogin 이면 API 로 만들었거나 비밀번호를 바꾼 계정(처음 비밀번호와 다른 것)은 로그인을 거부합니다
	// (생성·변경 후 검증 실패 → 롤백 시험). 처음 비밀번호로 되돌리면 다시 로그인됩니다.
	DenyNewLogin bool
	// IgnoreRoleChange 면 RoleId 만 담은 PATCH 를 200 으로 받지만 실제로는 바꾸지 않습니다
	// (권한 변경 뒤 다시 읽어 확인 실패 → 이전 권한으로 되돌리기 시험).
	IgnoreRoleChange bool
}

const (
	acctSvcPath   = root + "/accountservice"
	acctCollPath  = acctSvcPath + "/accounts"
	acctRolesPath = acctSvcPath + "/roles"
)

func (s *Host) initAccounts() {
	if s.opts.Accounts == nil {
		return
	}
	for _, a := range s.opts.Accounts.Slots {
		a.seeded = a.Password
		s.accts = append(s.accts, a)
	}
}

// acctLoginLocked 는 계정 표로 로그인 가능한지 봅니다 (s.mu 보유).
func (s *Host) acctLoginLocked(user, pass string) bool {
	if s.opts.Accounts == nil || user == "" {
		return false
	}
	for _, a := range s.accts {
		if a.UserName == "" || !a.Enabled || a.UserName != user {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(a.Password), []byte(pass)) != 1 {
			continue
		}
		if s.opts.Accounts.DenyNewLogin && a.Password != a.seeded {
			return false
		}
		return true
	}
	return false
}

// Accounts 는 현재 계정 표의 사본입니다 (테스트 확인용, 비밀번호 포함).
func (s *Host) Accounts() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Account(nil), s.accts...)
}

// acctMemberPath 는 응답에 쓰는 멤버 경로입니다 (실제 BMC 처럼 대소문자를 살린 표기).
func acctMemberPath(i int) string {
	return "/redfish/v1/AccountService/Accounts/" + strconv.Itoa(i+1)
}

func (s *Host) acctMemberJSON(i int) []byte {
	a := s.accts[i]
	return mustJSON(map[string]interface{}{
		"@odata.id": acctMemberPath(i), "Id": strconv.Itoa(i + 1), "Name": "User Account",
		"UserName": a.UserName, "Enabled": a.Enabled, "RoleId": a.RoleID,
		"Oem": map[string]interface{}{"MockData": true},
	})
}

// acctHandle 은 계정 서비스 경로 요청을 처리했으면 true 를 돌려줍니다 (인증은 handle 에서 이미 통과).
func (s *Host) acctHandle(w *statusWriter, r *http.Request, np string, body []byte) bool {
	if s.opts.Accounts == nil {
		return false
	}
	if np != acctSvcPath && np != acctRolesPath && np != acctCollPath && !strings.HasPrefix(np, acctCollPath+"/") {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		s.acctGet(w, np)
	case http.MethodPost:
		if np != acctCollPath {
			writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "POST not supported")
			return true
		}
		s.acctPost(w, body)
	case http.MethodPatch:
		s.acctPatch(w, np, body)
	case http.MethodDelete:
		s.acctDelete(w, np)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "method not supported")
	}
	return true
}

func (s *Host) acctIndex(np string) (int, bool) {
	if !strings.HasPrefix(np, acctCollPath+"/") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(np, acctCollPath+"/"))
	if err != nil || n < 1 || n > len(s.accts) {
		return 0, false
	}
	return n - 1, true
}

func (s *Host) acctGet(w *statusWriter, np string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case np == acctSvcPath:
		writeJSON(w, http.StatusOK, mustJSON(map[string]interface{}{
			"@odata.id": "/redfish/v1/AccountService", "Id": "AccountService",
			"Accounts": map[string]string{"@odata.id": "/redfish/v1/AccountService/Accounts"},
			"Roles":    map[string]string{"@odata.id": "/redfish/v1/AccountService/Roles"},
		}))
	case np == acctRolesPath:
		var ms []map[string]string
		for _, r := range []string{"Administrator", "Operator", "ReadOnly"} {
			ms = append(ms, map[string]string{"@odata.id": "/redfish/v1/AccountService/Roles/" + r})
		}
		writeJSON(w, http.StatusOK, mustJSON(map[string]interface{}{
			"@odata.id": "/redfish/v1/AccountService/Roles", "Members": ms, "Members@odata.count": len(ms),
		}))
	case np == acctCollPath:
		var ms []map[string]string
		for i, a := range s.accts {
			if a.UserName == "" && !s.opts.Accounts.SlotStyle {
				continue
			}
			ms = append(ms, map[string]string{"@odata.id": acctMemberPath(i)})
		}
		writeJSON(w, http.StatusOK, mustJSON(map[string]interface{}{
			"@odata.id": "/redfish/v1/AccountService/Accounts", "Members": ms, "Members@odata.count": len(ms),
		}))
	default:
		i, ok := s.acctIndex(np)
		if !ok {
			writeErr(w, http.StatusNotFound, "ResourceMissingAtURI", "no such account")
			return
		}
		writeJSON(w, http.StatusOK, s.acctMemberJSON(i))
	}
}

func (s *Host) acctPost(w *statusWriter, body []byte) {
	if s.opts.Accounts.SlotStyle {
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "POST not supported (slot style)")
		return
	}
	var req struct {
		UserName, Password, RoleId string
		Enabled                    *bool
	}
	if json.Unmarshal(body, &req) != nil || req.UserName == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "PropertyMissing", "UserName and Password are required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.accts {
		if a.UserName == req.UserName {
			writeErr(w, http.StatusConflict, "ResourceAlreadyExists", "account already exists")
			return
		}
	}
	en := true
	if req.Enabled != nil {
		en = *req.Enabled
	}
	// 빈 슬롯이 있으면 재사용하고, 없으면 끝에 추가한다.
	idx := -1
	for i, a := range s.accts {
		if a.UserName == "" {
			idx = i
			break
		}
	}
	acc := Account{UserName: req.UserName, Password: req.Password, RoleID: req.RoleId, Enabled: en}
	if idx < 0 {
		s.accts = append(s.accts, acc)
		idx = len(s.accts) - 1
	} else {
		s.accts[idx] = acc
	}
	w.Header().Set("Location", acctMemberPath(idx))
	writeJSON(w, http.StatusCreated, s.acctMemberJSON(idx))
}

func (s *Host) acctPatch(w *statusWriter, np string, body []byte) {
	var req map[string]json.RawMessage
	if json.Unmarshal(body, &req) != nil || req == nil {
		writeErr(w, http.StatusBadRequest, "MalformedJSON", "malformed JSON")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.acctIndex(np)
	if !ok {
		writeErr(w, http.StatusNotFound, "ResourceMissingAtURI", "no such account")
		return
	}
	a := s.accts[i]
	if _, ok := req["RoleId"]; ok && len(req) == 1 && s.opts.Accounts.IgnoreRoleChange {
		writeJSON(w, http.StatusOK, s.acctMemberJSON(i))
		return
	}
	for k, v := range req {
		switch k {
		case "UserName":
			var u string
			if json.Unmarshal(v, &u) != nil {
				writeErr(w, http.StatusBadRequest, "PropertyValueTypeError", "UserName")
				return
			}
			if u != "" {
				for j, o := range s.accts {
					if j != i && o.UserName == u {
						writeErr(w, http.StatusConflict, "ResourceAlreadyExists", "account already exists")
						return
					}
				}
			}
			a.UserName = u
		case "Password":
			var p string
			if json.Unmarshal(v, &p) != nil || p == "" {
				writeErr(w, http.StatusBadRequest, "PropertyValueTypeError", "Password")
				return
			}
			a.Password = p
		case "RoleId":
			var r string
			if json.Unmarshal(v, &r) != nil {
				writeErr(w, http.StatusBadRequest, "PropertyValueTypeError", "RoleId")
				return
			}
			a.RoleID = r
		case "Enabled":
			var e bool
			if json.Unmarshal(v, &e) != nil {
				writeErr(w, http.StatusBadRequest, "PropertyValueTypeError", "Enabled")
				return
			}
			a.Enabled = e
		default:
			writeErr(w, http.StatusBadRequest, "PropertyNotWritable", k)
			return
		}
	}
	if a.UserName == "" { // 비운 슬롯은 초기화
		a = Account{}
	}
	s.accts[i] = a
	writeJSON(w, http.StatusOK, s.acctMemberJSON(i))
}

func (s *Host) acctDelete(w *statusWriter, np string) {
	if s.opts.Accounts.SlotStyle {
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "DELETE not supported (slot style)")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.acctIndex(np)
	if !ok || s.accts[i].UserName == "" {
		writeErr(w, http.StatusNotFound, "ResourceMissingAtURI", "no such account")
		return
	}
	s.accts[i] = Account{}
	w.WriteHeader(http.StatusNoContent)
}
