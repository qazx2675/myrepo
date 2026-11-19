package main

// 대시보드 작업 기록(records) 및 메모(notes)
//
// - 확인자가 승인/반려를 결정하는 순간 공유폴더/records/YYYY-MM/<세션ID>.json 을 1회 생성한다.
//   (O_EXCL 생성: 한 번 만들어진 기록은 다시 쓰거나 덮어쓰지 않는다)
// - 세션 파일(sessions/*.json)은 기존대로 30일 후 정리되지만 작업 기록은 영구 보관한다.
// - 메모는 엑셀 F2 메모처럼 기록에 덧붙는 형태이며, 메모 1개 = 파일 1개(records/notes/<세션ID>/*.json)로
//   저장해 여러 사람이 동시에 작성해도 서로 덮어쓰지 않는다. 수정/삭제 API 는 제공하지 않는다.
// - 기존 승인 로그(logs/*.log), 세션 파일, 기존 API 의 형식과 동작은 변경하지 않는다.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// WorkRecord is an immutable dashboard record created once when a session is decided.
type WorkRecord struct {
	SessionId       string   `json:"sessionId"`
	Status          string   `json:"status"` // APPROVED | REJECTED
	CreatedAt       string   `json:"createdAt"`
	DecidedAt       string   `json:"decidedAt"`
	Worker          User     `json:"worker"`
	Checker         User     `json:"checker"`
	IncidentId      string   `json:"incidentId"`
	WorkDescription string   `json:"workDescription"`
	SpecialNotes    string   `json:"specialNotes,omitempty"`
	AwxContextLabel string   `json:"awxContextLabel,omitempty"`
	TargetHosts     []string `json:"targetHosts"`
	TargetSource    string   `json:"targetSource"` // incident | target | detail | awx | none
	DecisionComment string   `json:"decisionComment,omitempty"`
	RecordedAt      string   `json:"recordedAt"`
	RecordSource    string   `json:"recordSource"` // decision | peer | backfill
}

// RecordNote is an append-only memo attached to a work record (Excel F2 comment style).
type RecordNote struct {
	NoteId     string `json:"noteId"`
	SessionId  string `json:"sessionId"`
	Author     User   `json:"author"`
	AuthorRole string `json:"authorRole"` // 작업자 | 확인자 | 작업자/확인자
	Text       string `json:"text"`
	CreatedAt  string `json:"createdAt"`
}

// WorkRecordView is a record plus its notes as returned by GET /api/records.
type WorkRecordView struct {
	WorkRecord
	Notes []RecordNote `json:"notes"`
}

const (
	maxRecordNoteRunes          = 1000
	recordBackfillMinInterval   = 60 * time.Second
	defaultFastApprovalSeconds  = 60
	workRecordMonthDirLayout    = "2006-01"
	workRecordNotesDirName      = "notes"
	workRecordSessionIDMaxBytes = 128
)

var (
	recordMonthDirRe  = regexp.MustCompile(`^\d{4}-\d{2}$`)
	recordSessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)
	errRecordExists   = errors.New("work record already exists")
)

func (s *AgentServer) recordsDirPath() string {
	return filepath.Join(s.config.SharedDir, "records")
}

func (s *AgentServer) recordNotesDirPath(sessionId string) string {
	return filepath.Join(s.recordsDirPath(), workRecordNotesDirName, filepath.Base(sessionId))
}

func isValidRecordSessionID(id string) bool {
	return id != "" && len(id) <= workRecordSessionIDMaxBytes && recordSessionIDRe.MatchString(id) && id != "." && id != ".."
}

// cleanHostList trims, drops empties and de-duplicates while keeping order.
func cleanHostList(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	seen := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// selectFinalTargetHosts mirrors the UI rule (getGuideTargetHosts): 인시던트 → 작업대상 → 상세목록 (→ AWX).
func selectFinalTargetHosts(wd WorkerData) ([]string, string) {
	if h := cleanHostList(wd.IncidentHosts); len(h) > 0 {
		return h, "incident"
	}
	if h := cleanHostList(wd.TargetHosts); len(h) > 0 {
		return h, "target"
	}
	if h := cleanHostList(wd.DetailHosts); len(h) > 0 {
		return h, "detail"
	}
	if h := cleanHostList(wd.AwxHosts); len(h) > 0 {
		return h, "awx"
	}
	return []string{}, "none"
}

func parseRecordTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
		return t
	}
	return time.Time{}
}

