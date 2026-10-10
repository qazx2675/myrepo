# ACCOUNT_SPEC.md — 계정 생성/비밀번호 변경 기능 확정 사양 (인터뷰 2026-10-11)

구현 전 합의 사양입니다. 구현 상태는 CHANGELOG.md 를 확인하십시오. 벤더별 API 근거는 `id_json/<벤더>/SUMMARY.md` 입니다.

## 1. 목적과 형태
- 1순위 공용계정(bios.conf id + pass.enc)이 BMC 에 없을 때 만들고(`create`), 비밀번호를 바꾼다(`change`).
- Go 서브커맨드 `biostool account` + bash 래퍼(`account_create.sh`, `account_change.sh`). Go 표준 라이브러리만 사용.
- `bios_check.sh` 는 계정이 없으면(AUTH_FAIL + 2순위 로그인 가능) "만들려면 account_create.sh" 안내만 한다. 쓰기 경로는 분리한다.

## 2. 확정 결정
| 항목 | 결정 |
|---|---|
| 생성할 id/pw | bios.conf 1순위 id + pass.enc 비밀번호 |
| 생성 호출 권한 | rf.conf 벤더 admin (2순위 계정) |
| y/n 단위 | 점검 후 대상 목록(호스트/벤더/모델/만들 id)을 보여주고 한 번에 y/n |
| 벤더 범위 | 모든 벤더: Dell(iDRAC8/9 빈 슬롯 PATCH, iDRAC10 POST), HPE, Lenovo, Supermicro, Cisco CIMC(Redfish), Cisco UCSM(XML), Cray |
| 권한(Role) | Administrator 고정. 같은 id 가 이미 있으면 생성 생략(덮어쓰기 금지) |
| 기본 동작 | 기본 적용(y 입력 시 실행), `-dry-run` 으로 미리보기 |
| 미확인 방식 | verified=N 이어도 y 로 적용 허용, 경고만 표시 (설정 체크의 verified 거부 규칙과 다름) |
| 검증/실패 | 적용 후 새 계정으로 1회 로그인 검증. 실패한 서버만 롤백 + 보고 |
| UCSM | 허용목록에 쓰기(aaaLogin → configConfMo `sys/user-ext/user-<id>` → aaaLogout) 추가해 실제 적용 |
| 비밀번호 정책 | 사전 길이/복잡도 검사 안 함. BMC 응답으로 판단. `@`, `!` 등 특수문자가 JSON/XML/URL 에서 깨지지 않아야 함 |
| change 입력 | 비에코 `*` 표시, 2회 입력, 붙여넣기 감지 시 거부 + "복사·붙여넣기 불가, 직접 입력" 메시지 |
| change 후 pass.enc | 자동 갱신하지 않음. 즉시 실행 가능한 csh 기준 명령을 출력 |
| 결과 | `results/<ts>/account_result.tsv`(0600) + 화면 요약. 비밀번호·해시는 출력/저장 안 함 |
| 동시 실행 | 아래 4절 |

## 3. 구현 시 반드시 지킬 것 (확정 사항에서 파생)
1. **롤백 정의(가정, 구현 전 확인 불필요하나 문서화):** 생성 롤백 = 이번 실행에서 만든 계정만 삭제. 변경 롤백 = 직전 비밀번호(pass.enc) 로 되돌림. 이번 실행이 만들지 않은 계정은 절대 삭제하지 않음. 롤백 자체가 실패하면 "수동 조치 필요" 로 별도 목록에 남김.
2. **쓰기 허용목록 분리:** redfish.go `checkAllowed` 에 계정 쓰기 경로(`AccountService/Accounts*`, `Managers/*/Accounts/*`)만 추가하고, `ModeAccount` 에서만 열린다. BIOS 설정/점검 모드에서는 계정 경로가 계속 막혀야 한다. UCSM 도 읽기 전용 허용목록과 별도로 쓰기 허용 집합을 둔다. TestNoRawHTTPOutsideRedfish 유지.
3. **Dell iDRAC8/9:** 슬롯 2~16 중 UserName 이 빈 슬롯을 조회해 PATCH(UserName, Password, RoleId/Privilege 511, Enabled). 슬롯 1(anonymous) 사용 금지. iDRAC10 은 POST. iDRAC9 POST 지원은 미확인이므로 405 면 PATCH 로 전환.
4. **HPE:** 슬롯 12개, Id 가 `1..12` 또는 `65536` 대이므로 컬렉션 GET 후 UserName 으로 중복 확인. 용어 반전 주의(`UserName`=로그인 ID).
5. **로그인 시도 횟수:** 생성 검증 1회, 롤백 판정 후 재시도 금지. 잠금 정책 때문에 점검 때와 같은 breaker(warm-up) 적용.
6. **비밀번호 취급:** 인자(argv)/환경변수/로그/결과/에러 메시지에 절대 노출 금지(`ps` 에도 보이지 않게 stdin 또는 tty 입력). change 의 새 비밀번호는 메모리에서만, 사용 후 0 으로 채움.
7. **입력 처리(붙여넣기 거부):** tty raw 입력 + 문자 도착 간격/한 번에 읽은 바이트 수로 붙여넣기 판정 + bracketed paste(`ESC[200~`) 거부. 감지 못 하는 환경은 안내 문구만 출력하고 문서에 한계로 명시. 비 tty(파이프)면 change 는 거부.
8. **csh 명령 출력:** change 성공 후 `cd <도구폴더>; bash encrypt.sh` 형태로 출력(비밀번호는 encrypt.sh 가 직접 입력받으므로 명령에 포함되지 않음). `!` 는 csh 히스토리 확장 대상이므로 출력 명령에 비밀번호/`!` 를 넣지 않는다.
9. **일부 실패 시 pass.enc:** 갱신하지 않으므로 전체 성공/부분 실패와 무관하게 안내만 하되, 부분 실패면 "실패 N대는 옛 비밀번호 유지" 를 함께 표시.

