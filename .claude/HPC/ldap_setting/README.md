# ldap_setting

대상 노드들의 **LDAP · DNS · NTP · autofs 설정을 일괄 적용**하는 도구입니다.

관리 노드에서 Go 바이너리를 실행하면, 사이트별 적용 스크립트를 만들어 `gossh` 로
대상 노드에 밀어 넣고 실행합니다. 적용 후 정합성 검사는 같은 저장소의
[`../ldap_check/`](../ldap_check/) 가 담당합니다.

- 저장소를 통째로 내려받으면 **폐쇄망에서 그대로 빌드**됩니다. 외부 의존성이 없습니다(Go 표준 라이브러리만 사용).
- `CGO_ENABLED=0` 정적 빌드라 RHEL 7 을 포함한 구형 배포판에서도 glibc 버전과 무관하게 동작합니다.
- 대상 노드에 Go 도 jq 도 필요 없습니다. 노드에서 도는 것은 bash 뿐입니다.
- 파일을 **통째로 덮어쓰지 않고 해당 키만 갱신**합니다.

---

## 1. 빌드 및 설치 방법

### 1.1 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| Go 툴체인 | 관리 노드 | `go.mod` 기준 1.26.5. 빌드할 때만 필요 |
| `gossh` | 관리 노드 | 저장소의 [`.claude/공통/gossh/v2`](../../공통/gossh/v2/) |
| bash / awk / sed | 대상 노드 | 기본 설치분으로 충분 |

> **`gossh` 는 v2 를 쓰십시오.** 구버전에는 명령 앞뒤 따옴표를 잘라내는 버그가 있어
> 원격 주입이 깨집니다. `gossh -h` 에 `-cf` 와 `-b` 가 보이면 v2 입니다.

### 1.2 내려받아 빌드하기

```bash
git clone https://github.com/qazx2675/myrepo.git
cd "myrepo/.claude/HPC/ldap_setting"
./setup.sh
```

`setup.sh` 는 `GOPROXY=off` 로 인터넷 접속을 시도하지 않고 `bin/ldap-config-engine` 을 만듭니다.

수동 빌드:

```bash
CGO_ENABLED=0 go build -o bin/ldap-config-engine ./cmd/ldap-config-engine
```

### 1.3 설정 파일 채우기

```bash
cp conf/ldap_config.conf.sample conf/ldap_config.conf
cp conf/assets.txt.sample       conf/assets.txt
chmod 600 conf/ldap_config.conf
```

- `conf/ldap_config.conf` — 인프라별 DNS/NTP/LDAP 값과 사이트 규칙.
  **bindpw 가 평문이므로 커밋 금지** (`.gitignore` 등록됨).
  이 파일은 `../ldap_check/ldap_config.conf` 와 **같은 내용이어야 합니다.**
- `conf/assets.txt` — 자산현황. `hostname<TAB>site` 한 줄에 하나.

```
svr001	a1
svr002	a3
```

### 1.4 설치 확인

```bash
go test ./...     # 단위 테스트
./test_all.sh     # 장비 없이 돌리는 왕복 회귀 테스트
```

`test_all.sh` 는 적용 결과를 `../ldap_check/ldap_check.sh` 로 검사하므로 **그 디렉터리가 함께 있어야 합니다.**
다른 위치에 뒀다면 `CHECK=<경로> ./test_all.sh` 로 지정하십시오.

---

## 2. 사용 방법

### 2.1 표준 절차 — 반드시 이 순서로

```bash
# 1) 무엇이 바뀔지 먼저 확인 (파일을 건드리지 않음)
./scripts/deploy_ldap.sh -infra zxcv -dry-run

# 2) 소수 노드에 먼저 적용하고 로그인·id·ls /appl 확인
./bin/ldap-config-engine -config conf/ldap_config.conf -assets conf/assets.txt \
                         -infra zxcv -host svr001

# 3) 문제 없으면 전체 적용 + 검증
./scripts/deploy_ldap.sh -infra zxcv

# 4) 나중에 검증만 다시
./scripts/deploy_ldap.sh -infra zxcv -check-only
```

