# PR_CHECKLIST

pm_monitor 변경 시 커밋·푸시 전에 확인하세요.

- [ ] `install_mn_os8` 을 재빌드해 함께 커밋했다 (`bash build.sh`, 바이너리를 .gitignore 로 제외하지 않음, vendor/ 없음)
- [ ] `gofmt -l .` 출력 없음, `go vet ./...` 경고 없음
- [ ] `go test ./...` 통과 (골든 파일을 바꿨다면 의도한 변경인지 확인)
- [ ] 변경한 동작을 스텁 gossh·ssh 로 시나리오 하나 이상 직접 확인했다 (`사용법.txt` 참고)
- [ ] `bash -n build.sh install_mn.sh` 통과, 모든 텍스트 파일이 LF 줄바꿈이다
- [ ] 부하 시험 `test/load_check.sh` 통과 (CPU·RSS·fd·ICMP 속도·gossh 동시 1개·nice 10)
- [ ] README 의 설정 키·TUI 키·상태 표가 코드와 일치한다 (`pm_monitor.conf.sample` 포함)
- [ ] CHANGELOG.md 에 변경 내용을 적었다
