# vc_password_update

여러 vCenter를 순회하며 **admin 계정(`Administrator@vsphere.local`) 권한으로** 대상 계정
(기본 `lscsystems@vsphere.local`)의 비밀번호를 **기존과 동일한 값으로 강제 재설정**하는
도구입니다.

vCenter 비밀번호는 만료일 자체를 늘릴 수 없어 값을 "변경"해야만 하는데, 실제 비밀번호
값은 바꾸고 싶지 않은 경우(그 계정을 참조하는 다른 자동화/연동이 있는 경우 등) 이 도구로
"같은 값 재설정"을 통해 만료 타이머만 리셋합니다. admin 강제 재설정(SSO Admin API)은
직전 비밀번호 재사용 금지 정책(`ProhibitedPreviousPasswordsCount`)을 우회하므로 같은
값으로도 성공합니다 — 랩 vCenter(192.168.0.50)에서 직접 검증했습니다(§6 참고).

제어 계층(Bash `run.sh` / `cron_wrapper.sh`)과 실행 계층(Go 바이너리 `vc_password_update`)을
분리했습니다.

> ⚠️ **이 도구는 실제로 vCenter 계정의 비밀번호를 변경(write)합니다.**

---

## ⚠️ 주의사항 (Disclaimer)

본 스크립트 및 툴은 **100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을
권장**합니다.

이 도구는 설정(비밀번호) 변경 스크립트이므로, **실행 후 반드시 랜덤한 vCenter 몇 개를
골라 직접 로그인하거나 `-list` 성격의 재확인 절차로 실제로 갱신되었는지 확인**하십시오.
종료 코드 0 이 곧 모든 vCenter에서 실제로 반영되었음을 100% 보장하지는 않습니다.

---

## 1. 빌드 및 설치 방법

### 1.1 요구사항

| 항목 | 값 |
|---|---|
| Go | 1.26.5 이상 (`go.mod` 기준) |
| OS | Linux (Rocky Linux 8 에서 검증) |
| 외부 명령 | `openssl` (대상 계정 비밀번호 복호화에 사용) |
| 네트워크 | 각 vCenter 443/tcp 접근 |

### 1.2 빌드 (폐쇄망 지원)

의존성(govmomi + ssoadmin/sts)이 공유 `govendor/`에 모두 포함되어 있어, **저장소를
통째로 내려받으면 인터넷 없이 빌드**됩니다.

```bash
git clone <저장소 주소>
cd <저장소>/.claude/VM/vc_password_update
bash setup.sh
```

`setup.sh`는 `../../공통/govendor/govmomi-0.55.1-vc-password-update`를 이 폴더의
`vendor/`로 심볼릭 링크한 뒤 `GOFLAGS=-mod=vendor`, `GOPROXY=off`로 빌드합니다(인터넷을
조회하지 않음). 빌드가 끝나면 `vc_password_update` 바이너리가 생깁니다.

### 1.3 입력 준비

`vcenter.txt.example`을 복사해 실제 vCenter 목록으로 채웁니다.

```bash
cp vcenter.txt.example vcenter.txt
```

대상 계정(기본 `lscsystems@vsphere.local`)의 비밀번호는 **직접 이 저장소에 두지 않고**,
기존 `passwd_update.sh`(`.claude/VM/V2/passwd_update.sh`)로 만든 `secret/` 폴더를
그대로 가리킵니다.

```bash
# 예: 이미 passwd_update.sh 로 저장해 둔 폴더가 /home/V2/secret 이라면
./run.sh -dir /home/V2/secret -vc vcenter.txt
```

`-dir`에 지정한 폴더 안에서 `key`(암호화 키)와
`vcenter_<계정, '@'/'./'는 그대로>.enc`(예: `vcenter_lscsystems@vsphere.local.enc`)
파일을 찾습니다 — `passwd_update.sh`가 만드는 파일 그대로입니다. 이 대상 계정의
비밀번호가 아직 저장되어 있지 않다면 먼저 `passwd_update.sh`를 실행해 저장하십시오.

### 1.4 admin 계정 비밀번호

admin(`Administrator@vsphere.local`) 비밀번호는 `.enc` 파일로 저장하지 않고, **환경변수
`ADMIN_PASSWORD`로만** 받습니다.

```bash
read -rsp 'admin 계정 비밀번호: ' ADMIN_PASSWORD; export ADMIN_PASSWORD; echo
./run.sh -dir /home/V2/secret -vc vcenter.txt
```

