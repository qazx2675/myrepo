# cisco — Cisco 장비 조사 장애(UNSUPPORTED · XML 실패) 원인 진단

Cisco 장비(예: **UCSX-210C-M7** 블레이드)가 `bios.sh` 에서 **vendor·모델·BIOS 버전은 나오는데 attr 이 비고**, 비고가 `UNSUPPORTED [XML 실패 → Redfish]` 로 나올 때(전체 설정 비교에서도 UNSUPPORTED) **어느 쪽이 왜 막히는지** 읽기 전용으로 좁혀 주는 도구입니다. 상위 폴더의 `trs`(BMC 접속 장애 진단) 도구와 같은 바이너리를 쓰고, Cisco 전용 단계(`biostool trs-cisco`)만 더한 것입니다. 소스와 전체 도구는 `bios-collect` 브랜치의 `.claude/HPC/redfish` 에 있습니다.

> 설정 변경·재부팅은 하지 않습니다. 보내는 요청은 Redfish GET/HEAD/OPTIONS 와 이번 실행이 만든 세션의 생성·삭제, XML API 확인(`GET /`, 쿠키 없는 `configResolveClass` 1회)과 — XML API 가 있을 때만 — `aaaLogin` 1회·조회(`configResolveClass`)·`aaaLogout` 뿐입니다. 비밀번호·토큰은 화면·파일에 남기지 않습니다.

## 1. 빌드 및 설치 방법

**빌드가 필요 없습니다.** 상위 폴더 `bin/` 의 빌드 완료 바이너리를 그대로 씁니다(폐쇄망 OK). 이 브랜치를 **폴더째** 내려받으면 됩니다(`cisco/` 만 따로 내려받으면 안 됩니다 — 상위의 `lib.sh`·`bin/`·`encrypt.sh` 를 씁니다).

| 파일 | 대상 |
|---|---|
| `../bin/biostool` | RHEL8 등 커널 3.x 이상 (Go 1.26.5, 정적 링크) |
| `../bin/biostool_os6` | RHEL6 (커널 2.x, Go 1.20.14, 정적 링크) |

`cisco/trs.sh` 가 `uname -r` 로 알맞은 바이너리를 자동 선택합니다. 실행 권한이 없으면:

```bash
chmod +x bin/biostool bin/biostool_os6 *.sh cisco/*.sh
```

## 2. 사용 방법

처음 한 번, **저장소 루트**(`lib.sh` 가 있는 폴더)에서 준비합니다. `bios.sh` 에서 쓰던 `bios.conf`·`pass.enc`·`key.bin` 을 그대로 복사해 써도 됩니다.

```bash
cp bios.conf.example bios.conf     # user= 에 BMC(UCSM) 계정 ID 입력
bash encrypt.sh                    # 비밀번호 입력(2회) -> pass.enc + key.bin 생성
cp user.txt.example user.txt       # 진단할 Cisco 장비 주소를 한 줄에 하나씩
```

실행합니다 (어느 폴더에서 불러도 됩니다).

```bash
bash cisco/trs.sh -targets 10.1.2.3          # 주소 직접 지정 (쉼표·공백 구분, ip:포트 가능)
bash cisco/trs.sh                            # user.txt 의 주소를 모두 진단
bash cisco/trs.sh -targets c101-m -profile VM   # hosts 의 이름도 가능, 프로파일 # [xml] 클래스까지 XML 로 확인
```

결과는 화면에 나오고 `results/<일시>_trs_cisco/<호스트>.txt` 에도 저장됩니다. **UNSUPPORTED 가 나온 장비의 결과 전체(특히 `[4]` `[7]` `[진단]`)를 그대로 전달하면** 원인 분석과 도구 수정에 쓸 수 있습니다.

> **어느 주소를 넣나요?** `bios.sh` 가 그 장비를 읽을 때 접속한 주소(`hosts` 의 `이름-m` 이 가리키는 주소)를 넣습니다. UCSX 블레이드의 BMC 개별 주소와 UCSM(Fabric Interconnect 관리 VIP) 주소를 **각각** 넣어 보면 어느 쪽이 XML API 를 제공하는지 바로 비교됩니다.

## 3. 옵션별 상세 설명

| 옵션 | 설명 |
|---|---|
| `-targets <대상>` | 진단할 장비 (IP/hostname, 쉼표·공백 구분, `ip:포트` 가능). 지정하면 `user.txt` 를 읽지 않음 |
| `-user <파일>` | 대상 목록 (기본 `user.txt`) |
| `-profile <이름>` | XML 로 확인할 `# [xml]` 구역 프로파일 이름 (기본 `VM`, 없어도 진행). 로그인에 성공하면 그 구역의 클래스(`biosVf…`)가 조회되는지 봅니다. `profiles/` 폴더가 있어야 하며 없으면 서버 목록까지만 봅니다 |
| `-path <경로>` | 추가로 GET 해 볼 Redfish 경로 (쉼표 구분) |
| `-conf <파일>` `-hosts <파일>` `-rf-conf <파일>` | 설정 파일 / 이름 해석용 hosts / 2순위 계정 파일 |

