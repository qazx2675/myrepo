# CHANGELOG.md — bios_compare

## [0.1.0] - 2026-10-08
- 실제조사 JSON 과 bios_json 의 BIOS 설정이름을 양방향 비교(이름 존재만), 모델별 결과 저장.
- 요약 폴더(DL360_G9_XL170R_G9_… 등) 분해 매칭, 모델 자동 판단(JSON Model), 시리얼·MAC 등 제외(ignore.txt).
- 외부 의존성 없음, 오프라인 빌드 setup.sh, 빌드 완료 바이너리(RHEL8) 포함. 검증은 랩의 합성 입력으로만 했으며 실제 out/ 데이터는 미검증.