`run.sh`는 `ADMIN_PASSWORD`가 비어 있고 터미널이 연결되어 있으면 직접 물어봅니다. 크론
등 비대화형 실행은 `cron_wrapper.sh`(§2.3)를 쓰십시오 — crontab 라인에 비밀번호를 직접
적지 않습니다.

### 1.5 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 에 포함된 디렉터리로 옮기거나, 실행 파일이 있는 경로를 PATH 에
추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
sudo cp vc_password_update /usr/local/bin/
```

단, `run.sh`/`cron_wrapper.sh`는 자기 위치 기준으로 바이너리와 입력 파일을 찾으므로,
크론 등록이나 전체 흐름을 돌릴 때는 프로젝트 폴더 안의 스크립트를 쓰는 것을
권장합니다.

---

## 2. 사용 방법

### 2.1 수동 1회 실행 (권장 진입점)

```bash
./run.sh -dir /home/V2/secret -vc vcenter.txt
```

바이너리가 없으면 `run.sh`가 자동으로 `setup.sh`를 호출해 빌드합니다.

### 2.2 Go 바이너리 직접 실행

```bash
export ADMIN_PASSWORD='...'
./vc_password_update -dir /home/V2/secret -vc vcenter.txt
```

### 2.3 crontab 등록 (85일 주기)

cron의 day-of-month 필드는 매월 1일 기준으로 리셋되어 `*/85`로는 "정확히 85일마다"를
표현할 수 없습니다. 그래서 **매일 실행되도록 등록**하고, `cron_wrapper.sh`가 내부적으로
"마지막 성공 실행 이후 85일이 지났는지"를 직접 계산해서 지났을 때만 실제로 갱신합니다.
일부 vCenter만 실패한 경우에는 기준일을 갱신하지 않아 다음날 다시 시도합니다.

```bash
# 1) admin 비밀번호를 파일로 저장 (root 만 읽기)
echo 'admin 비밀번호' > admin_password.secret
chmod 600 admin_password.secret

# 2) 대상 vCenter 목록 준비
cp vcenter.txt.example vcenter.txt   # 실제 값으로 채움

# 3) VC_SECRET_DIR 환경변수로 passwd_update.sh 의 secret/ 폴더 경로를 알려준다
#    (crontab -e 에서 직접 등록)
0 3 * * * VC_SECRET_DIR=/home/V2/secret /경로/vc_password_update/cron_wrapper.sh >> /경로/vc_password_update/cron.log 2>&1
```

`cron_wrapper.sh`가 참고하는 환경변수:

| 변수 | 기본값 | 설명 |
|---|---|---|
| `VC_SECRET_DIR` | `./secret` | `-dir`로 전달할 경로 (passwd_update.sh 의 secret 폴더) |
| `VC_LIST_FILE` | `./vcenter.txt` | `-vc`로 전달할 경로 |

### 2.4 실행 후 확인

`ResetPersonPassword` 성공(exit 0)은 "vCenter가 요청을 받아들였다"는 뜻입니다.
비정기적으로 랜덤한 vCenter 몇 개를 골라 실제 로그인이 여전히 되는지, SSO 계정 관리
화면에서 최근 비밀번호 변경 시각이 갱신됐는지 직접 확인하십시오(§ 주의사항 참고).

---

## 3. 옵션별 상세 설명

### 3.1 `vc_password_update` 플래그

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-dir` | (필수) | `key` + `vcenter_<계정>.enc` 가 있는 secret 폴더 경로 |
| `-vc` | (필수) | vCenter 목록 파일 (한 줄에 IP 하나, `#` 주석) |
| `-id` | `lscsystems@vsphere.local` | 비밀번호를 갱신할 대상 계정 |
| `-adminId` | `Administrator@vsphere.local` | 로그인에 쓸 admin 계정 |
| `-timeout` | `30` | vCenter 한 대당 접속 제한시간(초). 응답 없는 vCenter가 전체 실행을 막지 않게 함 |

### 3.2 환경변수

| 변수 | 필수 | 설명 |
|---|---|---|
| `ADMIN_PASSWORD` | 예 | admin 계정 로그인 비밀번호 |

### 3.3 `run.sh` / `cron_wrapper.sh`

`run.sh`는 `vc_password_update`에 인자를 그대로 전달합니다 (바이너리 없으면 자동 빌드,
`ADMIN_PASSWORD` 없고 대화형이면 프롬프트). `cron_wrapper.sh` 옵션은 §2.3 표 참고.

