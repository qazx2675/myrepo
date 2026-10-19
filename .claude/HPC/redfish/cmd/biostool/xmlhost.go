package main

// xmlhost.go — `biostool bios` 의 XML 대상(Target.XML, classifyTargets) 점검입니다. 읽기 전용입니다.
//
// 대상 1대마다 그 호스트의 관리 주소(이름-m 의 IP)로 UCSM/CIMC XML API 에 같은 계정(conf user / pass.enc)으로
// 로그인해 ucsmCheckHost(ucsm.go)로 서버(computeBlade·computeRackUnit)·서비스 프로파일(lsServer)·bios* 클래스를 읽고,
// 프로파일 # [xml] 구역으로 판정합니다 (ucsmJudge). 보내는 요청은 aaaLogin·configResolveClass·aaaLogout 뿐입니다.
//
//   - 서버가 1대면 그 서버가 이 호스트입니다.
//   - 여러 대면 서비스 프로파일 이름이 호스트 이름(-m·포트를 뗀 것)과 같은(대소문자 무시) 서버를 고르고,
//     하나도 없으면 모든 서버를 `<호스트>/<서버 dn>` 이름으로 보고합니다 (비고에 서비스 프로파일 이름).
//   - 로그인 전에 실패하면(연결 실패·타임아웃·XML 이 아닌 응답·HTTP 오류 등, AUTH_FAIL 제외) 그 호스트만
//     Redfish 로 1번 다시 점검하고 비고에 "XML 실패 → Redfish" 를 남깁니다. AUTH_FAIL 은 계정 잠금 방지로 다시 시도하지 않습니다.
//
// XML 로 점검한 호스트(hostCheck.XML)는 점검만 합니다: 전체비교 스냅샷은 StatusXMLReadOnly, 설정 계획에서 빠집니다.

import (
	"strings"

	"biostool/internal/ucsmxml"
)

const (
	// StatusXMLReadOnly 는 XML 대상이라 전체비교·설정에서 뺀 호스트의 스냅샷·비교 상태입니다 (TUI: 제외(읽기 전용)).
	StatusXMLReadOnly = "XML_READONLY"
	xmlProtocol       = "xml"
	xmlMgmtModel      = "Cisco XML API"
	xmlNote           = "XML"
	xmlFallbackNote   = "XML 실패 → Redfish"
	// xmlCompareNote 는 전체비교에서 XML 대상 호스트 줄(N/A)의 비고입니다. 오류가 아니라 제외라 화면에서 빨강 대신 회색.
	xmlCompareNote = "XML 대상 — 비교 제외(읽기 전용)"
)

// lineIsError 는 빨강으로 보일 호스트 오류 줄인지 봅니다 (XML 대상의 전체비교 제외 줄은 오류가 아님).
func lineIsError(l ResultLine) bool { return l.HostError && l.Note != xmlCompareNote }

// checkXML 은 XML 대상 1대를 점검합니다 (로그인 전 실패면 Redfish 로 1번 다시).
func (e *checkEnv) checkXML(t Target) *hostCheck {
	uh := ucsmCheckHost(e.conf, e.password, e.prof.XML, t)
	if t.Err == "" && uh.Status != "" && uh.Status != StatusAuthFail && !uh.loggedIn {
		h := e.checkOnline(t)
		h.ProtoNote = xmlFallbackNote
		h.Notes = append(h.Notes, xmlFallbackNote+" ("+uh.Status+": "+uh.Detail+")")
		return h
	}
	h := newHostCheck(t)
	h.XML, h.xmlLoggedIn, h.ProtoNote = true, uh.loggedIn, xmlNote
	h.MgmtModel, h.MgmtVersion = xmlMgmtModel, "-"
	h.Notes = append(h.Notes, uh.Notes...)
	if uh.Status != "" {
		h.Status, h.Detail = uh.Status, uh.Detail
		return h
	}
	servers := xmlPickServers(uh.Servers, targetBaseName(t))
	label := len(servers) > 1
	h.Info = &SystemInfo{Vendor: ucsmVendor, Manufacturer: ucsmVendor, Model: servers[0].Model, BiosVersion: "-"}
	for _, s := range servers {
		for _, it := range s.Items {
			r := itemResult{Std: it.Std, Attr: "-", Expected: it.Expected, Current: it.Current, Pending: "-", Result: it.Result, Reason: it.Reason}
			if it.Class != "-" {
				r.Attr = it.Class + "." + it.Attr
			}
			if label {
				r.Server, r.Model = t.Hostname+"/"+s.DN, s.Model
				r.Note = xmlNote + " SP=" + orDefault(s.SP, "-")
			}
			h.Items = append(h.Items, r)
		}
	}
	return h
}

// xmlProbe 는 allcheck 의 XML 대상이 XML API 장비인지 로그인 1번으로 확인합니다 (aaaLogin·aaaLogout 만, 조회 없음).
// 로그인 전에 실패하면(연결 실패·타임아웃·XML 이 아닌 응답·HTTP 오류 등, AUTH_FAIL 제외) isXML=false 라 부르는 쪽이
// bios(checkXML)와 같이 Redfish 로 1번 다시 읽습니다. AUTH_FAIL 은 다시 시도하지 않고 status 로 돌려줍니다.
func xmlProbe(conf *Config, password string, t Target) (isXML bool, status, detail string) {
	h := &ucsmHost{Target: t}
	c := ucsmxml.New(ucsmxml.Options{BaseURL: t.BaseURL(), User: conf.User, Pass: password, Insecure: conf.Insecure, Timeout: conf.Timeout})
	defer c.Logout() // 로그인에 실패해 쿠키가 없으면 아무것도 보내지 않는다
	if err := c.Login(); err != nil {
		h.setErr(err)
		if h.Status == StatusAuthFail {
			return true, h.Status, h.Detail
		}
		return false, "", ""
	}
	return true, "", ""
}

// xmlPickServers 는 호스트 이름에 해당하는 서버를 고릅니다: 1대면 그것, 여러 대면 서비스 프로파일 이름이 같은 서버
// (대소문자 무시), 하나도 맞지 않으면 전부.
func xmlPickServers(all []*ucsmServer, name string) []*ucsmServer {
	if len(all) <= 1 {
		return all
	}
	var out []*ucsmServer
	for _, s := range all {
		if s.SP != "" && strings.EqualFold(s.SP, name) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

// hostOutcomeOf 는 doCheck 의 AUTH_FAIL 차단기용 결과입니다 (Redfish 와 XML 대상을 함께 셈).
func hostOutcomeOf(h *hostCheck) hostOutcome {
	if !h.XML {
		return outcomeOf(h.Status, h.Stats, h.Target.Err == "")
	}
	switch {
	case h.Status == StatusAuthFail:
		return outAuthFail
	case h.Target.Err != "":
		return outNone
	case h.xmlLoggedIn:
		return outLoggedIn
	}
	return outOther
}

// xmlServerLabels 는 서버가 여럿이라 `<호스트>/<서버 dn>` 으로 나눠 보고한 이름들입니다 (등장 순, 없으면 nil).
func xmlServerLabels(h *hostCheck) []string {
	if h == nil || !h.XML {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, it := range h.Items {
		if it.Server != "" && !seen[it.Server] {
			seen[it.Server] = true
			out = append(out, it.Server)
		}
	}
	return out
}
