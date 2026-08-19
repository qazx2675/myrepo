package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"awxkit/awx"
	"awxkit/config"
)

// runNodeInfo는 [S1] NodeInfo 템플릿을 hostsPath에 나열된 hostname마다 각각 실행하고,
// 결과를 hostname별 파일로 저장한다.
func runNodeInfo(confPath, user, hostsFlag string) {
	cfg, client, err := loadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if cfg.S1Template == "" {
		fmt.Println("[X] conf의 s1_template이 설정되지 않았습니다.")
		os.Exit(1)
	}

	if hostsFlag == "" && user == "" {
		fmt.Println("[X] 사용자를 식별할 수 없어 ${user}.txt를 찾을 수 없습니다. -hosts로 직접 지정하거나 -user/AWXKIT_USER를 설정하세요.")
		os.Exit(1)
	}
	hostsPath, err := config.ResolveNamedPath(hostsFlag, user+".txt")
	if err != nil {
		fmt.Printf("[X] 호스트 목록 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}
	hosts, err := config.ReadHostList(hostsPath)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[i] 호스트 목록: %s (%d개)\n", hostsPath, len(hosts))

	t, err := client.ResolveTemplate(cfg.S1Template)
	if err != nil {
		fmt.Printf("[X] 템플릿(%s)을 찾을 수 없습니다: %v\n", cfg.S1Template, err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.S1OutputDir, 0o755); err != nil {
		fmt.Printf("[X] 결과 저장 디렉터리를 만들 수 없습니다 (%s): %v\n", cfg.S1OutputDir, err)
		os.Exit(1)
	}

	var succeeded, failed []string
	for i, host := range hosts {
		fmt.Printf("[%d/%d] %s 실행 중...\n", i+1, len(hosts), host)

		result, err := client.Launch(t.ID, map[string]interface{}{cfg.S1HostnameKey: host})
		if err != nil {
			fmt.Printf("    [X] 실행 요청 실패: %v\n", err)
			failed = append(failed, host)
			appendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo host=%s status=launch_error error=%q", user, host, err.Error()))
			continue
		}
		if len(result.IgnoredFields) > 0 {
			fmt.Printf("    [!] 일부 값이 무시되었습니다(ignored_fields): %v — 템플릿의 ask_variables_on_launch 설정을 확인하세요.\n", result.IgnoredFields)
		}

		job, err := pollJob(client, result.Job, cfg.PollIntervalSec)
		if err != nil {
			fmt.Printf("    [X] 상태 조회 실패: %v\n", err)
			failed = append(failed, host)
			appendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo host=%s job=%d status=poll_error error=%q", user, host, result.Job, err.Error()))
			continue
		}
		if job.Status != "successful" {
			fmt.Printf("    [X] Job 실패 (status=%s)\n", job.Status)
			printStdoutTail(client, job.ID, 30)
			failed = append(failed, host)
			appendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo host=%s job=%d status=%s", user, host, job.ID, job.Status))
			continue
		}

		outPath := filepath.Join(cfg.S1OutputDir, host+".yaml")
		if err := saveNodeInfoResult(client, cfg, job, outPath); err != nil {
			fmt.Printf("    [X] 결과 저장 실패: %v\n", err)
			failed = append(failed, host)
			appendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo host=%s job=%d status=fetch_error error=%q", user, host, job.ID, err.Error()))
			continue
		}

		fmt.Printf("    [✔] 완료 (job %d)\n", job.ID)
		succeeded = append(succeeded, host)
		appendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo host=%s job=%d status=successful output=%s", user, host, job.ID, outPath))
	}

	fmt.Printf("\n총 %d개 중 성공 %d개, 실패 %d개\n", len(hosts), len(succeeded), len(failed))
	if len(failed) > 0 {
		fmt.Printf("실패한 호스트: %v\n", failed)
		os.Exit(1)
	}
}

// saveNodeInfoResult는 cfg.S1Fetch 설정에 따라 결과를 취득해 outPath에 저장한다.
// s1_fetch=remote인 경우 API로 파일을 받을 수 없으므로 안내만 출력한다.
func saveNodeInfoResult(client *awx.Client, cfg *config.Config, job *awx.Job, outPath string) error {
	switch cfg.S1Fetch {
	case "stdout":
		content, err := client.GetJobStdout(job.ID)
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, []byte(content), 0o644)

	case "remote":
		fmt.Printf("    [i] 결과 파일은 AWX 실행 노드의 %s 경로에 남아 있습니다. 수동으로 확인하세요.\n", cfg.S1RemotePath)
		return nil

	default: // artifacts
		if cfg.S1ArtifactKey != "" {
			val, ok := job.Artifacts[cfg.S1ArtifactKey]
			if !ok {
				return fmt.Errorf("artifacts에 s1_artifact_key(%s)가 없습니다 (실제 키: %v)", cfg.S1ArtifactKey, artifactKeys(job.Artifacts))
			}
			if s, ok := val.(string); ok {
				return os.WriteFile(outPath, []byte(s), 0o644)
			}
			data, err := json.MarshalIndent(val, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(outPath, data, 0o644)
		}

		if len(job.Artifacts) == 0 {
			return fmt.Errorf("Job에 artifacts가 없습니다 (플레이북이 set_stats를 사용하는지 확인하세요)")
		}
		fmt.Println("    [!] conf에 s1_artifact_key가 설정되지 않아 artifacts 전체를 저장합니다.")
		data, err := json.MarshalIndent(job.Artifacts, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, data, 0o644)
	}
}

func artifactKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
