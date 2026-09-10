# Network_Change_Integration_Script — 통합 VM 망변경 스크립트

IP 변경 · LDAP 설정 · 포트그룹(VLAN) 이관을 **한 번의 실행**으로 순차 처리합니다.
기존 3개 프로젝트(`ip_change`, `ldap_setting`, `vm-network-migration`)를 그대로
호출하고, 그 앞뒤(전처리·결과 집계·인시던트·롤백)만 이 스크립트가 담당합니다.

> ### 주의사항 (Disclaimer)
>
> 본 스크립트 및 툴은 **100% 신뢰하기보다는 참고용(보조 도구)** 으로 사용하는 것을
> 권장합니다.
>
> 이 도구는 설정을 **실제로 변경(write)** 합니다. 작업 완료 후 반드시
> **무작위로 서버 몇 대를 직접 접속해서 의도한 대로 바뀌었는지 눈으로
> 확인**하십시오. 각 단계의 "성공" 표시가 곧 서비스 정상 동작을 뜻하지는
> 않습니다.
>
> IP 변경 직후 대상 VM 은 **"새 IP 설정 + 옛 VLAN"** 상태입니다(의도된 설계).
> `ip_change` 는 네트워크 서비스를 재시작하지 않으므로 **리부팅 전까지 옛 IP 로
> 통신이 유지**되고, 포트그룹 변경 후 리부팅 시점에 새 IP 가 활성화됩니다.
> 이 사이에 누가 VM 을 리부팅하거나 네트워크를 재시작하면 콘솔 외 접근이
> 불가능해집니다.

---

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망(오프라인) 빌드

**이 폴더(`Network_Change_Integration_Script`) 하나만 있으면 됩니다.**
`myrepo` 의 다른 위치(원본 `ip_change`/`ldap_setting`/`vm-network-migration`)를
상대경로로 참조하지 않고, 세 프로젝트의 소스를 `projects/` 아래에 자체
보관합니다 — 다른 부서로 이 폴더만 단독 이관돼도 그대로 빌드·실행됩니다.

```bash
# myrepo 전체를 내려받았다면:
cd myrepo/.claude/VM/Network_Change_Integration_Script
# 이 폴더만 통째로 복사/이관받았다면, 그 폴더 안에서:
./setup.sh
```

`setup.sh` 가 하는 일:

| 프로젝트 | 위치 | 빌드 방식 |
|---|---|---|
| `ip_change` | `./projects/ip_change` | 표준 라이브러리만 사용, `CGO_ENABLED=0` 정적 빌드 |
| `ldap_setting` | `./projects/ldap_setting` | 표준 라이브러리만 사용, 정적 빌드 |
| `vm-network-migration` | `./projects/vm-network-migration` | `vendor/`(govmomi) 사용, `-mod=vendor GOPROXY=off` |

빌드 결과:
- `bin/ip-change-engine`, `bin/ldap-config-engine` — 이 폴더로 복사됨
- `nm-*` (7종) — `projects/vm-network-migration/bin/` 에 그대로 (이 스크립트가 `run.sh` 를 통해 씀)

Go 툴체인만 있으면 됩니다. 랩 기준 `go1.26.5`.

> `projects/` 아래 세 프로젝트는 원본(`.claude/HPC/ip_change`,
> `.claude/HPC/ldap_setting`, `.claude/VM/vm-network-migration`)의 **스냅샷
> 사본**입니다. 이관 전 원본이 갱신됐다면 이관 직전에 한 번 더 동기화하고,
> 이관 후에는 각자 독립적으로 관리됩니다(자동 동기화 없음). 이 세
> 프로젝트 각각의 `README.md`/`CHANGELOG.md`도 `projects/<이름>/` 안에
> 그대로 들어 있습니다.

### 1.2 설정

```bash
cp integration.conf.sample integration.conf
$EDITOR integration.conf        # 아래 값은 반드시 채웁니다

cp conf/ldap_config.conf.sample conf/ldap_config.conf
cp conf/assets.txt.sample       conf/assets.txt
$EDITOR conf/ldap_config.conf conf/assets.txt   # 실제 LDAP 설정·자산현황 값 채우기
```

