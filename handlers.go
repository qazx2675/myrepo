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

// Handler: GET/POST /api/config
type ConfigUpdateRequest struct {
	WorkerName   *string `json:"worker_name"`
	VerifierName *string `json:"verifier_name"`
	TargetObject *string `json:"target_object"`
}

func (s *ServerState) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"config":  s.cfgMgr.GetConfig(),
			"app_dir": GetAppDir(),
		})
	case http.MethodPost:
		var req ConfigUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
			return
		}
		if req.WorkerName != nil {
			name := strings.TrimSpace(*req.WorkerName)
			if name == "" {
				respondError(w, http.StatusBadRequest, "작업자 이름을 입력해주세요.")
				return
			}
			if err := s.cfgMgr.SetWorkerName(name); err != nil {
				respondError(w, http.StatusInternalServerError, "작업자 이름 저장 실패: "+err.Error())
				return
			}
		}
		if req.VerifierName != nil || req.TargetObject != nil {
			cfg := s.cfgMgr.GetConfig()
			verifier, object := cfg.VerifierName, cfg.TargetObject
			if req.VerifierName != nil {
				verifier = strings.TrimSpace(*req.VerifierName)
			}
			if req.TargetObject != nil {
				object = strings.TrimSpace(*req.TargetObject)
			}
			if err := s.cfgMgr.UpdateInputs(verifier, object, ""); err != nil {
				respondError(w, http.StatusInternalServerError, "설정 저장 실패: "+err.Error())
				return
			}
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"config":  s.cfgMgr.GetConfig(),
		})
	default:
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// Handler: POST /api/target/load (JSON {target_path} or multipart target_file)
func (s *ServerState) handleLoadTarget(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var targetPath string
	locked := false // 업로드 파일 쓰기~스캔 동안 muGen 보유 (생성 중 Target 덮어쓰기 방지)
	defer func() {
		if locked {
			s.muGen.Unlock()
		}
	}()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		// 32 MB max
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			respondError(w, http.StatusBadRequest, "파일 업로드 처리 실패: "+err.Error())
			return
		}
		file, header, err := r.FormFile("target_file")
		if err != nil {
			respondError(w, http.StatusBadRequest, "SOP 파일 수신 실패: "+err.Error())
			return
		}
		defer file.Close()

		name := filepath.Base(header.Filename)
		if !strings.HasSuffix(strings.ToLower(name), ".xlsx") {
			respondError(w, http.StatusBadRequest, "엑셀 파일(.xlsx)만 업로드 가능합니다.")
			return
		}

		targetPath = filepath.Join(GetAppDir(), name)
		s.muGen.Lock()
		locked = true
		dst, err := os.Create(targetPath)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "파일 저장 실패: "+err.Error())
			return
		}
		if _, err := io.Copy(dst, file); err != nil {
			dst.Close()
			respondError(w, http.StatusInternalServerError, "파일 쓰기 실패: "+err.Error())
			return
		}
		if err := dst.Close(); err != nil {
			respondError(w, http.StatusInternalServerError, "파일 저장 실패: "+err.Error())
			return
		}
	} else {
		var req struct {
			TargetPath string `json:"target_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
			return
		}
		targetPath = strings.TrimSpace(req.TargetPath)
		if targetPath == "" {
			targetPath = "target.xlsx"
		}
		if !filepath.IsAbs(targetPath) {
			targetPath = filepath.Join(GetAppDir(), targetPath)
		}
		if _, err := os.Stat(targetPath); err != nil {
			respondError(w, http.StatusNotFound, fmt.Sprintf("SOP 파일이 존재하지 않습니다: %s", targetPath))
			return
		}
	}

	if !locked {
		s.muGen.Lock()
		locked = true
	}
	sheets, err := ScanTargetSheets(targetPath)
	s.muGen.Unlock()
	locked = false
	if err != nil {
		respondError(w, http.StatusInternalServerError, "시트 스캔 실패: "+err.Error())
		return
	}
	if sheets == nil {
		sheets = []TargetSheetInfo{}
	}

	cfg := s.cfgMgr.GetConfig()
	_ = s.cfgMgr.UpdateInputs(cfg.VerifierName, cfg.TargetObject, targetPath)

	respondJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"target_path": targetPath,
		"sheets":      sheets,
		"count":       len(sheets),
	})
}

// Handler: POST /api/generate
func (s *ServerState) handleGenerateSOP(w http.ResponseWriter, r *http.Request) {
	s.recordHeartbeat()
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	s.muGen.Lock()
	defer s.muGen.Unlock()

	var req SOPGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "요청 파싱 실패: "+err.Error())
		return
	}

	cfg := s.cfgMgr.GetConfig()
	req.WorkerName = strings.TrimSpace(req.WorkerName)
	if req.WorkerName == "" {
		req.WorkerName = cfg.WorkerName
	}
	if strings.TrimSpace(req.WorkerName) == "" {
		respondError(w, http.StatusBadRequest, "작업자 이름을 먼저 등록해주세요.")
		return
	}
	if len(req.SelectedSheets) == 0 {
		respondError(w, http.StatusBadRequest, "SOP에 포함할 시트를 1개 이상 선택해주세요.")
		return
	}
	if strings.TrimSpace(req.TargetPath) == "" {
		req.TargetPath = "target.xlsx"
	}
	req.VerifierName = strings.TrimSpace(req.VerifierName)
	req.TargetObject = strings.TrimSpace(req.TargetObject)

	res, err := GenerateSOPExcel(req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "SOP 엑셀 생성 실패: "+err.Error())
		return
	}

	_ = s.cfgMgr.UpdateInputs(req.VerifierName, req.TargetObject, req.TargetPath)

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
		respondError(w, http.StatusInternalServerError, "샘플 SOP 파일 생성 실패: "+err.Error())
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
		filePath = filepath.Join(GetAppDir(), "SOP_결과본.xlsx")
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
