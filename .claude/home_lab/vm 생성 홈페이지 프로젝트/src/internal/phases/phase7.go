package phases

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"vm-portal/internal/config"
	"vm-portal/internal/models"
	"vm-portal/internal/secrets"
)

// Phase7Input mirrors power_setting's flags: -id -vcTargetIP -worklistFile.
// This is a destructive action (changes host power management policy) — RBAC
// level 4 and a confirmation step are enforced at the HTTP handler.
type Phase7Input struct {
	Hosts  []string // ESXi (physical host) names, not VM names
	DryRun bool      // M8: preview the worklist/command without executing power_setting
}

// RunPhase7 applies the "High Performance" power policy to hosts
// (main_power_policy.txt / binary "power_setting"), RBAC level 4.
func RunPhase7(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase7Input, requestedBy int64) (jobID int64, res *Result, err error) {
	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist_bm", in.Hosts)
	if err != nil {
		return 0, nil, fmt.Errorf("worklist 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	jobID, err = CreateJob(db, "phase7_power_policy", cred.ID, worklistName, "", "", requestedBy)
	if err != nil {
		return 0, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
	}

	if in.DryRun {
		return jobID, dryRunPreview(db, jobID, "power_setting", args, in.Hosts), nil
	}

	env := []string{"VC_PASSWORD=" + cred.VCPassword}
	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "power_setting"), cfg.TmpDir, args, env)
	return jobID, res, err
}

// dryRunPreview logs what a destructive phase *would* do — the binary path,
// its args (password omitted), and the affected host/VM list — without
// invoking exec.Command at all, then marks the job dry_run instead of
// running/success/failed. This intentionally does not validate against
// vCenter (that would mean reimplementing each binary's lookup logic, which
// is exactly what the "wrap, don't rewrite" approach avoids); it is a
// preview of the command that would run, not a vCenter-side dry-run.
func dryRunPreview(db *sql.DB, jobID int64, binName string, args []string, targets []string) *Result {
	lines := []string{
		"[DRY-RUN] 실제로 실행되지 않았습니다. 아래는 예정된 작업 미리보기입니다.",
		fmt.Sprintf("[DRY-RUN] 실행 파일: %s", binName),
		fmt.Sprintf("[DRY-RUN] 인자: %v", args),
		fmt.Sprintf("[DRY-RUN] 대상 (%d개): %v", len(targets), targets),
	}
	for _, l := range lines {
		appendLog(db, jobID, l)
	}
	_ = setJobStatus(db, jobID, models.JobStatusDryRun)
	return &Result{Success: true, Lines: lines, DryRun: true}
}
