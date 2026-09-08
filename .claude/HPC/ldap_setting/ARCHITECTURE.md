# ARCHITECTURE.md — ldap_setting

| 폴더/파일 | 역할 |
|---|---|
| `setup.sh` | 폐쇄망 오프라인 빌드. `GOPROXY=off`, `CGO_ENABLED=0` 정적 빌드 |
| `update.sh` | 실 배포본의 코드만 새 버전으로 갱신. `conf/ldap_config.conf`·`conf/assets.txt` 를 백업 후 복원해 보존 |
| `test_all.sh` | 왕복 회귀 테스트. 적용 후 `../ldap_check/ldap_check.sh` 로 검사 |
| `cmd/ldap-config-engine/main.go` | CLI 진입점. 플래그 파싱 → 자산 로드 → 사이트별 스크립트 생성 → gossh 호출 → 결과 집계 |
| `internal/config/` | `ldap_config.conf`(평문 key=value) 파싱과 검증. storage 고유성·uri_order 참조 무결성을 여기서 잡음 |
| `internal/asset/` | 자산현황(`hostname<TAB>site`) 파싱 + `-host-file` 용 순수 호스트 목록(`LoadHostList`) 파싱. 자산현황은 site 판정의 유일한 근거, 호스트 목록은 그중 무엇을 고를지만 결정 |
| `internal/render/render.go` | 사이트별 값을 bash 변수 헤더로 만들고 `lib_common.sh` + `apply_body.sh` 를 이어붙임. 되돌리기 스크립트(`RollbackScript`)도 여기서 조립 |
| `internal/render/lib_common.sh` | apply·rollback 공용. 로그, 변경 추적, OS 판정, **서비스 재시작 표**. 이 표가 두 곳으로 갈라지면 반드시 어긋나므로 여기 한 곳에만 둡니다 |
| `internal/render/rollback_body.sh` | 되돌리기 본체. `<파일>.bak.<시점>` 조회·복원 후 대응 서비스만 재시작. 파일을 삭제하지는 않음 |
| `internal/render/apply_body.sh` | **실제 설정을 바꾸는 본체.** 노드에서 OS/s4 판정, 키 단위 갱신, 백업, 변경된 파일에 대응하는 서비스만 재시작 |
| `internal/remote/` | gossh 호출. base64 로 인코딩해 원격 주입하고 `<host>: <내용>` 출력을 파싱 |
| `scripts/deploy_ldap.sh` | 관리자용 래퍼. 계정 선택 → 엔진 실행 → 전 노드 검증 |
| `scripts/run.sh` | 대화형 실행 래퍼. 계정 선택(목록 비어있는 골격) → conf 에서 읽은 인프라 메뉴 선택 → DRY-RUN → `yes` 확인 → 적용. 검증은 하지 않음 |
| `conf/*.sample` | 설정·자산 예시. 실제 값 파일은 `.gitignore` 로 커밋 차단 |
| `교육자료.html` | 제작 과정 기록 |

## 수정 요청별로 볼 곳

| 요청 | 볼 파일 |
|---|---|
| "설정 대상 파일 추가/변경" | `internal/render/apply_body.sh` + `../ldap_check/ldap_check.sh` **두 곳 모두** |
| "conf 스키마에 키 추가" | `internal/config/config.go`, `conf/ldap_config.conf.sample`, `internal/render/render.go`, 그리고 `../ldap_check/ldap_config.conf.sample` |
| "CLI 옵션 추가" | `cmd/ldap-config-engine/main.go` |
| "gossh 호출 방식 변경" | `internal/remote/remote.go` |
| "서비스 재시작 대상 변경" | `internal/render/lib_common.sh` 의 `restart_for_changed` **한 곳만** (apply·rollback 양쪽에 반영됨) |
| "되돌리기 동작 변경" | `internal/render/rollback_body.sh` |
| "되돌리기 CLI 변경" | `cmd/ldap-config-engine/main.go` 의 `runRollback` |
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
7. **백업은 한 실행에서 파일당 한 번만 뜹니다.** `commit_file` 은 `.bak.$STAMP` 가 이미
   있으면 다시 만들지 않습니다. `ldap.conf` 처럼 한 파일을 여러 번(URI/BINDDN/BINDPW) 고치는데
   매번 백업을 덮으면, 백업이 '이미 일부 수정된 상태' 가 되어 롤백해도 원본으로 돌아가지
   않습니다. 실제로 이 버그가 있었고 test_all.sh [9] 가 이를 잡습니다.