// buildWorkRecord converts a decided session into an immutable dashboard record.
func buildWorkRecord(sess *Session, source string) (WorkRecord, error) {
	if sess == nil {
		return WorkRecord{}, errors.New("nil session")
	}
	if sess.Status != "APPROVED" && sess.Status != "REJECTED" {
		return WorkRecord{}, fmt.Errorf("session %s is not decided (status=%s)", sess.SessionId, sess.Status)
	}
	if !isValidRecordSessionID(sess.SessionId) {
		return WorkRecord{}, fmt.Errorf("invalid session id: %q", sess.SessionId)
	}
	hosts, src := selectFinalTargetHosts(sess.WorkerData)
	decidedAt := sess.DecidedAt
	if decidedAt == "" {
		decidedAt = time.Now().Format(time.RFC3339)
	}
	comment := ""
	if sess.CheckerData != nil {
		comment = strings.TrimSpace(sess.CheckerData.DecisionComment)
	}
	incident := strings.TrimSpace(sess.Metadata.IncidentId)
	if incident == "" {
		incident = "-"
	}
	return WorkRecord{
		SessionId:       sess.SessionId,
		Status:          sess.Status,
		CreatedAt:       sess.CreatedAt,
		DecidedAt:       decidedAt,
		Worker:          sess.Worker,
		Checker:         sess.Checker,
		IncidentId:      incident,
		WorkDescription: strings.TrimSpace(sess.Metadata.WorkDescription),
		SpecialNotes:    strings.TrimSpace(sess.Metadata.SpecialNotes),
		AwxContextLabel: strings.TrimSpace(sess.Metadata.AwxContextLabel),
		TargetHosts:     hosts,
		TargetSource:    src,
		DecisionComment: comment,
		RecordedAt:      time.Now().Format(time.RFC3339),
		RecordSource:    source,
	}, nil
}

func (s *AgentServer) workRecordPath(rec WorkRecord) string {
	month := time.Now().Format(workRecordMonthDirLayout)
	if t := parseRecordTime(rec.DecidedAt); !t.IsZero() {
		month = t.Local().Format(workRecordMonthDirLayout)
	}
	return filepath.Join(s.recordsDirPath(), month, filepath.Base(rec.SessionId)+".json")
}

// findWorkRecordFile locates an existing record for a session in any month directory.
func (s *AgentServer) findWorkRecordFile(sessionId string) string {
	if !isValidRecordSessionID(sessionId) {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(s.recordsDirPath(), "*", filepath.Base(sessionId)+".json"))
	if err != nil {
		return ""
	}
	for _, m := range matches {
		if recordMonthDirRe.MatchString(filepath.Base(filepath.Dir(m))) {
			return m
		}
	}
	return ""
}

// writeFileExclusive creates a new file and fails with os.ErrExist when it already exists (never overwrites).
func writeFileExclusive(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}

// createWorkRecord writes the record once. An existing record is never modified (returns it with errRecordExists suppressed).
func (s *AgentServer) createWorkRecord(sess *Session, source string) (bool, error) {
	rec, err := buildWorkRecord(sess, source)
	if err != nil {
		return false, err
	}
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()
	if s.findWorkRecordFile(rec.SessionId) != "" {
		return false, nil
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return false, err
	}
	if err := writeFileExclusive(s.workRecordPath(rec), data); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// backfillWorkRecords creates records for decided sessions that do not have one yet
// (sessions decided before this feature existed, or by an older exe version on another PC).
func (s *AgentServer) backfillWorkRecords() int {
	files, err := filepath.Glob(filepath.Join(s.config.SharedDir, "sessions", "*.json"))
	if err != nil {
		return 0
	}
	created := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var sess Session
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		if sess.Status != "APPROVED" && sess.Status != "REJECTED" {
			continue
		}
		if sess.DecidedAt == "" {
			if info, sErr := os.Stat(f); sErr == nil {
				sess.DecidedAt = info.ModTime().Format(time.RFC3339)
			}
		}
		ok, err := s.createWorkRecord(&sess, "backfill")
		if err != nil {
			log.Printf("[RECORD] backfill skipped %s: %v\n", filepath.Base(f), err)
			continue
		}
		if ok {
			created++
		}
	}
	s.recordsMu.Lock()
	s.recordBackfillAt = time.Now()
	s.recordsMu.Unlock()
	if created > 0 {
		log.Printf("[RECORD] backfilled %d work record(s)\n", created)
	}
	return created
}

