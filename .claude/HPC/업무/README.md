# hpcbot — 서버 운영 매뉴얼 챗봇 (RHEL8, 오프라인)

> **면책**: 답변은 사내 서버 운영 매뉴얼 원문을 그대로 보여주는 참고용입니다. 작업 전에는 매뉴얼 원본과 승인 절차를 확인하세요. 정확도는 `test_report.md` 에 있는 질문셋 기준이며 실제 질문과 다를 수 있습니다.

## 1. 무엇을 하나
- 질문에서 키워드를 찾아 점수를 매기고, 매뉴얼의 **항목(블록, 86개) 원문 + §번호**를 출력합니다.
- 점수(키워드 → 항목 가중치)는 **빌드할 때 Jev API 로 받아 `data/kb.json` 에 저장**합니다. 운영 바이너리는 기본적으로 네트워크를 쓰지 않습니다.
- 영타로 친 한글(`xptmxm` → 테스트)을 자동 감지해 변환하고, `/h <문장>` 으로 강제 변환할 수 있습니다.
- 질문마다 4자리 `code` 를 붙여 화면과 `/tmp/hpcbot/query.log` 에 질문·답변 전문을 남깁니다.

## 2. 빠른 사용
```bash
./build.sh                       # 테스트 후 dist/hpcbot (정적 바이너리) 생성
dist/hpcbot "gpu 드라이버 설치"   # 한 줄 실행 (종료 코드 0=답변, 2=후보, 1=미매칭)
dist/hpcbot                      # 대화형
dist/hpcbot --jev "질문"         # 로컬 판정이 애매할 때만 Jev 1회 호출 (기본 꺼짐)
```
자세한 명령은 `사용법.txt`.

## 3. 빌드 (폐쇄망 가능)
- 표준 라이브러리만 사용합니다 (외부 의존성·vendor 없음). Go 1.26.5 (`GOTOOLCHAIN=go1.26.5`).
- **`data/manual.md`(매뉴얼 원문)는 공개 저장소에 올리지 않으므로 직접 복사**해야 빌드됩니다. `build.sh` 는 파일이 없으면 안내 후 종료합니다.
- 매뉴얼과 `kb.json` 은 `go:embed` 로 내장되고, `$HPCBOT_DATA/<파일>` 또는 실행 파일 폴더의 같은 이름 파일이 있으면 그 파일이 우선합니다 (`-v` 로 출처 확인).
- CI 워크플로는 두지 않았습니다: 매뉴얼이 저장소에 없어 CI 에서 빌드·테스트할 수 없습니다.

## 4. 정확도 (요약, 상세는 test_report.md)
| 세트 | 1위 정확도 | 3위 내 포함 | os↔gpu 혼동 |
|---|---|---|---|
| train (70) | 97.1% | 100% | 0 |
| holdout (30) | 93.3% | 96.7% | 0 |

holdout 은 기준(95%)에 미달합니다. 판정 2회를 모두 사용했고 원인·후속 방안은 `test_report.md` 에 있습니다.

## 5. 키워드 가중치 다시 만들기 (개발자용)
```bash
export GOTOOLCHAIN=go1.26.5
go run ./tools/kbgen weights -single   # data/kb_seed.json → data/kb.json (Jev 키 필요, 캐시 사용)
go run ./tools/kbgen validate -single  # 질문셋 라벨 교차검증 → testdata/jev_labels.json
HPCBOT_REPORT=1 go test -run TestAccuracy .   # test_report.md 갱신
```
Jev 키는 `JEV_API_KEY` 환경변수 → `~/.jev-claude.env` → `~/.jev-router.env` 순으로 읽으며 출력·커밋하지 않습니다.
