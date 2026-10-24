// Package model 은 센터/랙/장비 및 CSV 검증 결과 구조체를 정의한다.
package model

// Rack 은 랙(캐비닛) 정보. 기본 52U, 서버 장착 범위 48U.
type Rack struct {
	SiteID      string `json:"siteId"`
	RackID      string `json:"rackId"`
	Name        string `json:"name"`
	RackX       int    `json:"rackX"`
	RackY       int    `json:"rackY"`
	GridX       int    `json:"gridX"`
	GridZ       int    `json:"gridZ"`
	WidthTiles  int    `json:"widthTiles"`
	DepthTiles  int    `json:"depthTiles"`
	MaxU        int    `json:"maxU"`
	ServerMaxU  int    `json:"serverMaxU"`
	RotationDeg int    `json:"rotationDeg"`
}

// Site 는 데이터센터(센터) 정보.
type Site struct {
	SiteID string `json:"siteId"`
	Name   string `json:"name"`
	Racks  []Rack `json:"racks"`
}

// Device 는 랙에 마운트된 장비(호스트).
type Device struct {
	DeviceID     string `json:"deviceId"`
	SiteID       string `json:"siteId"`
	RackID       string `json:"rackId"`
	StartU       int    `json:"startU"`
	EndU         int    `json:"endU"`
	FormFactorU  int    `json:"formFactorU"`
	Hostname     string `json:"hostname"`
	IsTemp       bool   `json:"isTemp"`
	Vendor       string `json:"vendor"`
	Model        string `json:"model"`
	DeviceType   string `json:"deviceType"`
	Orientation  string `json:"orientation"`
	IsSimulation bool   `json:"isSimulation"`
	RowNum       int    `json:"rowNum,omitempty"`
}

// InvalidItem 은 할당 불가 항목과 사유.
type InvalidItem struct {
	RowNum   int    `json:"rowNum"`
	DcName   string `json:"dcName"`
	RackID   string `json:"rackId"`
	URange   string `json:"uRange"`
	Hostname string `json:"hostname"`
	Model    string `json:"model"`
	Reason   string `json:"reason"`
}

// Summary 는 검증 요약.
type Summary struct {
	Total        int `json:"total"`
	SuccessCount int `json:"successCount"`
	FailCount    int `json:"failCount"`
}

// ValidationResult 는 CSV 검증 결과 (JS validateCsvAllocations 와 동일 구조).
type ValidationResult struct {
	ValidList         []Device      `json:"validList"`
	InvalidList       []InvalidItem `json:"invalidList"`
	NewlyCreatedRacks []Rack        `json:"newlyCreatedRacks"`
	TargetSites       []string      `json:"targetSites"`
	Summary           Summary       `json:"summary"`
}
