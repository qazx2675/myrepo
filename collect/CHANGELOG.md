# CHANGELOG.md — collect (biosdump)

## [0.1.1] - 2026-10-08

- `-timeout` 옵션 추가(기본 120초), 오류 메시지에 접속 대상 경로 포함.

## [0.1.0] - 2026-10-08

- list.txt 의 hostname 마다 /etc/hosts 의 `<hostname>-m` IP 로 Redfish 접속, Systems 별 Bios(+Bios/Settings) 전체를 JSON 으로 저장 (읽기 전용, 일회성 수집).
- 외부 의존성 없음(표준 라이브러리). 오프라인 빌드 `setup.sh`, 빌드 완료 바이너리 `biosdump`(Rocky/RHEL8) 포함.
- 검증은 랩의 mock BMC(Dell R660, HPE DL360 Gen11)로만 했으며 실제 BMC 는 미검증.