func (s *AgentServer) maybeBackfillWorkRecords() {
	s.recordsMu.Lock()
	due := time.Since(s.recordBackfillAt) >= recordBackfillMinInterval
	s.recordsMu.Unlock()
	if due {
		s.backfillWorkRecords()
	}
}

func readWorkRecordFile(path string) (WorkRecord, error) {
	var rec WorkRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, err
	}
	if rec.TargetHosts == nil {
		rec.TargetHosts = []string{}
	}
	return rec, nil
}

func (s *AgentServer) readRecordNotes(sessionId string) []RecordNote {
	notes := []RecordNote{}
	if !isValidRecordSessionID(sessionId) {
		return notes
	}
	files, err := filepath.Glob(filepath.Join(s.recordNotesDirPath(sessionId), "*.json"))
	if err != nil {
		return notes
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var n RecordNote
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		notes = append(notes, n)
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].CreatedAt == notes[j].CreatedAt {
			return notes[i].NoteId < notes[j].NoteId
		}
		return notes[i].CreatedAt < notes[j].CreatedAt
	})
	return notes
}

func monthRangeOverlaps(month string, from, to time.Time) bool {
	start, err := time.ParseInLocation(workRecordMonthDirLayout, month, time.Local)
	if err != nil {
		return false
	}
	end := start.AddDate(0, 1, 0)
	if !from.IsZero() && !end.After(from) {
		return false
	}
	if !to.IsZero() && start.After(to) {
		return false
	}
	return true
}

// listWorkRecords returns records whose decidedAt falls in [from, to] (zero = unbounded), newest first.
func (s *AgentServer) listWorkRecords(from, to time.Time) []WorkRecordView {
	views := []WorkRecordView{}
	monthDirs, err := filepath.Glob(filepath.Join(s.recordsDirPath(), "*"))
	if err != nil {
		return views
	}
	for _, dir := range monthDirs {
		month := filepath.Base(dir)
		if !recordMonthDirRe.MatchString(month) || !monthRangeOverlaps(month, from, to) {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range files {
			rec, err := readWorkRecordFile(f)
			if err != nil || rec.SessionId == "" {
				continue
			}
			t := parseRecordTime(rec.DecidedAt)
			if !t.IsZero() {
				if !from.IsZero() && t.Before(from) {
					continue
				}
				if !to.IsZero() && t.After(to) {
					continue
				}
			}
			views = append(views, WorkRecordView{WorkRecord: rec, Notes: s.readRecordNotes(rec.SessionId)})
		}
	}
	sort.SliceStable(views, func(i, j int) bool {
		ti, tj := parseRecordTime(views[i].DecidedAt), parseRecordTime(views[j].DecidedAt)
		if ti.Equal(tj) {
			return views[i].SessionId > views[j].SessionId
		}
		return ti.After(tj)
	})
	return views
}

// currentIdentity returns this agent's user (IP from detection, name from users.json).
func (s *AgentServer) currentIdentity() User {
	me := User{IP: s.detectedIP}
	if uf, err := s.readUsers(); err == nil {
		for _, u := range uf.Users {
			if isLocalHostIP(u.IP, s.detectedIP) {
				me.Name = u.Name
				me.IP = u.IP
				break
			}
		}
	}
	return me
}

// recordRoleFor reports whether the identity is the worker/checker of the record.
func recordRoleFor(rec WorkRecord, me User, detectedIP string) string {
	matches := func(ip string) bool {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			return false
		}
		return ipMatches(ip, me.IP) || ipMatches(ip, detectedIP) || isLocalHostIP(ip, detectedIP)
	}
	isWorker := matches(rec.Worker.IP)
	isChecker := matches(rec.Checker.IP)
	switch {
	case isWorker && isChecker:
		return "작업자/확인자"
	case isWorker:
		return "작업자"
	case isChecker:
		return "확인자"
	}
	return ""
}

