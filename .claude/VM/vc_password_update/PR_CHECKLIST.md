# PR_CHECKLIST.md

`vc_password_update`를 배포하거나 수정하기 전 확인 목록입니다.

## 빌드 / 정적 분석

- [ ] 폐쇄망 기준 빌드 성공 (`bash setup.sh` — `-mod=vendor`, `GOPROXY=off`)
- [ ] 빈 모듈 캐시로도 빌드되는지 확인
      (`GOMODCACHE=$(mktemp -d) GOPROXY=off go build -mod=vendor .`)
- [ ] `gofmt -l .` 출력 없음
- [ ] `go vet -mod=vendor .` 통과
- [ ] `go test -mod=vendor ./...` 통과 (테스트 추가 시)
- [ ] 의존성을 추가/변경했다면, 인터넷 되는 호스트에서 `go mod tidy && go mod vendor` 후
      결과 `vendor/`를 `.claude/공통/govendor/govmomi-0.55.1-vc-password-update`에
      복사해 커밋

## 동작 검증 (최소 1개 시나리오)

- [ ] 실제 vCenter로 대상 계정 비밀번호를 **같은 값으로** admin 재설정 성공
- [ ] 비밀번호 이력 정책이 걸린 상태에서도(연속 같은 값 재설정) 성공하는지 재확인
      (govmomi 버전을 올렸다면 특히 중요 — API 동작이 바뀔 수 있음)
- [ ] 응답 없는/존재하지 않는 vCenter를 목록에 섞어도 `-timeout` 이후 나머지는
      계속 처리되는지
- [ ] `ADMIN_PASSWORD` 미설정 시 vCenter 접속 전에 오류로 종료하는지
- [ ] `run.sh` 자동 빌드 + 인자 전달 확인
- [ ] `cron_wrapper.sh`: 최초 실행 → 성공 시 `.last_success` 갱신 → 즉시 재실행 시
      스킵되는지, 일부 실패 시 갱신하지 않고 다음 실행에서 재시도하는지

## 안전장치 (고쳤다면 반드시 재확인)

- [ ] `-dir`/`-vc` 누락, `ADMIN_PASSWORD` 미설정이면 vCenter 접속 전에 종료하는지
- [ ] 대상 계정이 그 vCenter에 없으면(오타 등) 재설정을 시도하지 않고 명확히
      실패로 보고하는지
- [ ] `admin_password.secret` 파일 권한이 600인지, crontab 라인에 비밀번호가
      직접 노출되지 않는지
- [ ] 종료 코드 규약(0/1/2)이 `README.md` 표와 일치하는지

## 문서

- [ ] `CHANGELOG.md`에 날짜순(최신 위) 항목 추가 — 검증 내용 포함
- [ ] `README.md` 옵션 표 / 알려진 한계 갱신 필요 여부 확인
- [ ] `ARCHITECTURE.md` 파일 역할 표 / `WORKFLOW.md` 흐름도 갱신 필요 여부 확인
- [ ] 배포 시 버전 태그 — 태그 메시지에 어느 CHANGELOG 항목까지 포함하는지 적을 것

```bash
git tag -a v0.1.0 -m "<변경 요약> (CHANGELOG 2026-09-27 항목)"
git push origin v0.1.0
```

## 운영 전 마지막 확인

- [ ] **비밀번호 갱신 후 랜덤한 vCenter 몇 개를 직접 로그인하거나 SSO 계정 관리
      화면에서 비밀번호 변경 시각을 확인**했는지 (종료 코드 0 ≠ 실제 반영 100% 보장)
- [ ] 이 도구를 실행하는 서버와 각 vCenter의 시계가 동기화되어 있는지 (STS 토큰
      발급이 시계 어긋남에 민감함 — CHANGELOG 2026-09-27 참고)
