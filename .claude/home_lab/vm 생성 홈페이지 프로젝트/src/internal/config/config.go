// Package config loads the portal's runtime configuration from environment variables.
package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	ListenAddr    string // e.g. ":8443"
	DBPath        string
	BinDir        string // directory holding the phase binaries (vmc, vswitch_setting, vm_create_v2, ...)
	TmpDir        string // worklist/mapfile scratch space
	ReportsDir    string // Phase 9 CSV report output (persistent, unlike TmpDir scratch files)
	MasterKeyFile string // 0600 file holding the AES-256 master key for credential encryption, when SecretBackend == "file"

	// SecretBackend selects where the AES-256 master key comes from: "file"
	// (default — local 0600 file, see secrets.LoadOrCreateMasterKey) or
	// "vault" (HashiCorp Vault KV v2, see secrets.LoadMasterKeyFromVault).
	// This is M9's "옵션" — the file backend remains the default and is all
	// that's required for a single-host deployment like this one.
	SecretBackend  string
	VaultAddr      string // e.g. "https://vault.example.com:8200"
	VaultToken     string
	VaultSecretPath string // KV v2 path, e.g. "secret/data/vm-portal/master-key"
	VaultKeyField  string // field name inside the KV v2 secret holding the base64 key, default "key"
}

func Load() Config {
	base := getenv("VMPORTAL_HOME", "/root/vm/portal")
	return Config{
		ListenAddr:    getenv("VMPORTAL_ADDR", ":8443"),
		DBPath:        getenv("VMPORTAL_DB", filepath.Join(base, "data", "portal.db")),
		BinDir:        getenv("VMPORTAL_BIN_DIR", filepath.Join(base, "bin")),
		TmpDir:        getenv("VMPORTAL_TMP_DIR", filepath.Join(base, "tmp")),
		ReportsDir:    getenv("VMPORTAL_REPORTS_DIR", filepath.Join(base, "data", "reports")),
		MasterKeyFile: getenv("VMPORTAL_MASTER_KEY_FILE", filepath.Join(base, "data", "master.key")),

		SecretBackend:   getenv("VMPORTAL_SECRET_BACKEND", "file"),
		VaultAddr:       getenv("VAULT_ADDR", ""),
		VaultToken:      getenv("VAULT_TOKEN", ""),
		VaultSecretPath: getenv("VMPORTAL_VAULT_SECRET_PATH", "secret/data/vm-portal/master-key"),
		VaultKeyField:   getenv("VMPORTAL_VAULT_KEY_FIELD", "key"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
