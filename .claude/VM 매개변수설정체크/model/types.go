// Package model은 vm-param-check 전체에서 공유하는 데이터 구조를 담는다.
package model

// VMInfo는 vCenter에서 조회한 VM 1대의 원시 정보다.
// checker 패키지들이 이 구조체를 기준으로 기대값과 비교한다.
type VMInfo struct {
	Name    string // vCenter상의 VM 표시 이름 (config.name)
	VCenter string // 이 VM을 조회한 vCenter 주소 (다중 vCenter 순회 시 출처 구분용)

	Hostname       string // 그룹 분류(ev01/ev02/ev03)에 실제로 사용하는 hostname
	HostnameSource string // "guest.hostName" | "config.name(fallback)" — CSV 비고란에 기록

	NumCPU            int32
	NumCoresPerSocket int32
	MemoryMB          int32
	DiskGB            float64 // 모든 VirtualDisk capacity 합산 (GB)

	ExtraConfig map[string]string // config.extraConfig 전체를 key->value(문자열)로 평탄화

	MemoryReservationLockedToMax *bool // "모든 게스트 메모리 예약" 여부 (nil이면 조회 실패/미설정)

	CPUSharesLevel string // "custom" | "normal" | "high" | "low" 등
	CPUShares      int32  // Level이 custom일 때만 의미 있는 ratio 값

	MemorySharesLevel string // CPUSharesLevel과 동일한 의미, config.memoryAllocation 기준
	MemoryShares      int32

	// NumaCoresPerNode는 config.numaInfo.coresPerNumaNode — ExtraConfig(numa.vcpu.maxPerVirtualNode)와는
	// 완전히 별개인, vSphere Client의 "CPU 토폴로지 > NUMA 노드당 코어 수" UI가 실제로 읽고 쓰는 API 필드다
	// (vSphere API 8.0.0.1+ 필요). nil이면 이 VM에 vNUMA 커스텀 설정이 없다는 뜻(정상적인 "미설정" 상태).
	NumaCoresPerNode *int32

	Networks []string // 연결된 네트워크 어댑터의 포트그룹 이름 목록

	HostName        string // 이 VM이 돌고 있는 ESXi 호스트 이름
	HostPowerPolicy string // 그 호스트의 현재 전원 정책 표시 이름 (예: "High Performance")
}

// Finding은 체크 항목 1건의 결과 — CSV 한 줄에 대응한다.
type Finding struct {
	VM       string
	Source   string // "ev01" | "ev02" | "ev03" | "-" | "host" | "network"
	Key      string
	Expected string
	Actual   string
	Result   string // "OK" | "FAIL" | "설정없음" | "정보"
	Note     string
}
