# ARCHITECTURE.md — ldap_setting

| 폴더/파일 | 역할 |
|---|---|
| `setup.sh` | 폐쇄망 오프라인 빌드. `GOPROXY=off`, `CGO_ENABLED=0` 정적 빌드 |
| `test_all.sh` | 왕복 회귀 테스트. 적용 후 `../ldap_check/ldap_check.sh` 로 검사 |
| `cmd/ldap-config-engine/main.go` | CLI 진입점. 플래그 파싱 → 자산 로드 → 사이트별 스크립트 생성 → gossh 호출 → 결과 집계 |
| `internal/config/` | `ldap_config.conf`(평문 key=value) 파싱과 검증. storage 고유성·uri_order 참조 무결성을 여기서 잡음 |
| `internal/asset/` | 자산현황(`hostname<TAB>site`) 파싱. 중복 호스트 검출, 사이트별 그룹핑 |
| `internal/render/render.go` | 사이트별 값을 bash 변수 헤더로 만들고 `apply_body.sh` 를 이어붙임 |
| `internal/render/apply_body.sh` | **실제 설정을 바꾸는 본체.** 노드에서 OS/s4 판정, 키 단위 갱신, 백업, 변경된 파일에 대응하는 서비스만 재시작 |
| `internal/remote/` | gossh 호출. base64 로 인코딩해 원격 주입하고 `<host>: <내용>` 출력을 파싱 |
| `scripts/deploy_ldap.sh` | 관리자용 래퍼. 계정 선택 → 엔진 실행 → 전 노드 검증 |
| `conf/*.sample` | 설정·자산 예시. 실제 값 파일은 `.gitignore` 로 커밋 차단 |
| `교육자료.html` | 제작 과정 기록 |

## 수정 요청별로 볼 곳

| 요청 | 볼 파일 |
|---|---|
| "설정 대상 파일 추가/변경" | `internal/render/apply_body.sh` + `../ldap_check/ldap_check.sh` **두 곳 모두** |
| "conf 스키마에 키 추가" | `internal/config/config.go`, `conf/ldap_config.conf.sample`, `internal/render/render.go`, 그리고 `../ldap_check/ldap_config.conf.sample` |
| "CLI 옵션 추가" | `cmd/ldap-config-engine/main.go` |
| "gossh 호출 방식 변경" | `internal/remote/remote.go` |
| "자산현황 형식 변경" | `internal/asset/asset.go` |

## 반드시 지킬 것

1. **`apply_body.sh` 와 `../ldap_check/ldap_check.sh` 는 짝입니다.** 한쪽에서 쓰는 값을 바꾸면
   다른 쪽 검사도 같이 고쳐야 합니다. 예를 들어 적용이 `pool` 줄을 지우도록 바뀌면 검사도
   `pool` 을 NTP 소스로 세야 합니다. 실제로 이 불일치 버그가 있었고, 짝 구조 덕분에 발견했습니다.
2. **awk 에 데이터를 `-v` 로 넘기지 마십시오.** awk 가 `\t`, `\[` 같은 이스케이프를 해석해
   값이 깨집니다. 반드시 환경변수 + `ENVIRON[]` 을 쓰십시오.
   이것 때문에 sssd.conf 를 빈 파일로 만든 버그가 있었습니다.
3. **`commit_file` 의 빈 파일 가드를 지우지 마십시오.** 편집 로직이 깨졌을 때 설정 파일을
   날려먹는 것을 막는 마지막 방어선입니다.
4. **`ROOT` 가 설정되면 서비스를 재시작하지 않습니다.** fixture 테스트 중 실제 sssd 를
   재시작하는 사고를 막습니다. 실제로 겪었습니다.
5. **`-infra` 에 기본값을 두지 마십시오.** 다른 인프라 값을 통째로 밀어넣는 사고가 가능해집니다.
6. **파일을 통째로 덮어쓰지 마십시오.** `autofs.conf` 와 `sssd.conf` 에는 우리가 모르는
   운영 설정이 함께 들어 있습니다.
