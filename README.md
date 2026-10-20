# trs — BMC 접속 장애(403 등 4xx) 트러블슈팅 도구

Redfish BMC(예: Lenovo SR630 V2)에서 403 같은 4xx 오류가 날 때, **어느 단계에서 왜 막히는지** 읽기 전용으로 좁혀 주는 도구입니다. `redfish`(biostool) 프로젝트의 `trs` 기능만 떼어 낸 독립 배포본이며, 소스와 전체 도구는 `bios-collect` 브랜치의 `.claude/HPC/redfish` 에 있습니다.

> 설정 변경·재부팅은 하지 않습니다. 보내는 요청은 GET/HEAD/OPTIONS 와 이번 실행이 만든 세션의 생성·삭제뿐이고, 비밀번호·토큰은 화면·파일에 남기지 않습니다.

## 1. 빌드 및 설치 방법

**빌드가 필요 없습니다.** 빌드 완료 바이너리가 `bin/` 에 들어 있어 Go 없이 그대로 씁니다(폐쇄망 OK). 이 폴더를 통째로 내려받으면 됩니다.

| 파일 | 대상 |
|---|---|
| `bin/biostool` | RHEL8 등 커널 3.x 이상 (Go 1.26.5 로 빌드, 정적 링크) |
| `bin/biostool_os6` | RHEL6 (커널 2.x, Go 1.20.14 로 빌드, 정적 링크) |

`trs.sh` 가 `uname -r` 로 알맞은 바이너리를 자동 선택합니다. 실행 권한이 없으면:

```bash
chmod +x bin/biostool bin/biostool_os6 *.sh
```

## 2. 사용 방법

처음 한 번 준비합니다.

```bash
cp bios.conf.example bios.conf     # user= 에 BMC 계정 ID 입력
bash encrypt.sh                    # BMC 비밀번호 입력(2회) -> pass.enc + key.bin 생성
cp user.txt.example user.txt       # 진단할 BMC 관리망 IP 를 한 줄에 하나씩
```

실행합니다.

```bash
bash trs.sh                                    # user.txt 의 IP 를 진단
bash trs.sh -targets 10.1.2.3                  # IP 직접 지정 (쉼표·공백 구분, ip:포트 가능)
bash trs.sh -targets 10.1.2.3 -path /redfish/v1/Systems/1/Bios/Settings   # 추가로 GET 해 볼 경로
```

결과는 화면에 나오고 `results/<일시>_trs/<호스트>.txt` 에도 저장됩니다. **장애가 난 서버의 결과를 그대로 전달하면** 원인 분석과 도구 수정에 쓸 수 있습니다.

## 3. 옵션별 상세 설명

| 옵션 | 설명 |
|---|---|
| `-targets <대상>` | 진단할 BMC (IP/hostname, 쉼표·공백 구분, `ip:포트` 가능). 지정하면 `user.txt` 를 읽지 않음 |
| `-user <파일>` | 대상 목록 (기본 `user.txt`) |
| `-path <경로>` | 추가로 GET 해 볼 Redfish 경로 (쉼표 구분) |
| `-conf <파일>` `-hosts <파일>` `-rf-conf <파일>` | 설정 파일 / 이름 해석용 hosts / 2순위 계정 파일 |

진단 단계:

1. **네트워크** — TCP 443, TLS 버전·암호·인증서 만료
2. **비인증 ServiceRoot** — 403 이면 User-Agent·Accept·OData-Version·끝 슬래시를 바꿔 비교 (IP 접근 제한인지, 헤더 필터인지 구분)
3. **인증** — Basic 1회 + 세션 생성 1회 (401 이면 계정 잠금 방지를 위해 즉시 중단, 필요 시 `rf.conf` 2순위 계정 1회 비교)
4. **인증 후 읽기** — Systems·Bios·Managers·NetworkProtocol·내 계정 상태(Role/Locked/PasswordChangeRequired)
5. **4xx 변형 비교** — 4xx 가 난 경로와 세션 생성 실패를 헤더·Content-Type·인증 방식을 바꿔 재요청 (405 는 OPTIONS Allow)
6. **bios.sh 방식 재현** — 실제 도구와 같은 조회로 실패 원인(User-Agent·연결 재사용·리다이렉트·8MB 초과)을 가림
7. **진단** — 원인 후보를 우선순위로 출력, 4xx 코드별(400~451) 설명 표 포함

## 문서별 설명

| 파일 | 설명 |
|---|---|
| `trs.sh` | 실행 래퍼 (바이너리 선택 → 파일 확인 → `biostool trs`) |
| `lib.sh` | 래퍼 공용 함수 (바이너리 선택, 설정 읽기) |
| `encrypt.sh` | BMC 비밀번호 암호화 (`pass.enc` + `key.bin`) |
| `bios.conf.example` / `user.txt.example` | 설정·대상 목록 예시 |
| `사용법.txt` | 명령어 모음 |

## 주의사항 (Disclaimer)

본 도구는 100% 신뢰하기보다는 **참고용(보조 도구)** 으로 사용하십시오. 진단 결과는 원인 후보이며 확정이 아닙니다. 인증 시도는 호스트당 최소로 제한하지만, 계정 잠금 정책이 엄격한 장비에서는 실행 횟수에 유의하십시오. 이 도구는 설정을 변경하지 않지만, 같은 계열의 설정 변경 도구를 쓴 뒤에는 **랜덤한 서버 몇 대를 직접 확인**하여 실제로 변경되었는지 확인하십시오.

## 5. 전역 명령어로 사용하기 (선택 사항)

```bash
export PATH="$PWD:$PATH"        # 이 폴더를 PATH 에 추가하면 어디서든 `trs.sh` 로 실행 (bash trs.sh 도 가능)
```
