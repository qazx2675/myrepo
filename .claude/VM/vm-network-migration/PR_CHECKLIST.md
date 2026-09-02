# PR_CHECKLIST.md

`vm-network-migration` 을 배포하거나 수정하기 전 확인 목록입니다.

## 빌드 / 정적 분석

- [ ] 폐쇄망 기준 빌드 성공 (`bash setup.sh` — `-mod=vendor`, `GOPROXY=off`)
- [ ] 빈 모듈 캐시로도 빌드되는지 확인
      (`GOMODCACHE=$(mktemp -d) GOPROXY=off go build -mod=vendor ./...`)
- [ ] `gofmt -l ./cmd ./internal` 출력 없음
- [ ] `go vet -mod=vendor ./...` 통과
- [ ] `go test -mod=vendor ./...` 통과
- [ ] 의존성을 추가/변경했다면 `go mod tidy && go mod vendor` 후 `vendor/` 커밋

## 동작 검증 (최소 1개 시나리오)

- [ ] 실제 vCenter 또는 vcsim 으로 **dry-run** 확인 (아무것도 바뀌지 않아야 함)
- [ ] 백업 → 생성 → 해제 → 연결 → 검증 전 과정 성공
- [ ] **재실행 시 멱등** — 두 번째 실행이 전부 "스킵" 으로 끝나는지
- [ ] **롤백 후 원상복구** — 포트그룹뿐 아니라 `connected` / `startConnected` 까지
      백업값과 일치하는지 독립적으로 재조회해서 확인
- [ ] 부분 실패 시 실패한 VM 만 원복되고 나머지는 계속 진행되는지
- [ ] 자격증명 / `-user` 누락 시 vCenter 접속 전에 종료 코드 2 로 멈추는지

## 안전장치 (고쳤다면 반드시 재확인)

- [ ] 백업 실패 시 상태 파일을 만들지 않고 중단하는지
- [ ] 기존 상태 파일을 `-force` 없이 덮어쓰지 않는지
- [ ] dry-run 이 실제 `state_{user}.json` 을 건드리지 않는지 (임시 파일 사용 + 정리)
- [ ] dry-run 실패 시 롤백을 호출하지 않는지
- [ ] 종료 코드 규약(0/1/2/3)이 `README.md` 표와 일치하는지

## 문서

- [ ] `CHANGELOG.md` 에 날짜순(최신 위) 항목 추가 — 검증 내용 포함
- [ ] `README.md` 옵션 표 / 알려진 한계 갱신 필요 여부 확인
- [ ] `ARCHITECTURE.md` 파일 역할 표 갱신 필요 여부 확인
- [ ] 배포 시 버전 태그 — `internal/cli.Version` 과 태그를 맞출 것

```bash
git tag -a v1.0.0 -m "<변경 요약> (CHANGELOG 2026-09-02 항목)"
git push origin v1.0.0
```

## 운영 전 마지막 확인

- [ ] **설정 변경 후 랜덤한 서버 몇 대를 vCenter 에서 직접 확인**했는지
      (자동 검증 통과 ≠ 서비스 정상)
- [ ] `state_{user}.json` 을 작업 종료 전까지 지우지 않았는지 (롤백 불가해짐)
