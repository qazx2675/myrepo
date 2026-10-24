// Package storage 는 ./data/ 또는 네트워크 공유 폴더(\\server\share)에
// 레이아웃 JSON 과 호스트 CSV 를 원자적으로 저장/로드한다.
package storage

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	layoutFile = "datacenter_layout.json"
	csvFile    = "allocated_hosts.csv"
	configFile = "data_path.txt"
)

// Store 는 데이터 디렉터리 기반 파일 저장소 (로컬 또는 Windows 네트워크 공유 폴더).
type Store struct {
	dir    string
	exeDir string
	mu     sync.Mutex
}

// New 는 디렉터리를 만들고 Store 를 반환한다.
func New(dir string, exeDir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("데이터 폴더 생성 실패 (%s): %w", dir, err)
	}
	return &Store{dir: dir, exeDir: exeDir}, nil
}

// Dir 은 데이터 디렉터리 경로.
func (s *Store) Dir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir
}

// SetDir 은 런타임에 데이터 폴더(네트워크 공유 폴더 등)를 전환하고 설정 파일(data_path.txt)에 영구 저장한다.
func (s *Store) SetDir(newDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanDir := strings.TrimSpace(newDir)
	if cleanDir == "" {
		return fmt.Errorf("폴더 경로가 비어있습니다")
	}

	// 디렉터리 존재 여부 확인 및 생성
	if err := os.MkdirAll(cleanDir, 0o755); err != nil {
		return fmt.Errorf("공유/데이터 폴더 접근 또는 생성 실패 (%s): %w", cleanDir, err)
	}

	// 읽기/쓰기 권한 테스트용 프로브 파일 생성 및 제거
	probeFile := filepath.Join(cleanDir, ".probe_write_test.tmp")
	if err := os.WriteFile(probeFile, []byte("ok"), 0o644); err != nil {
		return fmt.Errorf("공유 폴더 쓰기 권한이 없습니다 (%s): %w", cleanDir, err)
	}
	_ = os.Remove(probeFile)

	s.dir = cleanDir

	// 설정 파일(data_path.txt)에 저장하여 다음 실행 시에도 기억
	_ = s.saveConfigPath(cleanDir)
	return nil
}

func (s *Store) saveConfigPath(targetDir string) error {
	if s.exeDir != "" {
		p := filepath.Join(s.exeDir, configFile)
		if err := os.WriteFile(p, []byte(targetDir), 0o644); err == nil {
			return nil
		}
	}
	appData := os.Getenv("APPDATA")
	if appData != "" {
		appDir := filepath.Join(appData, "datacenter-viewer")
		_ = os.MkdirAll(appDir, 0o755)
		return os.WriteFile(filepath.Join(appDir, configFile), []byte(targetDir), 0o644)
	}
	return nil
}

// LoadConfigPath 는 data_path.txt 에 저장된 공유 폴더 경로를 로드한다.
func LoadConfigPath(exeDir string) string {
	if exeDir != "" {
		p := filepath.Join(exeDir, configFile)
		if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return string(bytes.TrimSpace(b))
		}
	}
	appData := os.Getenv("APPDATA")
	if appData != "" {
		p := filepath.Join(appData, "datacenter-viewer", configFile)
		if b, err := os.ReadFile(p); err == nil && len(bytes.TrimSpace(b)) > 0 {
			return string(bytes.TrimSpace(b))
		}
	}
	return ""
}

// Info 는 현재 데이터 디렉터리 상태 메타데이터를 반환한다.
func (s *Store) Info() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dir
	isUNC := strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, `//`)
	_, errLayout := os.Stat(filepath.Join(dir, layoutFile))
	_, errCsv := os.Stat(filepath.Join(dir, csvFile))

	return map[string]any{
		"dir":            dir,
		"isNetworkShare": isUNC,
		"hasLayout":      errLayout == nil,
		"hasCsv":         errCsv == nil,
	}
}

func (s *Store) read(name string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

// Load 는 저장된 레이아웃 JSON 과 CSV 텍스트를 반환한다.
func (s *Store) Load() (layout []byte, csv string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if layout, err = s.read(layoutFile); err != nil {
		return nil, "", err
	}
	c, err := s.read(csvFile)
	return layout, string(c), err
}

// Save 는 레이아웃과 CSV 를 Windows 네트워크 공유 폴더에서도 파일 락 충돌 없이 안전하게 원자적 저장한다.
func (s *Store) Save(layout []byte, csv string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(layout) > 0 {
		if err := s.atomicWrite(layoutFile, layout); err != nil {
			return err
		}
	}
	return s.atomicWrite(csvFile, []byte(csv))
}

// CSV 는 현재 저장된 CSV 텍스트.
func (s *Store) CSV() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read(csvFile)
	return string(b), err
}

// atomicWrite 는 임시 파일 작성 후 교체하며, Windows SMB 공유 폴더의 Rename 락 오류 시 직접 덮어쓰기로 폴백한다.
func (s *Store) atomicWrite(name string, data []byte) error {
	target := filepath.Join(s.dir, name)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return os.WriteFile(target, data, 0o644)
	}
	_ = os.Remove(target)
	if err := os.Rename(tmp, target); err != nil {
		if writeErr := os.WriteFile(target, data, 0o644); writeErr == nil {
			_ = os.Remove(tmp)
			return nil
		}
		return err
	}
	return nil
}
