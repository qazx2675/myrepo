// Package phases wraps the existing govmomi CLI binaries (vmc, vswitch_setting,
// vm_create, ...) with exec.Command, per the plan's "A안" (wrap, don't rewrite).
package phases

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// WriteTempFile creates <dir>/<prefix>_<8 random hex chars>.txt containing
// lines (one per line), per the plan's UUID-style collision-proof naming
// for worklist/hostgroup files (section 5 of the plan).
func WriteTempFile(dir, prefix string, lines []string) (path string, err error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	name := prefix + "_" + hex.EncodeToString(suffix) + ".txt"
	path = filepath.Join(dir, name)

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveQuiet deletes a temp file, ignoring errors (best-effort cleanup).
func RemoveQuiet(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// FirstLabel returns the segment before the first '.' in a host identifier.
//
// Phase 3 (main_vm_create_v2.txt) names VMs "<firstLabel>ev01"/"ev02" by
// splitting each worklist entry on "." itself — so it (and Phase 4, which
// mirrors the same split) already handles a full IP/FQDN correctly and must
// receive the untouched host string, since it separately uses the full
// string for finder.HostSystem()/hostgroupMap[] lookups.
//
// Phase 5 (main_affinity.txt) and Phase 6 (main_lpage.txt) do NOT split on
// "." — they match VMs by exact "<worklist entry>ev01"/"ev02". If a user
// enters the same full IP/FQDN there, it won't match the shorter name Phase 3
// actually created, and every host silently logs "대상 VM이 존재하지 않습니다"
// and does nothing. FirstLabel/NormalizeHosts pre-truncates for those two so
// they compute the same VM name Phase 3 did.
func FirstLabel(host string) string {
	if i := strings.IndexByte(host, '.'); i >= 0 {
		return host[:i]
	}
	return host
}

// NormalizeHosts applies FirstLabel to every entry and logs a note for any
// entry that actually changed, so the transformation is visible in the job's
// log output rather than being a silent surprise.
func NormalizeHosts(db *sql.DB, jobID int64, hosts []string) []string {
	out := make([]string, len(hosts))
	for i, h := range hosts {
		out[i] = FirstLabel(h)
		if out[i] != h {
			appendLog(db, jobID, "[INFO] 호스트 식별자 정규화: "+h+" -> "+out[i])
		}
	}
	return out
}
