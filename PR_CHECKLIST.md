# PR_CHECKLIST

auto_setup 데몬 + CLI 풀리퀘스트 검증 항목. 모든 항목을 확인하고 체크한 후 커밋/푸시하세요.

## 빌드 및 문법 검사

- [ ] `bash -n setup.sh build_os6.sh test/manual_check.sh test/run_e2e.sh` 에러 없음
- [ ] `go vet ./...` 경고 없음
- [ ] `gofmt -l .` 출력 없음 (형식 통일)
- [ ] `shellcheck -S warning setup.sh build_os6.sh test/manual_check.sh` 경고 0건

## 테스트 통과

- [ ] `go test ./...` 모든 케이스 통과
  - 포함: state, daemon, icmp, probe, check, runner 단위 테스트
  - 가짜 시계·pinger·checker·runner 사용
  
- [ ] `bash test/manual_check.sh -y` 자동 진행 완료
  - 단계 1~8: 점검 완료 (wall·설치 단계 제외)
  - 로그: `/tmp/auto_setup_manual/` 생성 및 정리됨
  
- [ ] `bash test/run_e2e.sh` 목업 E2E 통과 (root 필요)
  - 스크래치 사용, 원본 /tmp/auto_setup 미영향
  - 스텁 gossh/ssh/wall 동작 확인
  - 01 복사본 실행 → queue 생성 → 데몬 수거 → os_check 실행 → code 생성 → wall 메시지 확인

## 코드 규칙 (빈 변수)

- [ ] **main.go 최상단 빈 변수 4개** (소스 커밋 시 항상 빈 값):
  ```go
  var (
    os6_mgmt      = ""
    os6_gossh     = ""
    os6_autosetup = ""
    os_check_sh   = ""
  )
  ```
  확인 명령:
  ```bash
  grep -nE '^\s+(os6_mgmt|os6_gossh|os6_autosetup|os_check_sh)\s+=\s+""' main.go
  ```
  결과: 4줄 모두 `= ""`

- [ ] 소스에 테스트 값(IP, 경로) 없음
  ```bash
  grep -r '192.168\|/tmp/auto_setup\|/root' *.go | grep -v 'const\|runsDir\|binDir'
  ```
  테스트 값은 test/ 하네스만 사용

## 런타임 검증 (os6 포함)

- [ ] `/usr/local/bin/auto_setup` 설치 권한 확인
  ```bash
  ls -l /usr/local/bin/auto_setup
  ```
  결과: `-rwxr-xr-x`

- [ ] crontab 등록 확인 (중복 없음)
  ```bash
  crontab -l | grep '/usr/local/bin/auto_setup ensure'
  ```
  결과: 1줄 (매분)

- [ ] tmpfiles 등록 확인
  ```bash
  cat /etc/tmpfiles.d/auto_setup.conf
  ```
  결과: `x /tmp/auto_setup`

- [ ] os6 정적 빌드 확인 (Go 1.20 서버 .60 에서)
  ```bash
  bash build_os6.sh
  file auto_setup_os6
  ```
  결과: `ELF 64-bit LSB executable … statically linked`

## auto_setup 특화 항목

- [ ] **01 전달 동작**
  - 02 성공 직후 queue 파일 생성 확인
  - 파일 내용: user=, time=, 호스트줄들
  - 02 실패 시 전달 없음 확인
  
- [ ] **queue 수거 및 상태 저장**
  - jobs/<jobid>.json 생성 확인
  - DNS 조회 완료 (동시 32 제한)
  - route 판별 (local/os6) 정상
  
- [ ] **ping 감시 (10초 주기)**
  - raw ICMP 동작 (root 필요)
  - 무응답 2회 연속 → seen_down=true
  - os6 장기 ssh 세션 자동 재시작
  
- [ ] **준비확인 (30초 주기)**
  - gossh -pm 응답 파싱 정상
  - anaconda 감지(_os_install 파일) 정상
  - READY: uptime 초 < (현재 − submitted)
  
- [ ] **7분/3분 규칙**
  - 첫 READY 시각 고정 (연장 없음)
  - 전부 READY 시 즉시 실행
  - 늦은 READY 3분 묶음 실행
  - 각 run 은 별개 code 생성
  
- [ ] **os_check 실행(-auto)**
  - 1차 run: 전체 목록 전달
  - 재실행 run: 미처리 호스트만 전달
  - os6 호스트 포함 시 gossh 래퍼 경로 삽입
  - 실행: `cd /tmp/auto_setup/runs/<code> && bash os_check_sh -auto <user> targets.txt`
  
- [ ] **code 파일 생성 (codes/<code>.txt)**
  - 순서 확정: ① 결과 리포트 ② FAIL/NO FAIL ③ LDAP·Splunk·커널 요약 ④ 원본경로
  - FAIL 있는 호스트 줄 포함
  - FAIL 없는 호스트는 `NO FAIL : host1 host2 … (N대)` 형식
  - 블록 발췌 정확(LDAP 정보, SPLUNK 정보, 커널 버전, <infra> infra 커널 버전)
  
