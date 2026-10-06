# git-upload-sk 스킬 (빈 변수 가이드·패치 확장판)

`git-upload-sk` 스킬에 "빈 변수가 있는 프로젝트의 가이드·패치 스크립트" 규칙을 추가한 버전의 원본입니다. 이 저장소에서 버전 관리합니다.

| 위치 | 내용 |
|---|---|
| `git-upload-sk/SKILL.md` | 기존 규칙 + 맨 끝에 "빈 변수가 있는 프로젝트" 필수 단계(약 25줄) |
| `git-upload-sk/references/` | `guide-and-update.md`(만드는 방법), `design-spec.md`(규약), `example.vars.manifest`(예시) |
| `git-upload-sk/templates/` | `lib_common.sh`, `setup_guide.sh`, `update_engine.sh`, `make_update.sh`, `make_guide.sh` |
| `백업/SKILL.md.orig` | 수정 전 원본 SKILL.md |

## 적용 방법
`git-upload-sk` 는 Claude 계정에 동기화되는 스킬이라 git 에서 자동으로 반영되지 않습니다. `git-upload-sk/` 폴더를 zip 으로 묶어 Claude 앱의 스킬 관리 화면에서 교체하세요(zip 안의 최상위 폴더가 `git-upload-sk/`).

```bash
# zip 만들기 (이 디렉터리에서)
zip -r git-upload-sk.zip git-upload-sk
```

## 검증 결과 (스텁 기준, 랩 .58 + CentOS6 bash 4.1 컨테이너)
lib 370 / guide 133 / engine 110 / make 163 / make_guide 45 / pilot 308 (CentOS6 에서는 `patch`·`script` 명령이 없어 일부 건너뜀). 실제 현장 값은 검증하지 못했습니다. 개발 트리와 테스트는 상위의 `구현/` 에 있습니다.
