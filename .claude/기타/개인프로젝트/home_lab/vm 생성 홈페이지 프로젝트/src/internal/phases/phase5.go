package phases

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"vm-portal/internal/config"
	"vm-portal/internal/secrets"
)

// Phase5Input mirrors affinity_setting's flags: -id -vcTargetIP -worklistFile -affinityFile.
// ev01 (per host) always gets a deterministic 1:1 core pin regardless of the
// affinity file; the affinity file's "key=value" pairs are applied to ev02 only
// (this matches main_affinity.txt's behavior exactly, not a design choice made here).
type Phase5Input struct {
	Hosts         []string
	AffinityPairs map[string]string // "sched.vcpuN.affinity" -> "a,b", applied to ev02
}

// RunPhase5 sets CPU affinity (main_affinity.txt / binary "affinity_setting"), RBAC level 4.
func RunPhase5(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase5Input, requestedBy int64) (jobID int64, res *Result, err error) {
	jobID, err = CreateJob(db, "phase5_affinity", cred.ID, "", "", "", requestedBy)
	if err != nil {
		return 0, nil, err
	}

	// Unlike Phase 3/4, affinity_setting does NOT split on "." — it matches
	// VMs by exact "<worklist entry>ev01"/"ev02". Since Phase 3 names VMs
	// using only the first dot-label, this phase must pre-truncate to match,
	// or every host here silently logs "대상 VM이 존재하지 않습니다" and skips.
	hosts := NormalizeHosts(db, jobID, in.Hosts)

	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist", hosts)
	if err != nil {
		return jobID, nil, fmt.Errorf("worklist 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	var affinityLines []string
	for k, v := range in.AffinityPairs {
		affinityLines = append(affinityLines, k+"="+v)
	}
	affinityPath, err := WriteTempFile(cfg.TmpDir, "affinity", affinityLines)
	if err != nil {
		return jobID, nil, fmt.Errorf("affinity 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(affinityPath)
	affinityName := filepath.Base(affinityPath)

	if _, err := db.Exec(`UPDATE jobs SET worklist_file = ?, mapfile = ? WHERE id = ?`, worklistName, affinityName, jobID); err != nil {
		return jobID, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
		"-affinityFile", affinityName,
	}
	env := []string{"VC_PASSWORD=" + cred.VCPassword}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "affinity_setting"), cfg.TmpDir, args, env)
	return jobID, res, err
}