| 키 | 설명 |
|---|---|
| `ldap_conf` / `ldap_assets` | LDAP 설정·자산현황 파일 경로 (기본 `./conf/ldap_config.conf` / `./conf/assets.txt`) |
| `vc_id` | vCenter 로그인 계정 (포트그룹 단계) |
| `bm_domain` | BM 호스트에 붙일 도메인 (기본 `seccae.com`) |
| `default_site` | 자산현황에 없는 호스트에 적용할 site (비우면 그런 호스트는 건너뜀) |

`conf/ip_change.conf` 는 실행 시 `integration.conf` 의 `ipchange.*` 키에서
**자동 렌더링**되므로 직접 만들지 않습니다.

> **`conf/ldap_config.conf` / `conf/assets.txt` 는 이 프로젝트가 자체
> 보관하는 사본입니다.** 이 폴더가 다른 부서로 단독 이관될 수 있어, 원본
> `ldap_setting`(`../../HPC/ldap_setting/conf/...`)을 상대경로로 참조하지
> 않고 `conf/` 안에 따로 둡니다. 둘 다 `.gitignore` 로 커밋 차단됩니다.
> **원본 `ldap_setting` 쪽 설정이 바뀌면 이 사본도 함께 수동으로 갱신해야
> 합니다 — 자동 동기화되지 않습니다.**

> **LDAP 대상 인프라는 `integration.conf` 에 두지 않습니다.** 원본
> `ldap_setting` 이 `-infra` 에 기본값을 두지 않는 것과 같은 이유로, 이전
> 작업의 인프라 값이 그대로 남아 다음 실행에서 조용히 다른 인프라에 적용되는
> 사고를 막기 위함입니다. LDAP 단계 실행마다 `--infra <이름>` 을 넘기거나,
> 생략하면 `ldap_config.conf` 에 정의된 인프라 목록을 보여주고 대화형으로
> 물어봅니다(비대화형 실행은 `--infra` 가 없으면 오류로 중단).

### 1.3 사용자 커스텀 영역 (선택)

`lib/common.sh` 의 `user선택()` 함수는 **의도적으로 비어 있습니다.** 사이트마다
작업 계정을 정하는 방법이 달라서, 여기에 로직을 채우거나 항상 `-u <계정>` 으로
지정합니다. 통합 스크립트는 계정을 **딱 한 곳(여기)에서만** 고르고, 하위
프로젝트의 빈 선택 함수는 호출하지 않습니다.

---

## 2. 사용 방법

### 2.1 준비물

작업 디렉터리(이 폴더)에 아래 파일을 둡니다. (모두 `.gitignore` 로 커밋 차단)

| 파일 | 내용 | 예 |
|---|---|---|
| `<계정>.txt` | `VM이름 변경될IP` (공백 구분, 한 줄에 하나) | `web01 10.20.30.11` |
| `vswitch_<계정>.txt` | `BM호스트 [폴더] IP VLAN` — 전처리 대상 (§4) | `esxi7 svc 10.20.30.11 30` |
| `vcenter.txt` | vCenter 주소 (한 줄에 하나) — 포트그룹 단계용 | `192.168.0.50` |

`<계정>.txt` 의 1열은 **도메인 없는 VM 이름**입니다. 이 이름이 곧 vCenter 인벤토리의
VM 이름이자, IP/LDAP 단계에서 SSH 로 접속하는 대상입니다 (**DNS 가 짧은 이름을
해석해야 함**).

### 2.2 비밀번호 (환경변수)

```bash
read -rsp 'SSH PW: ' GOSSH_PW;   export GOSSH_PW;   echo    # IP/LDAP 단계
read -rsp 'vCenter PW: ' VC_PASSWORD; export VC_PASSWORD; echo  # 포트그룹 단계
```

### 2.3 실행

```bash
./change.sh web                 # 계정 web 의 전체 흐름
./change.sh                     # user선택() 로 계정 결정
./change.sh --dry-run web       # 변경 없이 예정만 (아래 4번 참고)
```

전체 흐름:

```
전처리(vswitch 표준화)
  → [C] IP 변경        대상표 출력 → 확인 → ip-change-engine
  → [D] LDAP 설정      대상 인프라 선택(--infra 또는 대화형) → 확인 → ldap-config-engine (site 는 자산현황/기본값)
  → 결과 파일 기록 (§6)
  → 포트그룹 진행할까요?
       y → [E] vm-network-migration run.sh 에 위임
       n → 인시던트 이름 입력 → 저장 (나중에 ./change.sh port <이름>)
```

### 2.4 나중에 이어서 / 재시도 / 롤백

```bash
./change.sh port <인시던트>       # 저장해 둔 인시던트의 포트그룹만 진행
./change.sh --retry ip web        # IP 단계에서 실패한 VM 만 다시
./change.sh --retry ldap web      # LDAP 단계에서 실패한 VM 만 다시
./change.sh rollback <인시던트>    # 역순 원복 (포트그룹 → LDAP → IP)
```

`rollback` 은 포트그룹(`nm run.sh --rollback`)과 LDAP(`ldap-config-engine
-rollback`)을 자동으로 되돌립니다. **IP 변경은 자동 롤백이 없어**(`ip_change` 에
rollback 명령이 없음) 대상 VM 목록을 출력하고 각 노드에서
`<ifcfg>.bak.<STAMP>` 를 수동 복원하도록 안내합니다.

### 2.5 진단

```bash
./change.sh --debug-inventory     # vCenter 가 보고하는 VM/호스트 이름 그대로 덤프
```

"상위폴더가 둘 이상인 환경에서 일부 VM/호스트를 못 찾는다" 는 증상이 나오면,
먼저 이걸로 vCenter 가 실제로 뭐라고 부르는지 확인한 뒤 `<계정>.txt` /
`vswitch_<계정>.txt` 의 표기와 대조합니다.

---

## 3. 옵션별 상세 설명

### 3.1 서브커맨드

| 명령 | 동작 |
|---|---|
| `change.sh [옵션] [계정]` | 전처리 → IP → LDAP → 결과 → 포트그룹 질의 |
| `change.sh port [계정\|인시던트]` | 포트그룹(2차)만 |
| `change.sh rollback <인시던트>` | 인시던트 역순 원복 |
| `change.sh --retry ip\|ldap [계정]` | 해당 단계 실패분만 재시도 |
| `change.sh --debug-inventory` | vCenter 인벤토리 덤프 (변경 없음) |

### 3.2 옵션

| 옵션 | 설명 |
|---|---|
| `-u, --user <계정>` | 작업 계정 (미지정 시 `user선택()` 호출) |
| `-y, --yes` | 확인 프롬프트 건너뜀 (자동화용) |
| `--dry-run` | 실제 변경 없이 예정만. **주의**: `ip-change-engine` 은 dry-run 을 지원하지 않아 **IP 단계는 대상만 출력하고 엔진을 실행하지 않습니다.** LDAP·포트그룹은 각 엔진의 dry-run 으로 diff 를 보여줍니다. |
| `-d1` | 단계별 진입/종료 + 소요시간 |
| `-d2` | 중간 산출물 보존 (`work/`, nm 입력 파일 등을 지우지 않음) |
| `-d3` | `-d2` + gossh raw 출력 + bash 트레이스(`set -x`) |
| `--only C\|D\|E` | 해당 단계만 실행 |
| `--from C\|D\|E` | 해당 단계부터 끝까지 |
| `--folder <이름>` | 전처리 3열(`BM IP VLAN`) 줄에 쓸 폴더명 (비대화형일 때 필수) |
| `--tag <문자열>` | 전처리 가운데 문자열 (기본 `integration.conf` 의 `preprocess_tag`) |
| `--infra <이름>` | LDAP 대상 인프라(`ldap_config.conf` 의 `infra.<이름>`). 미지정 시 대화형 선택(비대화형일 때 필수) |

단계 문자: **C** = IP 변경, **D** = LDAP, **E** = 포트그룹.

### 3.3 전처리 규칙 (§4)