func fastApprovalSeconds(cfg Config) int {
	if cfg.FastApprovalSeconds > 0 {
		return cfg.FastApprovalSeconds
	}
	return defaultFastApprovalSeconds
}

func parseRecordDateParam(v string, endOfDay bool) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return t, nil
}

// handleWorkRecords: GET /api/records?from=YYYY-MM-DD&to=YYYY-MM-DD (조회 전용, 수정/삭제 없음)
func (s *AgentServer) handleWorkRecords(w http.ResponseWriter, r *http.Request) {
	if enableCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "작업 기록은 조회만 가능합니다 (수정/삭제 불가)"})
		return
	}
	from, err := parseRecordDateParam(r.URL.Query().Get("from"), false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid from: expected YYYY-MM-DD"})
		return
	}
	to, err := parseRecordDateParam(r.URL.Query().Get("to"), true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid to: expected YYYY-MM-DD"})
		return
	}
	s.maybeBackfillWorkRecords()
	records := s.listWorkRecords(from, to)
	me := s.currentIdentity()
	type viewWithRole struct {
		WorkRecordView
		MyRole string `json:"myRole"`
	}
	out := make([]viewWithRole, 0, len(records))
	for _, rv := range records {
		out = append(out, viewWithRole{WorkRecordView: rv, MyRole: recordRoleFor(rv.WorkRecord, me, s.detectedIP)})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"records":             out,
		"count":               len(out),
		"me":                  me,
		"recordsDir":          s.recordsDirPath(),
		"fastApprovalSeconds": fastApprovalSeconds(s.config),
	})
}

// handleWorkRecordNote: POST /api/records/note {sessionId, text}
// 해당 건의 작업자/확인자만 메모를 추가할 수 있으며 추가된 메모는 수정/삭제할 수 없다.
func (s *AgentServer) handleWorkRecordNote(w http.ResponseWriter, r *http.Request) {
	if enableCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "메모는 추가만 가능합니다 (수정/삭제 불가)"})
		return
	}
	var req struct {
		SessionId string `json:"sessionId"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	req.SessionId = strings.TrimSpace(req.SessionId)
	text := strings.TrimSpace(strings.ReplaceAll(req.Text, "\r\n", "\n"))
	if !isValidRecordSessionID(req.SessionId) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sessionId"})
		return
	}
	if text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "메모 내용을 입력하세요"})
		return
	}
	if utf8.RuneCountInString(text) > maxRecordNoteRunes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("메모는 %d자 이내로 입력하세요", maxRecordNoteRunes)})
		return
	}

	recPath := s.findWorkRecordFile(req.SessionId)
	if recPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "작업 기록을 찾을 수 없습니다"})
		return
	}
	rec, err := readWorkRecordFile(recPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "작업 기록을 읽을 수 없습니다: " + err.Error()})
		return
	}
	me := s.currentIdentity()
	role := recordRoleFor(rec, me, s.detectedIP)
	if role == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "해당 건의 작업자 또는 확인자만 메모를 추가할 수 있습니다"})
		return
	}

	now := time.Now()
	note := RecordNote{
		NoteId:     fmt.Sprintf("%s_%09d_%s", now.Format("20060102_150405"), now.Nanosecond(), randomNoteSuffix()),
		SessionId:  rec.SessionId,
		Author:     me,
		AuthorRole: role,
		Text:       text,
		CreatedAt:  now.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(note, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := writeFileExclusive(filepath.Join(s.recordNotesDirPath(rec.SessionId), note.NoteId+".json"), data); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "메모 저장 실패: " + err.Error()})
		return
	}
	s.broadcastEvent("RECORD_NOTE_ADDED", rec.SessionId, note)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"note":    note,
		"notes":   s.readRecordNotes(rec.SessionId),
	})
}

func randomNoteSuffix() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	}
	return hex.EncodeToString(b)
}