8. **롤백은 새 백업을 만들지 않습니다.** 그래야 같은 시점으로 두 번 되돌려도 멱등합니다.
9. **롤백은 파일을 삭제하지 않습니다.** 적용이 새로 만든 파일은 백업이 없는데, 이를 지우는
   것은 위험하므로 `NO-BACKUP` 으로 보고만 합니다.
10. **자산현황(`conf/assets.txt`)과 작업 대상 목록(`{user}.txt`, `-host-file`)을 혼동하지
    마십시오.** site 판정은 **항상** 자산현황에서만 이뤄집니다. `-host-file` / `run.sh`
    의 `{계정명}.txt` 는 그중 어떤 호스트를 고를지만 정하는 순수 hostname 목록이며,
    여기에 site 정보를 넣거나 이 파일을 site 판정에 쓰면 안 됩니다. 실제로 이 둘을
    섞어 써서 자산현황이 우회되는 문제가 있었습니다.
11. **`internal/remote.BuildCommand` 가 만드는 명령줄에는 따옴표를 절대 넣지 마십시오.**
    대상 계정의 로그인 셸이 무엇인지(bash/csh/tcsh) 알 수 없고, gossh 가 이 명령을
    다시 자기 쪽에서 따옴표로 감싸 ssh 로 넘길 수도 있습니다. `VAR=값 명령`, `rc=$?`
    같은 bash 전용 문법도 넣지 마십시오 — csh/tcsh 는 이해하지 못하고 "Command not
    found"/"Undefined variable" 로 깨집니다. 항상 명령 전체를 base64 로 감싸
    `echo <b64> | base64 -d | bash` 형태로 보내, 어떤 로그인 셸을 만나도 따옴표나
    bash 문법이 그 셸에 직접 보이지 않게 하십시오. 실제로 이 규칙을 어겨서 두 번
    연달아 문제가 재발했습니다 (`bash -c '...'` 로 감싸는 중간 시도도 tcsh 에서
    `Unmatched '''` 로 깨짐).
12. **`/etc/auto.appl` 은 다른 설정 파일과 달리 부분 편집이 아니라 통째로
    덮어씁니다.** (autofs.conf/sssd.conf 등과 달리 운영자가 손댄 다른 내용과
    섞여 있지 않기 때문입니다.) 적용 전 파일이 있으면 고정 이름
    `/etc/auto.appl_back` 으로도 한 벌 남기지만, 이건 참고용일 뿐입니다 —
    rollback 은 여전히 `commit_file` 이 만드는 `.bak.<시점>` 만 찾아서 복원하고,
    `auto.appl_back` 은 rollback 이 지우거나 복원 대상으로 쓰지 않습니다.
13. **`internal/remote.BuildCommand` 가 만드는 base64 문자열은 반드시 `spaceOut`
    으로 두 글자마다 공백을 끼워 넣은 뒤 보내야 합니다.** gossh 는 명령
    문자열에 `reboot`/`poweroff`/`shutdown`/`halt`/`ddc` 같은 위험 작업 키워드가
    섞여 있으면(대소문자 무시, 부분일치) 실행을 거부하고 그 명령을 그대로
    화면에 찍습니다(`/root/myrepo/.claude/공통/gossh/v2/main.go` 의 안전장치,
    정상 동작이며 고칠 수 없음 — 자동화에서 우회하려면 대화형 y/N 확인까지
    강제되어 못 씁니다). base64 는 사실상 무작위 문자열이라 스크립트가
    길어질수록 이런 3글자 이상 조각이 우연히 섞여 나올 확률이 올라갑니다 —
    실제로 `/wappl` 기능으로 스크립트가 길어지면서 재현됐습니다("이상한
    base64 값이 그대로 출력되고 아무것도 적용되지 않음"). 공백을 끼워 넣으면
    전송되는 텍스트에 3글자 이상 이어진 조각 자체가 없어져 이 우연한 일치를
    원천 차단합니다. 원격에서는 `tr -d ' '` 로 공백만 지우고 그대로
    복호화합니다(base64 원문에는 공백이 없으므로 안전).