### 2.2 엔진 직접 실행

```bash
./bin/ldap-config-engine -config conf/ldap_config.conf \
                         -assets conf/assets.txt \
                         -infra zxcv -dry-run
```

```
인프라=zxcv  대상=120대  사이트=a1,a3  모드=DRY-RUN (실제 변경 없음)
----------------------------------------------------------------------
[a1] 80대 → gossh 전송
  svr001                   OK
  svr002                   NOCHANGE
...
----------------------------------------------------------------------
NOCHANGE     35대
OK           85대
```

### 2.3 생성될 스크립트를 눈으로 보기

```bash
./bin/ldap-config-engine -config conf/ldap_config.conf -infra zxcv -site a1 -print-script
```

무엇이 노드에서 실행되는지 그대로 나옵니다. 대규모 적용 전에 한 번 읽어 보십시오.

### 2.4 실제 장비 없이 시험하기

`-root` 를 주면 `/etc` 대신 그 아래에 기록하고, **서비스도 재시작하지 않습니다.**

```bash
./bin/ldap-config-engine ... -infra zxcv -root /tmp/fixture
ROOT=/tmp/fixture bash ../ldap_check/ldap_check.sh
```

---

## 3. 옵션별 상세 설명

### 3.1 `ldap-config-engine`

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-config` | `./ldap_config.conf` | 설정 파일 경로 |
| `-assets` | `./assets.txt` | 자산현황 파일 (`hostname<TAB>site`) |
| `-infra` | *(없음, 필수)* | 대상 인프라 이름. **기본값을 두지 않습니다** — 다른 인프라 값을 실수로 밀어 넣는 사고를 막기 위해서입니다 |
| `-site` | *(전체)* | 이 사이트만 처리 |
| `-host` | *(전체)* | 이 호스트만 처리. 소수 노드 선행 검증에 사용 |
| `-dry-run` | `false` | 파일을 바꾸지 않고 바뀔 내용(diff)만 보고. 서비스도 재시작하지 않음 |
| `-print-script` | `false` | 생성될 적용 스크립트를 표준출력으로 찍고 종료. `-site` 필요 |
| `-root` | *(빈값 = 실제 `/etc`)* | 원격에서 기록할 루트. 테스트용. **값이 있으면 서비스 재시작을 건너뜁니다** |
| `-gossh` | `gossh` | gossh 실행 파일 경로 |
| `-u` | `root` | SSH 접속 계정 |
| `-p` | *(없음)* | SSH 비밀번호 |
| `-i` | *(없음)* | SSH 키 파일 |
| `-P` | `22` | SSH 포트 |
| `-c` | *(gossh 기본)* | 동시 접속 수 |
| `-t` | *(gossh 기본)* | 접속 타임아웃(초) |
| `-remote-path` | `/root/ldap_apply.sh` | 원격에 잠시 떨어뜨릴 스크립트 경로. 실행 후 삭제 |

종료 코드: `0` 정상 / `1` 설정·인자 오류 / `2` 일부 노드 FAIL·응답 없음.

### 3.2 `scripts/deploy_ldap.sh`

| 옵션 | 설명 |
|---|---|
| `-infra <이름>` | 대상 인프라 (필수) |
| `-dry-run` | 적용 없이 확인만 |
| `-config <경로>` | 설정 파일 (환경변수 `CONFIG` 로도 지정) |
| `-assets <경로>` | 자산현황 파일 (환경변수 `ASSETS`) |
| `-check-only` | 설정 적용을 건너뛰고 검증만 |
| `-h` | 도움말 |

| 환경변수 | 기본값 | 설명 |
|---|---|---|
| `SHARED_CHECK` | `/user/asdf/ldap_check.sh` | 공유 경로의 검사 스크립트 위치 |
| `CHECK_LOCAL` | `../../ldap_check/ldap_check.sh` | 공유 경로가 없는 노드에 밀어 넣을 원본 |

### 3.3 `ldap_config.conf` 키

| 키 | 설명 |
|---|---|
| `s4.enabled` | `true` 면 hostname 접두사 예외 규칙을 켭니다 |
| `s4.prefix` | 예외를 적용할 hostname 접두사 (예: `s4`) |
| `s4.services` | `nslcd` = sssd 대신 nslcd 설정, `ntp` = chrony 대신 ntp 설정 |
| `infra.<이름>.dns` | 이 인프라의 DNS. 쉼표 구분 |
| `infra.<이름>.ntp` | 이 인프라의 NTP. 쉼표 구분 |
| `infra.<이름>.uri1~3` | LDAP 서버. `NONE` 이면 설정에서 완전히 제외 |
| `infra.<이름>.binddn` / `bindpw` | 바인드 자격증명 |
| `infra.<이름>.site.<사이트>.uri_order` | 그 사이트에 기록할 URI 순서. `uri1,uri2,uri3` 처럼 키 이름으로 씀 |
| `infra.<이름>.site.<사이트>.storage` | `auto.appl` 의 storage 이름. **인프라 안에서 고유해야 합니다** (사이트 판별 키) |
| `infra.<이름>.site.<사이트>.mountpoint` | `auto.appl` 의 원격 경로 |

시작 시 설정 파일을 검증합니다. storage 중복, `uri_order` 가 없는 URI 를 가리키는 경우,
필수 키 누락은 **한 대도 건드리기 전에** 오류로 멈춥니다.

---

## 4. 문서별 설명

| 파일 | 내용 |
|---|---|
| `README.md` | 이 문서 |
| `ARCHITECTURE.md` | 폴더/파일별 역할. 어디를 고쳐야 하는지 찾을 때 |
| `CHANGELOG.md` | 날짜순 변경 이력 |
| `PR_CHECKLIST.md` | 배포·수정 전 확인 목록 |
| `교육자료.html` | 이 도구를 어떤 지시와 명령으로 만들었는지 정리한 교육자료. 브라우저로 열어 보십시오 |
| `setup.sh` | 폐쇄망 오프라인 빌드 |
| `test_all.sh` | 장비 없이 돌리는 왕복 회귀 테스트 |
| `conf/*.sample` | 설정·자산 예시 |

### 동작 원리

**사이트별로 스크립트 하나.** 노드마다 다른 스크립트를 만들지 않습니다.
RHEL 버전과 hostname(s4) 판정은 노드 현장에서 이뤄지므로, 같은 사이트의 노드는
모두 같은 스크립트를 받습니다. gossh 호출이 노드 수가 아니라 **사이트 수(보통 4회)** 로 끝납니다.

**전송은 base64 파이프.** `gossh` 에 파일 전송 기능이 없기도 하고, base64 는
`[A-Za-z0-9+/=]` 만 쓰므로 따옴표·개행·특수문자로 명령이 깨질 일이 없습니다. `scp` 를 쓰지 않습니다.

**키 단위 수술적 갱신.** `autofs.conf` 나 `sssd.conf` 에는 우리가 건드리지 않는 운영 설정이
함께 들어 있습니다. 해당 키만 바꾸고 없으면 추가하며, 변경 전 원본은
`<파일명>.bak.<타임스탬프>` 로 백업합니다. 두 번 실행하면 두 번째는 `NOCHANGE` 입니다.

**바뀐 파일에 대응하는 서비스만 재시작.**

| 바뀐 파일 | 재시작 |
|---|---|
| `ldap.conf`, `resolv.conf` | 없음 |
| `nslcd.conf` | `nslcd` |
| `sssd.conf` | `sss_cache -E` 후 `sssd` |
| `autofs.conf`, `autofs_ldap_auth.conf`, `auto.appl` | `autofs` |
| `ntp.conf` | `ntpd` |
| `chrony.conf` | `chronyd` |

### 적용 대상 7개 파일

| # | 경로 | 기록 내용 |
|---|---|---|
| 1 | `/etc/openldap/ldap.conf` | `URI`(사이트 순서), `BINDDN`, `BINDPW` |
| 2 | `/etc/autofs_ldap_auth.conf` | `user=`, `secret=` (XML, 퍼미션 600) |
| 3 | `/etc/autofs.conf` | `ldap_uri="..."` — ldap.conf 와 동일 순서 |
| 4 | `/etc/nslcd.conf` 또는 `/etc/sssd/sssd.conf` | OS·s4 규칙에 따라 분기. 퍼미션 600 |
| 5 | `/etc/resolv.conf` | `nameserver` 줄 |
| 6 | `/etc/ntp.conf` 또는 `/etc/chrony.conf` | `server` 줄 (`pool` 줄은 제거) |
| 7 | `/etc/auto.appl` | `/appl` 줄의 storage·mountpoint |

---

## 5. 전역 명령어로 사용하기 (선택 사항)

```bash
sudo ln -sf "$(pwd)/bin/ldap-config-engine" /usr/local/bin/ldap-config-engine
sudo mkdir -p /etc/ldap-automation
sudo cp conf/ldap_config.conf conf/assets.txt /etc/ldap-automation/
sudo chmod 600 /etc/ldap-automation/ldap_config.conf
```

이후 어느 경로에서나:

```bash
ldap-config-engine -config /etc/ldap-automation/ldap_config.conf \
                   -assets /etc/ldap-automation/assets.txt \
                   -infra zxcv -dry-run
```

> `deploy_ldap.sh` 는 자기 위치를 기준으로 `../bin`, `../conf`, `../../ldap_check` 를 찾습니다.
> 전역으로 쓰려면 링크 대신 저장소 경로에서 실행하거나, `-config` / `-assets` / `CHECK_LOCAL` 을
> 절대 경로로 지정하십시오.

해제:

```bash
sudo rm -f /usr/local/bin/ldap-config-engine
```

---

## 6. 주의사항 (Disclaimer)

본 도구의 점검·분석 결과는 100% 신뢰하기보다 **참고용(보조 도구)** 으로 사용하는 것을 권장합니다.

**설정 변경을 적용한 뒤에는 반드시 대상 서버 중 무작위로 몇 대를 직접 접속하여, 설정이
실제로 변경되었는지 눈으로 확인하십시오.** 본 도구는 `/etc/openldap/ldap.conf`,
`/etc/sssd/sssd.conf`, `/etc/nslcd.conf`, `/etc/resolv.conf` 등 **인증과 이름해석에 직결되는
파일**을 수정하고 관련 서비스를 재시작합니다. 잘못 적용될 경우 해당 노드에 로그인이
불가능해질 수 있습니다.

권장 절차:

1. `-dry-run` 으로 바뀔 내용을 먼저 확인합니다.
2. `-host` 로 **소수 노드에 먼저 적용**하고 `id <계정>`, `ls /appl`, `systemctl status sssd` 를 확인합니다.
3. 문제가 없으면 전체에 적용합니다.
4. 적용 후 **무작위 표본을 직접 접속해 확인**합니다.

되돌리기: 변경 전 원본이 같은 디렉터리에 `<파일명>.bak.<타임스탬프>` 로 남습니다.
복원한 뒤 해당 서비스를 재시작하십시오.

```bash
gossh -w hosts.txt "ls -t /etc/sssd/sssd.conf.bak.* | head -1"
gossh -w hosts.txt "cp \$(ls -t /etc/sssd/sssd.conf.bak.* | head -1) /etc/sssd/sssd.conf && systemctl restart sssd"
```

기타:

- `ldap_config.conf` 에는 **bindpw 가 평문으로 들어갑니다.** 퍼미션 600 을 유지하고 커밋하지 마십시오.
- 원격 실행 시 적용 스크립트에 bindpw 가 담겨 `/root/ldap_apply.sh` 로 잠시 내려갑니다.
  퍼미션 600 으로 만들고 실행 직후 삭제하지만, 관리 노드의 `ps` 에는 base64 문자열이 잠깐 보입니다.
- `/etc/resolv.conf` 는 NetworkManager 가 재기동 시 덮어쓸 수 있습니다.
  NM 관리 제외 처리는 이 도구의 범위 밖입니다.
- `sssd.conf` 에 `[domain/...]` 섹션이 없으면 그 노드는 sssd 미구성으로 보고 FAIL 처리합니다.
  파일을 새로 만들어내지 않습니다.
