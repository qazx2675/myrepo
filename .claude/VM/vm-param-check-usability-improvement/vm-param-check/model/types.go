// Package model은 vm-param-check 전체에서 공유하는 데이터 구조를 담는다.
package model

import "github.com/vmware/govmomi/vim25/types"

// VMInfo는 vCenter에서 조회한 VM 1대의 원시 정보다.
// checker 패키지들이 이 구조체를 기준으로 기대값과 비교한다.
type VMInfo struct {
	Name    string // vCenter상의 VM 표시 이름 (config.name)
	VCenter string // 이 VM을 조회한 vCenter 주소 (다중 vCenter 순회 시 출처 구분용)

	// Ref는 이 VM의 ManagedObjectReference. fixer가 교정 대상을 이름으로 다시 찾지 않고
	// 조회 시점의 moref로 바로 Reconfigure할 수 있게 담아둔다(동명 VM 오조작 방지).
	Ref types.ManagedObjectReference

	// PoweredOn은 runtime.powerState == poweredOn. fixer의 전원 OFF 게이트에서 쓴다
	// (하드웨어 토폴로지 변경은 VM이 꺼져 있어야 안전하기 때문).
	PoweredOn bool

	Hostname       string // 그룹 분류(ev01/ev02/ev03)에 실제로 사용하는 hostname
	HostnameSource string // "guest.hostName" | "config.name(fallback)" — CSV 비고란에 기록

	// Folder는 이 VM이 속한 vCenter 인벤토리 폴더 이름(예: TST-CAE001-SAMP48c-QRST).
	// 폴더명 규칙으로 스펙 파일(_spec.txt)을 자동으로 찾는 데 쓴다. 폴더를 알 수 없으면 빈 문자열.
	Folder string

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

	// NumaAutoCoresPerNode는 config.numaInfo.autoCoresPerNumaNode. true면 NumaCoresPerNode가
	// "지난 전원 켜짐 시점의 값이 남아있을 뿐 무시해야 하는" 값이라는 뜻이다(govmomi 타입 주석 근거) —
	// UI에는 "전원을 켤 때 할당됨"으로 표시된다. 이 경우 checker는 NumaCoresPerNode 값을 기대값과
	// 비교하지 않고 "설정없음"으로 처리한다(우연히 값이 같은 과거 이력 때문에 OK로 오판되는 것 방지).
	NumaAutoCoresPerNode *bool

	// Networks는 VM에 붙어있는 네트워크 어댑터 전부(커넥트/디스커넥트 무관)를 담는다.
	// 포트그룹 이름은 연결 상태와 무관하게 항상 조사되어야 한다 — 디스커넥트라고 해서
	// 어댑터가 없는 게 아니라 vSphere에서 연결만 꺼둔 것뿐이라서다.
	Networks []NetworkAdapter

	HostName        string // 이 VM이 돌고 있는 ESXi 호스트 이름
	HostPowerPolicy string // 그 호스트의 현재 전원 정책 표시 이름 (예: "High Performance")
}

// NetworkAdapter는 VM에 붙은 네트워크 어댑터 1개 — 포트그룹 이름 + 커넥트/디스커넥트 상태.
type NetworkAdapter struct {
	Portgroup string
	Connected bool
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
