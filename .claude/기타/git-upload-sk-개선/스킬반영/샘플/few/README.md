# few (ping_tool)

간단 ping 점검 쉘 스크립트(`ping_tool.sh`). 폐쇄망에서 쓸 수 있도록 외부 의존성 없이 bash 만 사용한다.

## 1. 빌드 및 설치 방법
- 빌드 불필요 (bash 스크립트). 저장소를 통째로 내려받아(폐쇄망은 압축해서 반입) 원하는 경로에 풀면 된다.
- 필요 환경: bash, ping.
- 폐쇄망 값 세팅: 소스의 빈 변수 3개(`target_list`, `log_dir`, `mail_to`)를 채워야 한다. (세팅 가이드는 별도 안내 참조)
- 실행 권한: `chmod +x ping_tool.sh`

## 2. 사용 방법
```bash
bash ping_tool.sh
```

## 3. 옵션별 상세 설명
| 변수 | 설명 | 예시 |
|---|---|---|
| `target_list` | 점검 대상 목록 파일 | `/etc/ping_targets.txt` |
| `log_dir` | 로그 경로 | `/var/log/ping_tool` |
| `mail_to` | 결과 메일 수신자 | `admin@example.com` |

## 4. 문서별 설명
| 파일 | 설명 |
|---|---|
| ping_tool.sh | ping 점검 본체 |
| README.md | 설치·사용·옵션 설명 |
| 사용법.txt | 수동 테스트용 명령어 모음 |

## 주의사항 (Disclaimer)
본 스크립트는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 변수 값을 설정(변경)한 뒤에는 임의의 서버 몇 대에서 실제로 값이 반영되어 동작하는지 확인하십시오.

## 5. 전역 명령어로 사용하기 (선택 사항)
```bash
sudo cp ping_tool.sh /usr/local/bin/ping_tool && sudo chmod +x /usr/local/bin/ping_tool
```
