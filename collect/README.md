# collect — BIOS 전체값 수집 (biosdump)

`list.txt` 의 hostname 마다 `/etc/hosts` 의 `<hostname>-m`(관리망) IP 로 Redfish 에 접속해 **BIOS 전체 속성을 JSON 으로 저장**합니다. 읽기 전용·일회성 수집 도구입니다. 작업 흐름은 [WORKFLOW.md](WORKFLOW.md) 를 참고하세요.

## 1. 빌드 및 설치

외부 패키지를 쓰지 않는(표준 라이브러리만) Go 프로그램이라 **받을 의존성이 없고 폐쇄망에서 그대로 빌드**됩니다. 의존성이 없으므로 `vendor/` 에 복사할 패키지도 없고, `setup.sh` 는 `GOPROXY=off` + `-mod=vendor` 로 인터넷 접속을 시도하지 않습니다.

```bash
git clone <저장소 URL>            # 또는 폴더째 다운로드 후 폐쇄망으로 반입
cd collect
bash setup.sh                    # 오프라인 빌드 -> ./biosdump  (Go 1.20 이상 필요)
```

- Go 가 없는 서버: 저장소에 포함된 빌드 완료 바이너리 `biosdump`(Rocky/RHEL8, 정적)를 그대로 쓰면 됩니다.
- RHEL6(커널 2.6): Go 1.20.x 로 빌드한 바이너리를 `biosdump_os6` 이름으로 같은 폴더에 두면 `collect.sh` 가 자동 선택합니다. (현재 미포함)

## 2. 사용 방법

```bash
vi collect.sh        # 맨 위 USER_ID / USER_PW 에 BMC 계정을 평문으로 입력 (커밋 금지)
vi list.txt          # hostname 한 줄씩 (예시: list.txt.example)
bash collect.sh      # 수집 -> out/<일시>/<hostname>.json
```

`/etc/hosts` 에 `10.0.0.5 host01-m` 형태의 관리망 항목이 있어야 합니다. 없는 호스트는 `[실패]` 로 출력하고 계속 진행합니다.

## 3. 옵션별 상세 설명

`collect.sh` 는 아래 옵션으로 `biosdump` 를 호출합니다. 직접 실행도 가능합니다.

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-list` | `list.txt` | 수집할 hostname 목록 (`#` 로 시작하는 줄·빈 줄 무시) |
| `-hosts` | `/etc/hosts` | 관리망 IP 를 찾을 hosts 파일 |
| `-suffix` | `-m` | 관리망 이름 접미사 (`host01` → `host01-m`) |
| `-out` | `out` | 결과 폴더 (`collect.sh` 는 `out/<일시>` 를 지정) |
| `-user` / `-pass` | (없음) | BMC 계정 (`collect.sh` 의 `USER_ID`/`USER_PW`) |
| `-p` | `5` | 동시 수집 호스트 수 |
| `-timeout` | `120` | 요청당 응답 대기 시간(초). `awaiting headers` 타임아웃이 나면 늘리거나 `-p` 를 줄임 |

JSON 구조: `hostname`, `bmc_ip`, `collected_at`, `systems.<Systems 경로>` 아래에 `Model`·`Manufacturer`·`SerialNumber`·`BiosVersion`·`Bios`(전체 속성)·`Bios_Settings`(대기 중 설정, 있을 때만).

## 4. 문서별 설명

| 문서 | 내용 |
|---|---|
| [README.md](README.md) | 빌드·사용·옵션 (이 문서) |
| [사용법.txt](사용법.txt) | 수동 실행용 명령 모음 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 파일별 역할 |
| [WORKFLOW.md](WORKFLOW.md) / workflow.svg | 작업 흐름도 |
| [CHANGELOG.md](CHANGELOG.md) | 변경 이력 (태그 `collect-v0.1.0` 과 연결) |
| [PR_CHECKLIST.md](PR_CHECKLIST.md) | 수정·배포 전 체크리스트 |

## 주의사항 (Disclaimer)

> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.

- 이 도구는 **읽기 전용(GET)** 이라 설정을 바꾸지 않습니다. 수집 결과를 근거로 설정을 변경하는 작업을 한다면, 변경 후 **랜덤한 서버 몇 대를 직접 확인해 실제로 변경되었는지** 확인하십시오.
- **실제 BMC 에서는 아직 검증되지 않았습니다.** 랩의 mock BMC(Dell R660, HPE DL360 Gen11)로만 확인했습니다. 처음에는 1~2대로 먼저 실행해 보십시오.
- `collect.sh` 에 BMC 비밀번호가 평문으로 들어갑니다. **계정을 채운 채 저장소에 커밋·공유하지 마십시오.** (`chmod 600 collect.sh` 권장)
- BMC 에 호스트당 Basic 인증 접속 기록이 남습니다. 반복 실행은 자제하십시오. TLS 인증서는 검증하지 않습니다(자체 서명 BMC 대응).

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
# 방법 1: 바이너리만 복사 (옵션을 직접 지정해 쓰는 경우)
sudo cp biosdump /usr/local/bin/biosdump
biosdump -list list.txt -user ID -pass PW

# 방법 2: 이 폴더를 PATH 에 추가 (list.txt 는 이 폴더에 두고 사용)
echo 'export PATH="$PATH:/opt/collect"' >> ~/.bashrc      # /opt/collect = 이 폴더를 놓은 경로
```
