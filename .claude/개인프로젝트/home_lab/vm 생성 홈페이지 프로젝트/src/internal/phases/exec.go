package phases

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os/exec"
	"time"

	"vm-portal/internal/models"
)

// Result is the outcome of a synchronous binary run: exit status plus every
// line of interleaved stdout/stderr, already persisted to job_logs.
type Result struct {
	Success bool
	Lines   []string
	DryRun  bool // M8: true when this was a preview-only run (see dryRunPreview) — no binary actually executed
}

// Run executes binPath with args and the given extra environment variables
// (VC_PASSWORD / ESXI_PASSWORD), streaming combined output into job_logs as
// it arrives and flipping the job's status when the process exits.
//
// M4 runs this synchronously (the HTTP handler blocks until completion) —
// async queuing + SSE streaming is M5 per the plan's roadmap.
func Run(ctx context.Context, db *sql.DB, jobID int64, binPath string, dir string, args []string, env []string) (*Result, error) {
	if err := setJobStatus(db, jobID, models.JobStatusRunning); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = env
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = setJobStatus(db, jobID, models.JobStatusFailed)
		appendLog(db, jobID, fmt.Sprintf("[에러] 실행 실패: %v", err))
		return nil, err
	}

	res := &Result{}
	done := make(chan struct{}, 2)
	readStream := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			res.Lines = append(res.Lines, line)
			appendLog(db, jobID, line)
		}
		done <- struct{}{}
	}
	go readStream(stdout)
	go readStream(stderr)
	<-done
	<-done

	waitErr := cmd.Wait()
	res.Success = waitErr == nil

	status := models.JobStatusSuccess
	if !res.Success {
		status = models.JobStatusFailed
		appendLog(db, jobID, fmt.Sprintf("[에러] 프로세스 종료 코드 오류: %v", waitErr))
	}
	_ = setJobStatus(db, jobID, status)

	return res, nil
}

func setJobStatus(db *sql.DB, jobID int64, status string) error {
	_, err := db.Exec(`UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now(), jobID)
	return err
}

func appendLog(db *sql.DB, jobID int64, line string) {
	_, _ = db.Exec(`INSERT INTO job_logs (job_id, line) VALUES (?, ?)`, jobID, line)
}

// CreateJob inserts a new job row in "pending" state and returns its id.
func CreateJob(db *sql.DB, phase string, credentialID int64, worklistFile, mapFile, params string, requestedBy int64) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO jobs (phase, status, credential_id, worklist_file, mapfile, params, requested_by)
		VALUES (?, 'pending', ?, ?, ?, ?, ?)`,
		phase, credentialID, worklistFile, mapFile, params, requestedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
