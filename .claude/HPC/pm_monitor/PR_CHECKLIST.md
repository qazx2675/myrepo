# PR_CHECKLIST

pm_monitor 변경 시 커밋·푸시 전에 확인하세요.

- [ ] `install_mn_os8` 을 재빌드해 함께 커밋했다 (`bash build.sh`, 바이너리를 .gitignore 로 제외하지 않음, vendor/ 없음)
- [ ] `gofmt -l .` 출력 없음, `go vet ./...` 경고 없음
- [ ] `go test ./...` 통과 (골든 파일을 바꿨다면 의도한 변경인지 확인)
- [ ] 변경한 동작을 스텁 gossh·ssh 로 시나리오 하나 이상 직접 확인했다 (`사용법.txt` 참고)
- [ ] `bash -n build.sh install_mn.sh` 통과, 모든 텍스트 파일이 LF 줄바꿈이다
- [ ] 부하 시험 `test/load_check.sh` 통과 (CPU·RSS·fd·ICMP 속도·gossh 동시 1개·nice 10). 수집은 데몬이므로 `test/load_run.sh` 가 데몬 프로세스를 잰다
- [ ] 수집·데몬·화면 변경이면 `test/daemon_check.py`(붙기·떼기·재접속·종료·12시간 만료) 전부 PASS
- [ ] 자동 종료(12시간)를 늘리거나 없애지 않았다(공용 관리서버)
- [ ] yum 모드 변경이면: **yum 서버가 없는 호스트파일의 출력(화면·`c`·`r`·`--status`·state.json)이 v0.3.0 과 바이트 단위로 같다**, 기존 골든 파일이 바뀌지 않았다(새 골든만 추가)
- [ ] yum 서버가 섞여도 관리서버 부하 기준을 지킨다: gossh 는 회차당 한 번만 더(`NS_YUM` 한 줄 명령), 동시에 하나, `nice -n 10`. 8000대(yum 5% 포함) `test/load_run.sh 600 8000 5` 로 다시 쟀다
- [ ] yum 상태 전이(첫 접속·재부팅됨·커널 변경·커널 동일·ping X·ssh 실패·완료후꺼짐 복귀)를 바꿨다면 `applyYum` 단위 테스트와 `test/daemon_check.py`(yum 23항목 포함 51 PASS)·`test/demo_yum_check.py` 가 통과한다
- [ ] 스냅샷·소켓·`state.json` 에 필드를 더했거나 모양을 바꿨다면 `programVersion`(daemon.go)을 올렸다(데몬 지문에 들어가 바이너리 교체 뒤 옛 데몬이 교체됨)
- [ ] yum 서버가 `d` 대상·설치완료 수·`재부팅됨 N대` 목록에 섞이지 않고, `l` 에는 `yum_update/접속가능` 만 들어간다
- [ ] 서버 설정 조사·oklist·res 변경이면(v0.5.0): **`bindpw` 를 읽거나 저장하거나 보여 주지 않는다**(원격 명령은 ldap.conf 의 `URI`·`binddn` 줄만 고름, 명령 문자열에 `bindpw` 없음을 단위 테스트가 확인)
- [ ] `.res` 파일은 임시 파일 → 이름 변경으로 쓰고(원자적), 내용이 같으면 쓰지 않으며, 쓰기 실패가 수집을 멈추지 않는다. 열 순서·`설치/yumupdate여부`(한 번이라도 완료면 유지)·`설정동일여부`(oklist 가 없거나 조사 전이면 빈 칸)가 README 3.4 와 같다
- [ ] 골든 파일: `l` 패널 골든(폭 60/80/120: oklist 없음·있음(색)·`m`·끝 스크롤)만 의도해서 바꾸고 그 밖의 기존 골든은 바뀌지 않았다
- [ ] 부하 기준을 다시 쟀다: 자동 조사(회차당 100대)와 res 쓰기가 더해져도 `test/load_run.sh 600 8000 5`(oklist 포함, 끝에 `.res` 행 수 = 호스트 수 + 1 확인)가 CPU 평균 5% 미만·최고 30% 미만·RSS 50MB 미만 등 7.5 기준을 지킨다
- [ ] 스냅샷·소켓·`state.json` 모양을 바꿨다면 `programVersion` 을 올렸고(현재 0.5.0), oklist 의 경로·내용이 데몬 지문에 들어가 oklist 를 바꾸면 수집이 다시 시작되는 것을 확인했다(state.json 형식 번호 2, v0.4.0 파일 이행 포함)
- [ ] README 의 설정 키·TUI 키·상태 표가 코드와 일치한다 (`pm_monitor.conf.sample` 포함)
- [ ] CHANGELOG.md 에 변경 내용을 적었다
