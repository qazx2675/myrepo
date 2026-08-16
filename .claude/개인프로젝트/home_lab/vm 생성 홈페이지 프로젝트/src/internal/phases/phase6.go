package phases

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"

	"vm-portal/internal/config"
	"vm-portal/internal/secrets"
)

// Phase6Input mirrors lpage_setting's flags: -id -vcTargetIP -worklistFile
// -ev01Cores -ev01Sockets -ev02Cores -ev02Sockets (all required, sockets != 0).
type Phase6Input struct {
	Hosts       []string
	EV01Cores   int
	EV01Sockets int
	EV02Cores   int
	EV02Sockets int
}

// RunPhase6 injects 1GB HugePage / NUMA VMX tuning (main_lpage.txt / binary
// "lpage_setting"), RBAC level 4.
func RunPhase6(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase6Input, requestedBy int64) (jobID int64, res *Result, err error) {
	if in.EV01Sockets == 0 || in.EV02Sockets == 0 {
		return 0, nil, fmt.Errorf("소켓 수는 0이 될 수 없습니다")
	}

	params := fmt.Sprintf("ev01=%d/%d ev02=%d/%d", in.EV01Cores, in.EV01Sockets, in.EV02Cores, in.EV02Sockets)
	jobID, err = CreateJob(db, "phase6_lpage", cred.ID, "", "", params, requestedBy)
	if err != nil {
		return 0, nil, err
	}

	// Unlike Phase 3/4, lpage_setting does NOT split on "." — it matches VMs
	// by exact "<worklist entry>ev01"/"ev02". Since Phase 3 names VMs using
	// only the first dot-label, this phase must pre-truncate to match, or
	// every host here silently logs "대상 VM이 존재하지 않습니다" and skips.
	hosts := NormalizeHosts(db, jobID, in.Hosts)

	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist", hosts)
	if err != nil {
		return jobID, nil, fmt.Errorf("worklist 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	if _, err := db.Exec(`UPDATE jobs SET worklist_file = ? WHERE id = ?`, worklistName, jobID); err != nil {
		return jobID, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
		"-ev01Cores", strconv.Itoa(in.EV01Cores),
		"-ev01Sockets", strconv.Itoa(in.EV01Sockets),
		"-ev02Cores", strconv.Itoa(in.EV02Cores),
		"-ev02Sockets", strconv.Itoa(in.EV02Sockets),
	}
	env := []string{"VC_PASSWORD=" + cred.VCPassword}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "lpage_setting"), cfg.TmpDir, args, env)
	return jobID, res, err
}
