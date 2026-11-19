package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordsAPIResp struct {
	Records []struct {
		WorkRecord
		Notes  []RecordNote `json:"notes"`
		MyRole string       `json:"myRole"`
	} `json:"records"`
	Count               int  `json:"count"`
	Me                  User `json:"me"`
	FastApprovalSeconds int  `json:"fastApprovalSeconds"`
}

func recordsTestCreateSession(t *testing.T, tsURL string, sess Session) string {
	t.Helper()
	b, _ := json.Marshal(sess)
	resp, err := http.Post(tsURL+"/api/session/create", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out["sessionId"].(string)
}

func recordsTestDecide(t *testing.T, tsURL, sid, checkerIP, decision, comment string) {
	t.Helper()
	body := map[string]interface{}{
		"sessionId": sid,
		"checkerIp": checkerIP,
		"checkerData": map[string]interface{}{
			"decision":        decision,
			"isAllMatch":      decision == "APPROVED",
			"decisionComment": comment,
		},
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(tsURL+"/api/session/decision", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("decision failed (%d): %s", resp.StatusCode, raw)
	}
}

func recordsTestGet(t *testing.T, url string) recordsAPIResp {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s -> %d: %s", url, resp.StatusCode, raw)
	}
	var out recordsAPIResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func recordsTestPostNote(t *testing.T, tsURL, sid, text string) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"sessionId": sid, "text": text})
	resp, err := http.Post(tsURL+"/api/records/note", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// 승인/반려 시 작업 기록이 1회 생성되고, 기존 승인 로그 동작은 그대로 유지되는지 확인
func TestWorkRecordCreatedOnDecision(t *testing.T) {
	server, ts := setupTestServer(t)
	today := time.Now().Format("2006-01-02")

	sidApproved := recordsTestCreateSession(t, ts.URL, Session{
		Worker:   User{Name: "작업자A", IP: "192.168.0.23"},
		Checker:  User{Name: "확인자B", IP: "192.168.1.20"},
		Metadata: Metadata{IncidentId: "INC-2026-00123", WorkDescription: "커널 패치", SpecialNotes: "23시 작업"},
		WorkerData: WorkerData{
			DetailHosts:   []string{"web01", "web02", "web03"},
			IncidentHosts: []string{"web01", " web02 ", "web03", "web02"},
		},
	})
	recordsTestDecide(t, ts.URL, sidApproved, "192.168.1.20", "APPROVED", "")

	sidRejected := recordsTestCreateSession(t, ts.URL, Session{
		Worker:     User{Name: "작업자A", IP: "192.168.0.23"},
		Checker:    User{Name: "확인자B", IP: "192.168.1.20"},
		Metadata:   Metadata{},
		WorkerData: WorkerData{DetailHosts: []string{"db01"}},
	})
	recordsTestDecide(t, ts.URL, sidRejected, "192.168.1.20", "REJECTED", "대상 불일치")

	// 기존 승인 로그는 승인 건만 기록 (변경 없음)
	rawLog, err := os.ReadFile(filepath.Join(server.config.SharedDir, "logs", today+".log"))
	if err != nil {
		t.Fatalf("approval log missing: %v", err)
	}
	if n := len(strings.Split(strings.TrimSpace(string(rawLog)), "\n")); n != 1 {
		t.Fatalf("approval log should have 1 line, got %d", n)
	}

	out := recordsTestGet(t, ts.URL+"/api/records")
	if out.Count != 2 || len(out.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", out.Count)
	}
	if out.FastApprovalSeconds != 60 {
		t.Fatalf("expected default fastApprovalSeconds 60, got %d", out.FastApprovalSeconds)
	}
	byID := map[string]int{}
	for i, r := range out.Records {
		byID[r.SessionId] = i
	}
	a := out.Records[byID[sidApproved]]
	if a.Status != "APPROVED" || a.IncidentId != "INC-2026-00123" || a.WorkDescription != "커널 패치" || a.SpecialNotes != "23시 작업" {
		t.Fatalf("approved record fields wrong: %+v", a.WorkRecord)
	}
	if strings.Join(a.TargetHosts, ",") != "web01,web02,web03" || a.TargetSource != "incident" {
		t.Fatalf("approved record hosts wrong: %v (%s)", a.TargetHosts, a.TargetSource)
	}
	if a.MyRole != "작업자" {
		t.Fatalf("expected myRole 작업자, got %q", a.MyRole)
	}
	if a.CreatedAt == "" || a.DecidedAt == "" || a.RecordSource != "decision" {
		t.Fatalf("record times/source missing: %+v", a.WorkRecord)
	}
	r := out.Records[byID[sidRejected]]
	if r.Status != "REJECTED" || r.IncidentId != "-" || r.DecisionComment != "대상 불일치" || r.TargetSource != "detail" {
		t.Fatalf("rejected record fields wrong: %+v", r.WorkRecord)
	}

	// 기록 파일 위치: records/YYYY-MM/<sid>.json
	month := time.Now().Format("2006-01")
	recPath := filepath.Join(server.config.SharedDir, "records", month, sidApproved+".json")
	if _, err := os.Stat(recPath); err != nil {
		t.Fatalf("record file missing: %v", err)
	}
}

// 한 번 생성된 기록은 덮어쓰지 않는다
func TestWorkRecordNeverOverwritten(t *testing.T) {
	server, _ := setupTestServer(t)
	sess := &Session{
		SessionId:  "req_20261119_101010_abcdef",
		Status:     "APPROVED",
		CreatedAt:  time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
		DecidedAt:  time.Now().Format(time.RFC3339),
		Worker:     User{Name: "A", IP: "10.0.0.1"},
		Checker:    User{Name: "B", IP: "10.0.0.2"},
		Metadata:   Metadata{IncidentId: "INC-1"},
		WorkerData: WorkerData{TargetHosts: []string{"h1", "h2"}},
	}
	created, err := server.createWorkRecord(sess, "decision")
	if err != nil || !created {
		t.Fatalf("first create failed: created=%v err=%v", created, err)
	}
	sess.Metadata.IncidentId = "INC-CHANGED"
	sess.WorkerData.TargetHosts = []string{"zzz"}
	created, err = server.createWorkRecord(sess, "peer")
	if err != nil || created {
		t.Fatalf("second create must be a no-op: created=%v err=%v", created, err)
	}
	path := server.findWorkRecordFile(sess.SessionId)
	rec, err := readWorkRecordFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec.IncidentId != "INC-1" || strings.Join(rec.TargetHosts, ",") != "h1,h2" || rec.RecordSource != "decision" {
		t.Fatalf("record was modified: %+v", rec)
	}
	// 미결정 세션과 잘못된 세션 ID 는 기록하지 않음
	if _, err := server.createWorkRecord(&Session{SessionId: "req_x", Status: "PENDING"}, "decision"); err == nil {
		t.Fatal("pending session must not create a record")
	}
	if _, err := server.createWorkRecord(&Session{SessionId: "../evil", Status: "APPROVED"}, "decision"); err == nil {
		t.Fatal("invalid session id must be rejected")
	}
}

// 기존 결정 세션은 기동 시 기록으로 이관되고, 30일 정리 이후에도 기록은 남는다
func TestWorkRecordBackfillSurvivesPrune(t *testing.T) {
	tempDir := t.TempDir()
	sessDir := filepath.Join(tempDir, "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	write := func(s Session) {
		b, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(filepath.Join(sessDir, s.SessionId+".json"), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(Session{SessionId: "req_old_approved", Status: "APPROVED", CreatedAt: old.Add(-time.Minute).Format(time.RFC3339), DecidedAt: old.Format(time.RFC3339),
		Worker: User{Name: "A", IP: "10.0.0.1"}, Checker: User{Name: "B", IP: "10.0.0.2"}, Metadata: Metadata{IncidentId: "INC-OLD"},
		WorkerData: WorkerData{DetailHosts: []string{"old01"}}})
	write(Session{SessionId: "req_recent_rejected", Status: "REJECTED", CreatedAt: time.Now().Format(time.RFC3339), DecidedAt: time.Now().Format(time.RFC3339),
		Worker: User{Name: "A", IP: "10.0.0.1"}, Checker: User{Name: "B", IP: "10.0.0.2"}, WorkerData: WorkerData{DetailHosts: []string{"new01"}}})
	write(Session{SessionId: "req_pending", Status: "PENDING", CreatedAt: time.Now().Format(time.RFC3339),
		Worker: User{Name: "A", IP: "10.0.0.1"}, Checker: User{Name: "B", IP: "10.0.0.2"}, WorkerData: WorkerData{DetailHosts: []string{"p01"}}})

	server, err := NewAgentServer(Config{SharedDir: tempDir, ListenPort: 18080, PollIntervalMs: 50, BindIP: "192.168.0.23"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	// 40일 지난 세션 파일은 기존대로 정리됨
	if _, err := os.Stat(filepath.Join(sessDir, "req_old_approved.json")); !os.IsNotExist(err) {
		t.Fatalf("old session file should have been pruned (err=%v)", err)
	}
	all := server.listWorkRecords(time.Time{}, time.Time{})
	if len(all) != 2 {
		t.Fatalf("expected 2 backfilled records (pending excluded), got %d", len(all))
	}
	if all[0].SessionId != "req_recent_rejected" || all[1].SessionId != "req_old_approved" {
		t.Fatalf("records must be newest first: %s, %s", all[0].SessionId, all[1].SessionId)
	}
	if all[1].RecordSource != "backfill" || all[1].IncidentId != "INC-OLD" {
		t.Fatalf("backfilled record wrong: %+v", all[1].WorkRecord)
	}
	oldMonth := old.Local().Format("2006-01")
	if _, err := os.Stat(filepath.Join(tempDir, "records", oldMonth, "req_old_approved.json")); err != nil {
		t.Fatalf("old record should be stored in its decision month dir: %v", err)
	}

	// 기간 필터
	from := time.Now().Add(-24 * time.Hour)
	recent := server.listWorkRecords(from, time.Time{})
	if len(recent) != 1 || recent[0].SessionId != "req_recent_rejected" {
		t.Fatalf("date filter failed: %d", len(recent))
	}
	// 반복 이관은 중복을 만들지 않음
	if n := server.backfillWorkRecords(); n != 0 {
		t.Fatalf("second backfill must create 0, got %d", n)
	}
}

// 메모: 해당 건의 작업자/확인자만 추가 가능, 여러 개 추가 가능, 수정/삭제 불가
func TestWorkRecordNotesPermissionsAndAppendOnly(t *testing.T) {
	server, ts := setupTestServer(t)

	sidMine := recordsTestCreateSession(t, ts.URL, Session{
		Worker:     User{Name: "작업자A", IP: "192.168.0.23"},
		Checker:    User{Name: "확인자B", IP: "192.168.1.20"},
		Metadata:   Metadata{IncidentId: "INC-NOTE"},
		WorkerData: WorkerData{DetailHosts: []string{"a1"}},
	})
	recordsTestDecide(t, ts.URL, sidMine, "192.168.1.20", "APPROVED", "")

	sidOther := recordsTestCreateSession(t, ts.URL, Session{
		Worker:     User{Name: "다른작업자", IP: "192.168.1.30"},
		Checker:    User{Name: "확인자B", IP: "192.168.1.20"},
		Metadata:   Metadata{IncidentId: "INC-OTHER"},
		WorkerData: WorkerData{DetailHosts: []string{"b1"}},
	})
	recordsTestDecide(t, ts.URL, sidOther, "192.168.1.20", "APPROVED", "")

	if code, _ := recordsTestPostNote(t, ts.URL, sidMine, "DB 2대 제외 확인함"); code != http.StatusOK {
		t.Fatalf("worker note should succeed, got %d", code)
	}
	code, out := recordsTestPostNote(t, ts.URL, sidMine, "추가 메모")
	if code != http.StatusOK {
		t.Fatalf("second note should succeed, got %d", code)
	}
	if notes, _ := out["notes"].([]interface{}); len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %v", out["notes"])
	}
	if code, _ := recordsTestPostNote(t, ts.URL, sidOther, "남의 건"); code != http.StatusForbidden {
		t.Fatalf("non-participant note must be 403, got %d", code)
	}
	if code, _ := recordsTestPostNote(t, ts.URL, sidMine, "   "); code != http.StatusBadRequest {
		t.Fatalf("empty note must be 400, got %d", code)
	}
	if code, _ := recordsTestPostNote(t, ts.URL, sidMine, strings.Repeat("가", maxRecordNoteRunes+1)); code != http.StatusBadRequest {
		t.Fatalf("too long note must be 400, got %d", code)
	}
	if code, _ := recordsTestPostNote(t, ts.URL, "req_missing", "x"); code != http.StatusNotFound {
		t.Fatalf("missing record must be 404, got %d", code)
	}
	if code, _ := recordsTestPostNote(t, ts.URL, "../../etc", "x"); code != http.StatusBadRequest {
		t.Fatalf("invalid session id must be 400, got %d", code)
	}

	// 수정/삭제 메서드는 거부
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, p := range []string{"/api/records", "/api/records/note"} {
			req, _ := http.NewRequest(m, ts.URL+p, strings.NewReader(`{}`))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s must be 405, got %d", m, p, resp.StatusCode)
			}
		}
	}

	list := recordsTestGet(t, ts.URL+"/api/records")
	for _, r := range list.Records {
		switch r.SessionId {
		case sidMine:
			if len(r.Notes) != 2 || r.Notes[0].Text != "DB 2대 제외 확인함" || r.Notes[0].AuthorRole != "작업자" || r.Notes[0].Author.Name == "" && r.Notes[0].Author.IP == "" {
				t.Fatalf("mine notes wrong: %+v", r.Notes)
			}
			if r.MyRole != "작업자" {
				t.Fatalf("myRole wrong: %q", r.MyRole)
			}
		case sidOther:
			if len(r.Notes) != 0 || r.MyRole != "" {
				t.Fatalf("other record should have no notes/role: %+v %q", r.Notes, r.MyRole)
			}
		}
	}

	// 메모 1개 = 파일 1개
	files, _ := filepath.Glob(filepath.Join(server.config.SharedDir, "records", "notes", sidMine, "*.json"))
	if len(files) != 2 {
		t.Fatalf("expected 2 note files, got %d", len(files))
	}
}

func TestWorkRecordsDateParamValidation(t *testing.T) {
	_, ts := setupTestServer(t)
	resp, err := http.Get(ts.URL + "/api/records?from=2026/11/01")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad date must be 400, got %d", resp.StatusCode)
	}
	out := recordsTestGet(t, ts.URL+"/api/records?from=2026-01-01&to=2026-12-31")
	if out.Count != 0 {
		t.Fatalf("expected empty list, got %d", out.Count)
	}
}

func TestSelectFinalTargetHostsPriority(t *testing.T) {
	cases := []struct {
		wd   WorkerData
		want string
		src  string
	}{
		{WorkerData{IncidentHosts: []string{"i1"}, TargetHosts: []string{"t1"}, DetailHosts: []string{"d1"}}, "i1", "incident"},
		{WorkerData{TargetHosts: []string{"t1"}, DetailHosts: []string{"d1"}}, "t1", "target"},
		{WorkerData{DetailHosts: []string{"d1", "d1", ""}}, "d1", "detail"},
		{WorkerData{AwxHosts: []string{"a1"}}, "a1", "awx"},
		{WorkerData{}, "", "none"},
	}
	for _, c := range cases {
		h, src := selectFinalTargetHosts(c.wd)
		if strings.Join(h, ",") != c.want || src != c.src {
			t.Fatalf("got %v/%s want %s/%s", h, src, c.want, c.src)
		}
	}
}

func recordsTestPostDelete(t *testing.T, tsURL, sid, reason string) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"sessionId": sid, "reason": reason})
	resp, err := http.Post(tsURL+"/api/records/delete", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func recordsTestSetMyRole(t *testing.T, server *AgentServer, role string) {
	t.Helper()
	if err := server.writeUsers(UsersFile{Users: []User{
		{Name: "나", IP: "192.168.0.23", Role: role},
		{Name: "확인자B", IP: "192.168.1.20", Role: "확인자"},
	}}); err != nil {
		t.Fatal(err)
	}
	server.usersMutex.Lock()
	server.lastUsersSync = time.Time{}
	server.usersMutex.Unlock()
}

// users.json 역할이 관리자인 사용자만 기록을 삭제할 수 있고, 삭제 내역은 보관되며 다시 생성되지 않는다
func TestWorkRecordAdminDelete(t *testing.T) {
	server, ts := setupTestServer(t)
	recordsTestSetMyRole(t, server, "작업자")

	sid := recordsTestCreateSession(t, ts.URL, Session{
		Worker:     User{Name: "나", IP: "192.168.0.23"},
		Checker:    User{Name: "확인자B", IP: "192.168.1.20"},
		Metadata:   Metadata{IncidentId: "INC-DEL"},
		WorkerData: WorkerData{DetailHosts: []string{"x1", "x2"}},
	})
	recordsTestDecide(t, ts.URL, sid, "192.168.1.20", "APPROVED", "")
	if code, _ := recordsTestPostNote(t, ts.URL, sid, "삭제 전 메모"); code != http.StatusOK {
		t.Fatalf("note should succeed, got %d", code)
	}

	// 관리자가 아니면 403, isAdmin=false
	if out := recordsTestGet(t, ts.URL+"/api/records"); out.Count != 1 {
		t.Fatalf("expected 1 record, got %d", out.Count)
	}
	var adminFlag struct {
		IsAdmin bool `json:"isAdmin"`
	}
	getAdmin := func() bool {
		resp, err := http.Get(ts.URL + "/api/records")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_ = json.NewDecoder(resp.Body).Decode(&adminFlag)
		return adminFlag.IsAdmin
	}
	if getAdmin() {
		t.Fatal("non-admin must have isAdmin=false")
	}
	if code, _ := recordsTestPostDelete(t, ts.URL, sid, "중복 건"); code != http.StatusForbidden {
		t.Fatalf("non-admin delete must be 403, got %d", code)
	}

	// 등록 API 로 관리자 권한 상승 불가
	regBody, _ := json.Marshal(User{Name: "나", IP: "192.168.0.23", Role: "관리자"})
	resp, err := http.Post(ts.URL+"/api/users/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("register as 관리자 must be 403, got %d", resp.StatusCode)
	}

	// users.json 에서 관리자로 지정
	recordsTestSetMyRole(t, server, "관리자")
	if !getAdmin() {
		t.Fatal("admin must have isAdmin=true")
	}
	// 이름 변경 등록 시에도 관리자 역할 유지
	regBody, _ = json.Marshal(User{Name: "나(이름변경)", IP: "192.168.0.23"})
	resp, err = http.Post(ts.URL+"/api/users/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	server.usersMutex.Lock()
	server.lastUsersSync = time.Time{}
	server.usersMutex.Unlock()
	if !getAdmin() {
		t.Fatal("admin role must be kept after name re-registration")
	}

	if code, _ := recordsTestPostDelete(t, ts.URL, sid, "  "); code != http.StatusBadRequest {
		t.Fatalf("empty reason must be 400, got %d", code)
	}
	if code, _ := recordsTestPostDelete(t, ts.URL, "req_missing", "x"); code != http.StatusNotFound {
		t.Fatalf("missing record must be 404, got %d", code)
	}
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/records/delete", nil)
	if r2, err := http.DefaultClient.Do(req); err == nil {
		r2.Body.Close()
		if r2.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("DELETE method must be 405, got %d", r2.StatusCode)
		}
	}

	code, out := recordsTestPostDelete(t, ts.URL, sid, "테스트로 잘못 생성된 건")
	if code != http.StatusOK {
		t.Fatalf("admin delete should succeed, got %d %v", code, out)
	}
	if list := recordsTestGet(t, ts.URL+"/api/records"); list.Count != 0 {
		t.Fatalf("deleted record must disappear from dashboard, got %d", list.Count)
	}

	// 삭제 내역 보관 확인
	raw, err := os.ReadFile(filepath.Join(server.config.SharedDir, "records", "deleted", sid+".json"))
	if err != nil {
		t.Fatalf("deleted archive missing: %v", err)
	}
	var arch DeletedWorkRecord
	if err := json.Unmarshal(raw, &arch); err != nil {
		t.Fatal(err)
	}
	if arch.SessionId != sid || arch.IncidentId != "INC-DEL" || arch.Deletion.Reason != "테스트로 잘못 생성된 건" ||
		arch.Deletion.DeletedBy.IP != "192.168.0.23" || arch.Deletion.DeletedAt == "" {
		t.Fatalf("deleted archive wrong: %+v", arch)
	}
	if notes, _ := filepath.Glob(filepath.Join(server.config.SharedDir, "records", "deleted", "notes", sid, "*.json")); len(notes) != 1 {
		t.Fatalf("notes should be moved to deleted/notes, got %d", len(notes))
	}

	// 세션 파일이 남아 있어도 다시 생성되지 않음, 메모/재삭제 불가
	if n := server.backfillWorkRecords(); n != 0 {
		t.Fatalf("backfill must not recreate deleted record, created %d", n)
	}
	if list := recordsTestGet(t, ts.URL+"/api/records"); list.Count != 0 {
		t.Fatalf("deleted record reappeared: %d", list.Count)
	}
	if code, _ := recordsTestPostNote(t, ts.URL, sid, "삭제 후 메모"); code != http.StatusNotFound {
		t.Fatalf("note on deleted record must be 404, got %d", code)
	}
	if code, _ := recordsTestPostDelete(t, ts.URL, sid, "again"); code != http.StatusNotFound {
		t.Fatalf("second delete must be 404, got %d", code)
	}
	// 기존 승인 로그는 그대로 유지
	today := time.Now().Format("2006-01-02")
	if logRaw, err := os.ReadFile(filepath.Join(server.config.SharedDir, "logs", today+".log")); err != nil || !strings.Contains(string(logRaw), "INC-DEL") {
		t.Fatalf("approval log must be untouched (err=%v)", err)
	}
}