## 4. 동시 실행 수 권고
- 근거: 병목은 Go 가 아니라 BMC 의 쓰기 지연(계정 PATCH/POST 는 읽기보다 느림)과 BMC 세션 한도, 계정 잠금이다. gossh 2천 병렬이 가벼웠던 것은 읽기 위주였기 때문이며 쓰기에는 그대로 옮기지 않는다.
- 권고: 기본 `-j 100`, 상한 300. 시작 시 처음 10대는 순차 처리(warm-up)해 이상 징후가 있으면 breaker 로 중단. 만 대 기준 호스트당 6초 가정 시 약 10분(실측 아님).
- UCSM 은 단일 엔드포인트라 순차.
- 모두 실장비 미측정이며 첫 실행 로그로 조정한다.

## 5. 문서 작성 규칙 (설정 체크 스크립트와 동일)
README 순서(빌드/설치 → 사용 → 옵션 → 문서), Disclaimer("설정 변경 후 랜덤 서버 몇 대 확인" 포함), 실제 프롬프트/출력 예시, CI, ARCHITECTURE, WORKFLOW + svg, PR_CHECKLIST, CHANGELOG, setup.sh, 사용법.txt(명령만, 위에 `#` 주석), testcase(mock BMC), os6/os8 바이너리 커밋, 랩(.58/.60)에서만 빌드·테스트.

## 6. 구현 상태와 사양 대비 변경점 (2026-10-11)

구현됨: `cmd/biostool/account*.go`, `redfish.go` `ModeAccount`, `internal/ucsmxml` 계정 모드, `account_create.sh`, mock 계정 서비스, 테스트케이스 시나리오 7~11. 사용법·실제 출력은 README 2.12절.

인터뷰 이후 사용자 지시로 확정된 것:
- **점검 끝(`bios_check.sh`)에서도 기본 Y 로 생성을 제안**합니다(처음 사양의 “점검 스크립트는 안내만”을 대체). 터미널이 아니면 `-stdin-ok` 없이는 묻지 않고, `-no-prompt` 면 안내만 합니다.
- **`change` 는 바이너리를 직접 사용**(래퍼 없음).

구현 중 판단한 것(사용자 확인 필요 없다고 본 것, 바꾸려면 알려 주십시오):
- **change 는 1순위 계정으로 로그인된 호스트만** 바꿉니다. admin(2순위) 세션으로 남의 비밀번호를 바꾸면 이전 비밀번호를 알 수 없어 롤백이 불가능하기 때문입니다(보안 리뷰 반영). 이름은 정확히 일치해야 합니다.
- change 의 최종 확인은 `[y/N]`(기본 N) — 변경은 기존 도구 전체가 쓰는 비밀번호를 바꾸는 작업이라 명시적 `y` 를 요구합니다.
- 빈 슬롯은 `UserName==""`·`Enabled=false`·숫자 Id ≥ 2(Dell ≥ 3) 만 씁니다.
- 생성 롤백: POST 로 만들었으면 DELETE(안 되면 비활성→이름 비우기), 슬롯 PATCH 로 만들었으면 비활성→이름 비우기. 통신 오류로 검증하지 못하면 롤백하지 않습니다(`VERIFY_UNKNOWN`).
- 벤더 `cray` 를 rf.conf 키·벤더 식별에 추가했습니다(`cray_user`/`cray_pass`).
- 붙여넣기 감지: 한 번에 여러 글자·글자 간격 8ms 미만·bracketed paste. 감지하지 못하는 환경은 안내 문구만(완전 차단 불가).