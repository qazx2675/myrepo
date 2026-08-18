#!/bin/bash

# 1. 기존에 실행 중인 테스트 서버가 있다면 종료 (포트 충돌 방지)
echo '[1/3] 기존 서버 종료 중...'
pkill -f 'esxi-log-check -server'
sleep 1

# 2. 최신 소스코드로 다시 빌드 (수정사항 반영)
echo '[2/3] 최신 코드로 빌드 중...'
cd '/root/myrepo/.claude/VM 업무/esxi-log-check'
go build -o esxi-log-check .

# 3. 테스트(모의) 모드로 백그라운드 실행
echo '[3/3] 테스트 모드로 서버 시작...'
echo "192.168.0.58" > mock_test_hosts.txt
nohup ./esxi-log-check -server :12345 -w mock_test_hosts.txt -test-mock-host 192.168.0.58 > server.log 2>&1 &

echo ''
echo '==================================================='
echo '✅ 테스트 서버가 성공적으로 실행되었습니다!'
echo '👉 접속 주소: http://192.168.0.58:12345'
echo '==================================================='
