# gpu_install Confluence 가이드

NVIDIA 드라이버(`.run`) + CUDA 버전별 DCGM 4 일괄 설치 도구(`gpu_install`)의 Confluence Storage Format 문서와 번호 표시 화면 이미지입니다.

## 포함 파일

- `gpu_install_confluence.xhtml` : Confluence Storage Format 본문
- `fig1_setup_and_list.png` : `gpu_install.sh` 변수 설정 · `list` 작성 · 실행 (①~③)
- `fig2_version_and_warning.png` : 드라이버 버전 선택 및 시작 확인 경고 (①~③)
- `fig3_survey_and_reinstall.png` : 현황 수집 · 방식 판정 · 재설치 확인 (①~⑤)
- `fig4_install_progress.png` : 드라이버 · DCGM 4 · `/etc/rc.local` 병렬 실행 (①~④)
- `fig5_result_and_faillist.png` : 최종 결과 표 및 `fail_list` / `fail_list.retry` (①~③)
- `fig6_help_and_retry.png` : 도움말(`-h`) · 상세 로그(`lsof`) · 실패 호스트 재시도 (①~③)

## Confluence 적용 방법

1. Confluence 페이지 편집 모드에서 위 PNG 6장을 페이지 첨부파일로 업로드합니다.
2. Source Editor(`<>`)를 열고 `gpu_install_confluence.xhtml` 전체 내용을 붙여넣은 뒤 저장합니다.
