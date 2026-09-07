# ldap_check

노드의 **LDAP · DNS · NTP · autofs 설정 정합성을 검사**하는 단독 bash 스크립트입니다.
아무것도 바꾸지 않고 읽기만 합니다.

`gossh` 로 전 노드에 뿌려 일괄 점검하는 용도이며, 설정을 실제로 바꾸는 쪽은
같은 저장소의 [`../ldap_setting/`](../ldap_setting/) 입니다.

- **jq 가 필요 없습니다.** 설정 파일이 평문 `key = value` 입니다.
- bash / awk / sed 만 쓰므로 RHEL 6~10 어디서나 그대로 돕니다. 컴파일이 없습니다.
- 출력이 탭 구분이라 수백 대 결과를 그대로 집계할 수 있습니다.

---

## 1. 빌드 및 설치 방법

bash 스크립트라 **빌드 과정이 없습니다.** 내려받아 실행 권한만 주면 됩니다.

### 1.1 내려받기

```bash
git clone https://github.com/qazx2675/myrepo.git
cd "myrepo/.claude/HPC/ldap_check"
chmod +x ldap_check.sh test_check.sh
```

### 1.2 설정 파일 준비

```bash
cp ldap_config.conf.sample ldap_config.conf
vi ldap_config.conf          # 실제 인프라 값으로 채우기
chmod 600 ldap_config.conf   # bindpw 가 평문으로 들어갑니다
```

`ldap_check.sh` 는 기본적으로 **자기와 같은 디렉터리의 `ldap_config.conf`** 를 읽습니다.

### 1.3 배치

전 노드에 autofs 로 공유되는 관리자 경로에 두 파일을 같이 올려놓는 것이 표준 배치입니다.

```bash
cp ldap_check.sh ldap_config.conf /user/asdf/
chmod 600 /user/asdf/ldap_config.conf
```

마운트가 안 되는 노드가 있어도 괜찮습니다. `../ldap_setting/scripts/deploy_ldap.sh` 가
그런 노드에는 스크립트를 `/root` 로 밀어 넣고 실행한 뒤 지웁니다.

### 1.4 설치 확인

```bash
./test_check.sh
```

실제 장비 없이 fixture 로 10개 시나리오를 검증합니다. `PASS: 10 FAIL: 0` 이 나와야 합니다.

---

## 2. 사용 방법

### 2.1 노드 한 대에서

```bash
bash /user/asdf/ldap_check.sh
```

```
OK	/zxcv	resolv.conf
OK	/zxcv	chrony.conf chrony
OK	/zxcv	ldap.conf a1
OK	/zxcv	autofs.conf
OK	/zxcv	autofs_ldap_auth.conf
OK	/zxcv	sssd.conf
OK	/zxcv	auto.appl a1
```

### 2.2 전 노드 일괄 점검

```bash
gossh -script -w hosts.txt "bash /user/asdf/ldap_check.sh"
```

```
svr001: OK	/zxcv	resolv.conf
svr001: OK	/zxcv	ldap.conf	a1
svr002: FAIL	/unknown	infra-mismatch	dns+ntp=zxcv ldap=zxcv appl=qwer
```

> `gossh` 는 명령에 `/user/` 가 들어가면 autofs 보호를 위해 병렬 수를 350 으로 자동 제한합니다.
> 의도된 안전장치이므로 `-cf` 로 무리하게 늘리지 마십시오.

### 2.3 FAIL 난 호스트만 뽑기

```bash
gossh -script -w hosts.txt "bash /user/asdf/ldap_check.sh" \
  | grep 'FAIL' | cut -d: -f1 | sort -u > ng_hosts.txt
```

### 2.4 실제 장비 없이 시험하기

`ROOT` 를 주면 `/etc` 대신 그 아래를 검사합니다.

```bash
ROOT=/tmp/fixture LDAP_CONFIG=./ldap_config.conf bash ldap_check.sh
```

---

## 3. 옵션별 상세 설명

### 3.1 명령행 인자

인자를 받지 않습니다. 동작은 환경변수로 바꿉니다.

| 환경변수 | 기본값 | 설명 |
|---|---|---|
| `LDAP_CONFIG` | 스크립트와 같은 디렉터리의 `ldap_config.conf` | 기준값 설정 파일 경로 |
| `ROOT` | *(빈값 = 실제 `/etc`)* | 검사 대상 루트. fixture 시험용 |

### 3.2 종료 코드

| 코드 | 의미 |
|---|---|
| `0` | 검사 항목 전부 OK |
| `1` | 설정 불일치 (인프라 혼재 포함) |
| `2` | 판별 불가 — 설정 파일 없음, OS 버전 판정 실패 |

### 3.3 출력 형식

```
<OK|FAIL><TAB>/<infra><TAB><파일명>[<TAB>부가정보]
```

인프라 판별에 실패하면 `/unknown` 이 찍히고, 맨 앞에 원인 줄이 하나 더 나옵니다.

```
FAIL	/unknown	infra-mismatch	dns+ntp=zxcv ldap=zxcv appl=qwer
```

`dns+ntp` / `ldap` / `appl` 중 어느 축이 어긋났는지 바로 보입니다. `?` 는 그 축으로
아예 인프라를 특정하지 못했다는 뜻입니다.

### 3.4 `ldap_config.conf` 키

