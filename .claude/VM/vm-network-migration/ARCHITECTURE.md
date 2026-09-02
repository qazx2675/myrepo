# ARCHITECTURE.md

`vm-network-migration` 의 폴더/파일별 역할입니다. 전체를 읽지 않고도 어디를 고쳐야
할지 찾기 위한 표입니다.

## 전체 구조

제어 계층(Bash)과 실행 계층(Go)을 분리했습니다. `run.sh` 는 단계 순서와 실패 시
롤백만 결정하고, vSphere API 호출은 전부 Go 바이너리가 합니다.

```
run.sh  ──(종료 코드로 분기)──>  bin/nm-*  ──(govmomi)──>  vCenter
   │                                 │
   └── 실패 시 nm-rollback 호출       └── state_{user}.json 읽기/쓰기
```

## 파일별 역할

| 폴더/파일 | 역할 |
|---|---|
| `run.sh` | 사용자 진입점. 단계 순서 제어, 종료 코드 확인, 자동 롤백, 대화형 프롬프트 |
| `setup.sh` | 폐쇄망 오프라인 빌드 (`-mod=vendor`, `GOPROXY=off`) |
| `cmd/backup/` | **Step 0** 현재 NIC 상태를 읽어 상태 파일 생성. 목표 포트그룹 확정 |
| `cmd/pgcreate/` | **Step 2** BM 호스트 vSwitch 에 포트그룹 생성 (worklist 기준) |
| `cmd/disconnect/` | **Step 1** 대상 VM NIC 연결 해제 |
| `cmd/connect/` | **Step 3** NIC 백킹을 신규 포트그룹으로 교체 후 재연결 |
| `cmd/verify/` | **Step 4** 인벤토리 재조회로 실제 반영 여부 검증 |
| `cmd/rollback/` | 역순 실행(3-Undo → 1-Undo)으로 원복. `-prune` 으로 대상에서 제외 |
| `internal/cli/` | 공통 플래그·결과 리포트·종료 코드 규약. 모든 바이너리가 공유 |
| `internal/config/` | 입력 파일 파싱(`vcenter.txt`/`{user}.txt`/`vswitch_{user}.txt`)과 비밀번호 조회 |
| `internal/state/` | `state_{user}.json` 읽기/원자적 쓰기, 레코드 필터·제외 |
| `internal/vsphere/` | vCenter 접속, 인벤토리 색인, NIC/포트그룹 조작 |
| `internal/steps/` | 해제·연결·검증·롤백이 공유하는 "레코드마다 VM 처리" 뼈대 |
| `internal/pool/` | 세마포어 기반 워커 풀 (동시 실행 개수 제한) |
| `vendor/` | Go 의존성 (govmomi 등). 폐쇄망 빌드용, 직접 수정하지 않음 |
| `*.example` | 입력 파일 서식 예시 |

## internal/vsphere 안쪽

| 파일 | 역할 |
|---|---|
| `session.go` | `Connect`/`Fleet`(다중 vCenter), VM·호스트·네트워크 색인, VM 조회(이름/UUID) |
| `nic.go` | NIC 상태 읽기, 백킹/연결 상태 변경(`ReconfigVM_Task`), 포트그룹 생성 |

## 수정 요청별 진입점

| 요청 | 볼 곳 |
|---|---|
| "단계를 추가/순서 변경" | `run.sh` 본체 + 새 `cmd/<이름>/` |
| "플래그 추가" | `internal/cli/cli.go` (공통) 또는 해당 `cmd/*/main.go` (고유) |
| "입력 파일 형식 변경" | `internal/config/config.go` |
| "롤백 대상/조건 변경" | `cmd/rollback/main.go`, `internal/state/state.go` |
| "vSphere API 호출 변경" | `internal/vsphere/nic.go` |
| "동시 실행 방식 변경" | `internal/pool/pool.go` |
| "검증 항목 추가" | `cmd/verify/main.go` |
| "상태 파일 필드 추가" | `internal/state/state.go` + `cmd/backup/main.go` |

## 설계상 지켜야 할 규칙

이 규칙들을 깨면 안전장치가 무력해지므로, 고칠 때 함께 확인하십시오.

1. **모든 단계는 멱등이어야 합니다.** 이미 원하는 상태면 `Reconfigure` Task 를 띄우지
   않고 스킵합니다 (`internal/vsphere/nic.go` 의 `mutate` 콜백이 `false` 반환).
2. **백업 없이는 변경하지 않습니다.** `nm-backup` 이 한 건이라도 실패하면 상태 파일을
   아예 쓰지 않고 중단합니다. 절반만 백업된 채 변경하면 나머지는 롤백할 수 없습니다.
3. **상태 파일을 함부로 덮어쓰지 않습니다.** `nm-backup` 은 기존 파일이 있으면
   `-force` 없이는 거부합니다.
4. **종료 코드 2 는 "아무것도 건드리지 않음"을 뜻합니다.** 설정/입력 오류는 vCenter
   접속 전에 걸러야 합니다.
5. **부분 실패는 그 VM 에 갇혀야 합니다.** 한 대가 실패해도 나머지는 계속 진행하고,
   실패한 VM 만 원복 후 대상에서 제외합니다.
6. **비밀번호는 환경변수로만 받습니다.** 계정 ID 는 `-id` 로 받되(기본 `lscsystems@vsphere.local`),
   비밀번호는 플래그나 설정 파일에 두지 않습니다 — 셸 히스토리와 `ps` 에 남기 때문입니다.