`vswitch_<계정>.txt` 를 `vm-network-migration` 이 요구하는 **정확히 3열**
(`BM호스트.도메인  포트그룹명  VLAN`)로 표준화합니다.

| 입력 형태 | 변환 |
|---|---|
| `BM 폴더 IP VLAN` (4열) | `BM.도메인  폴더-<tag>-<IP앞3옥텟>-0  VLAN` |
| `BM IP VLAN` (3열, 2번째가 IP) | 폴더명을 입력받아 위와 동일하게 |
| `BM PG VLAN` (3열, 2번째가 PG명) | 도메인만 붙이고 그대로 |
| `BM.도메인 PG VLAN` (이미 FQDN) | 변경 없음 |

예: `web01 svc 10.20.30.11 30` → `web01.seccae.com  svc-cae-10-20-30-0  30`
(4번째 옥텟은 항상 `0`)

### 3.4 OS 6 분기 (§8.1)

`integration.conf` 의 `os6_hostgroup` 에 나열된 호스트는 `os6_gossh` /
`os6_bin_dir` 경로를 씁니다. 한 실행에 OS6/일반이 섞여 있으면 **gossh 를 그룹별로
나눠 2회 호출**합니다.

### 3.5 2패스 타임아웃 (§8.2)

`gossh -t` 는 전역 타임아웃이라 크게 잡으면 죽은 노드에서도 그만큼 대기합니다.
그래서:

1. **1차** — `timeout_pass1`(기본 60초)로 전체 실행
2. 타임아웃/실패한 호스트에 **ping 선검사**
3. **2차** — `ping` 되는 호스트만 `timeout_pass2`(기본 600초)로 재실행
4. `ping` 도 안 되면 즉시 실패 확정 (대기 안 함)

### 3.6 결과 파일 (§6)

`integration.conf` 의 `result_dir` (기본 `./results`) 아래:

| 파일 | 내용 |
|---|---|
| `on_off_<계정>.txt` | IP·LDAP **둘 다 성공**한 VM. **별도 전원 on/off 작업 스크립트의 입력** (포트그룹 입력 아님) |
| `ip_ok_<계정>.txt` / `ldap_ok_<계정>.txt` | 단계별 성공 |
| `on_off_<계정>.failed.txt` | 하나라도 실패한 VM 이름만 (재시도 입력) |
| `failed_<계정>.txt` | `VM이름 <코드>` — `C2`=IP실패 / `D3`=LDAP실패 / `CD`=둘다 |

기존 파일은 `.bak.<시점>` 으로 백업 후 새로 씁니다 (항상 "직전 실행 기준").

포트그룹 단계는 `ip_ok_<계정>.txt`(IP 변경 성공 VM)만 대상으로 합니다 —
IP 가 실패한 VM 이 새 VLAN 으로 옮겨져 "옛 IP + 새 VLAN" 으로 고립되는 것을
막기 위함입니다.

---

## 4. 에러코드 대응표

화면 복사가 안 되는 환경을 전제로, 오류 시 화면 첫 줄에 **코드만** 크게 찍습니다.
코드를 받아 적고 아래 표에서 찾으십시오.

