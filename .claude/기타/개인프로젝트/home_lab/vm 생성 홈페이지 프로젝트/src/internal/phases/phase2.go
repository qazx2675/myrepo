package phases

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"vm-portal/internal/config"
	"vm-portal/internal/secrets"
)

// VSwitchEntry is one "BMHost PGName VlanId" line (main_vs.txt's format).
type VSwitchEntry struct {
	BMHost string
	PGName string
	VlanID int
}

type Phase2Input struct {
	TargetVSwitch string
	Entries       []VSwitchEntry
}

// RunPhase2 creates vSwitch port groups (main_vs.txt / binary "vswitch_setting"), RBAC level 3.
func RunPhase2(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase2Input, requestedBy int64) (jobID int64, res *Result, err error) {
	lines := make([]string, 0, len(in.Entries))
	for _, e := range in.Entries {
		lines = append(lines, strings.Join([]string{e.BMHost, e.PGName, strconv.Itoa(e.VlanID)}, " "))
	}

	worklistPath, err := WriteTempFile(cfg.TmpDir, "vswitch", lines)
	if err != nil {
		return 0, nil, fmt.Errorf("vswitch 설정 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	jobID, err = CreateJob(db, "phase2_vswitch", cred.ID, worklistName, "", in.TargetVSwitch, requestedBy)
	if err != nil {
		return 0, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
		"-targetVSwitch", in.TargetVSwitch,
	}
	env := []string{"VC_PASSWORD=" + cred.VCPassword}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "vswitch_setting"), cfg.TmpDir, args, env)
	return jobID, res, err
}
