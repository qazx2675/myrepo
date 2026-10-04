package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// JEV Request types
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type SystemOneRequest struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// JEV Response types
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

type SystemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
}

var categories = map[string]string{
	"os_install":        "OS 설치, 재설치, PXE 설치, 환경설정 체크, dd 명령어, LACP 해제 등",
	"usb_block":         "USB 차단 블록 월간 육안 점검, 보안센터 수령 및 반납 프로세스",
	"network_config":    "IP 할당 요청, LDAP 설정, 망 변경, DHCP 할당 등 네트워크 문제",
	"asset_management":  "자산 폐기 프로세스 (ITOM 상태 변경), 신규 장비 자산 등록 및 서버명 변경",
	"gpu_management":    "GPU 드라이버 삭제 및 설치 (NVIDIA-Linux), Fabric Manager 설치, GPU 모드 변경",
	"incident_response": "계정 접속 불가(sssd), 좀비 프로세스 삭제, ulimit/nofile 변경, crontab, dracut 등 장애 및 점검",
	"splunk":            "Splunk 설치 스크립트 실행, 동보 발생, Password Expire 알람, 장기간 Down 알림 조치",
	"vuln_remediate":    "SSI 취약점 스크립트(change_SSI.sh) 조치, 서버 보안 조치 및 인증서 적용 가이드",
	"general_rules":     "지원 범위, VWP/컴퓨트 서버 승인 프로세스, 패스워드 변경 가이드 등",
}

// Responses mapped to intents
var manualResponses = map[string]string{
	"os_install": `[OS 설치 프로세스]
1. 승인 확인: 컴퓨트 서버는 CF 담당자, 일반 서버는 서비스 담당자 승인 필수
2. ITOM 등록 여부 확인
3. AWX nodeinfo 실행으로 DHCP/PXE 환경 준비
4. 크로스 체크: 카카오톡 작업 메시지 템플릿 사용 (예: "OO 등 x대 OS 재설치")
5. dd 명령어로 설치 진행
* 주의: MAC 주소 마지막 자리가 짝수여야 정상 설치 가능. LACP 사용 서버는 스위치 LACP 해제 후 설치.`,
	"usb_block": `[USB 차단 블록 점검]
1. 월간 점검: 매월 1회 센터 방문하여 USB 차단 블록 육안 점검 진행
2. 수령 장소: MR1동 보안센터 메모리 창고
3. 미장착 서버 발견 시 벤더 엔지니어에게 공유 및 추가 불출 진행`,
	"network_config": `[네트워크 및 IP 설정]
1. IP 할당 요청 (HPC->네트워크, SDS->SDS담당자)
2. 적용: AWX 활용 (IP/LDAP 변경 후 재부팅) 또는 Script (MGMT/Script/ 내 IP/LDAP 스크립트 활용)
3. 검증: 5분 후 접속 확인. 안 될 경우 service nscd reload 2~3회 실행`,
	"asset_management": `[자산 관리 프로세스]
* 자산 폐기: 
  - CF 담당자 Job 확인, 서비스 담당자 메일 통보
  - 케이블 제거 및 DNS 삭제 요청 -> ITOM 상태 '폐기 예정'으로 변경
* 자산 등록: '업무체크리스트_v2' 가이드 참조 (CPU flops 정보 등 정확히 기입)`,
	"gpu_management": `[GPU 관리]
1. GPU 드라이버 설치: nvidia-uninstall -s 로 기존 삭제 후, 재부팅하여 버전에 맞는 run 파일로 설치
2. NVSwitch 사용 시 Fabric Manager 필수 설치 (dnf module reset nvidia-driver 적용 후 설치)
3. GPU 모드 변경: nvidia-smi -c [모드번호] (주로 Exclusive Process인 3번 사용)`,
	"incident_response": `[트러블슈팅 및 장애 대응]
* 접속 불가: watchdog 확인 후 sssd 데몬 재시작
* 과점유 좀비 프로세스: kill -9 [pid]로 정리
* ulimit 변경: /etc/security/limits.d/nofile.conf 값 변경 후 터미널 재접속
* crontab 적용: systemctl restart crond
* dracut 에러: 드림아이 담당자에게 조치 요청`,
	"splunk": `[Splunk 조치]
* 설치: 인프라에 맞는 스크립트 실행 (HPC: SA/splunk/install.sh, SDS: SA_CLOUD/splunk/install_cloud_splunk.sh)
* Alert 조치: 
  - Password Expire: 루트 패스워드 갱신
  - 장기간 Down: 접속 불가 시 엔지니어 점검 요청
  - 동보 발생: 예외 리스트 정상 여부 확인 후 0보 완료 또는 1보 발행 판단`,
	"vuln_remediate": `[보안 및 취약점 조치]
* SSI 취약점 조치: 
  - ~svr/MGMT/Script/Config/SSI/ 경로의 {user}.txt에 hostname과 code 입력 후 change_SSI.sh 실행
* 인증서 적용: EDM에서 파일 다운로드 후 update-ca-trust 실행`,
	"general_rules": `[지원 기준 및 규칙]
* HPC 팀 미관리 또는 사업부 직접 구매 서버는 지원 불가 (반려)
* 컴퓨트 서버 작업 전 CF 담당자 최종 승인 및 Job 체크 필수
* 정기 점검: 분기별 패스워드 변경 절차 (shc 암호화 후 배포), 매일 출/퇴근 DNS Health Check 실행`,
}

