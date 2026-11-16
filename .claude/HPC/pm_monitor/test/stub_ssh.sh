#!/bin/bash
# stub_ssh.sh - 가짜 ssh (DHCP 조회 시험용)
# 받은 인자와 stdin 을 그대로 출력해서 테스트가 정확히 무엇을 받았는지 확인하게 한다.
#   STUB_SSH_FAIL  1 이면 "접속 실패" 를 stderr 로 찍고 종료코드 255
echo "NICE: $(nice)"
echo "ARGS: $*"
echo "STDIN:"
cat
if [ "$STUB_SSH_FAIL" = "1" ]; then
  echo "ssh: connect to host failed" >&2
  exit 255
fi
exit 0
