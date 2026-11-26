package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
	"excel-diff/internal/report"
)

const (
	maxWarnings  = 200 // 화면에 보내는 경고 수. 전체는 결과 파일에 있다
	maxHeaderLen = 40  // 키 열 목록에 보이는 머리글 글자 수
)

// 상태 값
const (
	statusUploading = "uploading"
	statusReading   = "reading"
	statusReady     = "ready"
	statusRunning   = "running"
	statusDone      = "done"
	statusError     = "error"
)

// upload 는 한쪽 파일이다. 필드는 Server.mu 로 보호한다.
type upload struct {
	ID      int         `json:"id"`
	Name    string      `json:"name"`
	Status  string      `json:"status"`
	Percent int         `json:"percent"`
	Error   string      `json:"error,omitempty"`
	Sheets  []sheetInfo `json:"sheets,omitempty"`
	wb      *reader.Workbook
}

type sheetInfo struct {
	Name    string       `json:"name"`
	Hidden  bool         `json:"hidden"`
	Columns []columnInfo `json:"columns"`
}

type columnInfo struct {
	Num    int    `json:"num"`
	Letter string `json:"letter"`
	Header string `json:"header"` // 열의 첫 번째 값
}

// job 은 비교 작업이다. 필드는 Server.mu 로 보호한다.
type job struct {
	Status    string  `json:"status"`
	Percent   int     `json:"percent"`
	Stage     string  `json:"stage"`
	ElapsedMs int64   `json:"elapsedMs"`
	Error     string  `json:"error,omitempty"`
	Result    *result `json:"result,omitempty"`
	start     time.Time
}

type result struct {
	Path         string       `json:"path"`
	OldName      string       `json:"oldName"`
	NewName      string       `json:"newName"`
	Items        int          `json:"items"`
	Summary      diff.Summary `json:"summary"`
	Warnings     []string     `json:"warnings"`
	WarningCount int          `json:"warningCount"`
}

type pair struct {
	Old    string `json:"old"`
	New    string `json:"new"`
	OldKey int    `json:"oldKey"`
	NewKey int    `json:"newKey"`
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	state := struct {
		Old   *upload `json:"old"`
		New   *upload `json:"new"`
		Pairs []pair  `json:"pairs"` // 양쪽을 다 읽었을 때의 기본 짝
		Job   *job    `json:"job"`
	}{}
	// 잠금을 풀고 전송할 수 있게 복사한다.
	clone := func(u *upload) *upload {
		if u == nil {
			return nil
		}
		c := *u
		return &c
	}
	state.Old, state.New = clone(s.files[0]), clone(s.files[1])
	if s.job != nil {
		j := *s.job
		if j.Status == statusRunning {
			j.ElapsedMs = time.Since(j.start).Milliseconds()
		}
		state.Job = &j
	}
	s.mu.Unlock()
	if o, n := state.Old, state.New; o != nil && n != nil && o.wb != nil && n.wb != nil {
		for _, p := range diff.DefaultPairs(o.wb, n.wb) {
			state.Pairs = append(state.Pairs, pair{Old: p.Old, New: p.New})
		}
	}
	reply(w, http.StatusOK, state)
}

// handleUpload 는 본문(파일 내용)을 임시 폴더에 저장하고 읽기를 시작한다.
// 질의 문자열: side=old|new, name=원본 파일명.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	side := slices.Index([]string{"old", "new"}, r.URL.Query().Get("side"))
	name := r.URL.Query().Get("name")
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	if side < 0 || name == "" {
		fail(w, http.StatusBadRequest, "잘못된 업로드 요청입니다")
		return
	}

	s.mu.Lock()
	if s.job != nil && s.job.Status == statusRunning {
		s.mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body) // 본문을 비워야 브라우저가 응답을 받는다
		fail(w, http.StatusConflict, "비교 중에는 파일을 바꿀 수 없습니다")
		return
	}
	s.nextID++
	u := &upload{ID: s.nextID, Name: name, Status: statusUploading}
	s.files[side], s.job = u, nil // 이전 파일과 결과는 버린다
	s.active++
	s.mu.Unlock()

	path, err := s.save(r.Body, strings.ToLower(filepath.Ext(name)))
	if err != nil {
		log.Printf("업로드 실패(%s): %v", name, err)
		message := "파일을 업로드하지 못했습니다: " + err.Error()
		s.mu.Lock()
		u.Status, u.Error = statusError, message
		s.mu.Unlock()
		s.done()
		fail(w, http.StatusInternalServerError, message)
		return
	}
	s.mu.Lock()
	u.Status = statusReading
	s.mu.Unlock()
	go s.read(side, u, path)
	reply(w, http.StatusOK, map[string]any{"id": u.ID})
}

