# ARCHITECTURE

## 폴더 구조 및 파일 역할

| 파일/폴더 | 역할 | 비고 |
|---|---|---|
| `01.AWX_nodeinfo_V2.sh` | 메인 스크립트 — 노드정보 수집, 가공, 분할, yml 생성, git 업로드, LDAP/LACP 점검 | 실행 대상 |
| `02.source_dhcp_pxe.sh` | 보조 스크립트 — yml별 인벤토리/DHCP/PXE 자동 반복 등록 | 01 에서 자동 호출 또는 단독 실행 |
| `test/run_tests.sh` | 검증 하네스 — 계획서 §9 기준, 목업 스텁으로 E2E 검증 (108건, DNS 케이스 21~23 포함) | 개발/CI 용도 |
| `test/fake_dns.py` | 가짜 DNS 서버(UDP, 127.0.0.1) — 실제 `dig`/`nslookup` 출력을 01 이 해석하는지 확인하는 케이스 23 용 | **랩·CI 전용** (운영에 배치하지 않음, python3) |
| `setup/` | `setup_guide.sh`·`update_v0.6.0.sh`·`update_v0.6.1.sh`·`update_unreleased_dns.sh`·`update_unreleased_tmp_01.sh`·`update_unreleased_tmp_02.sh`·`vars.manifest` — 빈 변수 입력 가이드·현장 사본 업데이트 (git-upload-sk 틀로 생성, 직접 고치지 말고 재생성). `update_unreleased_dns.sh` 는 01 만 변경 | Unreleased |
| `README.md` | 사용자 문서 — 설치, 사용, 옵션, 주의사항 | 배포 필수 |
| `CHANGELOG.md` | 변경 이력 | 배포 필수 |
| `ARCHITECTURE.md` | 이 문서 — 폴더 구조, 함수 흐름 | 개발 참고 |
| `WORKFLOW.md` | 흐름도(텍스트 mermaid) | 개발/설명 용도 |
| `workflow.svg` | 흐름도(SVG 이미지) | 브라우저 열람 용도 |
| `PR_CHECKLIST.md` | 풀리퀘스트 검증 항목 | CI 참고 |
| `사용법.txt` | 명령어 중심 빠른 시작 | 운영 담당자 용도 |
| `.github/workflows/ci.yml` | 폴더 내 CI 워크플로우 | 이 폴더 변경 시 자동 실행 |
| `.github/workflows/awx-script.yml` | 저장소 루트 CI 워크플로우 | 경로 필터링 |
| `awxkit/` | AWX 래퍼 스크립트(별도 폴더) | git 루트의 `.claude/HPC/awxkit/` |
| `LOG/` | 사용자별 실행 로그 | `LOG/${user}.log` 누적 |
| `tmp/` | 임시 파일 — 최종 결과물 `tmp/all_${user}` | 실행 후 보존 |
| `dhcp_pool/` | DHCP 삭제 정보 누적 파일 | `dhcp_pool_delete_info.txt` |

## 01.AWX_nodeinfo_V2.sh 함수 및 단계 매핑

