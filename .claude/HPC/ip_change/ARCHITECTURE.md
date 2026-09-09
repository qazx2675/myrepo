# ARCHITECTURE.md — ip_change

| 폴더/파일 | 역할 |
|---|---|
| `setup.sh` | 폐쇄망 오프라인 빌드. `GOPROXY=off`, `CGO_ENABLED=0` 정적 빌드 |
| `cmd/ip-change-engine/main.go` | CLI 진입점. 플래그 파싱 → 대상 로드 → apply 스크립트 생성 → gossh 호출 → 결과 출력(색상) |
| `internal/target/target.go` | `{user}.txt`(`hostname 변경될ip`) 파싱, 중복/형식 검증, 게이트웨이(마지막 옥텟 `.1`) 계산 |
| `internal/config/config.go` | `ip_change.conf`(평문 key=value) 파싱. 파일이 없어도 기본값으로 동작 |
| `internal/render/render.go` | 대상 전체를 담은 apply 스크립트 한 벌 조립. `apply_body.sh` 를 `go:embed` 하고 헤더에 `NETWORK_SCRIPTS_DIR`/`RHEL9_PATH`/호스트→IP 표를 삽입 |
| `internal/render/apply_body.sh` | **실제 IP/GATEWAY 를 바꾸는 본체.** 노드에서 자기 hostname 으로 호스트→IP 표에서 자기 행을 찾고, `hostname -I`(호스트 해석이 아니라 실제 인터페이스 주소)로 현재 IPv4 확인, ifcfg 탐색·백업·갱신·검증 |
| `internal/remote/remote.go` | gossh 호출. base64(spaceOut 포함)로 인코딩해 원격 주입하고 `<host>: <내용>` 출력을 파싱 |
| `internal/color/color.go` | 관리 노드 출력 전용 ANSI 색상. 대상 노드로 전송되는 스크립트에는 관여하지 않음 |
| `scripts/run_ip_change.sh` | 관리자용 래퍼. 계정 선택(`select_user_context`, 의도적으로 빈 자리) → 엔진 실행 |
| `conf/*.sample` | 설정·대상 목록 예시. 실제 값 파일은 `.gitignore` 로 커밋 차단 |

## 수정 요청별로 볼 곳

| 요청 | 볼 파일 |
|---|---|
| "IP/GATEWAY 갱신 로직 변경" | `internal/render/apply_body.sh` |
| "대상 판정 방식 변경(현재 IP 확인 등)" | `internal/render/apply_body.sh` 의 2번 블록 |
| "conf 스키마에 키 추가" | `internal/config/config.go`, `conf/ip_change.conf.sample`, 필요 시 `internal/render/render.go` (헤더 변수 추가) |
| "{user}.txt 형식 변경" | `internal/target/target.go` |
| "CLI 옵션 추가" | `cmd/ip-change-engine/main.go` |
| "gossh 호출 방식 변경" | `internal/remote/remote.go` |
| "출력 색상/화면 표시 형식 변경" | `internal/color/color.go`, `cmd/ip-change-engine/main.go` 의 `formatResultLine` |
| "결과 줄에 값 추가(기계용 포맷 변경)" | `internal/render/apply_body.sh` 의 `RESULT|...` 줄 + `cmd/ip-change-engine/main.go` 의 `formatResultLine` 둘 다 |
| "계정 선택 로직 채우기" | `scripts/run_ip_change.sh` 의 `select_user_context()` |

## 반드시 지킬 것

1. **ifcfg 파일을 통째로 덮어쓰지 마십시오.** `IPADDR`/`GATEWAY` 두 키만 바꿉니다. 운영자가
   직접 넣은 다른 지시자(BOOTPROTO, DEVICE, DNS1 등)가 함께 들어 있습니다.
2. **awk 에 데이터를 `-v` 로 넘기지 마십시오.** awk 가 `\t`, `\[` 같은 이스케이프를 해석해
   값이 깨질 수 있습니다. 반드시 환경변수 + `ENVIRON[]` 을 쓰십시오(`set_ifcfg_key` 참고).
3. **백업(`.bak.<STAMP>`)은 파일을 고치기 직전에, 한 실행당 한 번만 뜹니다.** 이 도구는
   호스트당 파일 하나만 건드리므로 ldap_setting 처럼 여러 번 고치는 상황은 없지만,
   함수를 확장할 때 이 전제를 깨지 않도록 주의하십시오.
4. **네트워크 서비스를 재시작하지 마십시오.** 계획서에서 명시적으로 제외한 항목입니다.
5. **게이트웨이는 항상 `.1` 로 고정합니다.** 넷마스크를 검증하지 않습니다(모든 대상이
   `/24` 라는 전제). 서브넷이 다른 대상이 생기면 `internal/target/target.go` 의
   `Gateway()` 와 계획서를 함께 갱신해야 합니다.
6. **`RHEL9Path` 는 ifcfg(KEY=VALUE) 형식 파일만 가정합니다.** NetworkManager
   keyfile(`.nmconnection`, `[ipv4]` 섹션 등) 형식은 다른 파서가 필요하며 미구현입니다.
   실제 RHEL 9+ 환경이 keyfile 방식으로 바뀌면 `apply_body.sh` 의 `set_ifcfg_key` /
   ifcfg 탐색 로직을 통째로 다시 짜야 합니다.
7. **`apply_body.sh` 의 결과 줄 형식(`RESULT|OK|host|기존IP|변경IP|GW`,
   `RESULT|FAIL|host|사유`)을 깨지 마십시오.** `cmd/ip-change-engine/main.go` 의
   `formatResultLine` 이 이 형식을 파싱해 화면의 `hostname 기존IP -> 변경IP` 표시를
   만듭니다. 필드 개수가 바뀌면 두 곳을 함께 고쳐야 합니다.
8. **`ROOT` 는 테스트 fixture 전용입니다.** `internal/render/apply_body.sh` 의
   `detect_os_major` 만 이 값을 씁니다. 운영 실행에서는 항상 비어 있어 실제 `/etc` 를 봅니다.