func (s *Server) save(body io.Reader, ext string) (path string, err error) {
	f, err := os.CreateTemp(s.tempDir, "upload-*"+ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// read 는 업로드한 파일을 한 번 읽어 둔다. 그 사이 다른 파일이 올라오면 버린다.
func (s *Server) read(side int, u *upload, path string) {
	var wb *reader.Workbook
	var sheets []sheetInfo
	var err error
	s.work.Lock()
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("읽기 패닉: %v\n%s", rec, debug.Stack())
			err = fmt.Errorf("파일을 읽는 중 내부 오류가 발생했습니다: %v", rec)
		}
		os.Remove(path)
		s.mu.Lock()
		if err != nil {
			// 오류에는 임시 파일명 대신 원본 파일명을 보인다.
			u.Status, u.Error = statusError, strings.ReplaceAll(err.Error(), filepath.Base(path), u.Name)
			log.Printf("읽기 실패: %s", u.Error)
		} else if wb != nil {
			u.Status, u.Percent, u.Sheets, u.wb = statusReady, 100, sheets, wb
		}
		s.mu.Unlock()
		s.work.Unlock()
		s.done()
	}()

	s.mu.Lock()
	stale := s.files[side] != u
	s.mu.Unlock()
	if stale {
		return
	}
	debug.FreeOSMemory() // 버린 이전 파일의 메모리를 돌려준다
	wb, err = reader.Read(path, func(done, total int) {
		s.mu.Lock()
		u.Percent = done * 100 / total
		s.mu.Unlock()
	})
	if err == nil {
		sheets = describeSheets(wb)
	}
}

// describeSheets 는 시트 목록과 키 열로 고를 수 있는 열을 만든다.
func describeSheets(wb *reader.Workbook) []sheetInfo {
	infos := make([]sheetInfo, len(wb.Sheets))
	for i, sh := range wb.Sheets {
		first := map[int]string{}
		for _, row := range sh.Rows {
			for _, c := range row.Cells {
				if _, ok := first[c.Col]; !ok {
					first[c.Col] = c.Display
				}
			}
		}
		cols := []columnInfo{}
		for num, header := range first {
			if h := []rune(header); len(h) > maxHeaderLen {
				header = string(h[:maxHeaderLen]) + "…"
			}
			letter, _ := excelize.ColumnNumberToName(num) // 읽은 열 번호는 Excel 범위 안이다
			cols = append(cols, columnInfo{num, letter, header})
		}
		slices.SortFunc(cols, func(a, b columnInfo) int { return a.Num - b.Num })
		infos[i] = sheetInfo{sh.Name, sh.Hidden, cols}
	}
	return infos
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pairs []pair `json:"pairs"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "잘못된 비교 요청입니다")
		return
	}
	if len(req.Pairs) == 0 {
		fail(w, http.StatusBadRequest, "비교할 시트를 하나 이상 선택하세요")
		return
	}
	pairs := make([]diff.SheetPair, len(req.Pairs))
	for i, p := range req.Pairs {
		pairs[i] = diff.SheetPair{Old: p.Old, New: p.New, OldKeyCol: p.OldKey, NewKeyCol: p.NewKey}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.Status == statusRunning {
		fail(w, http.StatusConflict, "이미 비교 중입니다")
		return
	}
	o, n := s.files[0], s.files[1]
	if o == nil || n == nil || o.wb == nil || n.wb == nil {
		fail(w, http.StatusConflict, "두 파일을 모두 올리고 읽기가 끝난 뒤에 비교하세요")
		return
	}
	j := &job{Status: statusRunning, Stage: "준비 중", start: time.Now()}
	s.job = j
	s.active++
	go s.compare(j, report.Options{
		Old: o.wb, New: n.wb, OldName: o.Name, NewName: n.Name, Pairs: pairs,
		OutDir: s.cfg.OutDir, FallbackDir: s.cfg.FallbackDir,
		Progress: func(percent int, stage string) {
			s.mu.Lock()
			j.Percent, j.Stage = percent, stage
			s.mu.Unlock()
		},
	})
	reply(w, http.StatusAccepted, map[string]any{})
}

func (s *Server) compare(j *job, o report.Options) {
	var out *report.Output
	var err error
	s.work.Lock()
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("비교 패닉: %v\n%s", rec, debug.Stack())
			err = fmt.Errorf("비교 중 내부 오류가 발생했습니다: %v", rec)
		}
		s.mu.Lock()
		j.ElapsedMs = time.Since(j.start).Milliseconds()
		if err != nil {
			log.Printf("비교 실패: %v", err)
			j.Status, j.Error = statusError, err.Error()
		} else {
			res := &result{Path: out.Path, OldName: o.OldName, NewName: o.NewName,
				Items: out.Items, Summary: out.Summary, Warnings: []string{}, WarningCount: len(out.Warnings)}
			for _, w := range out.Warnings[:min(len(out.Warnings), maxWarnings)] {
				res.Warnings = append(res.Warnings, w.String())
			}
			j.Status, j.Percent, j.Result = statusDone, 100, res
		}
		s.mu.Unlock()
		debug.FreeOSMemory() // 비교 결과가 쓰던 메모리를 돌려준다
		s.work.Unlock()
		s.done()
	}()
	debug.FreeOSMemory()
	out, err = s.run(o)
}

// handleOpen 은 서버가 만든 결과 파일 또는 그 폴더를 연다. 경로는 받지 않는다.
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	path := ""
	if s.job != nil && s.job.Result != nil {
		path = s.job.Result.Path
	}
	s.mu.Unlock()
	if path == "" {
		fail(w, http.StatusConflict, "열 결과 파일이 없습니다")
		return
	}
	switch r.URL.Query().Get("what") {
	case "file":
	case "folder":
		path = filepath.Dir(path)
	default:
		fail(w, http.StatusBadRequest, "잘못된 열기 요청입니다")
		return
	}
	if _, err := os.Stat(path); err != nil {
		fail(w, http.StatusNotFound, "파일이 없습니다. 옮기거나 지웠는지 확인하세요: "+path)
		return
	}
	if err := s.cfg.Open(path); err != nil {
		log.Printf("열기 실패(%s): %v", path, err)
		fail(w, http.StatusInternalServerError, "열지 못했습니다: "+err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]any{})
}
