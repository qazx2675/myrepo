package phases

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"vm-portal/internal/config"
	"vm-portal/internal/secrets"
)

// Phase9Input mirrors report's flags: -id -vcTargetIP -worklistFile -outCsv.
// Unlike Phase 5/6, the report binary matches hosts by trying both the exact
// worklist entry and the first "."-label (see main_report.go's targetHosts
// loop: strings.EqualFold(hostClean, cleanName) || host.Name == line), so
// like Phase 3/4 it needs no pre-truncation here.
type Phase9Input struct {
	Hosts []string
}

// RunPhase9 pulls a VM/PCI configuration inventory CSV across the given BM
// hosts (main_report.txt / binary "report"), RBAC level 1 (read-only, per
// the plan's RBAC table). The CSV the binary writes into its working
// directory is moved into cfg.ReportsDir (not deleted like the scratch
// worklist file) so it survives for later download via the job id.
func RunPhase9(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase9Input, requestedBy int64) (jobID int64, res *Result, reportFile string, err error) {
	jobID, err = CreateJob(db, "phase9_report", cred.ID, "", "", "", requestedBy)
	if err != nil {
		return 0, nil, "", err
	}

	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist", in.Hosts)
	if err != nil {
		return jobID, nil, "", fmt.Errorf("worklist 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	csvName := fmt.Sprintf("report_job%d.csv", jobID)

	if _, err := db.Exec(`UPDATE jobs SET worklist_file = ? WHERE id = ?`, worklistName, jobID); err != nil {
		return jobID, nil, "", err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
		"-outCsv", csvName,
	}
	env := []string{"VC_PASSWORD=" + cred.VCPassword}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "report"), cfg.TmpDir, args, env)
	if err != nil {
		return jobID, res, "", err
	}

	tmpCSVPath := filepath.Join(cfg.TmpDir, csvName)
	if _, statErr := os.Stat(tmpCSVPath); statErr != nil {
		// Binary ran but produced no CSV (e.g. 0 matched hosts) — not an error,
		// just nothing to download.
		return jobID, res, "", nil
	}

	if err := os.MkdirAll(cfg.ReportsDir, 0700); err != nil {
		return jobID, res, "", fmt.Errorf("리포트 디렉터리 생성 실패: %w", err)
	}
	finalPath := filepath.Join(cfg.ReportsDir, csvName)
	if err := os.Rename(tmpCSVPath, finalPath); err != nil {
		return jobID, res, "", fmt.Errorf("리포트 파일 이동 실패: %w", err)
	}

	if _, err := db.Exec(`UPDATE jobs SET mapfile = ? WHERE id = ?`, csvName, jobID); err != nil {
		return jobID, res, "", err
	}

	return jobID, res, csvName, nil
}
