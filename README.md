# bios_export — 비교서버·대상서버 BIOS 설정값(JSON) 추출

비교서버(기준)와 대상서버들의 BIOS 속성 **전체**를 BMC 의 Redfish 로 읽어, 서버마다 JSON 파일 하나로 저장합니다.
`diff` / `vimdiff` 로 두 서버의 값을 눈으로 비교할 수 있습니다. **읽기 전용**이며 설정은 바꾸지 않습니다.

이 브랜치는 그 기능만 담은 최소 구성입니다(전체 점검·설정 도구는 `bios-collect` 브랜치의 `.claude/HPC/redfish`).

## 1. 설치 (빌드 불필요)

빌드 완료된 바이너리가 포함되어 있어 폐쇄망에서 폴더째 내려받으면 바로 쓸 수 있습니다. Go 는 필요 없습니다.

| 파일 | 용도 |
|---|---|
| `bin/biostool` | RHEL8 등 커널 3.x 이상 |
| `bin/biostool_os6` | RHEL6 (커널 2.6.x). 스크립트가 `uname -r` 로 자동 선택 |
| `bios_export.sh` | 실행 스크립트 |
| `encrypt.sh`, `lib.sh` | 비밀번호 암호화, 공용 함수 |
| `bios.conf.example`, `user.txt.example` | 설정·대상 목록 예시 |

처음 한 번 준비합니다.

```bash
cp bios.conf.example bios.conf
vi bios.conf              # user= 에 BMC 계정 ID
bash encrypt.sh           # BMC 비밀번호 입력 -> pass.enc + key.bin 생성
```

## 2. 사용 방법

```bash
bash bios_export.sh <비교서버> <대상1> <대상2>    # hostname / IP / IP:포트
bash bios_export.sh <비교서버>                    # 대상은 user.txt (한 줄에 하나)
```

결과는 `results/<일시>_all/` 에 만들어집니다.

| 파일 | 내용 |
|---|---|
| `attrs/<호스트>.json` | 호스트별 BIOS 속성 전체(이름 순 정렬). 호스트 이름의 `:` 등은 `_` 로 바뀜 |
| `report.txt` | 비교서버 대비 다른 값만 모은 리포트 (같으면 `모두 동일`) |
| `all_diff.tsv` | 차이 전체 (비교서버=a → 대상=b, 한쪽에만 있는 속성은 ONLY_REF / ONLY_HOST) |

눈으로 비교하기:

```bash
cd results/<일시>_all/attrs
diff -u <비교서버>.json <대상>.json | less
diff -y -W 200 --suppress-common-lines <비교서버>.json <대상>.json | less
vimdiff <비교서버>.json <대상>.json
```

JSON 첫 줄의 `host`·`ip` 는 서버마다 달라 항상 다르게 나옵니다. `attributes` 아래의 차이만 보시면 됩니다.

## 3. 옵션

| 옵션 | 설명 |
|---|---|
| `-conf <파일>` | 설정 파일 (기본 `bios.conf`) |
| `-user <파일>` | 대상 목록 파일 (대상 인자를 주면 무시) |
| `-hosts <파일>` | 이름 해석용 hosts (기본 `/etc/hosts`) |
| `-ignore <파일>` | 리포트에서 뺄 속성 목록 (기본 내장 목록: 서비스태그·MAC·시간 등) |
| `-diff-max <N>` | 리포트에 보일 차이 속성 수 |

`attrs/*.json` 에는 제외 속성도 **모두** 들어 있습니다. `-ignore` 는 `report.txt` 와 `all_diff.tsv` 에만 적용됩니다.

## 4. 동작과 안전

- 서버마다 로그인 1회, GET 4번(ServiceRoot, Systems, System, Bios), 끝나면 자기 세션 삭제.
- 허용목록 밖의 요청(로그 조회·삭제, 전원, 리셋, 설정 변경)은 코드가 전송 전에 막습니다. 재부팅하지 않습니다.
- JSON 에는 호스트·모델·BIOS 버전·속성 값만 있고 비밀번호·토큰은 없습니다. 권한은 0600 입니다.
- 비교서버와 모델이 다른 대상은 리포트에서 `NO_REFERENCE` 로 분류되지만, 그 대상의 JSON 은 그대로 저장됩니다.
- 로그인 실패(401)가 `auth_fail_stop`(기본 3) 이상 쌓이면 계정 잠금을 막으려고 나머지는 접속하지 않습니다.

## 5. 주의사항 (Disclaimer)

본 스크립트와 도구는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.
이 도구는 읽기만 하지만, 다른 도구로 설정을 변경한 뒤에는 **랜덤한 서버 몇 대를 직접 확인**해 실제로 변경되었는지 확인하십시오.
실제 BMC 에서는 벤더·모델·펌웨어마다 속성 이름과 값이 다를 수 있고, 이 도구는 mock BMC 로만 시험했습니다.

## 6. 전역 명령어로 사용하기 (선택 사항)

```bash
# ~/.bashrc 에 추가 (폴더 경로는 실제 위치로)
alias bios_export='bash /opt/bios_export/bios_export.sh'
```

스크립트는 자기 폴더로 이동해 `lib.sh`·`bin/`·`bios.conf` 를 찾으므로, 어디서 실행해도 되지만 파일들은 이 폴더 안에 두어야 합니다. 심볼릭 링크로는 폴더를 찾지 못하므로 `alias` 를 쓰십시오.

## 바이너리 정보

| 파일 | Go | sha256 |
|---|---|---|
| `bin/biostool` | 1.26.5 | `5437e1cd64c553bd160a34d22d476f129631fe6fc18d3267220bd61d9643fa44` |
| `bin/biostool_os6` | 1.20.14 | `e5af437102c9627ac52d9f7782f458d5894afc0d0933573fe0fac88f0e6599a1` |

소스는 전체 도구(`bios-collect` 브랜치)의 `cmd/biostool/allexport.go`, `allcheck.go` 입니다.
