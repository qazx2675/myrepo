package main

import (
	"time"

	"github.com/vmware/govmomi/object"
)

// ConfPath 는 설정 파일의 고정 경로다(요구사항의 철자 그대로 vcetner).
const ConfPath = "/etc/vcetner_pwon_off/pwonoff.conf"

// Config 는 pwonoff.conf 의 내용이다. 기본값은 config.go 의 defaultConfig() 가 채운다.
type Config struct {
	AllowedDir       string   // 이 디렉터리에서만 실행 가능
	Users            []string // 선택 가능한 user (적은 순서 = 번호)
	VCenters         []string // vCenter 후보 목록
	VCID             string   // 기본 lscsystems@vsphere.local
	SecretLib        string   // secret_lib.sh 경로 (비우면 비밀번호 직접 입력)
	OS6Host          string   // 22 포트 확인 경유 서버(비우면 직접 확인만)
	OS6User          string
	Concurrency      int
	ShutdownTimeout  time.Duration
	BootTimeout      time.Duration
	BootPollInterval time.Duration
	PortCheckTimeout time.Duration
	CrossCheck       bool   // cross_check=on(기본) 이면 안전장치 2단계 확인자 선택·3단계 확인자 승인(VM 이름 입력 대신)·카운트다운 뒤 --start. off 면 기존 방식
	CrossCheckBin    string // cross_check 실행 파일 절대경로 (기본 /usr/local/bin/cross_check)
}

// VMInfo 는 vCenter 에서 조회한 대상 VM 한 대의 상태다.
type VMInfo struct {
	Name         string // 예: svr1ev01
	NameBM       string // 이름에서 ev 앞부분 (svr1)
	Ev           int    // 이름 끝 ev 번호 (1)
	Host         string // 실제 ESXi 호스트 짧은 이름 (첫 '.' 앞). 모르면 ""
	Power        string // poweredOn / poweredOff / suspended
	FolderPath   string // 예: DC1/vm/VWP-Prod/svr1  (vm 루트부터 VM 직전 폴더까지, '/' 구분)
	PortGroups   []string
	Lpage        bool // extraConfig sched.mem.lpage.enable1GPage 가 TRUE
	ToolsRunning bool
	Heartbeat    string   // green / yellow / red / gray
	IPs          []string // IPv4 만, link-local(169.254) 제외, guest.ipAddress 가 있으면 맨 앞
	Obj          *object.VirtualMachine
}

// GroupKey 는 BM 묶음의 기준이다. 실제 호스트를 알면 그것, 아니면 이름상 BM.
func (v *VMInfo) GroupKey() string {
	if v.Host != "" {
		return v.Host
	}
	return v.NameBM
}

// Action 은 사용자가 고른 기능이다.
type Action int

const (
	ActPowerOff   Action = 1 // Power Off
	ActPowerOnSeq Action = 2 // Power On (lpage BM 은 순차)
	ActPowerOnAll Action = 3 // 일괄 Power On (OS 설치용)
	ActReboot     Action = 4 // 재부팅 = Off 완료 확인 -> 일괄 On (모두 병렬)
)

func (a Action) String() string {
	switch a {
	case ActPowerOff:
		return "Power Off"
	case ActPowerOnSeq:
		return "Power On(순차)"
	case ActPowerOnAll:
		return "일괄 Power On(OS 설치용)"
	case ActReboot:
		return "재부팅(Off→On, 병렬)"
	}
	return "?"
}

// Slug 는 감사 로그 파일 이름용.
func (a Action) Slug() string {
	switch a {
	case ActPowerOff:
		return "poweroff"
	case ActPowerOnSeq:
		return "poweron_seq"
	case ActPowerOnAll:
		return "poweron_all"
	case ActReboot:
		return "reboot"
	}
	return "unknown"
}

// 종료 코드. 취소/오류/실패는 모두 1 (요구사항: 비정상 종료 exit 1).
const (
	ExitOK   = 0
	ExitFail = 1
)
