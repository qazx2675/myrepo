// Package models defines the core domain types shared across the app.
package models

import "time"

// RBAC levels, per the plan's 5-tier table.
const (
	LevelViewer         = 1 // dashboard/report read-only
	LevelRequester       = 2 // submit VM creation requests
	LevelOperator         = 3 // run phases 1~3 (host register, vSwitch, VM create)
	LevelSeniorOperator = 4 // + affinity/lpage tuning, power policy, VM delete
	LevelAdmin          = 5 // + user mgmt, credential mgmt, audit log
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	RBACLevel    int
	CreatedAt    time.Time
}

type Credential struct {
	ID                     int64
	Name                   string
	VCenterIP              string
	VCID                   string
	EncryptedPassword      []byte
	Nonce                  []byte
	ESXiEncryptedPassword  []byte
	ESXiNonce              []byte
	CreatedBy              int64
	CreatedAt              time.Time
}

const (
	JobStatusPending = "pending"
	JobStatusRunning = "running"
	JobStatusSuccess = "success"
	JobStatusFailed  = "failed"
	JobStatusDryRun  = "dry_run" // M8: preview-only run, no binary actually executed
)

type Job struct {
	ID           int64
	Phase        string
	Status       string
	CredentialID int64
	WorklistFile string
	MapFile      string
	Params       string
	RequestedBy  int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
