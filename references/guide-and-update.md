# 가이드·패치 스크립트 만드는 방법 (git-upload-sk 참조 문서)

전체 규약은 `design-spec.md`, 예시는 `example.vars.manifest` 를 본다. 틀은 `../templates/` 에 있다 — **새로 작성하지 말고 그대로 복사해 쓴다.**

## 1. 신규 프로젝트
1. 소스의 빈 변수를 모두 찾아 `vars.manifest` 를 만든다 (형식: `name|target|kind|role|group|required|desc|example|check|since|renamed_from`, `example.vars.manifest` 참고).
   - `target`: 쉘 소스는 `sh:<파일>`, conf 는 `conf:<파일>` (프로젝트 루트 기준 상대경로).
   - `desc`: 무엇을 설정하는지 + **정확한 동작**(예: 완료기록 변수 둘 중 하나만 쓰는 이유). `example`: 실제 모양의 예시(`/path/config_check.sh`).
   - 둘 중 하나만 쓰는 변수는 `xor:<id>` 그룹 + `#@xor` 지시자로 상황 질문을 적는다.
   - 마스킹(주석·코멘트) 판정 키워드가 있으면 `#@guard <ERE>`.
   - 프로젝트에 `setup.sh` 가 있으면 `#@meta setup=<상대경로>`, 없으면 두지 않는다 (가이드가 설치 단계를 건너뜀).
2. 가이드를 만든다 (프로젝트의 `setup/` 또는 `conf/` 안에):
   `bash templates/make_guide.sh --out <setup 디렉터리>/setup_guide.sh` 후 `vars.manifest` 를 같은 디렉터리에 둔다.
3. 사용자 실행: `bash setup/setup_guide.sh` (옵션 `--role os8|os6|common`, `--dir`, `--no-setup`, `--yes`).

## 2. 기존 프로젝트 업데이트
1. 이전 소스와 새 소스, 최신 manifest 로 생성한다:
   `bash templates/make_update.sh --old <이전> --new <신규> --manifest vars.manifest --version <v> --file <상대경로> --out <setup 디렉터리>/update_v<v>.sh`
2. **변경은 가능한 한 삽입(insert_*) 위주로 설계한다.** 필드에서 수정한 줄을 지우거나 바꾸는 replace 는 현장 사본이 다르면 중단되므로 최소화한다 (예: 기존 블록을 `if [ -z "$AUTO_MODE" ]; then … fi` 로 감싸고 가운데 줄은 그대로 둔다).
3. 생성기가 출력하는 검토 표(앵커·삽입/삭제 줄 수·가드 경고)를 읽고, 앵커가 유일하지 않거나 가드 경고가 있으면 소스 변경 방식을 고쳐 다시 생성한다.
4. 사용자 실행: `bash update_v<v>.sh [--dir <루트>] [--dry] [--yes] [--undo]` (현장 사본은 vim 으로 `\r` 제거한 LF 사본 기준, CRLF 도 자동 처리).
   - 앵커 줄이 현장에서 바뀌어 있으면 파일을 건드리지 않고 중단 + 위치를 알려준다. 신규 변수만 묻고 기존 값은 유지한다. 실패 시 자동 복구, `--undo` 로 되돌린다.

## 3. 검증 (필수)
- 랩(.58 Rocky 8)에서 테스트한다. bash 4.1 호환은 `podman run --rm -v <dir>:/w:Z docker.io/library/centos:6 bash …` (CentOS 6 컨테이너, bash 4.1.2/gawk 3.1.7) 로 확인한다. 컨테이너에 `patch`·`script` 가 없어 일부 테스트가 건너뛰어지는 것은 정상이다.
- 확인 시나리오: ① 빈 변수 사본 + 가이드 한 번으로 완료 ② 벤더 문자열·경로·주석·코멘트를 수정한 사본에 업데이트 → 수정 보존 + 신규 변수만 질문 ③ 잘못된 경로·CRLF·잘린 붙여넣기 ④ `--undo` 바이트 동일 복원.
- 실제 현장 값(경로·서버)은 검증할 수 없으므로 결과 보고에 스텁 검증이라고 명시한다.

## 4. 알려진 한계
- 업데이트는 앵커 줄이 현장에서 바뀌면 중단한다 (파일 불변). 이미 이전 방식으로 패치된 사본은 "이미 적용됨"이 아니라 앵커 불일치로 중단될 수 있다.
- `.bak.*`·`.update_journal` 은 대상 파일 옆(프로젝트 루트)에 생긴다.
- 업데이트 스크립트는 lib+엔진을 포함해 크다(약 90KB). 붙여넣기보다 파일 반입을 권장하며, 잘림은 줄 수·끝 표시로 감지해 중단한다.

## 5. manifest 작성 요령 (실수하기 쉬운 곳)
- `required`: 없으면 동작하지 않는 변수만 `y`, 나머지는 `n`. `check`: 경로 변수는 `file`/`dir`, 호스트는 `host`(연결 점검 훅이 있으면 `probe`), 그 외 `none`. 비밀번호(`secret`)는 `example` 에 실제 값 대신 안내 문구를 쓴다.
- `desc` 에는 "예:" 를 쓰지 않는다 (가이드가 `example` 필드로 `예:` 줄을 따로 출력해 중복된다). 값에 세로선 문자를 쓸 수 없으므로 목록 구분자 설명은 말로 쓴다.
- 비밀번호는 보통 계정 변수에 `dep:<계정변수>` 로 의존시킨다.
- 가이드 끝에는 한 줄 Disclaimer(설정 변경 후 랜덤 서버 몇 대 확인)가 출력된다. 전체 Disclaimer 는 README 에 쓴다.
- 6개 이상이면 묻지 않고 만들고, 5개 이하이면 만들기 전에 y/n 으로 묻는다 (문서 작성 전에).
