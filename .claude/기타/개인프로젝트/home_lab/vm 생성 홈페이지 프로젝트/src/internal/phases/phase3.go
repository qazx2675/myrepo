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

// VMSpec is one ev0N tier's Cpu/Mem(GB)/Disk(GB)/Share values.
type VMSpec struct {
	Cpu   int
	Mem   int
	Disk  int
	Share int
}

type Phase3Input struct {
	Datacenter   string            // optional; required only if vCenter has >1 datacenter
	Firmware     string            // "efi" (default/recommended) or "bios"
	VMCount      int               // 1..3
	Hosts        []string          // BM host list -> worklist_<rand>.txt
	HostGroupMap map[string]string // BM host -> portgroup name -> hostgroup_<rand>.txt
	Specs        map[int]VMSpec    // keys 1..3 (ev01/ev02/ev03)
}

// RunPhase3 creates VMs (main_vm_create_v2.txt / binary "vm_create"), RBAC level 3.
// This is the version the plan (section 0) standardizes on: hostgroup mapping +
// firmware option, replacing the older v1/flash_lite/v4 variants.
func RunPhase3(ctx context.Context, db *sql.DB, cfg config.Config, key []byte, cred *secrets.DecryptedCredential, in Phase3Input, requestedBy int64) (jobID int64, res *Result, err error) {
	if in.VMCount < 1 || in.VMCount > 3 {
		return 0, nil, fmt.Errorf("vmCount은 1~3 사이여야 합니다")
	}
	if in.Firmware != "efi" && in.Firmware != "bios" {
		in.Firmware = "efi"
	}

	jobID, err = CreateJob(db, "phase3_vm_create", cred.ID, "", "", in.Firmware, requestedBy)
	if err != nil {
		return 0, nil, err
	}

	// vm_create's own finder.HostSystem(ctx, bmHost) and hostgroupMap[bmHost]
	// lookups use the FULL worklist entry (it only splits on "." separately,
	// internally, to build the VM name) — so unlike Phase 5/6, the host list
	// and hostgroup map keys here must stay exactly as entered, not truncated.
	worklistPath, err := WriteTempFile(cfg.TmpDir, "worklist", in.Hosts)
	if err != nil {
		return jobID, nil, fmt.Errorf("worklist 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(worklistPath)
	worklistName := filepath.Base(worklistPath)

	mapLines := make([]string, 0, len(in.HostGroupMap))
	for host, pg := range in.HostGroupMap {
		mapLines = append(mapLines, host+" "+pg)
	}
	mapPath, err := WriteTempFile(cfg.TmpDir, "hostgroup", mapLines)
	if err != nil {
		return 0, nil, fmt.Errorf("hostgroup 매핑 파일 생성 실패: %w", err)
	}
	defer RemoveQuiet(mapPath)
	mapName := filepath.Base(mapPath)

	if _, err := db.Exec(`UPDATE jobs SET worklist_file = ?, mapfile = ? WHERE id = ?`, worklistName, mapName, jobID); err != nil {
		return jobID, nil, err
	}

	args := []string{
		"-id", cred.VCID,
		"-vcTargetIP", cred.VCenterIP,
		"-worklistFile", worklistName,
		"-mapFile", mapName,
		"-vmCount", strconv.Itoa(in.VMCount),
		"-firmware", in.Firmware,
	}
	if in.Datacenter != "" {
		args = append(args, "-datacenter", in.Datacenter)
	}
	for i := 1; i <= 3; i++ {
		spec, ok := in.Specs[i]
		if !ok {
			continue
		}
		prefix := fmt.Sprintf("-ev%02d", i)
		args = append(args,
			prefix+"Cpu", strconv.Itoa(spec.Cpu),
			prefix+"Mem", strconv.Itoa(spec.Mem),
			prefix+"Disk", strconv.Itoa(spec.Disk),
			prefix+"Share", strconv.Itoa(spec.Share),
		)
	}

	env := []string{"VC_PASSWORD=" + cred.VCPassword}

	res, err = Run(ctx, db, jobID, filepath.Join(cfg.BinDir, "vm_create"), cfg.TmpDir, args, env)
	return jobID, res, err
}
