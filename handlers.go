package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ServerState struct {
	cfgMgr        *ConfigManager
	lastHeartbeat atomic.Int64
	exitChan      chan struct{}
	onceExit      sync.Once
	muGen         sync.Mutex
}

func NewServerState(cfgMgr *ConfigManager) *ServerState {
	s := &ServerState{
		cfgMgr:   cfgMgr,
		exitChan: make(chan struct{}),
	}
	s.lastHeartbeat.Store(time.Now().Unix())
	return s
}

func (s *ServerState) recordHeartbeat() {
	s.lastHeartbeat.Store(time.Now().Unix())
}

func (s *ServerState) requestExit() {
	s.onceExit.Do(func() {
		close(s.exitChan)
	})
}

// JSON response helpers
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]any{
		"success": false,
		"message": msg,
	})
}

// Handler: GET /api/config
func (s *ServerState) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	cfg := s.cfgMgr.GetConfig()
	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"config":  cfg,
		"app_dir": GetAppDir(),
	})
}

// Handler: POST /api/config/worker
type WorkerRequest struct {
	WorkerName string `json:"worker_name"`
}

func (s *ServerState) handleSetWorker(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req WorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.WorkerName)
	if name == "" {
		respondError(w, http.StatusBadRequest, "작업자 이름을 입력해주세요.")
		return
	}

	if err := s.cfgMgr.SetWorkerName(name); err != nil {
		respondError(w, http.StatusInternalServerError, "작업자 이름 저장 실패: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"message":     "작업자 이름이 저장되었습니다.",
		"worker_name": name,
	})
}

// Handler: POST /api/config/category
type CategoryRequest struct {
	Type string `json:"type"` // "major" or "minor"
	Name string `json:"name"`
}

func (s *ServerState) handleAddCategory(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req CategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		respondError(w, http.StatusBadRequest, "분류명을 입력해주세요.")
		return
	}

	if req.Type == "major" {
		_ = s.cfgMgr.AddMajorCategory(name)
	} else if req.Type == "minor" {
		_ = s.cfgMgr.AddMinorCategory(name)
	} else {
		respondError(w, http.StatusBadRequest, "유효하지 않은 분류 타입입니다.")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"config":  s.cfgMgr.GetConfig(),
	})
}

// Handler: GET /api/target/sheets
func (s *ServerState) handleScanTargetSheets(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	targetFile := r.URL.Query().Get("target")
	if targetFile == "" {
		targetFile = "target.xlsx"
	}

	appDir := GetAppDir()
	targetPath := targetFile
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(appDir, targetPath)
	}

	if _, err := os.Stat(targetPath); err != nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("Target 파일이 존재하지 않습니다: %s", targetPath))
		return
	}

	sheets, err := ScanTargetSheets(targetPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "시트 스캔 실패: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"target_file": targetFile,
		"target_path": targetPath,
		"sheets":      sheets,
		"count":       len(sheets),
	})
}

// Handler: POST /api/templates/create-default
func (s *ServerState) handleCreateDefaultTemplate(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	appDir := GetAppDir()
	tmplPath := filepath.Join(appDir, "templates", "default_template.xlsx")

	if err := CreateDefaultTemplate(tmplPath); err != nil {
		respondError(w, http.StatusInternalServerError, "기본 양식 파일 생성 실패: "+err.Error())
		return
	}

	_ = s.cfgMgr.AddTemplate("default_template", "기본 표준 양식 (B8,C8,G8,H8)", tmplPath, true)

	respondJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "기본 양식(default_template.xlsx)이 성공적으로 생성되었습니다.",
		"file_path": tmplPath,
		"templates": s.cfgMgr.GetConfig().Templates,
	})
}

// Handler: POST /api/templates/upload
func (s *ServerState) handleUploadTemplate(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 32 MB max
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "파일 업로드 처리 실패: "+err.Error())
		return
	}

	file, header, err := r.FormFile("template_file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "양식 파일 수신 실패: "+err.Error())
		return
	}
	defer file.Close()

	name := header.Filename
	if !strings.HasSuffix(strings.ToLower(name), ".xlsx") {
		respondError(w, http.StatusBadRequest, "엑셀 파일(.xlsx)만 업로드 가능합니다.")
		return
	}

	appDir := GetAppDir()
	templatesDir := filepath.Join(appDir, "templates")
	_ = os.MkdirAll(templatesDir, 0755)

	destPath := filepath.Join(templatesDir, name)
	dst, err := os.Create(destPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "파일 저장 실패: "+err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		respondError(w, http.StatusInternalServerError, "파일 쓰기 실패: "+err.Error())
		return
	}

	tmplID := "custom_" + strings.TrimSuffix(name, filepath.Ext(name))
	displayName := strings.TrimSuffix(name, filepath.Ext(name))
	_ = s.cfgMgr.AddTemplate(tmplID, displayName, destPath, false)

	respondJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"message":     fmt.Sprintf("양식 파일 '%s' 업로드가 완료되었습니다.", name),
		"template_id": tmplID,
		"file_path":   destPath,
		"templates":   s.cfgMgr.GetConfig().Templates,
	})
}

