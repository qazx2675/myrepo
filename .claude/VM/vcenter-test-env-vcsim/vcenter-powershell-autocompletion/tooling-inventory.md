# 활용 도구 목록 정의서 (Phase 1 산출물)

이 프로젝트가 새로 만들지 않고 그대로 활용하는 기존 도구와, 실제로 새로 개발하는 부분을 구분해서 정리한다.

## 1. 기존 도구 그대로 활용 (신규 개발 없음)

| 도구 | 역할 | 비고 |
|---|---|---|
| PowerShell 7.6.4 (pwsh) | 실행 런타임, 크로스플랫폼(Linux 포함) | 폐쇄망 오프라인 설치 대상 |
| PSReadLine (7+ 기본 포함) | 인라인 예측, ListView, 구문 강조, 괄호/따옴표 자동완성 | 버전 2.2.2+ 필요 시 별도 확인 (커스텀 predictor는 안 씀 — 2장 참고) |
| PowerShell 네이티브 멤버 완성 (`TabExpansion2`) | `Get-View` 반환 객체(`VMware.Vim.*`)의 속성/메서드 자동완성 | PowerCLI 타입 어셈블리가 모듈 경로에 정상 로드되어 있으면 자동 동작 |
| VMware PowerCLI | vCenter 조작 cmdlet 전체 | 오프라인 모듈 번들 필요 |
| `vc-test-env` (`../` 형제 프로젝트) | 실습용 vcsim 테스트 환경 자동 구축 | 이미 구현·검증 완료, 그대로 연동만 |

## 2. 이 프로젝트에서 새로 개발하는 부분

| 구성요소 | 방식 | 비고 |
|---|---|---|
| 순차 조회 권고 로직 | PowerShell 프록시 함수(Proxy Function) — `Get-VM` 등을 감싸서 경고 메시지 출력 | `command-advisory-rules.md` 규칙 테이블 사용 |
| vCenter 인벤토리 인자 자동완성 | `Register-ArgumentCompleter` 스크립트 | VM/호스트/클러스터 이름을 실시간 조회해 Tab 후보로 제공 |
| 프로필 초기화 스크립트 | `$PROFILE`에 위 두 가지를 등록하는 초기화 로직 | 오프라인 배포 시 setup.sh가 자동 설치 |

## 3. 검토했지만 채택하지 않은 방식

| 방식 | 검토 결과 | 사유 |
|---|---|---|
| 커스텀 `ICommandPredictor` 플러그인 (C#/.NET) | 미채택 | 20ms 응답 제약, 인메모리 .NET 어셈블리 필요 — 안내 메시지 출력이라는 목적에 비해 개발/유지보수 비용 과함. 프록시 함수로 충분 |
| 독자적인 CLI/셸(Go 등)을 새로 개발 | 미채택 | pwsh가 이미 Linux에서 완전히 동작하고 PSReadLine이 IDE 수준 UX를 제공 — 새 셸을 만들 이유 없음 |
| Go 기반 자동완성 엔진 | 미채택 | 자동완성은 PowerShell 프로세스 내부(PSReadLine/TabExpansion2) 기능이라 외부 프로세스로 대체 불가 |

## 4. Go가 쓰이는 유일한 지점

- `vc-test-env`(실습 환경 복제 도구) — 이미 존재하는 별도 프로젝트, 이 프로젝트가 그대로 의존
- 오프라인 번들 무결성 검증 스크립트를 여러 배포판 대상 단일 정적 바이너리로 만들고 싶을 경우에만 선택적으로 검토 (3장 참고, 필수 아님)

## 5. 버전 고정 필요 목록 (Phase 2에서 확정)

- PowerShell: 7.6.4
- PowerCLI: (버전 미정 — 타겟 vCenter API 버전과 호환되는 버전으로 확정 필요)
- PSReadLine: (버전 미정 — 2.2.2 이상)
- `vc-test-env` / govmomi: README 기준 v0.55.1로 검증됨