| 코드 | 의미 | 대처 |
|---|---|---|
| **A1** | 전처리: vswitch 파일 열기 실패 | 경로·권한 확인 |
| **A2** | 전처리: 케이스 판정 불가 (열 개수가 3·4 아님 등) | 해당 줄을 `BM 폴더 IP VLAN` 또는 `BM PG VLAN` 형식으로 |
| **A3** | 전처리: IP/VLAN 형식 오류 | 3번째 필드가 IP 인지, VLAN 이 숫자인지 확인 |
| **B1** | 검증: `<계정>.txt` 없음 / 사용자가 중단 | 파일 준비, 또는 `-u` 로 계정 지정 |
| **B2** | 검증: `vswitch_<계정>.txt` 없음 | 파일 준비 |
| **B3** | 검증: `<계정>.txt` 형식 오류 | `VM이름 변경될IP` 형식 확인 |
| **C1** | IP: `ip-change-engine` 실행 실패 (설정/입력) | `logs/` 의 엔진 출력 확인, `conf/ip_change.conf` |
| **C2** | IP: 일부/전체 대상에서 변경 실패 | `failed_<계정>.txt` 확인 후 `--retry ip` |
| **D1** | LDAP: 자산현황·기본값 모두로 site 판정 실패 | 자산현황에 호스트 추가, 또는 `default_site` 설정 |
| **D2** | LDAP: `ldap-config-engine` 실행 실패 (설정/입력), 또는 `--infra` 미지정(비대화형)/`ldap_conf` 에서 infra 목록을 못 찾음 | `--infra <이름>` 지정, `ldap_conf` 경로·내용 확인 |
| **D3** | LDAP: 일부/전체 대상에서 변경 실패 | `failed_<계정>.txt` 확인 후 `--retry ldap` |
| **E1** | 포트그룹: VM 을 vCenter 에서 못 찾음 | `--debug-inventory` 로 실제 이름 확인 |
| **E2** | 포트그룹: 동명 VM 이 여러 개 | vCenter 에서 정리 후 재시도 |
| **E3** | 포트그룹: ESXi 호스트를 vCenter 에서 못 찾음 | vswitch 1열 표기 ↔ vCenter 등록 이름 대조 (`--debug-inventory`) |
| **E5** | 포트그룹: BM 호스트에 대응하는 worklist 항목 없음 | vCenter 가 보고한 호스트 이름과 vswitch 1열이 (FQDN/short 무시하고) 같은지 확인 |
| **E9** | 포트그룹: `nm run.sh` 가 실패 (기타) | `logs/` 및 `projects/vm-network-migration/` 의 nm 출력 확인 |
| **F1** | 인시던트: 디렉터리 생성 실패 | `incidents/` 권한 확인 |
| **F2** | 인시던트: meta/상태 파일 손상 | `incidents/<이름>/meta` 직접 확인 |
| **F3** | 인시던트/재시도: 대상이 없음 | 이름 오타, 또는 실패 목록이 비었는지 확인 |
| **G1** | 환경: `gossh` 실행 파일을 찾을 수 없음 | `integration.conf` 의 `gossh` 경로 |
| **G2** | 환경: 프로젝트 바이너리 없음 | `./setup.sh` 재실행 |
| **G3** | 환경: `integration.conf` / `vcenter.txt` 없음 | `.sample` 복사, 파일 준비 |
| **G4** | 환경: `GOSSH_PW` / `VC_PASSWORD` 미설정 | 환경변수 export |

> **base64 / gossh 위험키워드**: `ip-change-engine` · `ldap-config-engine` 은
> base64 를 두 글자마다 공백으로 끊어(`spaceOut`) `reboot`/`halt`/`ddc` 같은
> 위험 키워드가 우연히 생기는 것을 이미 막고 있습니다. 이 스크립트는 gossh 를
> 직접 호출하지 않고 항상 이 엔진들을 경유하므로 해당 이슈가 재발하지 않습니다.

---

## 5. 전역 명령어로 사용하기 (선택 사항)

```bash
sudo ln -s "$(pwd)/change.sh" /usr/local/bin/change
# 이후: change web
```

`change.sh` 는 자기 위치를 기준으로 `lib/` 와 하위 프로젝트를 찾으므로,
심볼릭 링크로 걸어도 동작합니다 (`cd "$(dirname "$0")"`).

---

## 문서별 설명

| 파일 | 내용 |
|---|---|
| `README.md` | 이 문서 — 빌드·사용·옵션·에러코드 |
| `Network_Change_Integration_Plan.md` | 구축 계획서(v2). 설계 근거와 확인된 사항, 미해결 항목 |
| `ARCHITECTURE.md` | 폴더/파일별 역할, 수정 요청별 진입점, 지켜야 할 규칙 |
| `PR_CHECKLIST.md` | 배포/수정 전 점검 목록 |
| `CHANGELOG.md` | 변경 이력 (날짜순) |
| `integration.conf.sample` | 중앙 설정 예시 |
| `lib/*.sh` | 단계별 구현 (common/conf/preprocess/stages/results/incident) |