// GetJevIntent calls the JEV API to determine the user intent.
func GetJevIntent(userQuery string, apiKey string) string {
	if apiKey == "" {
		// Fallback local matching if no API key is provided
		return localFallbackMatch(userQuery)
	}

	reqBody := SystemOneRequest{
		State: userQuery,
		Model: "jev-latest",
		Questions: map[string]Question{
			"intent": {
				Type:         "choice",
				Instructions: "사용자의 질문이나 요청이 다음 서버 운영 관리 기준 중 어디에 가장 잘 부합하는지 분류하세요.",
				Criteria:     categories,
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return "error"
	}

	req, err := http.NewRequest("POST", "https://api.typesafe.ai/v1/systemone", bytes.NewBuffer(jsonData))
	if err != nil {
		return localFallbackMatch(userQuery)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return localFallbackMatch(userQuery)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var res SystemOneResponse
	err = json.Unmarshal(body, &res)
	if err != nil {
		return localFallbackMatch(userQuery)
	}

	if intent, ok := res.Answers["intent"]; ok && intent.Choice != "" {
		// Check confidence threshold if needed
		if intent.Confidence > 0.4 {
			return intent.Choice
		}
	}

	return localFallbackMatch(userQuery)
}

func localFallbackMatch(query string) string {
	// Simple keyword fallback
	if strings.Contains(query, "OS") || strings.Contains(query, "설치") || strings.Contains(query, "PXE") {
		return "os_install"
	} else if strings.Contains(query, "GPU") || strings.Contains(query, "드라이버") || strings.Contains(query, "Fabric") {
		return "gpu_management"
	} else if strings.Contains(query, "네트워크") || strings.Contains(query, "IP") || strings.Contains(query, "망변경") {
		return "network_config"
	} else if strings.Contains(query, "USB") || strings.Contains(query, "블록") {
		return "usb_block"
	} else if strings.Contains(query, "폐기") || strings.Contains(query, "자산") || strings.Contains(query, "ITOM") {
		return "asset_management"
	} else if strings.Contains(query, "보안") || strings.Contains(query, "취약점") || strings.Contains(query, "SSI") {
		return "vuln_remediate"
	} else if strings.Contains(query, "Splunk") || strings.Contains(query, "스플렁크") || strings.Contains(query, "알람") {
		return "splunk"
	} else if strings.Contains(query, "에러") || strings.Contains(query, "장애") || strings.Contains(query, "좀비") || strings.Contains(query, "ulimit") {
		return "incident_response"
	}
	return "general_rules"
}

func GetManualResponse(intent string) string {
	if res, ok := manualResponses[intent]; ok {
		return res
	}
	return "관련 지침을 찾을 수 없습니다. 서비스 담당자나 매뉴얼 원본을 확인하세요."
}
