package phases

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"vm-portal/internal/config"
	"vm-portal/internal/secrets"
)

// Phase1Input mirrors vmc's flags: -id -vcTargetIP -folderName -worklistFile.
type Phase1Input struct {
	FolderName string   // target folder/cluster/datacenter name
	Hosts      []string // ESXi host list -> worklist_<rand>.txt
}

// RunPhase1 registers ESXi hosts (main_conn.txt / binary "vmc"), RBAC level 3.
//
// The binary resolves -worklistFile relative to its own working directory
// (filepath.Join(os.Getwd(), *worklistFile)), so we set cmd.Dir to the temp
// dir and pass just the bare filename rather than an absolute path.
func RunPhase1(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase1Input, requestedBy int64) (jobID int64, res *Result, err error) {
	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist", in.Hosts)
	if err != nil {
		return 0, nil, fmt.Errorf("worklist 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	jobID, err = CreateJob(db, "phase1_host_register", cred.ID, worklistName, "", in.FolderName, requestedBy)
	if err != nil {
		return 0, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-folderName", in.FolderName,
		"-worklistFile", worklistName,
	}
	env := []string{
		"VC_PASSWORD=" + cred.VCPassword,
		"ESXI_PASSWORD=" + cred.ESXiPassword,
	}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "vmc"), cfg.TmpDir, args, env)
	return jobID, res, err
}
