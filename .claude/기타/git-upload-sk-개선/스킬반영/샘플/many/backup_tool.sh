#!/bin/bash
# backup_tool.sh - 서버 설정 백업 도구
backup_host=""        # 백업 서버 호스트명
backup_dir=""         # 백업 저장 경로
notify_cmd=""         # 알림 스크립트 경로
exclude_list=""       # 제외 호스트 (공백 구분)
retention_days=""     # 보관 일수
remote_user=""        # 접속 계정
remote_pw=""          # 접속 비밀번호
echo "backup to ${backup_host}:${backup_dir}"
