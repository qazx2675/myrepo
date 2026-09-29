# 데이터 스키마 (수집기 → 웹 UI)

공유폴더 `data\` 아래에 생성되는 파일. `file://`/UNC 환경에서 `fetch()` 가 막히므로 모두 `<script>` 로 읽는 `.js` 파일이다.
JSON 부분은 `encoding/json` 출력 그대로(키 순서 무관). 값이 없으면 필드 생략 가능(UI 는 없는 필드를 `-` 로 표시).

## data/manifest.js

```js
window.VCP_MANIFEST = {
  "generated": "2026-09-29T12:00:41+09:00",      // 이번 수집 종료 시각 (RFC3339)
  "vcenters": [
    {
      "id": "vc01",                               // conf [vcenters] 의 id (파일명 = data/vc01.js)
      "name": "192.168.0.50",                     // URL 의 호스트명 (트리 최상위 표시명)
      "url": "https://192.168.0.50",
      "instanceUuid": "70f8ce88-a9ed-4c15-adfc-b4de5d88cf0b",
      "version": "8.0.3", "build": "24322831",
      "status": "ok",                             // "ok" | "fail"
      "error": "",                                // fail 사유 (이번 수집)
      "collectedAt": "2026-09-29T12:00:30+09:00", // data/vc01.js 가 만들어진 시각 (fail 이면 직전 성공 시각, 성공 이력 없으면 "")
      "counts": { "hosts": 1, "vms": 3, "clusters": 0 }
    }
  ]
};
```

## data/index.js (검색용 경량 인덱스, 전체 vCenter)

```js
window.VCP_INDEX = [
  // [vcId, type, moref, name, "ip1 ip2", hostName, guestHostName]
  ["vc01", "H", "host-12",  "192.168.0.59", "", "", ""],
  ["vc01", "V", "vm-3052",  "vcenter", "192.168.0.50", "192.168.0.59", "localhost"],
  ["vc01", "C", "domain-c8", "HPC-Cluster", "", "", ""]
];
```
- type: `H`=HostSystem, `V`=VirtualMachine, `C`=ClusterComputeResource
- ips: VM 의 guest IP 전부(IPv4 우선, 공백 구분). Host/Cluster 는 `""`.
- hostName: VM 이 올라간 ESXi 호스트 이름. guestHostName: VM guest.hostName.
- 수집 실패한 vCenter 는 직전 성공 때의 항목을 그대로 유지한다.

## data/<vcId>.js (vCenter별 상세)

```js
window.VCP_DATA = window.VCP_DATA || {};
window.VCP_DATA["vc01"] = {
  "id": "vc01", "name": "192.168.0.50", "url": "https://192.168.0.50",
  "instanceUuid": "70f8ce88-...", "version": "8.0.3", "build": "24322831",
  "collectedAt": "2026-09-29T12:00:30+09:00",
  "rootChildren": ["datacenter-3"],              // 트리 최상위(vCenter 노드) 바로 아래
  "objects": {
    "datacenter-3": { "type": "Datacenter", "name": "HPC", "parent": "", "children": ["group-h5"] },
    "group-h5":     { "type": "Folder", "name": "task", "parent": "datacenter-3", "children": ["host-12"] },
    "domain-c8":    { "type": "ClusterComputeResource", "name": "HPC-Cluster", "parent": "datacenter-3", "children": ["host-20"],
                      "numHosts": 2, "numEffectiveHosts": 2, "totalCpuMhz": 96000, "totalMemMB": 524288,
                      "usedCpuMhz": 1200, "usedMemMB": 20480, "drsEnabled": true, "haEnabled": false,
                      "overallStatus": "green", "datastores": ["datastore-15"], "networks": ["network-17"] },
    "host-12": { "type": "HostSystem", "name": "192.168.0.59", "parent": "group-h5", "children": ["vm-3052"],
                 "connectionState": "connected",   // connected | disconnected | notResponding
                 "powerState": "poweredOn", "inMaintenance": false, "overallStatus": "green",
                 "vendor": "VMware, Inc.", "model": "VMware7,1", "cpuModel": "Intel(R) Xeon(R) ...",
                 "cpuSockets": 2, "cpuCores": 16, "cpuThreads": 32, "cpuMhzTotal": 48000, "cpuMhzUsed": 1500,
                 "memTotalMB": 262144, "memUsedMB": 40000,
                 "esxVersion": "8.0.3", "esxBuild": "24280767", "esxFullName": "VMware ESXi 8.0.3 build-24280767",
                 "bootTime": "2026-09-01T09:00:00+09:00", "uptimeSec": 2419200,
                 "datastores": ["datastore-15"], "networks": ["network-17"] },
    "vm-3052": { "type": "VirtualMachine", "name": "vcenter", "parent": "host-12", "host": "host-12",
                 "powerState": "poweredOn",        // poweredOn | poweredOff | suspended
                 "connectionState": "connected", "overallStatus": "green", "template": false,
                 "guestFullName": "Other 3.x or later Linux (64-bit)", "guestId": "other3xLinux64Guest",
                 "guestHostName": "localhost", "ips": ["192.168.0.50"],
                 "toolsRunning": "guestToolsRunning", "toolsVersionStatus": "guestToolsUnmanaged", "toolsVersion": "12389",
                 "numCpu": 2, "memMB": 8192, "cpuUsageMhz": 0, "guestMemUsageMB": 0, "hostMemUsageMB": 8000,
                 "storageCommitted": 87832424448, "storageUncommitted": 0,   // bytes
                 "hwVersion": "vmx-10", "annotation": "",
                 "disks": [ { "label": "Hard disk 1", "capacityBytes": 52143636480, "thin": true, "datastore": "datastore-15", "file": "[datastore1] vcenter/vcenter.vmdk" } ],
                 "nics":  [ { "label": "Network adapter 1", "network": "VM Network", "mac": "00:0c:29:..", "connected": true } ],
                 "datastores": ["datastore-15"], "networks": ["network-17"] }
  },
  "datastores": { "datastore-15": { "name": "datastore1", "type": "VMFS", "capacity": 1099511627776, "free": 549755813888 } },
  "networks":   { "network-17":   { "name": "VM Network" } }
};
```

### 트리 규칙 (Hosts and Clusters 뷰)
- rootFolder, Datacenter 의 hostFolder 는 숨긴다(자식은 한 단계 위로 붙는다).
- vmFolder/datastoreFolder/networkFolder 계열 폴더는 포함하지 않는다.
- 단독 호스트의 ComputeResource 는 숨기고 HostSystem 을 그 상위에 직접 붙인다.
- VM 의 트리 부모는 `runtime.host`. 호스트가 없는 VM 은 제외한다.
- `children` 순서: 폴더 → 클러스터 → 호스트 → VM, 각 그룹 안에서는 이름 오름차순.

### 딥링크 (vSphere Client 8.0.3)
```
{url}/ui/app/{vm|host|cluster|datacenter|folder};nav=h/urn:vmomi:{Type}:{moref}:{instanceUuid}/summary
```
런처 링크: `vcportal://open?url=<위 딥링크를 encodeURIComponent>`
