# testcase — 직접 돌려 볼 수 있는 테스트케이스

## rf_fallback_case.sh

2순위(예비) 계정, 접속불가/계정 오류 구분, 설정 시 허용값 표기 선택을 **실제 BMC 없이** 확인합니다.

```bash
bash testcase/rf_fallback_case.sh                  # 실행: 시나리오별 PASS/FAIL 과 종합
bash testcase/rf_fallback_case.sh -v               # biostool 출력 전체도 보기
RF_FORCE_OS6=1 bash testcase/rf_fallback_case.sh   # 커널과 무관하게 *_os6 바이너리로 시험
```

- 필요한 것: bash(RHEL6 의 4.1 호환)와 기본 coreutils. **Go 불필요.** `bin/biostool`·`bin/mockbmc`(커널 메이저 < 3 이면 `_os6`)를 씁니다.
- 하는 일: 임시 폴더(`mktemp -d`)에 `bios.conf`·`user.txt`·프로파일·`rf.conf`(권한 600)·`pass.enc`/`key.bin` 을 만들고, `127.0.0.1` 의 포트 19101~19106 에 `testdata` 트리를 서빙하는 mock BMC 를 띄운 뒤 `biostool check -rf-conf <임시 rf.conf>` 를 실행합니다. 끝나면(오류·Ctrl-C 포함) mock 프로세스를 모두 종료하고 임시 폴더를 지웁니다.
- **`/etc/rf_tool/rf.conf` 는 건드리지 않습니다.** 시험용 계정·비밀번호는 스크립트 안의 mock 전용 값이며 출력하지 않습니다.
- 종료 코드: 모두 PASS(SKIP 허용) `0`, FAIL 이 있으면 `1`, 준비 실패 `2`.

### 시나리오와 기대 결과

| # | 포트·트리 | 상황 | 기대 결과(스크립트가 판정) |
|---|---|---|---|
| 1 | 19101 `dell-r660` | 1순위 계정 정상 | `ok.txt` 에 있음(일반 점검), `[2순위 계정]` 구역·`fallback_login.txt` 에 없음 |
| 2 | 19102 `hpe-dl360gen11` | 1순위 거부, 예비(`hpe_user`)만 유효 | `[2순위 계정(rf.conf)으로 접속]` 구역과 `fallback_login.txt` 에 있고 점검이 진행(`ok.txt`) |
| 3 | 19103 `lenovo-sr650v3` | 1순위·예비 모두 무효 | `[계정 오류 AUTH_FAIL]` 구역, `result.tsv` 에 `AUTH_FAIL`, `fallback_login.txt` 에 없음 |
| 4 | 19104 (아무도 듣지 않음) | 연결 거부 | `[접속불가]` 구역에만 있음(`[계정 오류]` 에 없음) |
| 5 | 19105 `supermicro-x12` | 1순위 정상, 기대 `Performance` ≠ 현재 `Custom`(`verified=N`) | `PowerTechnology` 항목이 `UNVERIFIED` |
| 6 | 19106 `lenovo-sr650v3` 사본 | Hyper-Threading 현재값 `Disable`, 프로파일 `Enabled\|Enable` `verified=Y`, `-dry-run` | 보낼 본문에 `"Processors_HyperThreading":"Enable"` (`Enabled` 아님) |
| 7 | 19107 `hpe-dl360gen11` (`-accounts`) | 1순위 계정이 없음, 빈 입력(기본 Y) | `[CREATED]`, 비밀번호가 출력·결과 파일에 없음, 이후 점검은 1순위 계정으로 접속 |
| 8 | 19107 (7 의 뒤) | 같은 id 가 이미 있음 | 다시 만들지 않음(`PRIMARY_OK`/`SKIP_EXISTS`) |
| 9 | 19108 `dell-r660` (`-accounts -account-slots 4 -account-slot-style`) | POST 불가, 빈 슬롯 방식 | 슬롯 3 에 PATCH 로 `[CREATED]` |
| 10 | 19109 `hpe-dl360gen11` (`-account-deny-new-login`) | 만든 뒤 새 계정 로그인 거부 | `[VERIFY_FAIL_ROLLED_BACK]` |
| 11 | 19107 | `account change -dry-run` | `[WOULD_CHANGE]`, 비밀번호 비노출 |

시나리오 7~11 은 mock 의 계정 서비스(-accounts)를 씁니다. 계정 서비스가 없는 오래된 mockbmc 에서는 SKIP 으로 표시됩니다. **새 비밀번호를 터미널에서 입력하는 ccount change 의 실제 입력은 이 스크립트가 시험하지 않습니다**(터미널이 필요) — README 2.12절 절차로 직접 해 보십시오.

### 시나리오 2 와 `-open-serviceroot`

mock 은 기본으로 **비인증 ServiceRoot 를 401** 로 막습니다(클라이언트가 토큰을 빼먹는 버그를 잡기 위해). 2순위 계정은 비인증 ServiceRoot 로 벤더를 식별하므로, 시나리오 2 는 mock 이 이를 열어 줄 때만 시험할 수 있습니다. 그래서 스크립트가 mock 에 `-open-serviceroot` 를 붙입니다.

- 스크립트는 `mockbmc -h` 에 `-open-serviceroot` 가 있으면 mock 에 그 플래그를 붙여 시나리오 2 를 실행합니다. 플래그가 없는 오래된 `mockbmc` 에서는 시나리오 2 를 `SKIP` 으로 표시하고 나머지는 그대로 판정합니다.
- 커밋된 `bin/mockbmc`·`bin/mockbmc_os6` 에는 이 플래그가 들어 있습니다.

### 출력 예

```text
 시나리오 1  PASS  1순위 정상 — 19101 점검 OK, 2순위 목록에 없음
 시나리오 2  PASS  예비 계정으로 접속 — 19102 가 [2순위 계정] 구역과 fallback_login.txt 에 있고 점검이 진행됨
 시나리오 3  PASS  둘 다 실패 — 19103 이 [계정 오류 AUTH_FAIL] 에 표시됨
 시나리오 4  PASS  접속불가 구분 — 19104 가 [접속불가] 에만 표시됨
 시나리오 5  PASS  값 불일치 표시 — 19105 PowerTechnology(기대 Performance, 현재 Custom)가 UNVERIFIED
 시나리오 6  PASS  표기 선택 — dry-run 본문에 "Processors_HyperThreading":"Enable" (Enabled 아님)

종합: PASS (SKIP 0건)
```

합성 데이터(`testdata/`)와 mock 으로 만든 시험이므로 실제 BMC 의 동작(벤더별 ServiceRoot 응답, 잠금 정책 등)은 검증하지 않습니다. 실장비에서 확인할 항목은 [FIRST_RUN.md](../FIRST_RUN.md) 26~30 입니다.
