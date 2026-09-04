# CHANGELOG

## 2026-09-04 — v2: 호스트명 압축 표기 + -b 접속불가 그룹 + autofs 제한값 350

- **신규 기능(`v2/main.go`)**: `splitTrailingDigits()`/`compressHosts()` 추가 — 접두어+자릿수가
  같고 번호가 연속인 호스트를 `prefix[start-end]` 형태로 압축(`expandHostLine`의 역함수). 결과
  파일(`writeHostsToFile`)과 `-b` 그룹 출력(`printBunched`)에 적용. 압축 결과는 그대로 `-w`로
  재사용 가능.
- **신규 기능(`v2/main.go`)**: `printUnreachableGroup()` 추가 — `-b` 사용 시 타임아웃/Refused로
  접속 자체가 안 된 호스트를 내용 비교 없이 별도 그룹으로 표시.
- **변경(`v2/main.go`)**: `autofsSafeConcurrency` 500 → 350.
- **확인만 하고 변경 없음**: "명령어 실행 타임아웃"은 실제로 존재하지 않음(-t는 접속 단계에만
  적용)을 확인하고 요청자에게 전달, 추가 구현 불필요로 확정.
- **영향 범위**: `.claude/공통/gossh/v2/main.go`, `v2/gossh_os6`(재빌드). 운영 중인 `main.go`는
  무수정.
- **검증**: 192.168.0.58에서 압축/재사용, `-b` 접속불가 그룹, 350 제한 전부 실행 테스트로 확인.
  192.168.0.60(Go 1.20)에서 OS6 바이너리 재빌드 후 정상 동작 확인.

## 2026-09-04 — v2: pdsh 호환 문법 + -b 그룹출력 + 버그수정 + 색상 + OS6 빌드

- **파싱(`v2/main.go`)**: `-w^file`/`-wfile`처럼 공백 없이 붙는 pdsh 스타일 표기를 `preprocessArgs()`로
  전처리해서 지원. `-w=value`/`-w` 단독은 그대로 동작.
- **버그 수정(`v2/main.go`)**: `command = strings.Trim(command, "\"'")` 제거. 명령어가 실제로
  따옴표로 끝나는 경우(`grep 'asdf'`) 그 따옴표까지 잘려서 깨지던 버그.
- **신규 기능(`v2/main.go`)**: `-b` 옵션(clush 스타일 그룹 출력, `-script`와 같이 쓰면 무시),
  ANSI 색상(성공=초록/에러=빨강/경고=노랑/요약헤더=굵은청록, `-script`에서는 비활성),
  실행 파일 이름이 `pdsh`면 `-script` 기본값 `true`.
- **의존성(`v2/go.mod`, `v2/go.sum`, `v2/vendor/`)**: OS6(RHEL/CentOS 6, Go 1.20) 빌드 호환을 위해
  `golang.org/x/crypto` v0.55.0 → v0.31.0, `golang.org/x/sys` v0.47.0 → v0.28.0으로 다운그레이드.
  `go.mod`의 `go` 지시자도 `1.20`으로 낮춤 — 최신 Go 툴체인과 Go 1.20 툴체인이 같은 vendor를 공유.
- **신규 산출물(`v2/gossh_os6`)**: `/opt/go1.20/bin/go`(192.168.0.60)로 빌드한 정적 링크 OS6용
  바이너리를 저장소에 커밋.
- **영향 범위**: `.claude/공통/gossh/v2/` 전체(`main.go`, `go.mod`, `go.sum`, `vendor/`,
  `gossh_os6` 신규). 운영 중인 `main.go`(1~4장)는 이번에도 무수정.
- **검증**: 192.168.0.58에서 `-w` 3가지 표기, `-b`(그룹/비그룹 케이스), `-b -script` 조합,
  버그 재현 케이스(`grep 'asdf'`), 색상(raw ANSI 코드 확인 + `-script`에서 비활성 확인), `pdsh`
  이름 기본 동작을 전부 실행 테스트로 확인. 192.168.0.60(Go 1.20)에서 OS6 빌드 성공 + 실제 SSH
  명령 실행까지 정상 동작 확인. 다운그레이드한 의존성으로 최신 Go 툴체인(.58, 1.26) 빌드도 재확인.