- [ ] **wall 메시지**
  ```
  [auto_setup] OS 설치 + 설정체크 완료 (user=<user>)
  host01 host02 host03 (3대)
  code : 4821   →  auto_setup code 4821
  ```
  완료 호스트 = processed (postapply 에 나온) 호스트

- [ ] **무기한 감시 및 cancel**
  - 남은 호스트 있으면 무기한 감시
  - `auto_setup cancel <jobid>` 로 종료 (jobs → done)
  - 완료 된 작업은 자동 종료
  
- [ ] **로그 파일**
  - /tmp/auto_setup/auto_setup.log 생성
  - 상태 변화·실행·오류만 기록 (주기 반복 로그 없음)
  - 10MB 초과 시 .1 로 1개만 보관

## 기존 사용법 불변 확인

- [ ] **01.AWX_nodeinfo_V2.sh 불변**
  - 프롬프트·출력 순서 동일
  - 종료코드 동일
  - auto_setup 전달은 로그 1줄뿐 (프롬프트·옵션 없음)
  
- [ ] **os_check_final_annotated.sh 호환성**
  - 인자 없이 실행: 100% 기존과 동일 (read -rp 두 프롬프트)
  - `-auto` 옵션 추가 시에만 분기 (프롬프트 없음)
  - 기존 테스트 전부 PASS 유지
  
## 문서 갱신 확인

- [ ] **README.md**: 설치·사용·옵션·문서별 설명·주의사항·버전 태그
- [ ] **CHANGELOG.md**: 2026-10-02 항목 (신규·보정·주의)
- [ ] **ARCHITECTURE.md**: 폴더 구조·파일 역할·런타임 디렉터리·함수 매핑
- [ ] **WORKFLOW.md**: mermaid 흐름도 + 단계별 설명
- [ ] **workflow.svg**: 정적 SVG 흐름도 (다크/라이트 지원)
- [ ] **PR_CHECKLIST.md**: 이 체크리스트
- [ ] **사용법.txt**: 명령어 중심 빠른 참고 (맨 위 1-7 섹션)
- [ ] **.github/workflows/ci.yml**: setup-go + go test/vet/build + bash -n

## 최종 체크 (커밋 전)

```bash
# 1. 빌드 및 정적 검사
bash -n setup.sh build_os6.sh test/manual_check.sh test/run_e2e.sh
go vet ./... && gofmt -l .
shellcheck -S warning setup.sh build_os6.sh test/manual_check.sh

# 2. 단위 테스트
go test ./...

# 3. 목업 E2E (root 필요)
sudo bash test/run_e2e.sh

# 4. 빈 변수 확인
grep -nE '^\s+(os6_mgmt|os6_gossh|os6_autosetup|os_check_sh)\s+=\s+""' main.go

# 5. git 상태 확인
git status
git diff main.go  # 빈 변수 확인

# 6. 커밋
git add .
git commit -m "feat(auto_setup): 데몬 + CLI 초기 구현"
git push origin <브랜치>
```

## 커밋 메시지 양식

```
feat(auto_setup): OS 설치 → 설정체크 자동 연계 데몬 및 CLI

- main.go: 최상단 빈 변수 4개, CLI 분기 (daemon/ensure/code/status/cancel/probe)
- daemon.go: 상태기계, queue 수거, 경로 판별, ping 감시, 준비확인, 7분/3분 스케줄, run 큐
- runner.go: os_check 실행(-auto), code 생성, wall 발송
- setup.sh: 빌드·설치·cron·tmpfiles 등록 (멱등)
- build_os6.sh: Go 1.20 정적 빌드 (auto_setup_os6)
- test: 대화형 점검(manual_check.sh), 목업 E2E(run_e2e.sh), 스텁
- 문서: README/CHANGELOG/ARCHITECTURE/WORKFLOW/workflow.svg/PR_CHECKLIST

계획서 §8 결정사항 반영:
- cron PATH 보강, os6 probe 로그 억제
- runner 오류 대기 로그 억제, 중단 run code 재사용 방지

기존 01/os_check 사용법 불변
```

## 검증 체크리스트 (최종)

- [ ] 모든 go 문법 검사 통과
- [ ] 모든 shell 문법 검사 통과
- [ ] Go 단위 테스트 PASS
- [ ] E2E 테스트 PASS
- [ ] 빈 변수 4개 비어있음
- [ ] 테스트 값 없음
- [ ] cron 등록 확인
- [ ] tmpfiles 등록 확인
- [ ] os6 정적 빌드 성공
- [ ] 문서 모두 갱신
- [ ] 기존 01/os_check 사용법 불변
- [ ] git 커밋 메시지 양식 준수