진단 단계:

0. **분류** — `bios.sh` 가 이 대상을 XML 로 먼저 보내는지(이름이 `xml_host_prefix` 로 시작) Redfish 로 보내는지
1. **네트워크 · 2. ServiceRoot · 3. 인증** — TCP/TLS, `/redfish/v1/`, Basic 1회 + 세션 1회(401 이면 잠금 방지로 즉시 중단)
4. **Cisco Redfish 구조** — System 의 제조사·모델·SKU·**Bios 링크 유무**·Oem 항목, Bios 후보 경로(`/Bios` `/BIOS` `/Bios/Settings` `/Oem/Cisco/Bios`), Chassis·Manager(CIMC 버전), Registries·펌웨어 목록의 BIOS 항목, System 아래·Oem 하위 리소스를 최대 60건·깊이 3 으로 따라가며 BIOS 속성을 가진 리소스 찾기
6. **bios.sh 방식 재현** — 실제 도구와 같은 시스템 판별·Bios 조회
7. **XML API** — `GET /` → 쿠키 없는 확인 요청 1회(XML API 가 있는지만) → **있을 때만** `aaaLogin` 1회 + 서버(`computeBlade`·`computeRackUnit`)·서비스 프로파일 조회 + 프로파일 클래스 조회
8. **진단** — 원인 후보를 우선순위로 출력

## 4. 결과 읽는 법

| 진단 문구 | 뜻 | 할 일 |
|---|---|---|
| 이 BMC 의 Redfish 에는 BIOS 설정 리소스가 없습니다 | System 에 `Bios` 링크가 없고 후보 경로·Oem 탐색에서도 BIOS 속성이 없음. `bios.sh` 의 `UNSUPPORTED — System 에 Bios 리소스가 없습니다` 의 원인 | 이 주소로는 Redfish 로 BIOS 값을 못 읽음 → XML(UCSM) 경로 확인 |
| 이 주소는 XML API(/nuova)를 제공하지 않습니다 | 이 IP 가 UCSM/단독 CIMC 가 아님. `bios.sh` 의 XML 시도가 로그인 전에 실패해 Redfish 로 넘어간 것(`XML 실패 → Redfish`) | UCSM VIP 를 아는 경우 `hosts` 의 해당 `이름-m` 을 UCSM VIP 로 (서비스 프로파일 이름 = 호스트 이름). Intersight 관리 모드(IMM)면 UCSM 이 없어 XML 로 읽을 수 없음 |
| XML API 가 정상 동작합니다 (서버 N대) | 이 주소로 XML 조회 가능 | `bios.sh` 가 다른 주소를 썼는지, 서비스 프로파일 이름이 호스트 이름과 같은지 확인 |
| XML 로그인이 거부되었습니다 (AUTH_FAIL) | 계정/비밀번호가 UCSM 과 다름 | `bios.conf` `user` 와 `pass.enc` 확인 (재시도는 하지 않음) |
| Redfish 로는 BIOS 를 읽을 수 있습니다 | Bios 가 정상 | `[6]` 재현 결과와 `bios.sh` 비고 전체를 전달 |
| 표준 Bios 링크는 없지만 다음 경로에 BIOS 속성이 있습니다 | 비표준 위치에 BIOS 속성 | 경로를 전달하면 도구가 그 경로를 읽도록 수정 |

진단은 **원인 후보**이며 확정이 아닙니다. 실제 UCSX-210C-M7 에서 확인한 결과가 아니라 mock 으로 시험한 로직이므로, 실장비 결과를 전달해 주시면 규칙을 보완합니다.

## 5. 문서별 설명

| 파일 | 설명 |
|---|---|
| `cisco/trs.sh` | 실행 래퍼 (바이너리 선택 → 파일 확인 → `biostool trs-cisco`) |
| `cisco/사용법.txt` | 명령어 모음 |
| `../trs.sh` | 일반 BMC 접속 장애(403 등) 진단 래퍼 |
| `../lib.sh` `../encrypt.sh` | 래퍼 공용 함수 / 비밀번호 암호화 |
| `../bios.conf.example` `../user.txt.example` | 설정·대상 목록 예시 |
| `../bin/biostool` `../bin/biostool_os6` | 빌드 완료 바이너리 |

## 주의사항 (Disclaimer)

본 도구는 100% 신뢰하기보다는 **참고용(보조 도구)** 으로 사용하십시오. 진단 결과는 원인 후보이며 확정이 아닙니다. 인증 시도는 호스트당 최소로 제한하지만(Redfish Basic 1회 + 세션 1회, XML 로그인 1회), 계정 잠금 정책이 엄격한 장비에서는 실행 횟수에 유의하십시오. 이 도구는 설정을 변경하지 않지만, 같은 계열의 설정 변경 도구를 쓴 뒤에는 **랜덤한 서버 몇 대를 직접 확인**하여 실제로 변경되었는지 확인하십시오.

## 6. 전역 명령어로 사용하기 (선택 사항)

```bash
alias trs-cisco='bash /경로/cisco/trs.sh'     # 어디서든 trs-cisco -targets <주소> 로 실행
```
