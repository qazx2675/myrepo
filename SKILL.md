---
name: git-upload-sk
description: "해당폴더를 통째로 다운로드하면 폐쇄망에서도 빌드가 가능한상태로 만들어야한다. 깃에서 다운로드하면 어떻게 설치하고 빌드하는지 설명도 있어야한다."
---

git에 업로드하는 go lang의 경우 해당폴더를 통째로 다운로드하면 폐쇄망에서도 빌드가 가능한상태로 만들어야한다.

깃에서 다운로드하면 어떻게 설치하고 빌드하는지 설명도 있어야한다.
사용방법 및 설치방법은 모든 언어에 적용되어야한다.

순서
1) 빌드 및 설치 방법 -> (2) 사용 방법 -> (3) 옵션별 상세 설명 -> 문서별 설명
주의사항 추가 필요 
주의사항
 주의사항 (Disclaimer) 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.  설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하라는 문구도 포함되어야한다.
5.전역 명령어로 사용하기 (선택 사항)
git에 등록되어있는 멘트

유지보수를 위해 추가할 것
문서(1~5번) 작성이 끝나면, 나중에 다른 사람이나 AI가 유지보수하기 쉽도록 아래 항목도 함께 만들어서 프로젝트 폴더에 넣는다.
CI 워크플로 — 커밋마다 빌드/정적분석/테스트가 자동으로 돌도록 `.github/workflows/ci.yml`을 추가한다. (프로젝트 경로가 저장소 루트가 아니면 `working-directory`로 지정)
```yaml
   name: CI
   on: [push, pull_request]
   jobs:
     build-test:
       runs-on: ubuntu-latest
       defaults:
         run:
           working-directory: <프로젝트 경로>
       steps:
         - uses: actions/checkout@v4
         - uses: actions/setup-go@v5
           with: { go-version: '<go.mod의 버전>' }
         - run: go build -mod=vendor ./...
         - run: go vet -mod=vendor ./...
         - run: go test -mod=vendor ./...
   ```
ARCHITECTURE.md — 폴더/파일별 역할을 표로 짧게 정리해서 낯선 사람이나 AI가 전체를 다 안 읽어도 어디를 고쳐야 할지 알 수 있게 한다.
```markdown
   # ARCHITECTURE.md

   | 폴더/파일 | 역할 |
   |---|---|
   | main.go | CLI 진입점, 전체 흐름 조립 |
   | <패키지명>/ | <역할 한 줄> |

   수정 요청이 "OO 추가"면 <관련 폴더> 몇 곳만 보면 됨.
   ```
   흐름이 있는 프로젝트라면 표 아래에 "작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고" 한 줄을 추가한다.
버전 태그 ↔ CHANGELOG 연결 — 배포 시점마다 태그를 남기고, 태그 메시지에 CHANGELOG의 어느 항목까지 포함하는지 적는다.
```bash
   git tag -a v0.3.0 -m "<변경 요약> (CHANGELOG YYYY-MM-DD 항목)"
   git push origin v0.3.0
   ```
WORKFLOW.md + workflow.svg — 명령 한 번(또는 주요 실행 경로) 이 실행됐을 때 단계별로 어떻게 흘러가는지 mermaid flowchart로 그린다. README.md와 ARCHITECTURE.md에서 `[WORKFLOW.md](WORKFLOW.md)`로 서로 링크해서 참조하게 한다. mermaid를 렌더링한 `workflow.svg`를 같은 폴더에 함께 저장해 GitHub에서 바로 그림으로 보이게 한다.
```markdown
   # 작업 흐름도 — <프로젝트명>

   `<명령 예시>` 한 번이 실행되는 흐름입니다. 옵션 전체는 [README.md](README.md)를 참고하세요.

   ```mermaid
   flowchart TD
       A["시작 (명령/입력)"] --> B["파싱/검증"]
       B --> C["핵심 처리"]
       C --> D["결과 출력/기록"]
   ```
   ```
setup.sh 고정 틀 — 오프라인(폐쇄망) 빌드 스크립트는 아래 구조를 그대로 따른다. 프로젝트마다 빌드 대상/안내 문구만 바꾼다.
```bash
   #!/usr/bin/env bash
   # setup.sh — 폐쇄망(오프라인) 빌드. vendor/ 안의 의존성만 사용, 인터넷 접속 시도 안 함.
   set -euo pipefail
   cd "$(dirname "$0")"

   if ! command -v go >/dev/null 2>&1; then
     echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
     exit 2
   fi

   export GOPROXY=off
   export GOFLAGS=-mod=vendor

   go build -mod=vendor -o <바이너리명> .

   echo
   echo '완료. 다음으로 진행하십시오:'
   echo '  1) ...'
   ```
