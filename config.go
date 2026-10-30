package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AppConfig represents persistent settings stored in config.json
type AppConfig struct {
	WorkerName   string `json:"worker_name"`
	VerifierName string `json:"verifier_name"`
	TargetObject string `json:"target_object"`
	LastTarget   string `json:"last_target"`
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

	cm := &ConfigManager{
		configPath: configPath,
		appDir:     appDir,
	}

	if err := cm.Load(); err != nil {
		// Initialize default configuration
		cm.cfg = AppConfig{
			WorkerName:   "",
			VerifierName: "",
			TargetObject: "",
			LastTarget:   "target.xlsx",
		}
		_ = cm.Save()
	}

	return cm, nil
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

	if cfg.LastTarget == "" {
		cfg.LastTarget = "target.xlsx"
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

// UpdateInputs updates verifier, target object and last target (empty lastTarget keeps the current value)
func (cm *ConfigManager) UpdateInputs(verifier, targetObject, lastTarget string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cfg.VerifierName = verifier
	cm.cfg.TargetObject = targetObject
	if lastTarget != "" {
		cm.cfg.LastTarget = lastTarget
	}
	return cm.saveLocked()
}
