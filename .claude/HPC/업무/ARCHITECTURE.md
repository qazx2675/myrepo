# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| main.go | REPL·한 줄 실행·후보 번호 선택·플래그(`--jev`, `-v`), 종료 코드 |
| match.go | 점수 엔진: 정규화·키워드 매칭(긴 것 우선)·판정(답변/후보/미매칭)·영타 자동 감지 |
| hangul.go | 영타→한글(2벌식), 토큰 변환, 문장부호 처리 |
| kb.go | kb.json·manual.md 로드(외부 파일 우선, 내장 대체), 매뉴얼 파서, 앵커 해석, 보호 단어 |
| render.go | 블록 원문 → 터미널 출력 |
| qlog.go (+ qlog_unix.go / qlog_windows.go) | code 발급, 공용 로그 기록(flock·회전) |
| jev.go | 운영 `--jev` 옵션용 Jev 클라이언트·키 탐색 |
| data/kb_seed.json | 블록·앵커·키워드·stopwords·overrides 원본 (사람이 편집) |
| data/kb.json | kbgen 산출물 (seed + Jev 가중치 + 임계값) — 내장 대상 |
| data/manual.md | 매뉴얼 원문 (**gitignore**, 로컬 복사) |
| tools/kbgen/ | 빌드 도구: Jev 로 키워드 가중치 생성·질문 라벨 검증, cache.json(응답 캐시) |
| testdata/questions.json, jev_labels.json | 질문셋 100개(train 70/holdout 30), Jev 교차검증 결과 |
| *_test.go, accuracy_test.go | 단위·회귀·정확도 테스트 (`HPCBOT_REPORT=1` 이면 test_report.md 생성) |
| test_report.md | 정확도 리포트·튜닝 기록 |
| build.sh | manual.md 확인 → vet/test → 정적 빌드 → dist/hpcbot |
| 계획서.md | 설계·결정 사항 (§5 가 원문보다 우선) |

> **수정 팁**
> - 질문이 엉뚱한 항목으로 가면: `data/kb_seed.json` 의 해당 블록 `keywords` 를 보강 → `kbgen weights -single`(새 키워드만 호출) → `go test`.
> - 특정 일반어가 한 항목으로 쏠리면: seed 의 `overrides`(최대 20개, `reason` 필수).
> - 답변 형식은 `render.go`, 판정 임계값은 `kb_seed.json` 의 `thresholds`.
> - 매뉴얼 제목이 바뀌어 `[경고] 앵커 없음` 이 나오면 seed 의 `anchor` 를 고친다.
