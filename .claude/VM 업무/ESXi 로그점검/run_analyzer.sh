#!/bin/bash
# ESXi 로그 분석 및 점검 툴 실행기

cd "$(dirname "$0")"

while true; do
    echo "==========================================="
    echo "       ESXi 로그 분석 및 점검 툴"
    echo "==========================================="
    echo "1. 실제 ESXi 서버 분석 (IP 직접 입력)"
    echo "2. 실제 ESXi 서버 분석 (호스트 목록 파일 기반)"
    echo "3. 모의 테스트 모드 실행 (테스트 패턴 주입 및 HTML 리포트 생성)"
    echo "4. 웹 서버 모드로 실행 (백그라운드)"
    echo "5. 임시 파일 및 캐시 정리 (.csv, .txt 등)"
    echo "6. 종료"
    echo "==========================================="
    read -p "원하는 작업을 선택하세요 (1-6): " choice

    case $choice in
        1)
            read -p "타겟 ESXi IP 주소를 입력하세요: " target_ip
            if [ -z "$target_ip" ]; then
                echo "[!] IP가 입력되지 않았습니다."
                continue
            fi
            echo "$target_ip" > temp_target_host.txt
            echo "[INFO] $target_ip 대상으로 분석을 시작합니다..."
            ./esxi-log-check -w temp_target_host.txt -format text
            rm -f temp_target_host.txt
            ;;
        2)
            read -p "호스트 목록 파일 이름(예: real_hosts.txt)을 입력하세요: " host_file
            if [ -z "$host_file" ]; then
                echo "[!] 파일 이름이 입력되지 않았습니다."
                continue
            fi
            if [ ! -f "$host_file" ]; then
                echo "[!] '$host_file' 파일이 없습니다. 파일을 생성 후 다시 시도하세요."
                continue
            fi
            echo "[INFO] '$host_file' 대상을 분석합니다..."
            ./esxi-log-check -w "$host_file" -format html -out real_report.html
            echo "[INFO] 완료! real_report.html 파일을 확인하세요."
            ;;
        3)
            echo "[INFO] 모의 테스트 환경을 준비합니다..."
            echo "192.168.0.58" > mock_test_hosts.txt
            /usr/local/bin/esxi_mock_logger -all -logdir /var/run/log -fast
            echo "[INFO] 모의 패턴을 생성 완료했습니다. 분석을 시작합니다..."
            ./esxi-log-check -w mock_test_hosts.txt -format html -out mock_test_report.html
            echo "[INFO] 완료! mock_test_report.html 파일을 확인하세요."
            ;;
        4)
            echo "웹 서버에서 모니터링할 대상을 선택하세요:"
            echo "  1) 모의 테스트 환경 (기본값)"
            echo "  2) 실제 ESXi IP 직접 입력"
            echo "  3) 실제 호스트 목록 파일 기반"
            read -p "선택 (1-3): " web_choice
            
            # 기존에 실행중인 웹 서버가 있다면 종료
            pkill -f "esxi-log-check -server :12345" 2>/dev/null
            
            if [ "$web_choice" == "2" ]; then
                read -p "타겟 ESXi IP 주소를 입력하세요: " web_ip
                if [ -z "$web_ip" ]; then
                    echo "[!] IP가 입력되지 않았습니다."
                    continue
                fi
                echo "$web_ip" > web_target_host.txt
                nohup ./esxi-log-check -server :12345 -w web_target_host.txt > server.log 2>&1 &
                echo "[INFO] 웹 서버가 시작되었습니다. (대상 IP: $web_ip)"
            elif [ "$web_choice" == "3" ]; then
                read -p "호스트 목록 파일 이름(예: real_hosts.txt)을 입력하세요: " web_file
                if [ -z "$web_file" ] || [ ! -f "$web_file" ]; then
                    echo "[!] 유효한 파일이 아닙니다."
                    continue
                fi
                nohup ./esxi-log-check -server :12345 -w "$web_file" > server.log 2>&1 &
                echo "[INFO] 웹 서버가 시작되었습니다. (대상 파일: $web_file)"
            else
                echo "[INFO] 모의 테스트 환경으로 웹 서버를 시작합니다."
                bash start_server.sh
            fi
            
            echo "[INFO] 접속 주소: http://192.168.0.58:12345"
            ;;
        5)
            echo "[INFO] 임시 파일 및 캐시 파일 정리를 시작합니다..."
            rm -f *.csv
            rm -f collected/*.txt
            rm -f mock_test_hosts.txt temp_target_host.txt
            rm -f test_all_report.html mock_result.json mock_test_report.html
            rm -f /var/run/log/vobd.log /var/run/log/vmkernel.log /var/run/log/hostd.log /var/run/log/vmkwarning.log /var/run/log/vmksummary.log /var/run/log/sensord.log /var/run/log/ipmi/sel
            echo "[INFO] 정리가 완료되었습니다."
            ;;
        6)
            echo "프로그램을 종료합니다."
            break
            ;;
        *)
            echo "[!] 잘못된 선택입니다. 1~6 사이의 숫자를 입력하세요."
            ;;
    esac
    echo ""
done
