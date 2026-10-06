# backup_tool

서버 설정을 백업 서버로 보내는 bash 스크립트 (`backup_tool.sh`).

## 1. 빌드 및 설치 방법
- 빌드 과정은 없습니다 (bash 스크립트). 저장소 폴더를 통째로 내려받아 폐쇄망 서버로 복사하면 됩니다 (인터넷·외부 패키지 불필요, bash 4.1 이상 + sed/awk/grep).
- 폐쇄망에서만 아는 값 7개(`backup_host` 등)는 소스에 빈 변수로 비어 있습니다. 가이드 스크립트 한 번으로 채웁니다.

```bash
bash setup/setup_guide.sh
```

## 2. 사용 방법
```bash
bash backup_tool.sh
```

## 3. 옵션별 상세 설명 (설정 변수)
| 변수 | 필수 | 설명 | 예시 |
|---|---|---|---|
| backup_host | y | 백업 서버 호스트명 | backup01.example.local |
| backup_dir | y | 백업 서버의 저장 경로 | /data/backup/config |
| notify_cmd | n | 완료 알림 스크립트 | /opt/tools/notify.sh |
| exclude_list | n | 제외 호스트 (공백 구분) | test01 test02 |
| retention_days | n | 보관 일수 | 30 |
| remote_user | y | 접속 계정 | backup |
| remote_pw | n | 접속 비밀번호 (소스에 평문, chmod 600, 저장소 커밋 금지) | - |

가이드 옵션: `--role common`, `--dir <루트>`, `--no-setup`, `--yes`. 저장소에는 항상 빈 값으로 커밋하십시오.

## 문서별 설명
| 문서 | 내용 |
|---|---|
| README.md | 설치·사용·옵션 |
| 사용법.txt | 수동 실행용 명령 모음 |
| setup/vars.manifest | 빈 변수 정의 (설명·예시·검증) |
| setup/setup_guide.sh | 빈 변수 대화형 가이드 (자체포함) |

## 주의사항 (Disclaimer)
본 스크립트는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 후에는 랜덤한 서버 몇 대를 골라 실제로 변경되었는지 확인하십시오. `remote_pw` 는 평문으로 기록되므로 저장소에 커밋하지 마십시오.

## 5. 전역 명령어로 사용하기 (선택 사항)
```bash
sudo ln -s "$(pwd)/backup_tool.sh" /usr/local/bin/backup_tool
```