| 키 | 설명 |
|---|---|
| `s4.enabled` | `true` 면 hostname 접두사 예외 규칙을 켭니다 |
| `s4.prefix` | 예외를 적용할 hostname 접두사 (예: `s4`) |
| `s4.services` | `nslcd` = sssd 대신 nslcd 검사, `ntp` = chrony 대신 ntp 검사 |
| `infra.<이름>.dns` | 인프라 판별 키 1. 쉼표 구분, **순서 무관** 집합 비교 |
| `infra.<이름>.ntp` | 인프라 판별 키 2. 쉼표 구분, **순서 무관** |
| `infra.<이름>.uri1~3` | LDAP 서버. `NONE` 이면 검사에서 완전히 제외 |
| `infra.<이름>.binddn` / `bindpw` | 바인드 자격증명. 파일 내용과 문자열 비교 |
| `infra.<이름>.site.<사이트>.uri_order` | 그 사이트의 URI 순서. **순서까지 엄격 비교** |
| `infra.<이름>.site.<사이트>.storage` | `auto.appl` 의 storage 이름. **사이트 판별 키** |
| `infra.<이름>.site.<사이트>.mountpoint` | `auto.appl` 의 원격 경로 |

> 이 파일은 `../ldap_setting/conf/ldap_config.conf.sample` 과 **같은 내용이어야 합니다.**
> 설정하는 쪽과 검사하는 쪽이 다른 기준을 보면 안 됩니다.

---

## 4. 문서별 설명

| 파일 | 내용 |
|---|---|
| `README.md` | 이 문서 |
| `ARCHITECTURE.md` | 스크립트 내부 구조와 수정 시 주의점 |
| `CHANGELOG.md` | 날짜순 변경 이력 |
| `PR_CHECKLIST.md` | 수정·배포 전 확인 목록 |
| `교육자료.html` | 이 도구를 어떤 지시와 명령으로 만들었는지 정리한 교육자료. 브라우저로 열어 보십시오 |
| `ldap_check.sh` | 검사 스크립트 본체 |
| `ldap_config.conf.sample` | 설정 예시. 각 키 설명이 주석으로 들어 있음 |
| `test_check.sh` | 장비 없이 돌리는 회귀 테스트 |

### 판별 방식 — 3중 교차 검증

| 축 | 근거 |
|---|---|
| `infra_dnsntp` | `resolv.conf` 의 nameserver + `chrony.conf`/`ntp.conf` 의 `server`·`pool` |
| `infra_ldap` | `ldap.conf` 의 URI 집합 + `BINDDN` + `BINDPW` |
| `infra_appl` | `auto.appl` 의 storage 이름 |

**세 축이 모두 같은 인프라를 가리킬 때만 정상**입니다. 하나라도 다르면 그 노드는
인프라가 섞인 것이므로 전 항목 FAIL 입니다.

사이트는 **`auto.appl` 의 storage 로 결정**합니다. URI 순서는 사이트끼리 겹칠 수 있어
(특히 `uri3 = NONE` 일 때 a1 과 a4 가 같아집니다) 판별 키로 쓸 수 없고,
사이트가 정해진 뒤 그 기대 순서와 맞는지 **검증에만** 씁니다.

### OS 분기

| 조건 | 인증 | 시각 동기화 |
|---|---|---|
| RHEL 7 이하 | `/etc/nslcd.conf` | `/etc/ntp.conf` |
| RHEL 8 이상 | `/etc/sssd/sssd.conf` | `/etc/chrony.conf` |
| hostname 이 `s4` 로 시작 (RHEL 8 이상이어도) | `/etc/nslcd.conf` | `/etc/ntp.conf` |

`s4` 예외 규칙은 `ldap_config.conf` 의 `s4.*` 로 켜고 끌 수 있습니다.

---

## 5. 전역 명령어로 사용하기 (선택 사항)

```bash
sudo ln -sf "$(pwd)/ldap_check.sh" /usr/local/bin/ldap_check
sudo mkdir -p /etc/ldap-automation
sudo cp ldap_config.conf /etc/ldap-automation/
sudo chmod 600 /etc/ldap-automation/ldap_config.conf
```

심볼릭 링크로 쓰면 설정 파일을 자동으로 못 찾으므로 `LDAP_CONFIG` 를 지정하십시오.

```bash
LDAP_CONFIG=/etc/ldap-automation/ldap_config.conf ldap_check
```

매번 지정하기 번거로우면 프로필에 넣어 두십시오.

```bash
echo 'export LDAP_CONFIG=/etc/ldap-automation/ldap_config.conf' >> /etc/profile.d/ldap_check.sh
```

해제는 링크를 지우면 됩니다.

```bash
sudo rm -f /usr/local/bin/ldap_check
```

---

## 6. 주의사항 (Disclaimer)

본 스크립트의 점검 결과는 100% 신뢰하기보다 **참고용(보조 도구)** 으로 사용하는 것을 권장합니다.

- 이 스크립트는 **읽기 전용**입니다. 설정을 바꾸지 않으므로 실행 자체는 안전합니다.
  다만 판정 결과를 근거로 대규모 변경 작업을 하기 전에는, **FAIL 로 나온 노드 중 몇 대를
  직접 접속해 실제로 그런 상태인지 눈으로 확인**하십시오.
- 판정은 **설정 파일의 내용만** 봅니다. 서비스가 실제로 그 설정으로 떠 있는지,
  LDAP 서버에 실제로 바인드가 되는지는 검사하지 않습니다.
  (`bindpw` 도 파일 내용 비교일 뿐 실제 바인드 테스트가 아닙니다.)
  최종 확인은 `id <계정>`, `ls /appl`, `systemctl status sssd` 로 하십시오.
- `ldap_config.conf` 에는 **bindpw 가 평문으로 들어갑니다.** 퍼미션 600 을 유지하고
  저장소에 커밋하지 마십시오. 공유 경로에 둘 때도 권한을 확인하십시오.
- 기준값 파일이 실제 환경과 어긋나 있으면 정상 노드도 FAIL 로 나옵니다.
  FAIL 이 대량으로 나오면 노드가 아니라 **`ldap_config.conf` 를 먼저 의심**하십시오.
