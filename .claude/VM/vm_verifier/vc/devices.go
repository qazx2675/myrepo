package vc

import (
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// macsFromDevices는 VM 가상 하드웨어 장치 목록에서 vNIC MAC 주소들을 뽑아 소문자로 정규화한다.
func macsFromDevices(devices []types.BaseVirtualDevice) []string {
	var macs []string
	for _, d := range devices {
		if nic, ok := d.(types.BaseVirtualEthernetCard); ok {
			mac := nic.GetVirtualEthernetCard().MacAddress
			if mac != "" {
				macs = append(macs, strings.ToLower(mac))
			}
		}
	}
	return macs
}
