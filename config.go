package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TemplateInfo represents a registered template file
type TemplateInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	FilePath  string `json:"file_path"`
	IsDefault bool   `json:"is_default"`
}

// AppConfig represents persistent settings stored in config.json
type AppConfig struct {
	WorkerName       string         `json:"worker_name"`
	MajorCategories  []string       `json:"major_categories"`
	MinorCategories  []string       `json:"minor_categories"`
	Templates        []TemplateInfo `json:"templates"`
	SelectedTemplate string         `json:"selected_template"`
	LastTargetFile   string         `json:"last_target_file"`
	LastDstFilename  string         `json:"last_dst_filename"`
	LastDetail       string         `json:"last_detail"`
}

// ConfigManager safely manages config.json in the executable's directory
type ConfigManager struct {
	mu         sync.RWMutex
	configPath string
	appDir     string
	cfg        AppConfig
}

// GetAppDir returns the directory containing the running executable or current working directory
func GetAppDir() string {
	cwd, errCwd := os.Getwd()
	exePath, errExe := os.Executable()
	if errExe == nil {
		dir := filepath.Dir(exePath)
		// If running via 'go run', exePath is in Temp/go-build
		if !strings.Contains(dir, "Temp") && !strings.Contains(dir, "go-build") {
			return dir
		}
	}
	if errCwd == nil && cwd != "" {
		return cwd
	}
	return "."
}

// NewConfigManager initializes or loads the config
func NewConfigManager() (*ConfigManager, error) {
	appDir := GetAppDir()
	configPath := filepath.Join(appDir, "config.json")
	templatesDir := filepath.Join(appDir, "templates")

	// Ensure templates directory exists
	_ = os.MkdirAll(templatesDir, 0755)

	cm := &ConfigManager{
		configPath: configPath,
		appDir:     appDir,
	}

	if err := cm.Load(); err != nil {
		// Initialize default configuration
		cm.cfg = AppConfig{
			WorkerName: "",
			MajorCategories: []string{
				"VM설치",
				"점검",
				"백업",
				"설정",
				"네트워크",
				"보안패치",
			},
			MinorCategories: []string{
				"공통",
				"서버",
				"스토리지",
				"네트워크",
				"데이터베이스",
				"보안",
			},
			Templates:        []TemplateInfo{},
			SelectedTemplate: "",
			LastTargetFile:   "target.xlsx",
			LastDstFilename:  "SOP_완료.xlsx",
			LastDetail:       "",
		}
		_ = cm.Save()
	}

	// Ensure default template is present in templates folder and registered
	cm.ensureDefaultTemplateRegistered()

	return cm, nil
}

// ensureDefaultTemplateRegistered checks if default_template exists and is registered
func (cm *ConfigManager) ensureDefaultTemplateRegistered() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	defaultPath := filepath.Join(cm.appDir, "templates", "default_template.xlsx")
	hasDefault := false
	for _, t := range cm.cfg.Templates {
		if t.ID == "default_template" || t.IsDefault {
			hasDefault = true
			break
		}
	}

	if !hasDefault {
		cm.cfg.Templates = append([]TemplateInfo{
			{
				ID:        "default_template",
				Name:      "기본 표준 양식 (B8,C8,G8,H8)",
				FilePath:  defaultPath,
				IsDefault: true,
			},
		}, cm.cfg.Templates...)

		if cm.cfg.SelectedTemplate == "" {
			cm.cfg.SelectedTemplate = "default_template"
		}
		_ = cm.saveLocked()
	}
}

// Load reads config.json from disk
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return err
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	cm.cfg = cfg
	return nil
}

// Save writes current config to config.json
func (cm *ConfigManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.saveLocked()
}

func (cm *ConfigManager) saveLocked() error {
	data, err := json.MarshalIndent(cm.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.configPath, data, 0644)
}

// GetConfig returns a copy of current configuration
func (cm *ConfigManager) GetConfig() AppConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.cfg
}

// SetWorkerName updates and saves worker name
func (cm *ConfigManager) SetWorkerName(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cfg.WorkerName = name
	return cm.saveLocked()
}

// AddMajorCategory adds a major category if not already present
func (cm *ConfigManager) AddMajorCategory(cat string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cat == "" {
		return nil
	}
	for _, c := range cm.cfg.MajorCategories {
		if c == cat {
			return nil
		}
	}
	cm.cfg.MajorCategories = append(cm.cfg.MajorCategories, cat)
	return cm.saveLocked()
}

// AddMinorCategory adds a minor category if not already present
func (cm *ConfigManager) AddMinorCategory(cat string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cat == "" {
		return nil
	}
	for _, c := range cm.cfg.MinorCategories {
		if c == cat {
			return nil
		}
	}
	cm.cfg.MinorCategories = append(cm.cfg.MinorCategories, cat)
	return cm.saveLocked()
}

// AddTemplate adds or updates a template in config
func (cm *ConfigManager) AddTemplate(id, name, path string, isDefault bool) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	found := false
	for i, t := range cm.cfg.Templates {
		if t.ID == id || t.FilePath == path {
			cm.cfg.Templates[i] = TemplateInfo{
				ID:        id,
				Name:      name,
				FilePath:  path,
				IsDefault: isDefault,
			}
			found = true
			break
		}
	}
	if !found {
		cm.cfg.Templates = append(cm.cfg.Templates, TemplateInfo{
			ID:        id,
			Name:      name,
			FilePath:  path,
			IsDefault: isDefault,
		})
	}
	cm.cfg.SelectedTemplate = id
	return cm.saveLocked()
}

// SelectTemplate sets the active template ID
func (cm *ConfigManager) SelectTemplate(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cfg.SelectedTemplate = id
	return cm.saveLocked()
}

// UpdateLastUsed updates recently used inputs
func (cm *ConfigManager) UpdateLastUsed(targetFile, dstFilename, detail string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if targetFile != "" {
		cm.cfg.LastTargetFile = targetFile
	}
	if dstFilename != "" {
		cm.cfg.LastDstFilename = dstFilename
	}
	cm.cfg.LastDetail = detail
	return cm.saveLocked()
}