PR 체크리스트 (markdown, 자동 저장) — 배포/수정 전 실수 방지를 위한 체크리스트를 markdown 파일로 만든다. 이 파일은 작업한 프로젝트 폴더, 즉 그 프로젝트의 README.md와 같은 경로에 `PR_CHECKLIST.md`라는 이름으로 저장한다(별도로 묻지 않고 문서 작성 단계에서 자동으로 만들어 둔다).
```markdown
   # PR_CHECKLIST.md

   - [ ] 빌드 성공 (오프라인 빌드 프로젝트면 vendor 기준으로 확인)
   - [ ] 테스트 통과 (go test ./... 등)
   - [ ] 실제 환경 또는 시뮬레이터로 최소 1개 시나리오 검증
   - [ ] CHANGELOG.md 항목 추가
   - [ ] README.md 관련 표/설명 갱신 필요 여부 확인
   - [ ] 흐름이 바뀌었으면 WORKFLOW.md / ARCHITECTURE.md 흐름도 갱신
   ```
이 여섯 가지(CI, ARCHITECTURE.md, 버전 태그 규칙 안내, WORKFLOW.md+workflow.svg, setup.sh 고정 틀, PR_CHECKLIST.md)는 프로젝트 문서를 새로 만들거나 크게 손볼 때마다 이 스킬의 기본 산출물에 포함시킨다. PR_CHECKLIST.md는 항상 README.md와 동일한 디렉터리에 저장한다.
그리고 수동으로 테스트할수있도록 내가써야하는 명령어만 포함한 사용법.txt를 만든다. 각명령위에 설명은 주석으로 포함

## 빈 변수가 있는 프로젝트: 가이드·패치 스크립트 (필수 — 건너뛰지 말 것)
폐쇄망에서만 알 수 있는 값(경로·호스트·비밀번호)을 소스의 빈 변수(`var=""`, conf 의 빈 값)로 두는 프로젝트는, 신규 작성이든 업데이트든 **스크립트 한 번 실행으로 폐쇄환경 세팅이 끝나게** 아래를 반드시 만든다.
1. 프로젝트의 빈 변수 목록을 **문서(README 등)를 쓰기 전에** 먼저 확인한다. **5개 이하이면 사용자에게 `가이드·패치 스크립트를 만들까요? (y/n)` 를 먼저 묻고**(n 이면 변수 목록과 채울 위치만 안내하고, README 에는 직접 채우는 방법을 쓴다), **6개 이상이면 묻지 않고 바로 만든다.**
2. `references/guide-and-update.md` 를 반드시 Read 한다 (manifest 규칙·생성 절차·검증). 스크립트를 새로 짜지 말고 `templates/` 의 틀을 그대로 쓴다.
3. conf 디렉터리가 있으면 `conf/`, 없으면 새 `setup/` 에 `vars.manifest` 와 가이드(`setup_guide.sh`)를 둔다. 가이드는 `bash templates/make_guide.sh --out <디렉터리>/setup_guide.sh` 로 **생성**해서 넣고(`make_*.sh`·`lib_common.sh` 같은 틀은 프로젝트에 복사하지 않는다), manifest 를 고치면 가이드·업데이트 스크립트를 **다시 생성해 프로젝트에 다시 넣는다**. 이 디렉터리에는 **가이드·패치에 필요한 스크립트와 파일만** 둔다.
4. 이미 배포된 프로젝트를 수정하는 경우: 이전→신규 소스로 `make_update.sh` 를 돌려 `update_v<버전>.sh` 를 만든다. 현장에서 수정한 값·주석·코멘트는 건드리지 않고(바뀌게 되면 중단), 신규·변경 변수만 가이드로 묻는다.
5. 변수 설명에는 무엇을 설정하는지·정확한 동작·예시(예: `/path/config_check.sh`)를 쓴다. 둘 중 하나만 채우는 변수는 `xor` 그룹으로 묶는다.
6. 가독성 색: 정상=초록, 경고=노랑, 오류=빨강, 안내=시안, 변수명=굵게, 예시=회색. `NO_COLOR` 또는 tty 가 아니면 끈다.
7. 비밀번호 변수는 소스에 평문 + 파일 권한 600 + "저장소 커밋 금지" 경고, 화면·로그는 마스킹. 저장소에는 항상 빈 값으로 커밋한다.
8. `사용법.txt` 에 가이드 실행(`bash setup/setup_guide.sh`)과 업데이트 실행(`bash setup/update_v<버전>.sh`) 명령을 넣고, 가이드가 끝나면 Disclaimer(설정 변경 후 랜덤 서버 몇 대 확인)를 출력한다.
완료 점검: □ 빈 변수 개수 확인(5개 이하면 y/n 질문) □ `setup/` 또는 `conf/` 생성 □ manifest 설명·예시 작성 □ 랩에서 테스트 통과 □ `사용법.txt` 반영
