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

// Phase4Input mirrors mac_info's flags: -id -vcTargetIP -worklistFile -arg1 -argInt -argStr.
// The binary both prints and writes Provisioning_List_<ip>.txt; we rely on
// the printed lines (captured into job_logs) rather than re-reading the file.
type Phase4Input struct {
	Hosts  []string
	Arg1   string
	ArgInt int
	ArgStr string
}

// RunPhase4 extracts MAC/IP and emits a Kickstart provisioning list
// (main_mac.txt / binary "mac_info"), RBAC level 3.
func RunPhase4(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase4Input, requestedBy int64) (jobID int64, res *Result, err error) {
	jobID, err = CreateJob(db, "phase4_mac_extract", cred.ID, "", "", in.Arg1, requestedBy)
	if err != nil {
		return 0, nil, err
	}

	// mac_info derives VM names via strings.Split(bmHost, ".")[0] internally
	// (same as Phase 3), so it already handles a full IP/FQDN correctly —
	// no pre-truncation needed here, unlike Phase 5/6.
	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist", in.Hosts)
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
		"-arg1", in.Arg1,
		"-argInt", strconv.Itoa(in.ArgInt),
		"-argStr", in.ArgStr,
	}
	env := []string{"VC_PASSWORD=" + cred.VCPassword}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "mac_info"), cfg.TmpDir, args, env)

	// Best-effort cleanup of the provisioning-list file the binary writes to
	// cwd (== cfg.TmpDir here); its content is already captured in job_logs.
	safeIP := ipToFileSafe(cred.VCenterIP)
	RemoveQuiet(filepath.Join(cfg.TmpDir, "Provisioning_List_"+safeIP+".txt"))

	return jobID, res, err
}

func ipToFileSafe(ip string) string {
	out := make([]byte, len(ip))
	for i := 0; i < len(ip); i++ {
		if ip[i] == '.' {
			out[i] = '_'
		} else {
			out[i] = ip[i]
		}
	}
	return string(out)
}