// Handler: POST /api/templates/select
type SelectTemplateRequest struct {
	TemplateID string `json:"template_id"`
}

func (s *ServerState) handleSelectTemplate(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	var req SelectTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
		return
	}

	if err := s.cfgMgr.SelectTemplate(req.TemplateID); err != nil {
		respondError(w, http.StatusInternalServerError, "양식 선택 저장 실패: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"config":  s.cfgMgr.GetConfig(),
	})
}

// Handler: POST /api/generate
func (s *ServerState) handleGenerateSOP(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	s.muGen.Lock()
	defer s.muGen.Unlock()

	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req SOPGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
		return
	}

	if strings.TrimSpace(req.MajorCategory) == "" {
		respondError(w, http.StatusBadRequest, "대분류를 선택하거나 직접 입력해주세요.")
		return
	}
	if strings.TrimSpace(req.MinorCategory) == "" {
		respondError(w, http.StatusBadRequest, "중분류를 선택하거나 직접 입력해주세요.")
		return
	}
	if len(req.SelectedSheets) == 0 {
		respondError(w, http.StatusBadRequest, "작업 순서에 포함할 시트를 1개 이상 선택해주세요.")
		return
	}

	// Resolve template path
	appDir := GetAppDir()
	cfg := s.cfgMgr.GetConfig()
	templatePath := req.TemplatePath
	if templatePath == "" {
		for _, t := range cfg.Templates {
			if t.ID == cfg.SelectedTemplate {
				templatePath = t.FilePath
				break
			}
		}
	}
	if templatePath == "" {
		templatePath = filepath.Join(appDir, "templates", "default_template.xlsx")
	}
	req.TemplatePath = templatePath

	if req.TargetPath == "" {
		req.TargetPath = "target.xlsx"
	}

	if req.DstFilename == "" {
		req.DstFilename = "SOP_완료.xlsx"
	}

	// Auto-persist new categories if typed
	_ = s.cfgMgr.AddMajorCategory(req.MajorCategory)
	_ = s.cfgMgr.AddMinorCategory(req.MinorCategory)
	_ = s.cfgMgr.UpdateLastUsed(req.TargetPath, req.DstFilename, req.DetailText)

	// Execute SOP Generation
	res, err := GenerateSOPExcel(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SOP 엑셀 생성 실패: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": res.Message,
		"result":  res,
		"config":  s.cfgMgr.GetConfig(),
	})
}

// Handler: POST /api/create-sample-target
func (s *ServerState) handleCreateSampleTarget(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	appDir := GetAppDir()
	targetPath := filepath.Join(appDir, "target.xlsx")

	if err := CreateSampleTarget(targetPath); err != nil {
		respondError(w, http.StatusInternalServerError, "샘플 Target 파일 생성 실패: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"message":   "3개 시트가 포함된 테스트용 target.xlsx 파일이 성공적으로 생성되었습니다.",
		"file_path": targetPath,
	})
}

// Handler: POST /api/open-file
type OpenFileRequest struct {
	Path       string `json:"path"`
	OpenFolder bool   `json:"open_folder"`
}

func (s *ServerState) handleOpenFile(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	var req OpenFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
		return
	}

	filePath := req.Path
	if filePath == "" {
		filePath = filepath.Join(GetAppDir(), "SOP_완료.xlsx")
	}
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(GetAppDir(), filePath)
	}

	if req.OpenFolder {
		cmd := exec.Command("explorer.exe", "/select,", filePath)
		_ = cmd.Start()
	} else {
		cmd := exec.Command("cmd", "/c", "start", "", filePath)
		_ = cmd.Start()
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"path":    filePath,
	})
}

// Handler: POST /api/exit
func (s *ServerState) handleExit(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "프로그램을 종료합니다.",
	})
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.requestExit()
	}()
}
