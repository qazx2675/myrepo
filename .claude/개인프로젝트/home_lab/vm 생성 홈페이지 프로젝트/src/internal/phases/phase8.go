package phases

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"vm-portal/internal/config"
	"vm-portal/internal/secrets"
)

// Phase8Input mirrors vm_delete's flags: -id -vcTargetIP -worklistFile -confirm.
// This targets exact VM names (not BM host names) deliberately — deletion is
// the single most destructive action in the portal, so it should never guess
// which VMs a host name expands to.
type Phase8Input struct {
	VMNames []string
	DryRun  bool // M8: preview the delete list/command without executing vm_delete
}

// RunPhase8 powers off (if needed) and destroys VMs (new development — no
// equivalent CLI script existed; see main_vm_delete.go / binary "vm_delete"),
// RBAC level 4. The binary itself also requires -confirm=DELETE as a second,
// independent safety check beyond the HTTP handler's confirmation gate.
func RunPhase8(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase8Input, requestedBy int64) (jobID int64, res *Result, err error) {
	worklistPath, err := WriteTempFile(cfg.TmpDir, "delete_list", in.VMNames)
	if err != nil {
		return 0, nil, fmt.Errorf("삭제 목록 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	jobID, err = CreateJob(db, "phase8_vm_delete", cred.ID, worklistName, "", "", requestedBy)
	if err != nil {
		return 0, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
		"-confirm", "DELETE",
	}

	if in.DryRun {
		return jobID, dryRunPreview(db, jobID, "vm_delete", args, in.VMNames), nil
	}

	env := []string{"VC_PASSWORD=" + cred.VCPassword}
	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "vm_delete"), cfg.TmpDir, args, env)
	return jobID, res, err
}