### 3.4 종료 코드

| 코드 | 의미 |
|---|---|
| `0` | 전체 vCenter 성공 |
| `1` | 일부 또는 전체 vCenter 실패 (연결 실패/계정 없음/재설정 거부 등) |
| `2` | 필수 인자 누락 또는 `ADMIN_PASSWORD` 미설정 — **아무 vCenter도 건드리지 않음** |

---

## 4. 문서별 설명

| 파일 | 역할 |
|---|---|
| `README.md` | 이 문서 (빌드/사용/옵션) |
| `ARCHITECTURE.md` | 폴더·파일별 역할 표 |
| `WORKFLOW.md` | 실행 흐름도 (mermaid) |
| `CHANGELOG.md` | 날짜순 변경 기록 (최신이 위) |
| `PR_CHECKLIST.md` | 배포/수정 전 확인 목록 |
| `계획서.md` | 최초 기획 문서 (가정/설계 결정 배경) |
| `사용법.txt` | 수동 테스트용 명령어 모음 (주석 포함) |
| `main.go` | 실제 작업(로그인/복호화/재설정) — 단일 파일, 별도 패키지 분리 없음 |
| `run.sh` | 실행 편의 (자동 빌드, 인자 전달, 대화형 비밀번호 입력) |
| `cron_wrapper.sh` | crontab 진입점 (85일 경과 체크 + 비대화형 비밀번호 로딩) |
| `setup.sh` | 폐쇄망 오프라인 빌드 |
| `vendor/` | 빌드 의존성 심볼릭 링크 (공유 govendor, git 비대상) |
| `vcenter.txt.example` | vCenter 목록 서식 예시 |

---

## 5. 알려진 한계

- **admin 계정은 `Administrator@vsphere.local` 고정**입니다. 다른 SSO 도메인을 쓰는
  환경이면 `-adminId`로 지정하십시오.
- **대상 계정 하나만 갱신합니다.** 여러 계정을 한 번에 갱신하려면 `-id`를 바꿔 여러 번
  실행해야 합니다.
- **STS 토큰 발급은 시계에 민감합니다.** 이 도구를 실행하는 서버와 각 vCenter의 시계가
  크게 어긋나 있으면(NTP 미동기화 등) `STS 토큰 발급 실패`로 끝날 수 있습니다 — 랩
  환경에서 실제로 이 문제를 겪었고, vCenter 시계를 맞춘 뒤 해결됨을 확인했습니다.
- **비밀번호 복잡도 정책은 그대로 적용됩니다.** `-dir`에 저장된 값이 대상 vCenter의
  비밀번호 정책(길이/문자 조합 등)을 만족하지 못하면 재설정이 거부됩니다.
- **크론 85일 주기는 "매일 체크 + 기준일 비교" 방식입니다.** 서버가 장기간 꺼져 있다가
  85일을 훌쩍 넘겨 다시 켜지면, 켜진 직후 첫 체크에서 곧바로 실행됩니다(의도된 동작).

---

## 6. 검증 이력

Rocky Linux 8 / go1.26.5 / vCenter 8.0.3 (192.168.0.50, govmomi v0.55.1) 환경에서
확인했습니다.

- 빈 모듈 캐시 + `GOPROXY=off`로 **폐쇄망 오프라인 빌드** 성공
- 실제 vCenter 대상으로 `lscsystems` 계정 비밀번호를 admin 권한으로 재설정 성공
- **핵심 가정 검증**: 비밀번호 이력 정책(`ProhibitedPreviousPasswordsCount=5`)이 걸려
  있는 상태에서, **같은 값으로 연속 두 번 admin 재설정**해도 둘 다 성공함을 확인
  (self-change 였다면 거부되었을 상황) — "같은 비밀번호로 admin 재설정 = 만료 타이머만
  리셋" 설계가 유효함을 실측으로 확인
- 응답 없는 vCenter(존재하지 않는 IP)를 목록에 섞어도 `-timeout` 이후 해당 항목만
  실패 처리하고 나머지 vCenter는 계속 처리함을 확인
- `ADMIN_PASSWORD` 미설정 시 vCenter 접속 전에 오류로 종료함을 확인
- `run.sh`(자동 빌드 + 인자 전달), `cron_wrapper.sh`(85일 경과 체크 — 최초 실행/직후
  재실행 스킵 모두)를 실제 vCenter 대상으로 end-to-end 확인

자세한 내용은 [`CHANGELOG.md`](CHANGELOG.md) 참고.
