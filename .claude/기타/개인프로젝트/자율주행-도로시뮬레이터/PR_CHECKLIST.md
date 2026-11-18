# PR_CHECKLIST.md

- [ ] 오프라인 설치 확인 (`bash setup.sh` 또는 `setup.bat`, wheels/ 기준)
- [ ] 테스트 통과 (`python tests/test_sim.py` 또는 `python -m pytest tests`)
- [ ] 헤드리스 자동주행 충돌 0 (`python main.py --headless --seconds 600 --level hard`)
- [ ] 창 모드로 최소 1개 코스 직접 주행 (운전석/추적/위에서 시점, 주차장 포함)
- [ ] CHANGELOG.md 항목 추가
- [ ] README.md 옵션/키 표 갱신 필요 여부 확인
- [ ] 흐름이 바뀌었으면 WORKFLOW.md / workflow.svg / ARCHITECTURE.md 갱신
