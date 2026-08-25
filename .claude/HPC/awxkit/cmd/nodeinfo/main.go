// awxkit-nodeinfo는 [S1] NodeInfo 템플릿을 한 번 실행한다.
// ${user}.txt(또는 -hosts로 지정한 파일)에 나열된 hostname 전체를 줄바꿈으로 이어붙여
// 하나의 extra_vars 값으로 넘기고, 그 결과를 파일 하나로 받는다.
// (NodeInfo 템플릿 자체가 여러 hostname을 텍스트로 한 번에 받아 처리하는 구조이므로,
// hostname마다 별도로 launch하지 않는다.)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"awxkit/awx"
	"awxkit/cli"
	"awxkit/config"
)

func main() {
	confFlag := flag.String("conf", "", "설정 파일 경로를 직접 지정합니다")
	userFlag := flag.String("user", "", "사용자 식별자를 직접 지정합니다 (AWXKIT_USER 환경변수보다 낮은 우선순위)")
	hostsFlag := flag.String("hosts", "", "사용할 호스트 목록 파일 경로 (기본: ${user}.txt)")
	osFlag := flag.String("os", "", "OS 버전 (예: 8.10). s1_osver_key가 설정된 경우에만 사용됨")
	flag.Parse()

	user := config.ResolveUser(*userFlag)
	confPath, err := config.ResolvePath(*confFlag, user)
	if err != nil {
		fmt.Fprintf(os.Stderr, cli.MarkFail+" 설정 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}

	cfg, client, err := cli.LoadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if cfg.S1Template == "" {
		fmt.Println(cli.MarkFail+" conf의 s1_template이 설정되지 않았습니다.")
		os.Exit(1)
	}

	if *hostsFlag == "" && user == "" {
		fmt.Println(cli.MarkFail+" 사용자를 식별할 수 없어 ${user}.txt를 찾을 수 없습니다. -hosts로 직접 지정하거나 -user/AWXKIT_USER를 설정하세요.")
		os.Exit(1)
	}
	hostsPath, err := config.ResolveNamedPath(*hostsFlag, user+".txt")
	if err != nil {
		fmt.Printf(cli.MarkFail+" 호스트 목록 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}
	hosts, err := config.ReadHostList(hostsPath)
	if err != nil {
		fmt.Printf(cli.MarkFail+" %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[i] 호스트 목록: %s (%d개)\n", hostsPath, len(hosts))

	t, err := client.ResolveTemplate(cfg.S1Template)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 템플릿(%s)을 찾을 수 없습니다: %v\n", cfg.S1Template, err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.S1OutputDir, 0o755); err != nil {
		fmt.Printf(cli.MarkFail+" 결과 저장 디렉터리를 만들 수 없습니다 (%s): %v\n", cfg.S1OutputDir, err)
		os.Exit(1)
	}

	extraVars, err := config.ParseKeyValues(cfg.S1ExtraVars)
	if err != nil {
		fmt.Printf(cli.MarkFail+" conf의 s1_extra_vars 형식이 올바르지 않습니다: %v\n", err)
		os.Exit(1)
	}

	hostText := strings.Join(hosts, "\n")
	launchVars := map[string]interface{}{cfg.S1HostnameKey: hostText}
	for k, v := range extraVars {
		launchVars[k] = v
	}

	if cfg.S1OSVerKey != "" {
		osVer, err := cli.ResolveChoice("OS 버전", cli.ParseChoices(cfg.S1OSVerChoices), *osFlag)
		if err != nil {
			fmt.Printf(cli.MarkFail+" %v\n", err)
			os.Exit(1)
		}
		launchVars[cfg.S1OSVerKey] = osVer
	}
	fmt.Printf("[i] %s 실행 중... (%d개 hostname을 한 번에 전달)\n", t.Name, len(hosts))

	result, err := client.Launch(t.ID, launchVars)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 실행 요청 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo hosts=%d status=launch_error error=%q", user, len(hosts), err.Error()))
		os.Exit(1)
	}
	if len(result.IgnoredFields) > 0 {
		fmt.Printf("    " + cli.MarkWarn + " 일부 값이 무시되었습니다(ignored_fields): %v — 템플릿의 ask_variables_on_launch 설정을 확인하세요.\n", result.IgnoredFields)
	}

	job, err := cli.PollJob(client, result.Job, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 상태 조회 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo hosts=%d job=%d status=poll_error error=%q", user, len(hosts), result.Job, err.Error()))
		os.Exit(1)
	}
	if job.Status != "successful" {
		fmt.Printf(cli.MarkFail+" Job 실패 (status=%s)\n", job.Status)
		cli.PrintStdoutTail(client, job.ID, 30)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo hosts=%d job=%d status=%s", user, len(hosts), job.ID, job.Status))
		os.Exit(1)
	}

	outPath := filepath.Join(cfg.S1OutputDir, user+"_nodeinfo.yaml")
	if user == "" {
		outPath = filepath.Join(cfg.S1OutputDir, "nodeinfo_result.yaml")
	}
	if err := saveNodeInfoResult(client, cfg, job, outPath); err != nil {
		fmt.Printf(cli.MarkFail+" 결과 저장 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo hosts=%d job=%d status=fetch_error error=%q", user, len(hosts), job.ID, err.Error()))
		os.Exit(1)
	}

	fmt.Printf(cli.MarkOK+" 다운로드 완료 (job %d) — 결과 저장: %s\n", job.ID, outPath)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=nodeinfo hosts=%d job=%d status=successful output=%s", user, len(hosts), job.ID, outPath))
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
		fmt.Println("    " + cli.MarkWarn + " conf에 s1_artifact_key가 설정되지 않아 artifacts 전체를 저장합니다.")
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