| 단계 | 함수 | 입력 | 출력/부작용 |
|---|---|---|---|
| [0] | (초기화) | | `trap cleanup EXIT`, 변수/로그 준비 |
| [1] | `user()` | (현장 코드) | `$user` 설정, 로그 파일 열기 |
| [2] | `download_txt()` | 프롬프트 Y/N | `${user}.txt` (12필드 또는 nodeinfo YAML) |
| [3] | `parse_msg()` | `${user}.txt` | msg 행 파싱, 12필드 검증 또는 종료 |
| [3-1] | `apply_infra_alias()` | `${user}.txt`, `infra_alias` | 3번째 필드(infra)의 미등록 이름을 치환하고 건수 출력 |
| [4] | `fix_mac()` | `${user}.txt` | MAC 짝수 보정 (spice/pice/dspr/pspr + ev 미포함) |
| [4-1] | `dns_check()` (+ `dns_server_spec`·`dns_read_conf`·`dns_cands`·`dns_ask`·`dns_lookup_a`·`dns_worker`·`dns_batch`·`dns_ptr_worker`) | `${user}.txt`, `/etc/resolv.conf`(`AWX_RESOLV_CONF`) | resolv.conf 의 DNS 서버(최대 3)에 직접 A 질의(dig → nslookup, `xargs -P 32`, 질의당 2초·재시도 1회)해 판정 5종·중복 경고(목록·DNS·PTR) 표 출력, 불일치면 a/n/s/q (auto 모드는 `/dev/tty` y/N, `AWX_DNS_CHECKED=1` 은 표만), 교체는 5번째 필드만 + LOG `[DNS]`. auto_setup `dnscheck.go` 와 같은 규칙 |
| [5] | `show_targets()` | `${user}.txt` | 호스트명 20개씩 세로 다단 출력 + 총 대수 |
| [6] | (메인 루프) | 프롬프트 Y/N | N → 종료, Y → 계속 |
| [7] | `inventory_delete()` | ssh 설정 | 원격 호스트에서 삭제 스크립트 실행 |
| [8] | `split_files()` | `${user}.txt` | 임시 디렉터리에 분할/전체 yaml 파일 생성 |
| [9] | `dhcp_info()` | `${user}.txt` | `dhcp_pool/dhcp_pool_delete_info.txt` 누적 |
| [10] | `gen_inventory()` | 임시 yaml | scp → custom_inventory.sh → `svr_dir` yml 수집 |
| [11] | `git_upload()` | `$yaml` 목록 | git 블록 원문 실행 + `cd "$now_pwd"` |
| [12] | `check_servers()` (+ `os6_enabled/os6_run/os6_failed`) | `${user}.txt` | LDAP(ldap.conf URI)·LACP·응답없음 점검, `tmp/all_${user}` 생성. os8_mgmt 접속 불가 호스트는 os6_mgmt 재조사로 교체(02 `check_partitions` 도 동일 함수 사용) |
| [13] | (메뉴 루프) | 프롬프트 | su/exit/ls/파일명 명령 처리 |
| [14] | (02 호출) | `$group_yml` + 옵션 + `--all=$all_yml` | `bash 02.source_dhcp_pxe.sh` 실행 후 break 또는 재시도 (02: OS 선택 → 그룹 yml 등록 → 그룹 2개 이상이면 전체 yml invsync → 최종 수량) |
| [15] | `verify_hosts()` | 붙여넣은 서버 목록 | 등록 대상(`$hostfile`)과 비교해 "모두 존재함" 또는 없는 서버 보고(exit 1) |

## 02.source_dhcp_pxe.sh 처리 흐름

| 단계 | 처리 | 입력 | 출력 |
|---|---|---|---|
| (인자) | `user=$1; shift` | `$1` = user, `$@` = yml 사양 | `$user` 설정 |
| (호환) | 인자 0개 → 대화형 | `ls *.yml`, 프롬프트 | invsync/dhcp/pxe 3회 호출 → exit 0 |
| (파싱) | `<yml>=<infra>,<os>,<boot>,<splunk>` | `"$@"` | 배열: `ymls`, `infras`, `oss`, `boots`, `splunks` |
| (표) | `print_table` | 배열 | 옵션 확인표 출력 (Y/N 프롬프트) |
| (수정) | N → 값 재입력 | 프롬프트 Enter/입력 | 배열 갱신 (Enter = 유지) |
| (실행) | yml별 invsync → (dhcp ∥ pxe 동시) | 배열 순회, 임시 디렉터리에 출력 보관 | dhcp·pxe 가 모두 끝나야 다음 yml, 실패 여부 누적 |
| (요약) | 성공/실패 카운트 | 배열 | "요약 : 전체 X / 성공 Y / 실패 Z" + exit 코드 |

## 흐름도

흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.

## 수정 요청 가이드

| 요청 | 확인 대상 |
|---|---|
| 입력 형식 변경 (12필드 필드 순서, msg 파싱) | `parse_msg()`, 계획서 §5-4 |
| MAC 보정 규칙 변경 | `fix_mac()`, 계획서 §5-5 |
| 분할 키 추가/제거 (infra/nic/disk/용량/os/boot/splunk 조합) | `split_files()`, 계획서 §8 |
| 출력 형식 변경 (다단 열/행, 총 대수) | `show_targets()`, 계획서 §5-6 |
| LDAP/LACP 점검 로직 | `check_servers()`, 계획서 §5-12 |
| 02 옵션 확인표 레이아웃 | `02` `print_table()`, 계획서 §5-15 |
| DNS 점검 규칙(판정·중복 경고·속도·auto 모드 질문) | `dns_check()` 와 보조 `dns_*` 함수, **auto_setup `dnscheck.go` 와 같이 고칠 것**, 테스트 케이스 21~23 |
| nodeinfo 질문 자동 답변 | `download_txt()` 의 `AWX_AUTO_NODEINFO` 분기 (`AWX_AUTO` 있을 때·없을 때) |
| 메뉴 명령어 추가 | [13] 메뉴 루프, 계획서 §5-13 |
| 재시도 정책 | [14] 02 호출, 계획서 §5-14 |
| 로깅 형식 | `log()` 함수, 계획서 §5-2 |
| 최상단 변수 추가 | 계획서 §5-1, README 최상단 변수 표 |
