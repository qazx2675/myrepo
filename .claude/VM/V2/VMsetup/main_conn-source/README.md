# main_conn-source

BM(ESXi 호스트) 여러 대를 vCenter 의 **지정한 폴더(또는 클러스터)** 에 **병렬로 등록**하는 도구입니다.
이미 등록된 호스트는 건너뛰고(PASS), SSL 인증서가 신뢰되지 않는 호스트는 vCenter 가 돌려주는 Thumbprint 를 자동으로 넣어 한 번 재시도합니다.
`vm_setup.sh` 의 첫 실행 단계("호스트 등록")가 `-folderName=Task` 로 이 도구를 부릅니다.

> ⚠️ **이 도구는 실제로 vCenter 에 ESXi 호스트를 추가(write)합니다.**

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 방법

```bash
bash setup.sh        # → main_conn  (V2 공유 vendor 로 오프라인 빌드. OS6 는 ../../setup.sh 가 bin_os6 에서 복사)
```

## 2. 사용 방법

```bash
export VC_PASSWORD='...' ESXI_PASSWORD='...'      # vCenter 계정 / ESXi root 비밀번호 (환경변수로만 받음)
./main_conn -vcTargetIP=<vCenter> -folderName=Task -worklistFile=bm_all.txt
```

| 옵션 | 설명 |
|---|---|
| `-vcTargetIP` | (필수) vCenter 주소 |
| `-folderName` | (필수) 데이터센터의 호스트 및 클러스터 트리에서 등록할 **폴더 또는 클러스터 이름**. 데이터센터 이름을 주면 그 데이터센터의 기본 호스트 폴더 |
| `-worklistFile` | 등록할 호스트 목록(한 줄에 하나, `#` 주석). 기본 `worklist.txt` |
| `-id` | vCenter 계정 (기본 `lscsystems@vsphere.local`) |
| `-datacenter` | 데이터센터가 2개 이상일 때 지정 |
| `-concurrency` | 동시 처리 수 (기본 20) |

- 폴더는 **미리 vCenter 에 만들어 두어야** 합니다. 없으면 "위치(클러스터/폴더)를 찾을 수 없습니다" 로 끝납니다.
- 호스트 이름은 worklist 에 적은 그대로 vCenter 에 등록하고, 등록 여부도 같은 이름으로 찾습니다.
- 일부 실패해도 나머지는 계속하며, 실패가 있으면 종료 메시지에 호스트 이름을 모아 보여 줍니다.

## 3. 동작 순서

1. vCenter 에 1회 로그인(세션 공유), 데이터센터 확정.
2. `-folderName` 을 클러스터 → 폴더 → 데이터센터 순으로 찾음.
3. worklist 의 호스트가 이미 등록됐는지 병렬 확인.
4. 미등록 호스트만 병렬(`-concurrency`)로 `AddHost`/`AddStandaloneHost` — SSL 미신뢰 시 Thumbprint 주입 후 재시도.

## 4. 디렉토리 구조

```
main_conn-source/
├── README.md
├── main.go          # 등록 로직 전체 (vm-setting-go-lang/main_connect.go 와 같은 코드)
├── go.mod / go.sum
├── setup.sh         # vendor 로 폐쇄망 빌드
├── main_conn        # 빌드된 실행 바이너리 (git 제외)
└── vendor           # setup.sh 가 만드는 링크 (git 제외)
```
